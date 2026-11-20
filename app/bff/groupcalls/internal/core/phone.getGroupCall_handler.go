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

// listMax is the most participants one page gives.
const listMax = 100

// PhoneGetGroupCall
// phone.getGroupCall#41845db call:InputGroupCall limit:int = phone.GroupCall;
//
// The call with the first participants: tdesktop asks 3 when it reloads
// after a gap in the versions (data_group_call.cpp:600-680), iOS 4
// (GroupCalls.swift:215); participants_next_offset continues the list in
// phone.getGroupParticipants. The rows are the viewer's own, with what he
// set for each. A conference may be named by its slug or an invitation;
// one that is over is groupCallDiscarded, not an error: iOS then offers to
// call the same people (GroupCalls.swift:168-169), Web K says the invite
// expired (appImManager.ts:2618).
func (c *GroupCallsCore) PhoneGetGroupCall(in *mtproto.TLPhoneGetGroupCall) (*mtproto.Phone_GroupCall, error) {
	call, err := c.load(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.viewable(call)
	if err != nil {
		return nil, err
	}
	metricReloads.Inc()
	page := &groupcalls.Page{}
	if !call.Discarded {
		if page, err = c.svcCtx.Dao.Store.List(c.ctx, call.Id, "", clampLimit(in.GetLimit())); err != nil {
			return nil, storeError(err)
		}
	}
	list, users, chats := c.rows(a, call, page.Participants)
	return mtproto.MakeTLPhoneGroupCall(&mtproto.Phone_GroupCall{
		Call:                   call.TL(c.callView(a, call, c.MD.UserId)),
		Participants:           list,
		ParticipantsNextOffset: page.Next,
		Chats:                  chats,
		Users:                  users,
	}).To_Phone_GroupCall(), nil
}

func clampLimit(limit int32) int {
	switch {
	case limit <= 0:
		return 1
	case limit > listMax:
		return listMax
	}
	return int(limit)
}

// viewable loads the policy of a call for an account that may see the
// call: a member, or a reader of the public channel; of a conference, one
// its policy lets see it (conferenceAccess). Being in the call of a chat is
// not enough: one removed from the chat is out of its call too (the
// collector takes him out).
func (c *GroupCallsCore) viewable(call *groupcalls.Call) (*access, error) {
	a, err := c.accessOf(call, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	return a, nil
}

// rows are participants as the account sees them, with their users and
// chats (the chat of the call first).
func (c *GroupCallsCore) rows(a *access, call *groupcalls.Call, ps []*groupcalls.Participant) ([]*mtproto.GroupCallParticipant, []*mtproto.User, []*mtproto.Chat) {
	prefs, err := c.svcCtx.Dao.Store.Prefs(c.ctx, call.Id, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("groupcalls: settings of %d in call %d: %v", c.MD.UserId, call.Id, err)
	}
	list := make([]*mtproto.GroupCallParticipant, 0, len(ps))
	for _, p := range ps {
		list = append(list, p.TL(groupcalls.ParticipantView{Viewer: c.MD.UserId, Pref: prefs[p.Key()]}))
	}
	users, channels := peersOf(ps)
	env := c.envelope(c.MD.UserId, nil, c.namesOf([]int64{c.MD.UserId}, users, channels), a)
	return list, env.GetUsers(), env.GetChats()
}
