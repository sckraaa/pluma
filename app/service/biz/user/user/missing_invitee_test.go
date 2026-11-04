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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func inviteUser(id int64, premium bool, rules ...*mtproto.PrivacyRule) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, Premium: premium}).To_UserData(),
		KeysPrivacyRules: []*mtproto.PrivacyKeyRules{
			mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{Key: mtproto.CHAT_INVITE, Rules: rules}).To_PrivacyKeyRules(),
		},
	}).To_ImmutableUser()
}

func TestMissingInvitee(t *testing.T) {
	var (
		contacts  = mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()
		premium   = mtproto.MakeTLPrivacyValueAllowPremium(nil).To_PrivacyRule()
		everybody = mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()
		notMe     = mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: []int64{1}}).To_PrivacyRule()
	)
	inviter := inviteUser(1, false)
	for _, tc := range []struct {
		name       string
		invitee    *mtproto.ImmutableUser
		missing    bool
		wouldAllow bool
	}{
		{"everybody", inviteUser(2, false, everybody), false, false},
		{"contacts", inviteUser(2, false, contacts), true, false},
		{"contacts and Premium users", inviteUser(2, false, contacts, premium), true, true},
		// The inviter is named in the exceptions: Premium would not help.
		{"Premium users, but not the inviter", inviteUser(2, false, contacts, premium, notMe), true, false},
	} {
		m := MissingInvitee(inviter, tc.invitee)
		if (m != nil) != tc.missing || m.GetPremiumWouldAllowInvite() != tc.wouldAllow {
			t.Errorf("%s: %v, want missing %v, premium_would_allow_invite %v", tc.name, m, tc.missing, tc.wouldAllow)
		}
	}
}
