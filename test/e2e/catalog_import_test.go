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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Telegram's ids in the collection; none may come back.
const (
	tgSet     = int64(1258816259751998)
	tgSticker = int64(5368324170671202286)

	tgPremiumSet     = int64(1258816259751999)
	tgPremiumSticker = int64(5368324170671202287)
)

// premiumSetShortName names the set of the collection with a premium
// sticker; premiumEffect is that sticker's effect file.
const premiumSetShortName = "PremiumStickers"

var premiumEffect = bytes.Repeat([]byte("tgs-effect-"), 50)

// catalogImport imports a collection of one set with one sticker, a
// reaction drawn with it and an emoji list naming it, as tweb gives them,
// then reads them back: every id is this server's, and the sticker's file
// and thumbnail download (dfs.importDocument stored them where
// upload.getFile reads them).
func catalogImport(ctx context.Context, t *testing.T, u *user) {
	t.Helper()
	sticker, thumb := bytes.Repeat([]byte("tgs-"), 700), []byte("webp-thumbnail")
	dir := writeE2ECollection(t, sticker, thumb)
	dst := fmt.Sprintf("/tmp/catalog-%d", randomID())
	if out, err := exec.Command(runScript, "compose", "cp", dir, "bff:"+dst).CombinedOutput(); err != nil {
		t.Fatalf("compose cp: %v\n%s", err, out)
	}
	t.Log(admin(t, "catalog", "import", dst))

	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetAvailableReactions{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAvailableReactions),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := o.(interface {
		To_Messages_AvailableReactions() *mtproto.Messages_AvailableReactions
	}).To_Messages_AvailableReactions()
	if len(r.GetReactions()) != 5 || r.GetReactions()[0].GetTitle() != "Grin" || r.GetHash() == 0 {
		t.Fatalf("reactions after the import: %v", r)
	}
	doc := r.GetReactions()[0].GetStaticIcon()
	if doc.GetId() == tgSticker || doc.GetId() == 0 || len(doc.GetAttributes()) == 0 {
		t.Fatalf("the reaction's document: %v", doc)
	}
	if set := doc.GetAttributes()[0].GetStickerset(); set.GetId() == tgSet || set.GetId() == 0 {
		t.Fatalf("the sticker's set: %v", set)
	}

	// Not modified for its own hash.
	o, err = u.c.Invoke(ctx, &mtproto.TLMessagesGetAvailableReactions{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAvailableReactions),
		Hash:        r.GetHash(),
	})
	if err != nil || o.(interface {
		To_Messages_AvailableReactions() *mtproto.Messages_AvailableReactions
	}).To_Messages_AvailableReactions().GetPredicateName() != mtproto.Predicate_messages_availableReactionsNotModified {
		t.Fatalf("with its hash: %v, %v", o, err)
	}

	// The set by kind, the same document.
	o, err = u.c.Invoke(ctx, &mtproto.TLMessagesGetStickerSet{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getStickerSet),
		Stickerset:  &mtproto.InputStickerSet{Constructor: u.c.Constructor(mtproto.Predicate_inputStickerSetAnimatedEmoji)},
	})
	if err != nil {
		t.Fatal(err)
	}
	set := o.(interface {
		To_Messages_StickerSet() *mtproto.Messages_StickerSet
	}).To_Messages_StickerSet()
	if set.GetSet().GetShortName() != "AnimatedEmojies" || len(set.GetDocuments()) != 1 || set.GetDocuments()[0].GetId() != doc.GetId() ||
		set.GetPacks()[0].GetDocuments()[0] != doc.GetId() {
		t.Fatalf("the set: %v", set)
	}

	// The emoji list names it, and it is given by id.
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLMessagesGetCustomEmojiDocuments{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getCustomEmojiDocuments),
		DocumentId:  []int64{doc.GetId()},
	})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].(interface{ To_Document() *mtproto.Document }).To_Document().GetId() != doc.GetId() {
		t.Fatalf("messages.getCustomEmojiDocuments: %v", docs)
	}

	for typ, want := range map[string][]byte{"": sticker, "m": thumb} {
		o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
			Location: &mtproto.InputFileLocation{
				Constructor:   u.c.Constructor(mtproto.Predicate_inputDocumentFileLocation),
				Id:            doc.GetId(),
				AccessHash:    doc.GetAccessHash(),
				FileReference: doc.GetFileReference(),
				ThumbSize:     typ,
			},
			Limit: 1 << 20,
		})
		if err != nil {
			t.Fatalf("upload.getFile %q: %v", typ, err)
		}
		f := o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File()
		if !bytes.Equal(f.GetBytes(), want) {
			t.Fatalf("upload.getFile %q: %d bytes, want %d", typ, len(f.GetBytes()), len(want))
		}
	}
}

func writeE2ECollection(t *testing.T, sticker, thumb []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	js := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		write(name, b)
	}
	doc := map[string]any{
		"_": "document", "pFlags": map[string]any{}, "id": fmt.Sprint(tgSticker), "access_hash": "-5", "file_reference": []any{1, 2},
		"date": 1700000000, "mime_type": "application/x-tgsticker", "size": len(sticker), "dc_id": 2,
		"thumbs": []any{
			map[string]any{"_": "photoPathSize", "type": "j", "bytes": []any{9, 8}},
			map[string]any{"_": "photoSize", "type": "m", "w": 128, "h": 128, "size": len(thumb)},
		},
		"attributes": []any{
			map[string]any{"_": "documentAttributeSticker", "pFlags": map[string]any{}, "alt": "😀",
				"stickerset": map[string]any{"_": "inputStickerSetID", "id": fmt.Sprint(tgSet), "access_hash": "8"}},
			map[string]any{"_": "documentAttributeFilename", "file_name": "AnimatedSticker.tgs"},
		},
	}
	// A premium sticker: its video_thumbs entry "f" is the effect played
	// full screen (Android MessageObject.isPremiumSticker, iOS
	// TelegramMediaFile.isPremiumSticker, tdesktop isPremiumSticker).
	premium := map[string]any{
		"_": "document", "pFlags": map[string]any{}, "id": fmt.Sprint(tgPremiumSticker), "access_hash": "-6", "file_reference": []any{3},
		"date": 1700000000, "mime_type": "application/x-tgsticker", "size": len(sticker), "dc_id": 2,
		"thumbs":       []any{map[string]any{"_": "photoSize", "type": "m", "w": 128, "h": 128, "size": len(thumb)}},
		"video_thumbs": []any{map[string]any{"_": "videoSize", "pFlags": map[string]any{}, "type": "f", "w": 512, "h": 512, "size": len(premiumEffect)}},
		"attributes": []any{
			map[string]any{"_": "documentAttributeSticker", "pFlags": map[string]any{}, "alt": "😎",
				"stickerset": map[string]any{"_": "inputStickerSetID", "id": fmt.Sprint(tgPremiumSet), "access_hash": "9"}},
			map[string]any{"_": "documentAttributeFilename", "file_name": "AnimatedSticker.tgs"},
		},
	}
	js("sets/premium.json", map[string]any{
		"_": "messages.stickerSet",
		"set": map[string]any{"_": "stickerSet", "pFlags": map[string]any{}, "id": fmt.Sprint(tgPremiumSet), "access_hash": "9", "title": "Premium",
			"short_name": premiumSetShortName, "count": 1, "hash": 2},
		"packs":     []any{map[string]any{"_": "stickerPack", "emoticon": "😎", "documents": []any{fmt.Sprint(tgPremiumSticker)}}},
		"keywords":  []any{},
		"documents": []any{premium},
	})
	write("files/premium", sticker)
	write("files/premium.m", thumb)
	write("files/premium.f", premiumEffect)
	js("sets/set.json", map[string]any{
		"_": "messages.stickerSet",
		"set": map[string]any{"_": "stickerSet", "pFlags": map[string]any{}, "id": fmt.Sprint(tgSet), "access_hash": "8", "title": "Animated",
			"short_name": "AnimatedEmojies", "count": 1, "hash": 1},
		"packs":     []any{map[string]any{"_": "stickerPack", "emoticon": "😀", "documents": []any{fmt.Sprint(tgSticker)}}},
		"keywords":  []any{},
		"documents": []any{doc},
	})
	// The reactions of the catalog, all drawn with the one document: the
	// reactions tests (task 053) send several of them, which the server
	// checks against the catalog when it has one.
	reactionsOf := func(pairs ...string) []any {
		var out []any
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, map[string]any{
				"_": "availableReaction", "pFlags": map[string]any{}, "reaction": pairs[i], "title": pairs[i+1],
				"static_icon": doc, "appear_animation": doc, "select_animation": doc, "activate_animation": doc, "effect_animation": doc,
			})
		}
		return out
	}
	js("answers/reactions.json", map[string]any{
		"_": "messages.availableReactions", "hash": 7,
		"reactions": reactionsOf("😀", "Grin", "👍", "Like", "🔥", "Fire", "❤", "Heart", "😂", "Joy"),
	})
	js("answers/profile-emojis.json", map[string]any{"_": "emojiList", "hash": 3, "document_id": []any{fmt.Sprint(tgSticker)}})
	write("files/doc", sticker)
	write("files/doc.m", thumb)
	js("manifest.json", map[string]any{
		"layer": 228, "client": "e2e", "collected_at": 1,
		"answers": []any{
			map[string]any{"method": "messages.getAvailableReactions", "arg": "", "file": "answers/reactions.json"},
			map[string]any{"method": "account.getDefaultProfilePhotoEmojis", "arg": "", "file": "answers/profile-emojis.json"},
		},
		"sets": []any{
			map[string]any{"file": "sets/set.json", "kind": "inputStickerSetAnimatedEmoji"},
			map[string]any{"file": "sets/premium.json"},
		},
		"files": []any{
			map[string]any{"id": fmt.Sprint(tgSticker), "file": "files/doc", "thumbs": map[string]any{"m": "files/doc.m"}},
			map[string]any{"id": fmt.Sprint(tgPremiumSticker), "file": "files/premium",
				"thumbs": map[string]any{"m": "files/premium.m", "f": "files/premium.f"}},
		},
	})
	return dir
}
