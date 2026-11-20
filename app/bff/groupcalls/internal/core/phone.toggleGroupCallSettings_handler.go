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

// PhoneToggleGroupCallSettings
// phone.toggleGroupCallSettings#974392f2 flags:# reset_invite_hash:flags.1?true call:InputGroupCall join_muted:flags.0?Bool messages_enabled:flags.2?Bool send_paid_messages_stars:flags.3?long = Updates;
//
// join_muted (who joins does not get the word) and new invite links, by
// one who manages the call. join_muted applies to those who join after it;
// whether Telegram also takes the word from those in the call is not
// verified. Messages in a call and their price come in a later stage
// (ADR 0059, stage 5): GROUPCALL_NOT_MODIFIED. How the clients show it is
// not verified. Of a conference only the link is reset, by its creator
// (Android CallLogActivity.java:1613-1625, iOS GroupCalls.swift:3399-3415,
// tdesktop calls_group_common.cpp:308-330): a new slug, and the old one
// finds nothing from then on; join_muted is GROUPCALL_FORBIDDEN (decision
// 11 of task 117).
func (c *GroupCallsCore) PhoneToggleGroupCallSettings(in *mtproto.TLPhoneToggleGroupCallSettings) (*mtproto.Updates, error) {
	if in.GetMessagesEnabled() != nil || in.GetSendPaidMessagesStars() != nil {
		return nil, mtproto.ErrGroupcallNotModified
	}
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.accessOf(call, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if call.Conference {
		return c.resetConferenceLink(a, call, in)
	}
	if !a.canManage(c.MD.UserId) {
		return nil, mtproto.ErrChatAdminRequired
	}
	after, changed := call, false
	if v := in.GetJoinMuted(); v != nil {
		var ch bool
		if after, ch, err = c.svcCtx.Dao.Store.SetJoinMuted(c.ctx, call.Id, mtproto.FromBool(v)); err != nil {
			return nil, storeError(err)
		}
		changed = changed || ch
	}
	if in.GetResetInviteHash() {
		listener, speaker, err := newInviteHashes()
		if err != nil {
			return nil, err
		}
		if after, _, err = c.svcCtx.Dao.Store.SetInviteHashes(c.ctx, call.Id, listener, speaker); err != nil {
			return nil, storeError(err)
		}
		changed = true
	}
	if !changed {
		return nil, mtproto.ErrGroupcallNotModified
	}
	c.deliver(a, after, fanOut{call: true})
	return c.answer(a, after, nil, nil, true), nil
}

// resetConferenceLink is phone.toggleGroupCallSettings of a conference.
func (c *GroupCallsCore) resetConferenceLink(a *access, call *groupcalls.Call, in *mtproto.TLPhoneToggleGroupCallSettings) (*mtproto.Updates, error) {
	if !a.canManage(c.MD.UserId) || in.GetJoinMuted() != nil {
		return nil, mtproto.ErrGroupcallForbidden
	}
	if !in.GetResetInviteHash() {
		return nil, mtproto.ErrGroupcallNotModified
	}
	slug, err := groupcalls.NewSlug()
	if err != nil {
		return nil, err
	}
	after, err := c.svcCtx.Dao.Store.ResetSlug(c.ctx, call.Id, slug)
	if err != nil {
		return nil, storeError(err)
	}
	c.deliver(a, after, fanOut{call: true})
	return c.answer(a, after, nil, nil, true), nil
}
