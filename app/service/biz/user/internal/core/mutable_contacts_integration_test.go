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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// The users loaded together carry the contact rows between them both ways:
// the owner's Contacts and the contact's ReverseContacts. The privacy rules
// "my contacts" ask the owner's side, so a contact passes them.
func TestMutableUsersContacts(t *testing.T) {
	c := userCore(t)
	ann, _ := newUser(t, "Ann")
	bob, _ := newUser(t, "Bob")
	load := func() (*mtproto.ImmutableUser, *mtproto.ImmutableUser) {
		t.Helper()
		r, err := c.UserGetMutableUsersV2(&user.TLUserGetMutableUsersV2{Id: []int64{ann, bob}, Privacy: true, HasTo: true, To: []int64{ann, bob}})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := r.GetImmutableUser(ann)
		b, _ := r.GetImmutableUser(bob)
		if a == nil || b == nil {
			t.Fatalf("users %v", r.GetUsers())
		}
		return a, b
	}

	if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: ann, Id: bob, FirstName: "Bob", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	a, b := load()
	if ok, _ := a.CheckContact(bob); !ok {
		t.Error("Ann has Bob, but not by CheckContact")
	}
	if ok, _ := b.CheckReverseContact(ann); !ok {
		t.Error("Bob is Ann's contact, but not by CheckReverseContact")
	}
	if ok, _ := b.CheckContact(ann); ok {
		t.Error("Bob does not have Ann, but CheckContact says so")
	}

	// Bob shows his last seen time to his contacts only.
	contactsOnly := []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}
	if _, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: bob, KeyType: mtproto.STATUS_TIMESTAMP, Rules: contactsOnly}); err != nil {
		t.Fatal(err)
	}
	if _, b = load(); b.CheckPrivacy(mtproto.STATUS_TIMESTAMP, ann) {
		t.Error("Ann is not Bob's contact, but passes his rule")
	}
	if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: bob, Id: ann, FirstName: "Ann", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if _, b = load(); !b.CheckPrivacy(mtproto.STATUS_TIMESTAMP, ann) {
		t.Error("Ann is Bob's contact, but does not pass his rule")
	}
	if ok, mutual := b.CheckContact(ann); !ok || !mutual {
		t.Errorf("Bob's contact Ann: %v, mutual %v", ok, mutual)
	}
}
