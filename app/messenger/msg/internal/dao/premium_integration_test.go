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

//go:build integration

package dao_test

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/premium"
)

// A user who lets only contacts, chats there already and Premium users
// write gets no message from a stranger without Premium (task 014).
func TestSendToPremiumOnly(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	target, stranger, rich := newUser(), newUser(), newUser()
	w.users.mu.Lock()
	w.users.premium[target], w.users.premiumOnly[target] = true, true
	w.users.premium[rich] = true
	w.users.mu.Unlock()

	sendText := func(from, to int64) error {
		_, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{
			UserId: from, AuthKeyId: from * 10, PeerType: mtproto.PEER_USER, PeerId: to,
			Message: []*msg.OutboxMessage{outbox(textMessage(from, mtproto.MakePeerUser(to), "Hi"))},
		})
		return err
	}
	if err := sendText(stranger, target); !errors.Is(err, premium.ErrPrivacyPremiumRequired) {
		t.Fatalf("a stranger without Premium: %v, want PRIVACY_PREMIUM_REQUIRED", err)
	}
	if w.hasDialog(target, mtproto.PEER_USER, stranger) || w.hasDialog(stranger, mtproto.PEER_USER, target) {
		t.Fatal("a refused message made a dialog")
	}
	w.send(rich, mtproto.PEER_USER, target, "From Premium")

	// Once the target writes, there is a chat and the stranger may answer.
	w.send(target, mtproto.PEER_USER, stranger, "Hello")
	if has, err := w.d.HasPrivateChat(ctx, target, stranger); err != nil || !has {
		t.Fatalf("the target's chat with the stranger: %v, %v", has, err)
	}
	w.send(stranger, mtproto.PEER_USER, target, "Hi back")

	// Without Premium the setting does not work.
	other := newUser()
	w.users.mu.Lock()
	w.users.premium[target] = false
	w.users.mu.Unlock()
	w.send(other, mtproto.PEER_USER, target, "Hi")
}
