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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneDiscardGroupCall
// phone.discardGroupCall#7a777135 call:InputGroupCall = Updates;
//
// Ends the call, by one who manages it: everyone in it leaves with it,
// its room closes, messageActionGroupCall with the duration goes into the
// chat and the members get groupCallDiscarded with the chat without
// call_active. An empty call of a chat is not ended by itself (ADR 0059
// §10). A conference is ended by its creator alone (Android
// GroupCallActivity.java:1767), for another GROUPCALL_FORBIDDEN (decision
// 11 of task 117); its ringing invitations are missed, those of the
// invitees who joined get its duration, and it posts no message. One that
// is over already answers as ended: Web K discards a conference whose
// creation it could not finish (appGroupCallsManager.ts).
func (c *GroupCallsCore) PhoneDiscardGroupCall(in *mtproto.TLPhoneDiscardGroupCall) (*mtproto.Updates, error) {
	call, err := c.load(in.GetCall())
	if err != nil {
		return nil, err
	}
	if call.Discarded && !call.Conference {
		return nil, mtproto.ErrGroupcallAlreadyDiscarded
	}
	a, err := c.accessOf(call, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canManage(c.MD.UserId) {
		if call.Conference {
			return nil, mtproto.ErrGroupcallForbidden
		}
		return nil, mtproto.ErrChatAdminRequired
	}
	if call.Discarded {
		return c.answer(a, call, nil, nil, true), nil
	}
	after, left, won, err := c.svcCtx.Dao.Store.Discard(c.ctx, call.Id)
	if err != nil {
		return nil, storeError(err)
	}
	if !won {
		if call.Conference {
			return c.answer(a, after, nil, nil, true), nil
		}
		return nil, mtproto.ErrGroupcallAlreadyDiscarded
	}
	if call.Conference {
		a = c.endConference(a, after, left, "discard")
		return c.answer(a, after, nil, nil, true), nil
	}
	metricCalls.Inc("discarded")
	c.svcCtx.Members.Forget(call.Id)
	var gone []int64
	for _, p := range left {
		metricLeaves.Inc("discard")
		gone = append(gone, p.UserId)
	}
	c.closeRoom(after)
	c.post(after)
	a = c.deliver(a, after, fanOut{call: true, chats: true, left: gone})
	return c.answer(a, after, nil, nil, true), nil
}

// closeRoom closes the room of a call that is over and forgets its node; a
// node that does not answer keeps it named for the collector to try again.
func (c *GroupCallsCore) closeRoom(call *groupcalls.Call) {
	if call.SfuNode == "" {
		return
	}
	if c.svcCtx.SFU.Has(call.SfuNode) {
		if err := c.svcCtx.SFU.CloseRoom(c.ctx, call.SfuNode, call.Id); err != nil {
			metricSFUErrors.Inc("close_room")
			c.Logger.Errorf("groupcalls: close the room of call %d on %s: %v", call.Id, call.SfuNode, err)
			return
		}
	}
	if err := c.svcCtx.Dao.Store.ForgetNode(c.ctx, call.Id, call.SfuNode); err != nil {
		c.Logger.Errorf("groupcalls: node of call %d: %v", call.Id, err)
	}
}
