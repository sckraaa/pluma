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

package mtproto

import "testing"

// pluma: TestPhoneOfKeptNumber: a viewer who keeps the user's number in its
// contacts sees it whatever the user's privacy; a contact added without the
// number, or with another one, does not.
func TestPhoneOfKeptNumber(t *testing.T) {
	const viewerId, ownerId, phone = 2, 1, "79990001122"
	owner := func(rule *PrivacyRule, rows ...*ContactData) *ImmutableUser {
		return MakeTLImmutableUser(&ImmutableUser{
			User: MakeTLUserData(&UserData{Id: ownerId, Phone: phone}).To_UserData(),
			KeysPrivacyRules: []*PrivacyKeyRules{
				MakeTLPrivacyKeyRules(&PrivacyKeyRules{Key: PHONE_NUMBER, Rules: []*PrivacyRule{rule}}).To_PrivacyKeyRules(),
			},
			ReverseContacts: rows,
		}).To_ImmutableUser()
	}
	row := func(saved string) *ContactData {
		return MakeTLContactData(&ContactData{UserId: viewerId, ContactUserId: ownerId, Phone: MakeFlagsString(saved)}).To_ContactData()
	}
	viewer := MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{Id: viewerId}).To_UserData()}).To_ImmutableUser()
	for _, tc := range []struct {
		name  string
		owner *ImmutableUser
		row   *ContactData
		want  bool
	}{
		{"nobody, no contact", owner(disallowAll()), nil, false},
		{"nobody, the number kept", owner(disallowAll(), row(phone)), row(phone), true},
		{"nobody, the number kept with a plus", owner(disallowAll(), row("+"+phone)), row("+" + phone), true},
		{"nobody, a contact without the number", owner(disallowAll(), row("")), row(""), false},
		{"nobody, another number kept", owner(disallowAll(), row("79990009999")), row("79990009999"), false},
		{"everybody", owner(allowAll()), nil, true},
	} {
		v := viewer
		if tc.row != nil {
			v = MakeTLImmutableUser(&ImmutableUser{User: viewer.User, Contacts: []*ContactData{tc.row}}).To_ImmutableUser()
		}
		for path, got := range map[string]string{
			"ToUnsafeUser": tc.owner.ToUnsafeUser(v).GetPhone().GetValue(),
			"ToUserFor":    tc.owner.ToUserFor(viewer).GetPhone().GetValue(),
			"ToUser":       tc.owner.ToUser(viewerId).GetPhone().GetValue(),
		} {
			if (got == phone) != tc.want {
				t.Errorf("%s, %s: phone %q, want shown %v", tc.name, path, got, tc.want)
			}
		}
	}
}
