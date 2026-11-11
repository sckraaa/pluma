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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// TestGhostAccount: user.setGhost (pluma-admin ghost set) makes an account
// the support ghost at once, through the cache: its phone goes to nobody
// else, not even to one who keeps the number and to whom its privacy shows
// it; nobody finds it by the number, and an import of the number answers
// as for a number without an account; its own setting that only contacts
// may write closes nothing. Its devices and its contacts get updateUser.
// The same value changes nothing; an unknown account is USER_ID_INVALID.
func TestGhostAccount(t *testing.T) {
	c := userCore(t)
	support, phone := newUser(t, "Support")
	ann, _ := newUser(t, "Ann")
	// The account shows its phone to everybody, lets everybody find it and
	// only contacts write; Ann keeps the number.
	if _, err := c.UserSetGlobalPrivacySettings(&user.TLUserSetGlobalPrivacySettings{UserId: support,
		Settings: mtproto.MakeTLGlobalPrivacySettings(&mtproto.GlobalPrivacySettings{NewNoncontactPeersRequirePremium: true}).To_GlobalPrivacySettings()}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []int{mtproto.PHONE_NUMBER, mtproto.ADDED_BY_PHONE} {
		if _, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: support, KeyType: int32(key), Rules: []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}}); err != nil {
			t.Fatal(err)
		}
	}
	importContacts(t, ann, phoneContact(1, "+"+phone, "Help", ""))
	if contactOf(t, ann, support) == nil {
		t.Fatal("Ann did not get the account as a contact before")
	}
	seen := func() (phoneShown, findable, requirePremium bool) {
		t.Helper()
		users, err := c.UserGetMutableUsers(&user.TLUserGetMutableUsers{Id: []int64{support, ann}, To: []int64{ann}})
		if err != nil {
			t.Fatal(err)
		}
		s, _ := users.GetImmutableUser(support)
		a, _ := users.GetImmutableUser(ann)
		if s == nil || a == nil {
			t.Fatal("users not loaded")
		}
		return s.ToUnsafeUser(a).GetPhone().GetValue() == phone, s.CheckPrivacyFor(mtproto.ADDED_BY_PHONE, a), s.ContactRequirePremium()
	}
	if p, f, r := seen(); !p || !f || !r {
		t.Fatalf("before: phone %v, findable %v, premium only %v", p, f, r)
	}

	set := func(v, want bool) {
		t.Helper()
		r, err := c.UserSetGhost(&user.TLUserSetGhost{UserId: support, Ghost: mtproto.ToBool(v)})
		if err != nil || mtproto.FromBool(r) != want {
			t.Fatalf("ghost %v: %v, %v; want changed %v", v, r, err, want)
		}
	}
	toAnn := func() int {
		t.Helper()
		return count(t, "SELECT COUNT(*) FROM mq_outbox WHERE initiator_id = ? AND recipient_id = ? AND method = 'inbox.TL_inbox_seqUpdatesToInbox'", support, ann)
	}
	before := toAnn()
	set(true, true)
	if p, f, r := seen(); p || f || r {
		t.Errorf("a ghost: phone %v, findable %v, premium only %v", p, f, r)
	}
	me, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, support, false)
	if err != nil || me.ToSelfUser().GetPhone().GetValue() != phone {
		t.Errorf("its own phone: %v", err)
	}
	set(true, false)

	// A new importer of the number finds no account.
	bob, _ := newUser(t, "Bob")
	r := importContacts(t, bob, phoneContact(1, "+"+phone, "Help", ""))
	if len(r.Imported) != 0 || len(r.Users) != 0 || len(r.PopularInvites) != 1 {
		t.Errorf("import of the ghost's number: %v", r)
	}
	if contactOf(t, bob, support) != nil {
		t.Error("Bob got the ghost as a contact")
	}

	if n := toAnn(); n != before+1 {
		t.Errorf("Ann got %d updates of the change", n-before)
	}

	set(false, true)
	if p, f, _ := seen(); !p || !f {
		t.Errorf("no ghost again: phone %v, findable %v", p, f)
	}
	if _, err := c.UserSetGhost(&user.TLUserSetGhost{UserId: 1 << 40, Ghost: mtproto.BoolTrue}); !errors.Is(err, mtproto.ErrUserIdInvalid) {
		t.Errorf("an unknown account: %v, want USER_ID_INVALID", err)
	}
}

// TestGhostSignUpReachesNoImporter: an account made the support ghost
// before its queued sign-up went out tells no importer of its number that
// the number has an account: no contact, no messageActionContactSignUp,
// and the sign-up is done.
func TestGhostSignUpReachesNoImporter(t *testing.T) {
	c := userCore(t)
	ann, _ := newUser(t, "Ann")
	phone := nextPhone()
	importContacts(t, ann, phoneContact(1, "+"+phone, "Help", ""))

	support := signUp(t, phone, "Support")
	if _, err := c.UserSetGhost(&user.TLUserSetGhost{UserId: support, Ghost: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	toAnn := func() int {
		t.Helper()
		return count(t, "SELECT COUNT(*) FROM mq_outbox WHERE initiator_id = ? AND recipient_id = ?", support, ann)
	}
	before := toAnn()
	deliver(t, support)

	if n := count(t, "SELECT COUNT(*) FROM contact_signups WHERE user_id = ?", support); n != 0 {
		t.Error("the sign-up is still queued")
	}
	if contactOf(t, ann, support) != nil {
		t.Error("Ann got the ghost as a contact")
	}
	if n := toAnn(); n != before {
		t.Errorf("Ann got %d events of the sign-up", n-before)
	}
}
