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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// The creator hands the group to a member: the member is the creator, the
// old one an admin; nobody else may, and the creator may not hand it to one
// outside.
func TestEditChatCreator(t *testing.T) {
	u := newUsers(4)
	a, b, c, outsider := u[0], u[1], u[2], u[3]
	id := newChat(t, a, b, c)
	cc := chatCore()

	_, err := cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: b, ChatId: id, UserId: c})
	wantErr(t, "a member hands the group", err, mtproto.ErrChatAdminRequired)
	_, err = cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: a, ChatId: id, UserId: outsider})
	wantErr(t, "to one outside", err, mtproto.ErrUserIdInvalid)

	got, err := cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: a, ChatId: id, UserId: b})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetChat().GetCreator() != b {
		t.Errorf("creator %d, want %d", got.GetChat().GetCreator(), b)
	}
	if p, _ := got.GetImmutableChatParticipant(b); !p.IsChatMemberCreator() {
		t.Errorf("the new creator is %v", p)
	}
	if p, _ := got.GetImmutableChatParticipant(a); !p.IsChatMemberAdmin() {
		t.Errorf("the old creator is %v", p)
	}
	_, err = cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: a, ChatId: id, UserId: c})
	wantErr(t, "the old creator hands it again", err, mtproto.ErrChatAdminRequired)
}
