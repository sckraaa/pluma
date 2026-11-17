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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestUserUsernames (task 104) end to end: the methods of more usernames
// of an account reach the server (RPCFragment) and, with its one editable
// username, answer as Telegram does for an account without purchased ones:
// the editable username is not hidden (USERNAME_INVALID), nor is a name
// that is not the account's, showing it again changes nothing
// (USERNAME_NOT_MODIFIED), and so does its one name as the order, as
// tdesktop sends it (api_user_names.cpp:227); an order with a name that is
// not the account's is ORDER_INVALID.
func TestUserUsernames(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	alice := login(ctx, t, phones[0], "Alice")

	name := fmt.Sprintf("pluma_names_%d", alice.self.GetId())
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateUsername),
		Username:    name,
	}); err != nil {
		t.Fatalf("account.updateUsername: %v", err)
	}
	toggle := func(username string, active bool) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountToggleUsername{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_toggleUsername),
			Username:    username,
			Active:      mtproto.ToBool(active),
		})
		return err
	}
	reorder := func(order ...string) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountReorderUsernames{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_reorderUsernames),
			Order:       order,
		})
		return err
	}
	if err := toggle(name, false); !mtclient.IsRPCError(err, "USERNAME_INVALID") {
		t.Errorf("hiding the editable username: %v, want USERNAME_INVALID", err)
	}
	if err := toggle(name, true); !mtclient.IsRPCError(err, "USERNAME_NOT_MODIFIED") {
		t.Errorf("showing the shown username: %v, want USERNAME_NOT_MODIFIED", err)
	}
	if err := toggle("nobody_"+name, true); !mtclient.IsRPCError(err, "USERNAME_INVALID") {
		t.Errorf("a name that is not the account's: %v, want USERNAME_INVALID", err)
	}
	if err := reorder(name); !mtclient.IsRPCError(err, "USERNAME_NOT_MODIFIED") {
		t.Errorf("the order of one name: %v, want USERNAME_NOT_MODIFIED", err)
	}
	if err := reorder(name, "nobody_"+name); !mtclient.IsRPCError(err, "ORDER_INVALID") {
		t.Errorf("an order with a name that is not the account's: %v, want ORDER_INVALID", err)
	}
	if err := reorder("nobody_" + name); !mtclient.IsRPCError(err, "ORDER_INVALID") {
		t.Errorf("an order without the account's name: %v, want ORDER_INVALID", err)
	}
}
