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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Suggestions (task 020, core.telegram.org/api/config): an account without
// a birthday is asked to set one, as Telegram does; a dismissed suggestion
// is listed as such on every device; with a birthday set the request stops.
func TestSuggestions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	p := promoData(ctx, t, alice)
	if left := int64(p.GetExpires()) - time.Now().Unix(); left < 14*60 || left > 16*60 {
		t.Errorf("expires in %d s, want 15 minutes", left)
	}
	if !slices.Equal(p.GetPendingSuggestions(), []string{"BIRTHDAY_SETUP"}) || len(p.GetDismissedSuggestions()) != 0 {
		t.Fatalf("a new account: pending %v, dismissed %v", p.GetPendingSuggestions(), p.GetDismissedSuggestions())
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLHelpDismissSuggestion{
		Constructor: alice.c.Constructor(mtproto.Predicate_help_dismissSuggestion),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Suggestion:  "BIRTHDAY_CONTACTS_TODAY",
	}); err != nil {
		t.Fatalf("help.dismissSuggestion: %v", err)
	}
	if p = promoData(ctx, t, alice); !slices.Equal(p.GetDismissedSuggestions(), []string{"BIRTHDAY_CONTACTS_TODAY"}) {
		t.Errorf("dismissed %v after dismissing the birthday banner", p.GetDismissedSuggestions())
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBirthday{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBirthday),
		Birthday:    mtproto.MakeTLBirthday(&mtproto.Birthday{Month: 5, Day: 17}).To_Birthday(),
	}); err != nil {
		t.Fatalf("account.updateBirthday: %v", err)
	}
	if p = promoData(ctx, t, alice); len(p.GetPendingSuggestions()) != 0 {
		t.Errorf("pending %v with a birthday set", p.GetPendingSuggestions())
	}
}

func promoData(ctx context.Context, t *testing.T, u *user) *mtproto.Help_PromoData {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetPromoData{Constructor: u.c.Constructor(mtproto.Predicate_help_getPromoData)})
	if err != nil {
		t.Fatalf("help.getPromoData: %v", err)
	}
	r, ok := o.(interface {
		To_Help_PromoData() *mtproto.Help_PromoData
	})
	if !ok {
		t.Fatalf("help.getPromoData returned %T", o)
	}
	if p := r.To_Help_PromoData(); p.GetPredicateName() == mtproto.Predicate_help_promoData {
		return p
	}
	t.Fatalf("help.getPromoData returned %s", r.To_Help_PromoData().GetPredicateName())
	return nil
}
