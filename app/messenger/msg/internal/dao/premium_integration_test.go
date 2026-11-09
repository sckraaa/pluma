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
// write: everybody has Premium (ADR 0041), so only a bot, the one account
// without it, is refused.
func TestSendToPremiumOnly(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	target, stranger, bot := newUser(), newUser(), newUser()
	w.users.mu.Lock()
	w.users.premiumOnly[target] = true
	w.users.bots[bot] = true
	w.users.mu.Unlock()

	w.send(stranger, mtproto.PEER_USER, target, "Hi")
	_, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{
		UserId: bot, AuthKeyId: bot * 10, PeerType: mtproto.PEER_USER, PeerId: target,
		Message: []*msg.OutboxMessage{outbox(textMessage(bot, mtproto.MakePeerUser(target), "Hi"))},
	})
	if !errors.Is(err, premium.ErrPrivacyPremiumRequired) {
		t.Fatalf("a bot: %v, want PRIVACY_PREMIUM_REQUIRED", err)
	}
	if w.hasDialog(target, mtproto.PEER_USER, bot) || w.hasDialog(bot, mtproto.PEER_USER, target) {
		t.Fatal("a refused message made a dialog")
	}
}
