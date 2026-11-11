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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// getUser returns users.getUsers of input as u sees it.
func getUser(ctx context.Context, t *testing.T, u *user, input *mtproto.InputUser) *mtproto.User {
	t.Helper()
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
		Constructor: u.c.Constructor(mtproto.Predicate_users_getUsers),
		Id:          []*mtproto.InputUser{input},
	})
	if err != nil {
		t.Fatalf("users.getUsers: %v", err)
	}
	users, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users.getUsers returned %d users", len(users))
	}
	x, ok := users[0].(interface{ To_User() *mtproto.User })
	if !ok {
		t.Fatalf("users.getUsers returned %T", users[0])
	}
	return x.To_User()
}

// TestUserVerify: pluma-admin user verify gives an account the verification
// badge. A contact's device gets updateUser with the account verified, and
// users.getUsers and users.getFullUser show the badge to the account itself
// and to the contact; off takes it away the same way.
func TestUserVerify(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	name := fmt.Sprintf("alice_e2e_%d", alice.self.GetId())
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateUsername),
		Username:    name,
	}); err != nil {
		t.Fatalf("account.updateUsername: %v", err)
	}
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, phones[0], "Alice", ""))
	aliceForBob := inputUserOf(ctx, t, bob, alice)
	if getUser(ctx, t, bob, aliceForBob).GetVerified() {
		t.Fatal("Alice is verified before the command")
	}

	check := func(verified bool) {
		t.Helper()
		_, err := bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, x := range us.GetUpdates() {
				if x.GetPredicateName() != mtproto.Predicate_updateUser || x.GetUserId() != alice.self.GetId() {
					continue
				}
				for _, u := range us.GetUsers() {
					if u.GetId() == alice.self.GetId() && u.GetVerified() == verified {
						return true
					}
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("no updateUser with Alice verified %v on Bob's device: %v", verified, err)
		}
		if got := getUser(ctx, t, bob, aliceForBob).GetVerified(); got != verified {
			t.Errorf("Bob's users.getUsers: verified %v, want %v", got, verified)
		}
		if _, u := fullUser(ctx, t, bob, aliceForBob); u.GetVerified() != verified {
			t.Errorf("Bob's users.getFullUser: verified %v, want %v", u.GetVerified(), verified)
		}
		if got := getUser(ctx, t, alice, inputSelf(alice)).GetVerified(); got != verified {
			t.Errorf("Alice's own users.getUsers: verified %v, want %v", got, verified)
		}
	}

	getState(ctx, t, bob)
	if out := admin(t, "user", "verify", "@"+name, "on"); !strings.Contains(out, fmt.Sprintf("user %d (@%s, Alice) is verified now", alice.self.GetId(), name)) {
		t.Fatalf("user verify on: %q", out)
	}
	check(true)
	if out := admin(t, "user", "verify", "@"+strings.ToUpper(name), "on"); !strings.Contains(out, "is already verified") {
		t.Errorf("user verify on again: %q", out)
	}

	if out := admin(t, "user", "verify", fmt.Sprint(alice.self.GetId()), "off"); !strings.Contains(out, "is not verified now") {
		t.Fatalf("user verify off: %q", out)
	}
	check(false)
}
