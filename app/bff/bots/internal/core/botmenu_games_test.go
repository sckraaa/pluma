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
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

func menuCore(bot bool) *BotsCore {
	ctx := context.Background()
	return &BotsCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 5, IsBot: bot}}
}

func wantStatus(t *testing.T, what string, err error, want string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || s.Message() != want {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

// The attachment menu is empty, with hash 0; a client's hash never makes
// it attachMenuBotsNotModified, 0 included: that is the hash of no copy.
func TestGetAttachMenuBots(t *testing.T) {
	c := menuCore(false)
	for _, hash := range []int64{0, 1, -5, 1 << 40} {
		r, err := c.MessagesGetAttachMenuBots(&mtproto.TLMessagesGetAttachMenuBots{Hash: hash})
		if err != nil {
			t.Fatal(err)
		}
		if r.GetPredicateName() != mtproto.Predicate_attachMenuBots || r.GetHash() != 0 || len(r.GetBots()) != 0 || len(r.GetUsers()) != 0 {
			t.Errorf("hash %d: %v, want attachMenuBots with nothing and hash 0", hash, r)
		}
		// The answer encodes: empty vectors, not nil fields.
		x := mtproto.NewEncodeBuf(64)
		if err = r.Encode(x, 233); err != nil {
			t.Errorf("hash %d: encode: %v", hash, err)
		}
	}
	_, err := menuCore(true).MessagesGetAttachMenuBots(&mtproto.TLMessagesGetAttachMenuBots{})
	wantStatus(t, "bot", err, "BOT_METHOD_INVALID")
}

// The hash of a list of bots follows core.telegram.org/api/offsets: it
// depends on the ids and their order, and a list that has bots is
// attachMenuBotsNotModified for its own hash.
func TestAttachMenuBotsHash(t *testing.T) {
	bot := func(id int64) *mtproto.AttachMenuBot {
		return mtproto.MakeTLAttachMenuBot(&mtproto.AttachMenuBot{BotId: id}).To_AttachMenuBot()
	}
	if h := attachMenuBotsHash(nil); h != 0 {
		t.Errorf("empty list: %d, want 0", h)
	}
	// One step of the algorithm from 0 is the id itself.
	if h := attachMenuBotsHash([]*mtproto.AttachMenuBot{bot(42)}); h != 42 {
		t.Errorf("one bot: %d, want 42", h)
	}
	ab := attachMenuBotsHash([]*mtproto.AttachMenuBot{bot(1), bot(2)})
	ba := attachMenuBotsHash([]*mtproto.AttachMenuBot{bot(2), bot(1)})
	if ab == ba || ab == 0 {
		t.Errorf("order does not count: %d, %d", ab, ba)
	}
	var want uint64
	for _, id := range []uint64{1, 2} {
		want ^= want >> 21
		want ^= want << 35
		want ^= want >> 4
		want += id
	}
	if ab != int64(want) {
		t.Errorf("hash %d, want %d", ab, int64(want))
	}
}

func TestGetEmojiGameInfo(t *testing.T) {
	r, err := menuCore(false).MessagesGetEmojiGameInfo(&mtproto.TLMessagesGetEmojiGameInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_emojiGameUnavailable {
		t.Errorf("%s, want messages.emojiGameUnavailable", r.GetPredicateName())
	}
	_, err = menuCore(true).MessagesGetEmojiGameInfo(&mtproto.TLMessagesGetEmojiGameInfo{})
	wantStatus(t, "bot", err, "BOT_METHOD_INVALID")
}
