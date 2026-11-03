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

// users.getFullUser of self shows the account online after
// account.updateStatus, as users.getUsers does and as Telegram answers
// (task 020): Web K's Settings showed "last seen Jan 1, 1970" under the
// account's own name.
func TestFullUserSelfOnline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateStatus{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateStatus),
		Offline:     mtproto.BoolFalse,
	}); err != nil {
		t.Fatalf("account.updateStatus: %v", err)
	}
	// The last seen time is written after the answer.
	var status *mtproto.UserStatus
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		_, u := fullUser(ctx, t, alice, inputSelf(alice))
		if status = u.GetStatus(); status.GetPredicateName() == mtproto.Predicate_userStatusOnline {
			return
		}
	}
	t.Errorf("users.getFullUser of self: status %v, want userStatusOnline", status)
}

func updateStatus(ctx context.Context, t *testing.T, u *user, offline bool) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateStatus{
		Constructor: u.c.Constructor(mtproto.Predicate_account_updateStatus),
		Offline:     mtproto.ToBool(offline),
	}); err != nil {
		t.Fatalf("account.updateStatus: %v", err)
	}
}

// statusUpdate waits on u for updateUserStatus of userId and returns its
// status, or nil if none comes within wait.
func statusUpdate(ctx context.Context, u *user, userId int64, wait time.Duration) *mtproto.UserStatus {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var status *mtproto.UserStatus
	_, _ = u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range append(us.GetUpdates(), us.GetUpdate()) {
			if x.GetPredicateName() == mtproto.Predicate_updateUserStatus && x.GetUserId() == userId {
				status = x.GetStatus_USERSTATUS()
				return true
			}
		}
		return false
	})
	return status
}

// account.updateStatus reaches at once, as updateUserStatus, everyone who
// sees the user (task 032): contacts, the partners of private dialogs, the
// members of shared basic groups, and the user's other devices. Before it, a
// friend who opened a chat was shown online only when the client asked for
// the user again. Nobody else is told, a repeated request soon after tells
// nobody again, and the users the status is hidden from are not told.
func TestStatusPush(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 5)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")     // has Alice as a contact
	carol := login(ctx, t, phones[2], "Carol") // nothing to do with Alice
	dave := login(ctx, t, phones[3], "Dave")   // wrote to Alice, no contact
	erin := login(ctx, t, phones[4], "Erin")   // in a group with Alice
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, phones[0], "Alice", ""))
	toAlice := inputUserOf(ctx, t, dave, alice)
	sendTo(ctx, t, dave, &mtproto.InputPeer{
		Constructor: dave.c.Constructor(mtproto.Predicate_inputPeerUser),
		UserId:      toAlice.GetUserId(),
		AccessHash:  toAlice.GetAccessHash(),
	}, "hi")
	createChat(ctx, t, erin, "Group", alice)
	for _, u := range []*user{laptop, bob, carol, dave, erin} {
		getState(ctx, t, u)
	}
	id := alice.self.GetId()

	before := time.Now().Unix()
	updateStatus(ctx, t, alice, false)
	for name, u := range map[string]*user{"the contact": bob, "the dialog partner": dave, "the group member": erin} {
		s := statusUpdate(ctx, u, id, 10*time.Second)
		if s.GetPredicateName() != mtproto.Predicate_userStatusOnline || int64(s.GetExpires()) < before+55 || int64(s.GetExpires()) > time.Now().Unix()+60 {
			t.Fatalf("%s got %v, want userStatusOnline a minute ahead", name, s)
		}
	}
	if s := statusUpdate(ctx, laptop, id, 10*time.Second); s.GetPredicateName() != mtproto.Predicate_userStatusOnline {
		t.Errorf("the other device got %v, want userStatusOnline", s)
	}
	if s := statusUpdate(ctx, carol, id, 3*time.Second); s != nil {
		t.Errorf("a user without Alice in the contacts got %v", s)
	}

	updateStatus(ctx, t, alice, false)
	if s := statusUpdate(ctx, bob, id, 3*time.Second); s != nil {
		t.Errorf("a repeated request told the contact again: %v", s)
	}

	updateStatus(ctx, t, alice, true)
	s := statusUpdate(ctx, bob, id, 10*time.Second)
	if s.GetPredicateName() != mtproto.Predicate_userStatusOffline || int64(s.GetWasOnline()) < before {
		t.Fatalf("after offline the contact got %v, want userStatusOffline", s)
	}
	if _, u := fullUser(ctx, t, bob, inputUserOf(ctx, t, bob, alice)); u.GetStatus().GetPredicateName() != mtproto.Predicate_userStatusOffline {
		t.Errorf("users.getFullUser after offline: %v, want userStatusOffline", u.GetStatus())
	}

	// Last seen hidden from everybody: coming online tells the contact nothing.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountSetPrivacy{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_setPrivacy),
		Key:         &mtproto.InputPrivacyKey{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyKeyStatusTimestamp)},
		Rules:       []*mtproto.InputPrivacyRule{{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyValueDisallowAll)}},
	}); err != nil {
		t.Fatalf("account.setPrivacy: %v", err)
	}
	updateStatus(ctx, t, alice, false)
	if s := statusUpdate(ctx, bob, id, 3*time.Second); s != nil {
		t.Errorf("a contact Alice hides her last seen from got %v", s)
	}
}
