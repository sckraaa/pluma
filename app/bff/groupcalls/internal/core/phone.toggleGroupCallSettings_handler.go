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
)

// PhoneToggleGroupCallSettings
// phone.toggleGroupCallSettings#974392f2 flags:# reset_invite_hash:flags.1?true call:InputGroupCall join_muted:flags.0?Bool messages_enabled:flags.2?Bool send_paid_messages_stars:flags.3?long = Updates;
//
// join_muted (who joins does not get the word) and new invite links, by
// one who manages the call. join_muted applies to those who join after it;
// whether Telegram also takes the word from those in the call is not
// verified. Messages in a call and their price come in a later stage
// (ADR 0059, stage 5): GROUPCALL_NOT_MODIFIED. How the clients show it is
// not verified.
func (c *GroupCallsCore) PhoneToggleGroupCallSettings(in *mtproto.TLPhoneToggleGroupCallSettings) (*mtproto.Updates, error) {
	if in.GetMessagesEnabled() != nil || in.GetSendPaidMessagesStars() != nil {
		return nil, mtproto.ErrGroupcallNotModified
	}
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
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
