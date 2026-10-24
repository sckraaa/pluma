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

package premium

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func user(id int64, premium, requires bool, contacts ...int64) *mtproto.ImmutableUser {
	u := &mtproto.ImmutableUser{User: mtproto.MakeTLUserData(&mtproto.UserData{
		Id:                               id,
		Premium:                          premium,
		NewNoncontactPeersRequirePremium: requires,
	}).To_UserData()}
	for _, c := range contacts {
		u.Contacts = append(u.Contacts, &mtproto.ContactData{UserId: id, ContactUserId: c})
	}
	return u
}

func TestRequiredToContact(t *testing.T) {
	const sender, target = 1, 2
	chat := func(has bool) func() (bool, error) { return func() (bool, error) { return has, nil } }
	unasked := func() (bool, error) { return false, errors.New("the chat was asked") }
	for _, tc := range []struct {
		name           string
		sender, target *mtproto.ImmutableUser
		hasChat        func() (bool, error)
		want           bool
	}{
		{"no setting", user(sender, false, false), user(target, true, false), unasked, false},
		{"setting without Premium", user(sender, false, false), user(target, false, true), unasked, false},
		{"Premium sender", user(sender, true, false), user(target, true, true), unasked, false},
		{"contact of target", user(sender, false, false), user(target, true, true, sender), unasked, false},
		{"target is a contact of sender only", user(sender, false, false, target), user(target, true, true), chat(false), true},
		{"chat there already", user(sender, false, false), user(target, true, true), chat(true), false},
		{"stranger", user(sender, false, false), user(target, true, true), chat(false), true},
		{"to oneself", user(target, false, false), user(target, true, true), unasked, false},
	} {
		got, err := RequiredToContact(tc.sender, tc.target, tc.hasChat)
		if err != nil || got != tc.want {
			t.Errorf("%s: %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
	if _, err := RequiredToContact(user(sender, false, false), user(target, true, true), func() (bool, error) {
		return false, errors.New("down")
	}); err == nil {
		t.Error("an error of hasChat is lost")
	}
}
