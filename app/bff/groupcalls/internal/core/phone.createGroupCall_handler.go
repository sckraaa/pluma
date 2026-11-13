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
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// scheduleMax is how far ahead a call may be scheduled. Telegram's limit
// is not verified; this one only keeps out a date no client offers.
const scheduleMax = 365 * 24 * 3600

// cutTitle is a title as the call keeps it: trimmed, at most TitleMax
// characters.
func cutTitle(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > groupcalls.TitleMax {
		s = string([]rune(s)[:groupcalls.TitleMax])
	}
	return s
}

// PhoneCreateGroupCall
// phone.createGroupCall#48cdc6d8 flags:# rtmp_stream:flags.2?true peer:InputPeer random_id:int title:flags.0?string schedule_date:flags.1?int = Updates;
//
// A call of a chat or a channel, started now or scheduled, by its creator
// or an admin with manage_call. The answer carries updateGroupCall, which
// the clients read the call from (tdesktop calls_group_call.cpp:1306, iOS
// GroupCalls.swift:330); a repeat of the same random_id gets the same call.
// RTMP streams are not served (ADR 0059, «Что не делается»).
func (c *GroupCallsCore) PhoneCreateGroupCall(in *mtproto.TLPhoneCreateGroupCall) (*mtproto.Updates, error) {
	if in.GetRtmpStream() {
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	if !c.svcCtx.SFU.Enabled() {
		return nil, errNotConfigured
	}
	a, err := c.loadInputPeer(in.GetPeer(), c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canManage(c.MD.UserId) {
		return nil, mtproto.ErrChatAdminRequired
	}
	var schedule int32
	if d := in.GetScheduleDate(); d != nil {
		if d.GetValue() <= now() || d.GetValue() > now()+scheduleMax {
			return nil, mtproto.ErrScheduleDateInvalid
		}
		schedule = d.GetValue()
	}
	id, err := positiveId()
	if err != nil {
		return nil, err
	}
	hash, err := positiveId()
	if err != nil {
		return nil, err
	}
	call, created, err := c.svcCtx.Dao.Store.Create(c.ctx, &groupcalls.Call{
		Id: id, AccessHash: hash, PeerType: a.peerType, PeerId: a.peerId, CreatorId: c.MD.UserId,
		RandomId: in.GetRandomId(), Title: cutTitle(in.GetTitle().GetValue()), ScheduleDate: schedule,
	})
	if errors.Is(err, groupcalls.ErrAlreadyActive) {
		return nil, mtproto.ErrGroupcallAlreadyStarted
	}
	if err != nil {
		return nil, err
	}
	if created {
		metricCalls.Inc("created")
		c.post(call)
		a = c.deliver(a, call, fanOut{call: true, chats: true})
	}
	return c.answer(a, call, nil, nil, true), nil
}
