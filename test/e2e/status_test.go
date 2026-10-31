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
