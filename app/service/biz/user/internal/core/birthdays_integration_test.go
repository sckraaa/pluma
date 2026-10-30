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

//go:build integration

package core

import (
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// contacts.getBirthdays (task 020): the contacts with a birthday within one
// day of today, whose birthday privacy shows it. By default only their
// contacts see it; a contact who allows everybody shows it to all.
func TestContactBirthdays(t *testing.T) {
	c := userCore(t)
	ann, _ := newUser(t, "Ann")
	near := func(days int) *mtproto.Birthday {
		d := time.Now().UTC().AddDate(0, 0, days)
		return mtproto.MakeTLBirthday(&mtproto.Birthday{Year: mtproto.MakeFlagsInt32(1990), Month: int32(d.Month()), Day: int32(d.Day())}).To_Birthday()
	}
	contact := func(name string, birthday *mtproto.Birthday, mutual bool) int64 {
		t.Helper()
		id, _ := newUser(t, name)
		if _, err := c.UserUpdateBirthday(&user.TLUserUpdateBirthday{UserId: id, Birthday: birthday}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: ann, Id: id, FirstName: name, AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
			t.Fatal(err)
		}
		if mutual {
			if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: id, Id: ann, FirstName: "Ann", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	today := contact("Today", near(0), true)
	tomorrow := contact("Tomorrow", near(1), true)
	contact("Later", near(5), true)
	contact("NotMutual", near(0), false)
	public := contact("Public", near(-1), false)
	if _, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: public, KeyType: mtproto.BIRTHDAY, Rules: []*mtproto.PrivacyRule{
		mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule(),
	}}); err != nil {
		t.Fatal(err)
	}
	hidden := contact("Hidden", near(0), true)
	if _, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: hidden, KeyType: mtproto.BIRTHDAY, Rules: []*mtproto.PrivacyRule{
		mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule(),
	}}); err != nil {
		t.Fatal(err)
	}

	r, err := c.UserGetBirthdays(&user.TLUserGetBirthdays{UserId: ann})
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, b := range r.GetDatas() {
		got = append(got, b.GetContactId())
		if b.GetBirthday().GetYear().GetValue() != 1990 {
			t.Errorf("contact %d: birthday %v", b.GetContactId(), b.GetBirthday())
		}
	}
	want := []int64{today, tomorrow, public}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("birthdays of %v, want %v", got, want)
	}
}
