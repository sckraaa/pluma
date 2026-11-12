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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PhoneLeaveGroupCall
// phone.leaveGroupCall#500377f9 call:InputGroupCall source:int = Updates;
//
// Takes the account out of the call. source names the connection: a leave
// of one the account replaced since (a join again with a new source) does
// not end the new one.
func (c *GroupCallsCore) PhoneLeaveGroupCall(in *mtproto.TLPhoneLeaveGroupCall) (*mtproto.Updates, error) {
	call, err := c.load(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if call.Discarded {
		return c.answer(a, call, nil, nil, true), nil
	}
	p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) || (err == nil && !p.InCall) {
		return nil, mtproto.ErrGroupcallJoinMissing
	}
	if err != nil {
		return nil, err
	}
	after, change, err := c.svcCtx.Dao.Store.Leave(c.ctx, call.Id, c.MD.UserId, groupcalls.LeaveCond{Source: uint32(in.GetSource())})
	if err != nil {
		return nil, storeError(err)
	}
	if change == nil {
		// An old connection: the account is in the call with another one.
		return c.answer(a, after, nil, nil, true), nil
	}
	c.disconnect(after, c.MD.UserId)
	metricLeaves.Inc("self")
	return c.left(a, call, after, change), nil
}

// disconnect ends both connections of userId on the SFU; one that is gone
// already is no error.
func (c *GroupCallsCore) disconnect(call *groupcalls.Call, userId int64) {
	if call.SfuNode == "" {
		return
	}
	for _, kind := range []pb.ConnectionKind{pb.ConnectionKind_CONNECTION_KIND_MAIN, pb.ConnectionKind_CONNECTION_KIND_PRESENTATION} {
		err := c.svcCtx.SFU.Leave(c.ctx, call.SfuNode, call.Id, userId, kind)
		if err != nil && status.Code(err) != codes.NotFound {
			metricSFUErrors.Inc("leave")
			c.Logger.Errorf("groupcalls: leave of %d from call %d on the SFU: %v", userId, call.Id, err)
		}
	}
}

// left delivers a leave and answers it.
func (c *GroupCallsCore) left(a *access, before, after *groupcalls.Call, change *groupcalls.Change) *mtproto.Updates {
	flip := (before.ParticipantsCount == 0) != (after.ParticipantsCount == 0)
	changes := []groupcalls.Change{*change}
	a = c.deliver(a, after, fanOut{changes: changes, call: true, chats: flip})
	return c.answer(a, after, changes, nil, true)
}
