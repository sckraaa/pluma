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
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneInviteToGroupCall
// phone.inviteToGroupCall#7b393160 call:InputGroupCall users:Vector<InputUser> = Updates;
//
// Invites members of the chat into the call: messageActionInviteToGroupCall
// from the inviter in the chat, which notifies them. Only members are
// invited; whether Telegram invites others is not verified. Those who are
// not members are left out, and only a list with no member at all is
// USER_NOT_PARTICIPANT: tdesktop first adds the non-members it was asked
// to invite and then invites everyone whatever the adding gave
// (calls_group_invite_controller.cpp:914-925), in slices, each failed
// slice silently (calls_group_call.cpp:4121-4146); one user it could not
// add must not lose the invitation of the others.
func (c *GroupCallsCore) PhoneInviteToGroupCall(in *mtproto.TLPhoneInviteToGroupCall) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, u := range in.GetUsers() {
		p := mtproto.FromInputUser(c.MD.UserId, u)
		if !p.IsUser() {
			return nil, mtproto.ErrUserIdInvalid
		}
		ids = append(ids, p.PeerId)
	}
	ids = uniq(ids)
	if len(ids) == 0 {
		return nil, mtproto.ErrUserIdInvalid
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, append([]int64{c.MD.UserId}, ids...)...)
	if err != nil {
		return nil, err
	}
	if !a.isMember(c.MD.UserId) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	members := ids[:0]
	for _, id := range ids {
		if a.isMember(id) {
			members = append(members, id)
		}
	}
	if len(members) == 0 {
		return nil, mtproto.ErrUserNotParticipant
	}
	ids = members
	randomId, err := positiveId()
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msg.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  call.PeerType,
		PeerId:    call.PeerId,
		Message: []*msg.OutboxMessage{msg.MakeTLOutboxMessage(&msg.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: groupcalls.PeerOf(call.PeerType, call.PeerId),
				Date:   now(),
				Action: mtproto.MakeTLMessageActionInviteToGroupCall(&mtproto.MessageAction{
					Call:  call.Input(),
					Users: ids,
				}).To_MessageAction(),
			}).To_Message(),
		}).To_OutboxMessage()},
	})
}
