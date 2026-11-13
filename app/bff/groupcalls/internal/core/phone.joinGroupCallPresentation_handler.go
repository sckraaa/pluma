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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PhoneJoinGroupCallPresentation
// phone.joinGroupCallPresentation#cbea6bc4 call:InputGroupCall params:DataJSON = Updates;
//
// The shared screen: a second connection of one in the call, on the node
// of the call. The answer carries updateGroupCallConnection with
// presentation (tdesktop takes the endpoint of the screen from it,
// calls_group_call.cpp:2393-2410), updateGroupCall and the row with the
// screen. A call whose room is gone answers GROUPCALL_JOIN_MISSING: the
// client shares the screen again after its own join again
// (calls_group_call.cpp:2020-2028).
func (c *GroupCallsCore) PhoneJoinGroupCallPresentation(in *mtproto.TLPhoneJoinGroupCallPresentation) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) || (err == nil && !p.InCall) {
		return nil, mtproto.ErrGroupcallJoinMissing
	}
	if err != nil {
		return nil, err
	}
	if !p.CanSelfUnmute {
		// An admin took the word: the screen's sound would speak for him.
		// tdesktop shows MutedNoScreen on this error (calls_group_call.cpp:
		// 2030-2036); Store.SetPresentation checks it again.
		return nil, mtproto.ErrChatAdminRequired
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	params := []byte(in.GetParams().GetData())
	req, err := tgcallsjson.ParseJoinRequest(params)
	if err != nil {
		return nil, mtproto.ErrDataJsonInvalid
	}
	if call.SfuNode == "" || !c.svcCtx.SFU.Has(call.SfuNode) {
		return nil, mtproto.ErrGroupcallJoinMissing
	}
	resp, err := c.svcCtx.SFU.Join(c.ctx, call.SfuNode, &pb.JoinRequest{
		CallId: call.Id, Participant: c.MD.UserId, Kind: pb.ConnectionKind_CONNECTION_KIND_PRESENTATION, Params: params,
	})
	if status.Code(err) == codes.NotFound {
		return nil, mtproto.ErrGroupcallJoinMissing
	}
	if err != nil {
		metricSFUErrors.Inc("join")
		return nil, joinError(c.ctx, call.Id, call.SfuNode, err)
	}
	after, row, _, err := c.svcCtx.Dao.Store.SetPresentation(c.ctx, call.Id, c.MD.UserId, req.SSRC, groupcalls.MediaOf(req, resp.GetEndpoint()))
	if err != nil {
		if lerr := c.svcCtx.SFU.Leave(c.ctx, call.SfuNode, call.Id, c.MD.UserId, pb.ConnectionKind_CONNECTION_KIND_PRESENTATION); lerr != nil {
			c.Logger.Errorf("groupcalls: leave of a refused screen of call %d: %v", call.Id, lerr)
		}
		return nil, storeError(err)
	}
	metricJoins.Inc("presentation")
	changes := []groupcalls.Change{{Participant: row}}
	c.deliver(a, after, fanOut{changes: changes})
	return c.answer(a, after, changes, []*mtproto.Update{connectionUpdate(resp.GetParams(), true)}, true), nil
}
