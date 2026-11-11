//go:build e2e

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

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestPhoneOfKeptNumber: Alice shows her number to nobody; Bob, who keeps
// it in his contacts, still sees it, as Telegram says under the setting
// ("Users who have your number saved in their contacts will also see it",
// Android PrivacyPhoneInfo); Carol, who does not, sees no number (the
// owner's report of 11.11: a friend's sign-up was announced, his profile
// showed no number).
func TestPhoneOfKeptNumber(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountSetPrivacy{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_setPrivacy),
		Key:         &mtproto.InputPrivacyKey{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyKeyPhoneNumber)},
		Rules:       []*mtproto.InputPrivacyRule{{Constructor: alice.c.Constructor(mtproto.Predicate_inputPrivacyValueDisallowAll)}},
	}); err != nil {
		t.Fatalf("account.setPrivacy: %v", err)
	}
	number := strings.TrimPrefix(alice.phone, "+")
	imported := importContacts(ctx, t, bob, phoneContact(bob.c, 1, number, "Alice", ""))
	if len(imported.GetUsers()) != 1 {
		t.Fatalf("Bob's import: %d users", len(imported.GetUsers()))
	}

	phoneOf := func(u *user) string {
		t.Helper()
		raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
			Constructor: u.c.Constructor(mtproto.Predicate_users_getUsers),
			Id:          []*mtproto.InputUser{inputUserOf(ctx, t, u, alice)},
		})
		if err != nil {
			t.Fatalf("users.getUsers: %v", err)
		}
		us, err := mtclient.DecodeVector(raw)
		if err != nil || len(us) != 1 {
			t.Fatalf("users.getUsers: %d users, %v", len(us), err)
		}
		return us[0].(interface{ To_User() *mtproto.User }).To_User().GetPhone().GetValue()
	}
	if got := phoneOf(bob); got != number {
		t.Errorf("Bob, who keeps the number: phone %q, want %q", got, number)
	}
	if got := phoneOf(carol); got != "" {
		t.Errorf("Carol, who does not: phone %q, want none", got)
	}
}
