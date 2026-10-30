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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The bio takes the 70 characters of about_length_limit_default, Cyrillic
// as well (140 bytes), and refuses the 71st (task 020).
func TestAboutLength(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	update := func(about string) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateProfile{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateProfile),
			About:       wrapperspb.String(about),
		})
		return err
	}

	about := strings.Repeat("я", 70)
	if err := update(about); err != nil {
		t.Fatalf("a bio of 70 letters: %v", err)
	}
	if full, _ := fullUser(ctx, t, alice, inputSelf(alice)); full.GetAbout().GetValue() != about {
		t.Errorf("the bio reads back as %d letters", len([]rune(full.GetAbout().GetValue())))
	}
	if err := update(about + "я"); !mtclient.IsRPCError(err, "ABOUT_TOO_LONG") {
		t.Errorf("a bio of 71 letters: %v", err)
	}
}
