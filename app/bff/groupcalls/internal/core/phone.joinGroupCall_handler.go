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
	"crypto/rand"
	"encoding/hex"

	"github.com/teamgram/proto/mtproto"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// PhoneJoinGroupCall
// phone.joinGroupCall#8fb53057 flags:# muted:flags.0?true video_stopped:flags.2?true call:InputGroupCall join_as:InputPeer invite_hash:flags.1?string public_key:flags.3?int256 block:flags.3?bytes params:DataJSON = Updates;
//
// Joins the account (as itself: joinAs) or connects it again with new
// params. Every group call is end-to-end encrypted (ADR 0060): the join
// carries the participant's public key and the block that adds him to the
// call's chain, and one without them is GROUPCALL_FORBIDDEN, which the
// clients take for a refusal without retrying (ADR 0060 §2, «Ошибки»).
//
// The block and the participant's row go in one transaction, then the SFU
// connects him, then the updates go out (ADR 0060 §2). The answer carries
// updateGroupCallConnection with the SFU's answer, updateGroupCall, the
// account's own row (iOS GroupCalls.swift:786-829, Android
// VoIPService.java:2145-2160, tdesktop calls_group_call.cpp:2393-2440) and
// updateGroupCallChainBlocks of both sub-chains: of sub-chain 0 the block of
// this join with next_offset its height plus one, of sub-chain 1 none with
// the count of broadcasts. iOS takes a join without either for a failure
// (GroupCalls.swift:922-944); tdesktop applies them as the first blocks
// (calls_group_call.cpp:1806-1816). The link of a speaker (invite_hash)
// gives the word.
func (c *GroupCallsCore) PhoneJoinGroupCall(in *mtproto.TLPhoneJoinGroupCall) (*mtproto.Updates, error) {
	if in.GetKeyFingerprint() != nil {
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
	if len(in.GetPublicKey()) == 0 || len(in.GetBlock()) == 0 {
		// A client without the end-to-end encryption (ADR 0060,
		// «Последствия»): the call is not for it.
		return nil, mtproto.ErrGroupcallForbidden
	}
	if len(in.GetPublicKey()) != 32 {
		return nil, errBlockInvalid
	}
	chain := &groupcalls.ChainJoin{PublicKey: [32]byte(in.GetPublicKey()), Block: in.GetBlock()}
	if !c.svcCtx.SFU.Enabled() {
		return nil, errNotConfigured
	}
	params := []byte(in.GetParams().GetData())
	req, err := tgcallsjson.ParseJoinRequest(params)
	if err != nil {
		return nil, mtproto.ErrDataJsonInvalid
	}
	endpoint, err := newEndpoint()
	if err != nil {
		return nil, err
	}
	manage := a.canManage(c.MD.UserId)
	speaker := call.SpeakerHash != "" && in.GetInviteHash().GetValue() == call.SpeakerHash
	wasMember := call.ChainHeight >= 0
	if wasMember {
		head, err := c.svcCtx.Dao.Store.Head(c.ctx, call.Id)
		if err != nil {
			return nil, storeError(err)
		}
		if wasMember, err = c.svcCtx.Members.IsMember(c.ctx, c.svcCtx.Dao.Store, head, c.MD.UserId); err != nil {
			return nil, err
		}
	}
	res, err := c.svcCtx.Dao.Store.Join(c.ctx, groupcalls.JoinParams{
		CallId: call.Id, UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId,
		Source: req.SSRC, Video: groupcalls.MediaOf(req, endpoint), VideoStopped: in.GetVideoStopped() || !req.HasVideo(),
		State: func(call *groupcalls.Call, prev *groupcalls.Participant) (bool, bool) {
			return groupcalls.JoinState(call, prev, manage, speaker, in.GetMuted())
		},
		VideoLimit: c.svcCtx.VideoLimit,
		Chain:      chain,
	})
	if err != nil {
		return nil, chainError(err)
	}
	c.svcCtx.E2E.Joined(call.Id, c.MD.UserId, wasMember)
	c.svcCtx.E2E.Chain(call.Id, res.Call.ChainHeight, res.Call.ChainHash, res.Chain.Members)
	block0, err := writeUpdate(call.Input(), groupcalls.SubChainMain, res.Chain)
	if err != nil {
		return nil, err
	}
	resp, err := c.connectJoined(res.Call, &pb.JoinRequest{
		CallId: call.Id, Participant: c.MD.UserId, Kind: pb.ConnectionKind_CONNECTION_KIND_MAIN,
		Params: params, Muted: !res.Self.CanSelfUnmute, Endpoint: endpoint,
	})
	if err != nil {
		// The block stands: the others apply it and, as the joiner is not
		// in the media, remove him with only_left (ADR 0060 §2). He leaves
		// before the block goes out, so that their removal is taken.
		c.unjoin(a, res.Call, req.SSRC)
		c.deliverChain(block0, res.Chain.Members)
		return nil, err
	}
	metricJoins.Inc("main")
	// The joiner's row goes out before his block. tdesktop takes every
	// member of the chain who is not among the participants it holds for
	// one who left, as soon as a block changes the chain, and removes him
	// with only_left (data_group_call.cpp:85-93, 233-255); the joiner must
	// be among them by then. A device of the same user still in the call
	// sees its own row with another source and hangs up (tdesktop
	// calls_group_call.cpp:2627-2636, Android VoIPService.java:1687-1692,
	// iOS PresentationGroupCall.swift:1118-1122) before the block that
	// replaces its key fails its tde2e and would have it join again. In one
	// Updates tdesktop applies the chain first (api_updates.cpp:305-326),
	// so the two go in separate pushes.
	flip := (call.ParticipantsCount == 0) != (res.Call.ParticipantsCount == 0)
	a = c.deliver(a, res.Call, fanOut{changes: res.Changes, call: true, chats: flip})
	c.deliverChain(block0, res.Chain.Members)
	answer := c.answer(a, res.Call, res.Changes, []*mtproto.Update{connectionUpdate(resp.GetParams(), false)}, true)
	answer.Updates = append(answer.Updates, block0, chainUpdate(call.Input(), groupcalls.SubChainBroadcast, nil, res.Call.BroadcastCount))
	return answer, nil
}

// newEndpoint is the id of a new connection on the SFU: in the Colibri
// messages and in groupCallParticipantVideo.endpoint. It is chosen here so
// that the row of the join is whole before the SFU connects it.
func newEndpoint() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// connectJoined connects the main connection of a join the state took. A
// room that moved to another node while it connected (two joins placing a
// lost room) is joined again on that node, once; a call that ended in
// between leaves no room open.
func (c *GroupCallsCore) connectJoined(call *groupcalls.Call, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	for attempt := 0; ; attempt++ {
		node, resp, err := connect(c.ctx, c.svcCtx.SFU, c.svcCtx.Dao.Store, call, req)
		if err != nil {
			metricSFUErrors.Inc("join")
			return nil, err
		}
		after, err := c.svcCtx.Dao.Store.Get(c.ctx, call.Id)
		if err != nil {
			return nil, err
		}
		if !after.Discarded && after.SfuNode == node {
			if p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, req.GetParticipant()); err == nil && p.InCall && p.CanSelfUnmute == req.GetMuted() {
				// An admin changed the word in between.
				c.setMuted(after, p)
			}
			return resp, nil
		}
		if lerr := c.svcCtx.SFU.Leave(c.ctx, node, call.Id, req.GetParticipant(), req.GetKind()); lerr != nil {
			c.Logger.Errorf("groupcalls: leave of a join of call %d on %s: %v", call.Id, node, lerr)
		}
		if after.Discarded || after.SfuNode == "" {
			// The call ended, or its room was forgotten, while this join
			// opened the room on node: nothing else would close it.
			if cerr := c.svcCtx.SFU.CloseRoom(c.ctx, node, call.Id); cerr != nil {
				metricSFUErrors.Inc("close_room")
				c.Logger.Errorf("groupcalls: close the room of call %d on %s: %v", call.Id, node, cerr)
			}
		}
		if after.Discarded {
			return nil, mtproto.ErrGroupcallAlreadyDiscarded
		}
		if attempt > 0 {
			return nil, mtproto.ErrGroupcallAddParticipantsFailed
		}
		call = after
	}
}

// unjoin takes out of the call a participant whose join the SFU did not
// connect, so that the others see him leave.
func (c *GroupCallsCore) unjoin(a *access, call *groupcalls.Call, source uint32) {
	after, change, err := c.svcCtx.Dao.Store.Leave(c.ctx, call.Id, c.MD.UserId, groupcalls.LeaveCond{Source: source})
	if err != nil {
		c.Logger.Errorf("groupcalls: leave of a join of call %d the SFU refused: %v", call.Id, err)
		return
	}
	if change == nil {
		return
	}
	c.svcCtx.E2E.Left(call.Id, c.MD.UserId)
	flip := (call.ParticipantsCount == 0) != (after.ParticipantsCount == 0)
	c.deliver(a, after, fanOut{changes: []groupcalls.Change{*change}, call: true, chats: flip})
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
