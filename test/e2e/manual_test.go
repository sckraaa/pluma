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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// TestManualSend writes to a real client on the stand, to check what it
// shows (a push with the app closed, docs/ops/clients.md). It runs only
// with PLUMA_E2E_SEND_TO, the phone of the client's account:
//
//	PLUMA_E2E_SEND_TO=+79990000081 E2E_GOTEST_FLAGS='-run TestManualSend' test/e2e/run.sh test
//
// PLUMA_E2E_SEND_TEXT is the text (default "Pluma: проверка").
func TestManualSend(t *testing.T) {
	to := os.Getenv("PLUMA_E2E_SEND_TO")
	if to == "" {
		t.Skip("PLUMA_E2E_SEND_TO is not set")
	}
	text := os.Getenv("PLUMA_E2E_SEND_TEXT")
	if text == "" {
		text = "Pluma: проверка"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sender := login(ctx, t, testPhones(t, 1)[0], "Tester")
	o, err := sender.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
		Constructor: sender.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
		Phone:       strings.TrimPrefix(to, "+"),
	})
	if err != nil {
		t.Fatalf("contacts.resolvePhone %s: %v", to, err)
	}
	resolved, ok := o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	})
	if !ok {
		t.Fatalf("contacts.resolvePhone returned %T", o)
	}
	r := resolved.To_Contacts_ResolvedPeer()
	for _, u := range r.GetUsers() {
		if u.GetId() == r.GetPeer().GetUserId() && u.GetAccessHash() != nil {
			sendTo(ctx, t, sender, &mtproto.InputPeer{
				Constructor: sender.c.Constructor(mtproto.Predicate_inputPeerUser),
				UserId:      u.GetId(),
				AccessHash:  u.GetAccessHash().GetValue(),
			}, text)
			t.Logf("sent %q to %s (user %d)", text, to, u.GetId())
			return
		}
	}
	t.Fatalf("contacts.resolvePhone did not return the user of %s", to)
}
