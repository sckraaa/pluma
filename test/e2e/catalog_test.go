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
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The services of the catalog (task 024) are routed to bff: before an
// import its methods answer empty lists with hash 0, never
// ERR_ENTERPRISE_IS_BLOCKED, which Android asks again for every second; a
// set it does not have is STICKERSET_INVALID; a method of those services it
// does not serve stays blocked. Then a collection is imported with
// pluma-admin catalog import, and the reaction, its set and its files come
// back with ids of this server (catalogImport). A stand used again holds
// the catalog of the run before: the checks of emptiness are skipped then.
func TestCatalog(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")

	call := func(name string, req mtproto.TLObject) mtproto.TLObject {
		t.Helper()
		o, err := u.c.Invoke(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return o
	}
	r := call("messages.getAvailableReactions", &mtproto.TLMessagesGetAvailableReactions{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAvailableReactions),
	}).(interface {
		To_Messages_AvailableReactions() *mtproto.Messages_AvailableReactions
	}).To_Messages_AvailableReactions()
	if r.GetPredicateName() != mtproto.Predicate_messages_availableReactions {
		t.Errorf("messages.getAvailableReactions: %v", r)
	}
	fresh := len(r.GetReactions()) == 0
	all := call("messages.getAllStickers", &mtproto.TLMessagesGetAllStickers{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAllStickers),
	}).(interface {
		To_Messages_AllStickers() *mtproto.Messages_AllStickers
	}).To_Messages_AllStickers()
	if all.GetPredicateName() != mtproto.Predicate_messages_allStickers || all.GetHash() != 0 {
		t.Errorf("messages.getAllStickers: %v", all)
	}
	colors := call("help.getPeerColors", &mtproto.TLHelpGetPeerColors{
		Constructor: u.c.Constructor(mtproto.Predicate_help_getPeerColors),
	}).(interface {
		To_Help_PeerColors() *mtproto.Help_PeerColors
	}).To_Help_PeerColors()
	if colors.GetPredicateName() != mtproto.Predicate_help_peerColors {
		t.Errorf("help.getPeerColors: %v", colors)
	}

	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetStickerSet{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getStickerSet),
		Stickerset:  &mtproto.InputStickerSet{Constructor: u.c.Constructor(mtproto.Predicate_inputStickerSetAnimatedEmoji)},
	}); fresh && !mtclient.IsRPCError(err, "STICKERSET_INVALID") {
		t.Errorf("messages.getStickerSet of an empty catalog: %v", err)
	}
	if _, err := u.c.Invoke(ctx, &mtproto.TLStickersCheckShortName{
		Constructor: u.c.Constructor(mtproto.Predicate_stickers_checkShortName),
		ShortName:   "pluma",
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("stickers.checkShortName: %v", err)
	}

	catalogImport(ctx, t, u)
}
