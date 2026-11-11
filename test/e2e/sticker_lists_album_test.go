//go:build e2e

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

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Stickers sent in an album (messages.sendMultiMedia) are recent ones as a
// sticker of messages.sendMedia is (task 081), the last of the album on
// top, as Android and iOS order their own list by message.
func TestStickerListsAlbum(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	first, second := uploadSticker(ctx, t, alice), uploadSticker(ctx, t, alice)
	if got := recentStickers(ctx, t, alice, false, 0); len(got.GetStickers()) != 0 {
		t.Fatalf("%d recent stickers after uploads, want none", len(got.GetStickers()))
	}
	single := func(d *mtproto.Document) *mtproto.InputSingleMedia {
		return &mtproto.InputSingleMedia{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputSingleMedia),
			Media: mtproto.MakeTLInputMediaDocument(&mtproto.InputMedia{Id_INPUTDOCUMENT: &mtproto.InputDocument{
				Constructor:   alice.c.Constructor(mtproto.Predicate_inputDocument),
				Id:            d.GetId(),
				AccessHash:    d.GetAccessHash(),
				FileReference: d.GetFileReference(),
			}}).To_InputMedia(),
			RandomId: randomID(),
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMultiMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMultiMedia),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		MultiMedia:  []*mtproto.InputSingleMedia{single(first), single(second)},
	}); err != nil {
		t.Fatalf("messages.sendMultiMedia of two stickers: %v", err)
	}
	got := recentStickers(ctx, t, alice, false, 0).GetStickers()
	if len(got) != 2 || got[0].GetId() != second.GetId() || got[1].GetId() != first.GetId() {
		ids := make([]int64, 0, len(got))
		for _, d := range got {
			ids = append(ids, d.GetId())
		}
		t.Fatalf("recent stickers %v, want [%d %d]", ids, second.GetId(), first.GetId())
	}
}
