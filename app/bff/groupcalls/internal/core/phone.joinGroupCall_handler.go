// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"errors"

	"github.com/teamgram/proto/mtproto"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// PhoneJoinGroupCall
// phone.joinGroupCall#8fb53057 flags:# muted:flags.0?true video_stopped:flags.2?true call:InputGroupCall join_as:InputPeer invite_hash:flags.1?string public_key:flags.3?int256 block:flags.3?bytes params:DataJSON = Updates;
//
// Joins the account (as itself: joinAs) or connects it again with new
// params. The answer carries updateGroupCallConnection with the
// SFU's answer, updateGroupCall and the account's own row: iOS takes a
// join without the first two for a failure (GroupCalls.swift:786-829),
// Android reads its source from the row (VoIPService.java:2145-2160),
// tdesktop all three (calls_group_call.cpp:2393-2440). The link of a
// speaker (invite_hash) gives the word. public_key, block and
// key_fingerprint belong to the end-to-end encryption of a later stage
// (ADR 0060).
func (c *GroupCallsCore) PhoneJoinGroupCall(in *mtproto.TLPhoneJoinGroupCall) (*mtproto.Updates, error) {
	if len(in.GetPublicKey()) > 0 || len(in.GetBlock()) > 0 || in.GetKeyFingerprint() != nil {
		return nil, mtproto.ErrGroupcallInvalid
	}
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	if err = c.joinAs(in.GetJoinAs()); err != nil {
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	if call.Scheduled() {
		return nil, storeError(groupcalls.ErrScheduled)
	}
	params := []byte(in.GetParams().GetData())
	req, err := tgcallsjson.ParseJoinRequest(params)
	if err != nil {
		return nil, mtproto.ErrDataJsonInvalid
	}
	manage := a.canManage(c.MD.UserId)
	speaker := call.SpeakerHash != "" && in.GetInviteHash().GetValue() == call.SpeakerHash
	state := func(call *groupcalls.Call, prev *groupcalls.Participant) (bool, bool) {
		return groupcalls.JoinState(call, prev, manage, speaker, in.GetMuted())
	}
	var (
		node          string
		resp          *pb.JoinResponse
		res           *groupcalls.JoinResult
		canSelfUnmute bool
	)
	for attempt := 0; ; attempt++ {
		prev, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, c.MD.UserId)
		if err != nil && !errors.Is(err, groupcalls.ErrNotFound) {
			return nil, err
		}
		_, canSelfUnmute = state(call, prev)
		node, resp, err = connect(c.ctx, c.svcCtx.SFU, c.svcCtx.Dao.Store, call, &pb.JoinRequest{
			CallId: call.Id, Participant: c.MD.UserId, Kind: pb.ConnectionKind_CONNECTION_KIND_MAIN,
			Params: params, Muted: !canSelfUnmute,
		})
		if err != nil {
			metricSFUErrors.Inc("join")
			return nil, err
		}
		res, err = c.svcCtx.Dao.Store.Join(c.ctx, groupcalls.JoinParams{
			CallId: call.Id, UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, Node: node,
			Source: req.SSRC, Video: groupcalls.MediaOf(req, resp.GetEndpoint()), VideoStopped: in.GetVideoStopped() || !req.HasVideo(),
			State: state, VideoLimit: c.svcCtx.VideoLimit,
		})
		if err == nil {
			break
		}
		// The state refuses the join: the connection goes again.
		if lerr := c.svcCtx.SFU.Leave(c.ctx, node, call.Id, c.MD.UserId, pb.ConnectionKind_CONNECTION_KIND_MAIN); lerr != nil {
			c.Logger.Errorf("groupcalls: leave of a refused join of call %d: %v", call.Id, lerr)
		}
		switch {
		case errors.Is(err, groupcalls.ErrDiscarded), errors.Is(err, groupcalls.ErrMoved):
			// The call ended, or its room moved, while this join opened
			// the room on node: no one else is there, and nothing would
			// close it (the call no longer names node).
			if cerr := c.svcCtx.SFU.CloseRoom(c.ctx, node, call.Id); cerr != nil {
				metricSFUErrors.Inc("close_room")
				c.Logger.Errorf("groupcalls: close the room of a refused join of call %d on %s: %v", call.Id, node, cerr)
			}
		}
		if !errors.Is(err, groupcalls.ErrMoved) || attempt > 0 {
			return nil, storeError(err)
		}
		if call, err = c.loadLive(in.GetCall()); err != nil {
			return nil, err
		}
	}
	if res.Self.CanSelfUnmute != canSelfUnmute {
		// An admin changed the word in between.
		c.setMuted(res.Call, res.Self)
	}
	metricJoins.Inc("main")
	flip := (call.ParticipantsCount == 0) != (res.Call.ParticipantsCount == 0)
	a = c.deliver(a, res.Call, fanOut{changes: res.Changes, call: true, chats: flip})
	return c.answer(a, res.Call, res.Changes, []*mtproto.Update{connectionUpdate(resp.GetParams(), false)}, true), nil
}

// setMuted tells the SFU whether to forward p's sound and camera: not while
// an admin has taken his word (ADR 0059 §3, SetMuted). A failure is logged;
// the collector compares the SFU's mute with the word and sends it again.
func (c *GroupCallsCore) setMuted(call *groupcalls.Call, p *groupcalls.Participant) {
	if call.SfuNode == "" {
		return
	}
	if err := c.svcCtx.SFU.SetMuted(c.ctx, call.SfuNode, call.Id, p.UserId, !p.CanSelfUnmute); err != nil {
		metricSFUErrors.Inc("set_muted")
		c.Logger.Errorf("groupcalls: mute of %d in call %d on the SFU: %v", p.UserId, call.Id, err)
	}
}
