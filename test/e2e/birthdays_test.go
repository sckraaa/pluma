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

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The birthday of a contact today is in contacts.getBirthdays with the
// contact's user, which the clients show as the birthday banner (task 020).
// By default the birthday is for contacts only: Alice and Bob have each
// other in their contacts and see it, Carol does not.
func TestContactBirthdays(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, formattedPhone(bob.phone), "Bob", ""))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, formattedPhone(alice.phone), "Alice", ""))

	today := time.Now().UTC()
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountUpdateBirthday{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_updateBirthday),
		Birthday:    mtproto.MakeTLBirthday(&mtproto.Birthday{Month: int32(today.Month()), Day: int32(today.Day())}).To_Birthday(),
	}); err != nil {
		t.Fatalf("account.updateBirthday: %v", err)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLContactsGetBirthdays{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_getBirthdays),
	})
	if err != nil {
		t.Fatalf("contacts.getBirthdays: %v", err)
	}
	r, ok := o.(interface {
		To_Contacts_ContactBirthdays() *mtproto.Contacts_ContactBirthdays
	})
	if !ok {
		t.Fatalf("contacts.getBirthdays returned %T", o)
	}
	b := r.To_Contacts_ContactBirthdays()
	if len(b.GetContacts()) != 1 || b.GetContacts()[0].GetContactId() != bob.self.GetId() || len(b.GetUsers()) != 1 {
		t.Fatalf("contacts.getBirthdays: %d birthdays, %d users", len(b.GetContacts()), len(b.GetUsers()))
	}
	if d := b.GetContacts()[0].GetBirthday(); d.GetMonth() != int32(today.Month()) || d.GetDay() != int32(today.Day()) || d.GetYear() != nil {
		t.Errorf("Bob's birthday %v", d)
	}

	// His profile shows it to his contact and not to anybody else.
	if full, _ := fullUser(ctx, t, alice, inputUserOf(ctx, t, alice, bob)); full.GetBirthday().GetDay() != int32(today.Day()) {
		t.Errorf("Bob's profile shows Alice the birthday %v", full.GetBirthday())
	}
	if full, _ := fullUser(ctx, t, carol, inputUserOf(ctx, t, carol, bob)); full.GetBirthday() != nil {
		t.Errorf("Bob's profile shows Carol, not a contact, the birthday %v", full.GetBirthday())
	}
}
