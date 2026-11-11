//go:build integration

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

package core

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// TestGhostNotAdded: nobody adds the support ghost (ADR 0048) to a basic
// group, where it would be shown added; the group stays as it was.
func TestGhostNotAdded(t *testing.T) {
	u := newUsers(3)
	a, b, support := u[0], u[1], u[2]
	if _, err := itRaw.Exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, ?, 'US', 'Support', 1)",
		support, fmt.Sprint("1777", support)); err != nil {
		t.Fatal(err)
	}
	id := newChat(t, a, b)
	before := rowOf(t, id)
	_, err := chatCore().ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: support})
	wantErr(t, "add the ghost", err, mtproto.ErrUserPrivacyRestricted)
	if p := participantOf(t, id, support); p != nil {
		t.Errorf("the ghost's row: %+v", p)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the chat row %+v, want %+v", after, before)
	}
}
