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

package user

import (
	"github.com/teamgram/proto/mtproto"
)

// MissingInvitee returns the missingInvitee of a user whose privacy keeps
// inviter from adding them to a group or a channel, or nil when it lets them.
// premium_would_allow_invite says the same rules would let the inviter
// with Premium (EvaluatePrivacy as for a Premium inviter).
func MissingInvitee(inviter, invitee *mtproto.ImmutableUser) *mtproto.MissingInvitee {
	if invitee.CheckPrivacyFor(mtproto.CHAT_INVITE, inviter) {
		return nil
	}
	wouldAllow := false
	if !inviter.Premium() {
		contact, _ := invitee.CheckContact(inviter.Id())
		for _, k := range invitee.GetKeysPrivacyRules() {
			if k.GetKey() == mtproto.CHAT_INVITE {
				wouldAllow = mtproto.EvaluatePrivacy(k.GetRules(), mtproto.PrivacyViewer{
					Id:      inviter.Id(),
					Contact: contact,
					Premium: true,
					Bot:     inviter.IsBot(),
				})
			}
		}
	}
	return mtproto.MakeTLMissingInvitee(&mtproto.MissingInvitee{
		PremiumWouldAllowInvite: wouldAllow,
		UserId:                  invitee.Id(),
	}).To_MissingInvitee()
}
