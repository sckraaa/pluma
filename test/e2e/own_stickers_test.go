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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

type stickerSetResult interface {
	To_Messages_StickerSet() *mtproto.Messages_StickerSet
}

// uploadStickerFile uploads data as the clients upload a sticker they made
// (messages.uploadMedia of a document with documentAttributeSticker of no
// set) and returns the document to make a sticker of.
func uploadStickerFile(ctx context.Context, t *testing.T, u *user, data []byte, mime, name string) *mtproto.InputDocument {
	t.Helper()
	fileId := randomID()
	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
		FileId:      fileId,
		Bytes:       data,
	}); err != nil {
		t.Fatalf("upload.saveFilePart: %v", err)
	}
	sum := md5.Sum(data)
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesUploadMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_uploadMedia),
		Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: name, Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
			MimeType: mime,
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
					Alt:        "😺",
					Stickerset: mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet(),
				}).To_DocumentAttribute(),
			},
		}).To_InputMedia(),
	})
	if err != nil {
		t.Fatalf("messages.uploadMedia: %v", err)
	}
	doc := o.(interface {
		To_MessageMedia() *mtproto.MessageMedia
	}).To_MessageMedia().GetDocument()
	if doc.GetId() == 0 {
		t.Fatalf("messages.uploadMedia made no document: %v", o)
	}
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: doc.GetId(), AccessHash: doc.GetAccessHash(), FileReference: doc.GetFileReference()}).To_InputDocument()
}

func stickerSetOf(t *testing.T, method string, o mtproto.TLObject, err error) *mtproto.Messages_StickerSet {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return o.(stickerSetResult).To_Messages_StickerSet()
}

func fileOf(ctx context.Context, t *testing.T, u *user, location *mtproto.InputFileLocation) []byte {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
		Location:    location,
		Limit:       1 << 20,
	})
	if err != nil {
		t.Fatalf("upload.getFile: %v", err)
	}
	return o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes()
}

// A user's own sticker set (task 084): made of an uploaded file, installed
// for its maker at once on every device; opened by its link and installed
// by another account, which is told when the set changes and loses it when
// it is deleted; changed by its maker alone; found by the search of sets
// and of stickers; with a thumbnail of its own, and behind the stickers
// put on a photo.
func TestOwnStickerSet(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	webp := stickerfile.SampleWebp(512, 512)
	first := uploadStickerFile(ctx, t, alice, webp, "image/webp", "sticker.webp")
	word := fmt.Sprintf("Zebrafinch%d", randomID()&0xffffff)
	name := "e2e_" + word
	self := mtproto.MakeTLInputUserSelf(nil).To_InputUser()
	shortName := func(n string) *mtproto.InputStickerSet {
		return mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: n}).To_InputStickerSet()
	}

	invokeTrue(ctx, t, alice, "stickers.checkShortName", &mtproto.TLStickersCheckShortName{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_checkShortName), ShortName: name,
	})
	o, err := alice.c.Invoke(ctx, &mtproto.TLStickersSuggestShortName{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_suggestShortName), Title: word + " stickers",
	})
	if err != nil {
		t.Fatalf("stickers.suggestShortName: %v", err)
	}
	if got := o.(interface {
		To_Stickers_SuggestedShortName() *mtproto.Stickers_SuggestedShortName
	}).To_Stickers_SuggestedShortName().GetShortName(); got != word+"_stickers" {
		t.Errorf("stickers.suggestShortName: %q", got)
	}
	// A file that is no sticker makes no set.
	small := uploadStickerFile(ctx, t, alice, stickerfile.SampleWebp(64, 64), "image/webp", "sticker.webp")
	if _, err := alice.c.Invoke(ctx, &mtproto.TLStickersCreateStickerSet{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_createStickerSet),
		UserId:      self, Title: "Small", ShortName: name,
		Stickers: []*mtproto.InputStickerSetItem{mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{Document: small, Emoji: "😺"}).To_InputStickerSetItem()},
	}); !mtclient.IsRPCError(err, "STICKER_PNG_DIMENSIONS") {
		t.Errorf("stickers.createStickerSet of a 64x64 image: %v", err)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLStickersCreateStickerSet{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_createStickerSet),
		UserId:      self, Title: word + " pack", ShortName: name,
		Stickers: []*mtproto.InputStickerSetItem{mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{
			Document: first, Emoji: "😺", Keywords: mtproto.MakeFlagsString(word + "word"),
		}).To_InputStickerSetItem()},
	})
	set := stickerSetOf(t, "stickers.createStickerSet", o, err)
	if !set.GetSet().GetCreator() || set.GetSet().GetInstalledDate() == nil || set.GetSet().GetShortName() != name || set.GetSet().GetCount() != 1 || len(set.GetDocuments()) != 1 {
		t.Fatalf("the set made: %v", set.GetSet())
	}
	sticker := set.GetDocuments()[0]
	if sticker.GetId() == first.GetId() || sticker.GetMimeType() != "image/webp" {
		t.Fatalf("the sticker of the set: %v", sticker)
	}
	if got := fileOf(ctx, t, bob, mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
		Id: sticker.GetId(), AccessHash: sticker.GetAccessHash(), FileReference: sticker.GetFileReference(),
	}).To_InputFileLocation()); !bytes.Equal(got, webp) {
		t.Errorf("the file of the sticker: %d bytes, want the %d uploaded", len(got), len(webp))
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewStickerSet, func(u *mtproto.Update) bool {
		return u.GetStickerset_MESSAGES_STICKERSET().GetSet().GetId() == set.GetSet().GetId()
	})
	invokeFails := func(u *user, method string, req mtproto.TLObject, want string) {
		t.Helper()
		if _, err := u.c.Invoke(ctx, req); !mtclient.IsRPCError(err, want) {
			t.Errorf("%s: %v, want %s", method, err, want)
		}
	}
	invokeFails(alice, "stickers.checkShortName of the set's name", &mtproto.TLStickersCheckShortName{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_checkShortName), ShortName: name,
	}, "SHORT_NAME_OCCUPIED")

	// Bob opens it by its link and installs it; he may not change it.
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetStickerSet{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getStickerSet), Stickerset: shortName(name),
	})
	bobs := stickerSetOf(t, "messages.getStickerSet", o, err)
	if bobs.GetSet().GetCreator() || bobs.GetSet().GetInstalledDate() != nil || len(bobs.GetDocuments()) != 1 {
		t.Fatalf("the set as another account gets it: %v", bobs.GetSet())
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesInstallStickerSet{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_installStickerSet), Stickerset: shortName(name), Archived: mtproto.BoolFalse,
	}); err != nil {
		t.Fatalf("messages.installStickerSet: %v", err)
	}
	second := uploadStickerFile(ctx, t, bob, stickerfile.SampleWebp(512, 256), "image/webp", "sticker.webp")
	item := func(doc *mtproto.InputDocument, emoji string) *mtproto.InputStickerSetItem {
		return mtproto.MakeTLInputStickerSetItem(&mtproto.InputStickerSetItem{Document: doc, Emoji: emoji}).To_InputStickerSetItem()
	}
	invokeFails(bob, "stickers.addStickerToSet by another account", &mtproto.TLStickersAddStickerToSet{
		Constructor: bob.c.Constructor(mtproto.Predicate_stickers_addStickerToSet), Stickerset: shortName(name), Sticker: item(second, "🐶"),
	}, "STICKERSET_INVALID")

	// Alice adds a sticker: Bob is told, and finds the set's new hash.
	second = uploadStickerFile(ctx, t, alice, stickerfile.SampleWebp(512, 256), "image/webp", "sticker.webp")
	o, err = alice.c.Invoke(ctx, &mtproto.TLStickersAddStickerToSet{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_addStickerToSet), Stickerset: shortName(name), Sticker: item(second, "🐶"),
	})
	added := stickerSetOf(t, "stickers.addStickerToSet", o, err)
	if added.GetSet().GetCount() != 2 || added.GetSet().GetHash() == set.GetSet().GetHash() || len(added.GetPacks()) != 2 {
		t.Fatalf("the set after stickers.addStickerToSet: %v", added.GetSet())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateStickerSets, func(u *mtproto.Update) bool { return !u.GetMasks() && !u.GetEmojis() })
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetAllStickers{Constructor: bob.c.Constructor(mtproto.Predicate_messages_getAllStickers)})
	if err != nil {
		t.Fatalf("messages.getAllStickers: %v", err)
	}
	all := o.(interface {
		To_Messages_AllStickers() *mtproto.Messages_AllStickers
	}).To_Messages_AllStickers()
	if at := slices.IndexFunc(all.GetSets(), func(s *mtproto.StickerSet) bool { return s.GetId() == set.GetSet().GetId() }); at < 0 ||
		all.GetSets()[at].GetHash() != added.GetSet().GetHash() || all.GetSets()[at].GetCreator() || all.GetSets()[at].GetCount() != 2 {
		t.Errorf("the changed set among Bob's sets: %v", all.GetSets())
	}

	// Order and title.
	o, err = alice.c.Invoke(ctx, &mtproto.TLStickersChangeStickerPosition{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_changeStickerPosition),
		Sticker:     mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: added.GetDocuments()[1].GetId(), AccessHash: added.GetDocuments()[1].GetAccessHash()}).To_InputDocument(),
		Position:    0,
	})
	if moved := stickerSetOf(t, "stickers.changeStickerPosition", o, err); moved.GetDocuments()[0].GetId() != added.GetDocuments()[1].GetId() {
		t.Errorf("stickers.changeStickerPosition left the order")
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLStickersRenameStickerSet{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_renameStickerSet), Stickerset: shortName(name), Title: word + " renamed",
	})
	if renamed := stickerSetOf(t, "stickers.renameStickerSet", o, err); renamed.GetSet().GetTitle() != word+" renamed" {
		t.Errorf("stickers.renameStickerSet: %q", renamed.GetSet().GetTitle())
	}

	// The account's sets.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMyStickers{Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMyStickers), Limit: 100})
	if err != nil {
		t.Fatalf("messages.getMyStickers: %v", err)
	}
	mine := o.(interface {
		To_Messages_MyStickers() *mtproto.Messages_MyStickers
	}).To_Messages_MyStickers()
	if mine.GetCount() != 1 || len(mine.GetSets()) != 1 || mine.GetSets()[0].GetSet().GetId() != set.GetSet().GetId() || mine.GetSets()[0].GetCover() == nil {
		t.Errorf("messages.getMyStickers: %v", mine)
	}

	// Search: the set by a word of its title, its stickers by a keyword.
	found := func() bool {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSearchStickerSets{Constructor: bob.c.Constructor(mtproto.Predicate_messages_searchStickerSets), Q: word})
		if err != nil {
			t.Fatalf("messages.searchStickerSets: %v", err)
		}
		sets := o.(interface {
			To_Messages_FoundStickerSets() *mtproto.Messages_FoundStickerSets
		}).To_Messages_FoundStickerSets().GetSets()
		return len(sets) == 1 && sets[0].GetSet().GetId() == set.GetSet().GetId() && sets[0].GetSet().GetTitle() == word+" renamed" && len(sets[0].GetCovers()) == 2
	}
	// The index follows the stored sets within a few seconds.
	for deadline := time.Now().Add(15 * time.Second); !found(); time.Sleep(500 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("messages.searchStickerSets does not find the set by %q", word)
		}
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSearchStickers{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_searchStickers), Q: word + "wo", LangCode: []string{"en"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("messages.searchStickers: %v", err)
	}
	if got := o.(interface {
		To_Messages_FoundStickers() *mtproto.Messages_FoundStickers
	}).To_Messages_FoundStickers(); len(got.GetStickers()) != 1 || got.GetStickers()[0].GetId() != sticker.GetId() {
		t.Errorf("messages.searchStickers by a keyword: %v", got)
	}

	// The set's own thumbnail is served for inputStickerSetThumb.
	icon := stickerfile.SampleWebp(100, 100)
	o, err = alice.c.Invoke(ctx, &mtproto.TLStickersSetStickerSetThumb{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_setStickerSetThumb), Stickerset: shortName(name),
		Thumb: uploadStickerFile(ctx, t, alice, icon, "image/webp", "icon.webp"),
	})
	thumbed := stickerSetOf(t, "stickers.setStickerSetThumb", o, err)
	if len(thumbed.GetSet().GetThumbs()) != 1 || thumbed.GetSet().GetThumbVersion() == nil {
		t.Fatalf("the set after stickers.setStickerSetThumb: %v", thumbed.GetSet())
	}
	if got := fileOf(ctx, t, bob, mtproto.MakeTLInputStickerSetThumb(&mtproto.InputFileLocation{
		Stickerset:   mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: set.GetSet().GetId(), AccessHash: set.GetSet().GetAccessHash()}).To_InputStickerSet(),
		ThumbVersion: thumbed.GetSet().GetThumbVersion().GetValue(),
	}).To_InputFileLocation()); !bytes.Equal(got, icon) {
		t.Errorf("the file of the set's thumbnail: %d bytes, want %d", len(got), len(icon))
	}

	// A photo with the sticker on it tells so, and names the set.
	photo := uploadPhoto(ctx, t, alice)
	photo.Stickers = []*mtproto.InputDocument{mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: sticker.GetId(), AccessHash: sticker.GetAccessHash()}).To_InputDocument()}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesUploadMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_uploadMedia),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Media:       photo,
	})
	if err != nil {
		t.Fatalf("messages.uploadMedia of a photo with a sticker: %v", err)
	}
	made := o.(interface {
		To_MessageMedia() *mtproto.MessageMedia
	}).To_MessageMedia().GetPhoto_FLAGPHOTO()
	if !made.GetHasStickers() {
		t.Errorf("a photo uploaded with a sticker has no has_stickers")
	}
	raw, err := bob.c.InvokeRaw(ctx, &mtproto.TLMessagesGetAttachedStickers{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getAttachedStickers),
		Media: mtproto.MakeTLInputStickeredMediaPhoto(&mtproto.InputStickeredMedia{
			Id_INPUTPHOTO: mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: made.GetId(), AccessHash: made.GetAccessHash(), FileReference: made.GetFileReference()}).To_InputPhoto(),
		}).To_InputStickeredMedia(),
	})
	if err != nil {
		t.Fatalf("messages.getAttachedStickers: %v", err)
	}
	if sets, err := mtclient.DecodeVector(raw); err != nil || len(sets) != 1 || sets[0].(interface {
		To_StickerSetCovered() *mtproto.StickerSetCovered
	}).To_StickerSetCovered().GetSet().GetId() != set.GetSet().GetId() {
		t.Errorf("messages.getAttachedStickers: %v, %v", sets, err)
	}

	// Deleted, the set leaves Bob's sets and its link.
	invokeTrue(ctx, t, alice, "stickers.deleteStickerSet", &mtproto.TLStickersDeleteStickerSet{
		Constructor: alice.c.Constructor(mtproto.Predicate_stickers_deleteStickerSet), Stickerset: shortName(name),
	})
	invokeFails(bob, "messages.getStickerSet of a deleted set", &mtproto.TLMessagesGetStickerSet{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getStickerSet), Stickerset: shortName(name),
	}, "STICKERSET_INVALID")
	deadline := time.Now().Add(15 * time.Second)
	for {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetAllStickers{Constructor: bob.c.Constructor(mtproto.Predicate_messages_getAllStickers)})
		if err != nil {
			t.Fatalf("messages.getAllStickers: %v", err)
		}
		sets := o.(interface {
			To_Messages_AllStickers() *mtproto.Messages_AllStickers
		}).To_Messages_AllStickers().GetSets()
		if !slices.ContainsFunc(sets, func(s *mtproto.StickerSet) bool { return s.GetId() == set.GetSet().GetId() }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a deleted set stays among Bob's sets")
		}
		time.Sleep(300 * time.Millisecond)
	}
}
