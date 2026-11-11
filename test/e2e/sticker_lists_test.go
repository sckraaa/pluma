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
	"crypto/md5"
	"encoding/hex"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Recent and faved stickers (task 079): a sticker sent with
// inputMediaDocument goes to the recent stickers of the account, as Android
// and Telegram Desktop expect; a faved one to the faved stickers; the other
// device gets updateRecentStickers and updateFavedStickers; each list
// answers not modified for its hash, and a sticker taken off or a list
// cleared is gone. A file that is no sticker cannot be faved.
func TestStickerLists(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	doc := uploadSticker(ctx, t, alice)
	input := &mtproto.InputDocument{
		Constructor:   alice.c.Constructor(mtproto.Predicate_inputDocument),
		Id:            doc.GetId(),
		AccessHash:    doc.GetAccessHash(),
		FileReference: doc.GetFileReference(),
	}

	// Sent again as a document, the sticker is a recent one.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Media:       mtproto.MakeTLInputMediaDocument(&mtproto.InputMedia{Id_INPUTDOCUMENT: input}).To_InputMedia(),
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMedia of the sticker: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateRecentStickers, func(*mtproto.Update) bool { return true })
	recent := recentStickers(ctx, t, laptop, false, 0)
	if recent.GetPredicateName() != mtproto.Predicate_messages_recentStickers || len(recent.GetStickers()) != 1 ||
		recent.GetStickers()[0].GetId() != doc.GetId() || len(recent.GetDates()) != 1 {
		t.Fatalf("messages.getRecentStickers: %s with %d stickers, %d dates", recent.GetPredicateName(), len(recent.GetStickers()), len(recent.GetDates()))
	}
	if again := recentStickers(ctx, t, laptop, false, recent.GetHash()); again.GetPredicateName() != mtproto.Predicate_messages_recentStickersNotModified {
		t.Errorf("messages.getRecentStickers with the hash of the list: %s", again.GetPredicateName())
	}
	if attached := recentStickers(ctx, t, laptop, true, 0); len(attached.GetStickers()) != 0 {
		t.Errorf("%d recent attached stickers, want none", len(attached.GetStickers()))
	}

	// Faved on one device, the other gets updateFavedStickers.
	invokeTrue(ctx, t, alice, "messages.faveSticker", &mtproto.TLMessagesFaveSticker{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_faveSticker),
		Id:          input,
		Unfave:      mtproto.BoolFalse,
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateFavedStickers, func(*mtproto.Update) bool { return true })
	faved := favedStickers(ctx, t, laptop, 0)
	if faved.GetPredicateName() != mtproto.Predicate_messages_favedStickers || len(faved.GetStickers()) != 1 || faved.GetStickers()[0].GetId() != doc.GetId() {
		t.Fatalf("messages.getFavedStickers: %s with %d stickers", faved.GetPredicateName(), len(faved.GetStickers()))
	}
	if len(faved.GetPacks()) != 1 || faved.GetPacks()[0].GetEmoticon() != "😺" {
		t.Errorf("packs of the faved stickers %v, want 😺", faved.GetPacks())
	}
	if again := favedStickers(ctx, t, laptop, faved.GetHash()); again.GetPredicateName() != mtproto.Predicate_messages_favedStickersNotModified {
		t.Errorf("messages.getFavedStickers with the hash of the list: %s", again.GetPredicateName())
	}

	gif := sendGif(ctx, t, alice)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesFaveSticker{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_faveSticker),
		Id:          &mtproto.InputDocument{Constructor: input.Constructor, Id: gif.GetId(), AccessHash: gif.GetAccessHash()},
		Unfave:      mtproto.BoolFalse,
	}); !mtclient.IsRPCError(err, "STICKER_ID_INVALID") {
		t.Errorf("messages.faveSticker of a GIF: %v", err)
	}

	invokeTrue(ctx, t, alice, "messages.faveSticker", &mtproto.TLMessagesFaveSticker{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_faveSticker),
		Id:          input,
		Unfave:      mtproto.BoolTrue,
	})
	if left := favedStickers(ctx, t, laptop, 0); len(left.GetStickers()) != 0 {
		t.Errorf("%d faved stickers after unfave", len(left.GetStickers()))
	}

	invokeTrue(ctx, t, laptop, "messages.clearRecentStickers", &mtproto.TLMessagesClearRecentStickers{
		Constructor: laptop.c.Constructor(mtproto.Predicate_messages_clearRecentStickers),
	})
	if left := recentStickers(ctx, t, alice, false, 0); len(left.GetStickers()) != 0 {
		t.Errorf("%d recent stickers after clear", len(left.GetStickers()))
	}

	// Saved on its own, as iOS and Web do on send, and taken off.
	invokeTrue(ctx, t, alice, "messages.saveRecentSticker", &mtproto.TLMessagesSaveRecentSticker{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_saveRecentSticker),
		Id:          input,
		Unsave:      mtproto.BoolFalse,
	})
	if got := recentStickers(ctx, t, alice, false, 0); len(got.GetStickers()) != 1 {
		t.Errorf("%d recent stickers after messages.saveRecentSticker", len(got.GetStickers()))
	}
	invokeTrue(ctx, t, alice, "messages.saveRecentSticker", &mtproto.TLMessagesSaveRecentSticker{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_saveRecentSticker),
		Id:          input,
		Unsave:      mtproto.BoolTrue,
	})
	if got := recentStickers(ctx, t, alice, false, 0); len(got.GetStickers()) != 0 {
		t.Errorf("%d recent stickers after unsave", len(got.GetStickers()))
	}
}

// uploadSticker uploads a sticker of no set, sends it to u's Saved Messages
// and returns its document.
func uploadSticker(ctx context.Context, t *testing.T, u *user) *mtproto.Document {
	t.Helper()
	// A RIFF WEBP header is enough: the server keeps the file as it is.
	data := []byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00")
	fileId := randomID()
	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
		FileId:      fileId,
		FilePart:    0,
		Bytes:       data,
	}); err != nil {
		t.Fatalf("upload.saveFilePart: %v", err)
	}
	sum := md5.Sum(data)
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: "sticker.webp", Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
			MimeType: "image/webp",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: 1, H: 1}).To_DocumentAttribute(),
				mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
					Alt:        "😺",
					Stickerset: mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet(),
				}).To_DocumentAttribute(),
				mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "sticker.webp"}).To_DocumentAttribute(),
			},
		}).To_InputMedia(),
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	up, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("messages.sendMedia returned %T", o)
	}
	for _, m := range newMessages(up.To_Updates()) {
		if d := m.GetMedia().GetDocument(); d.GetId() != 0 {
			return d
		}
	}
	t.Fatal("messages.sendMedia reply without the document")
	return nil
}

// invokeTrue invokes a method that answers boolTrue.
func invokeTrue(ctx context.Context, t *testing.T, u *user, method string, req mtproto.TLObject) {
	t.Helper()
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
		t.Fatalf("%s returned %v", method, o)
	}
}

func recentStickers(ctx context.Context, t *testing.T, u *user, attached bool, hash int64) *mtproto.Messages_RecentStickers {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetRecentStickers{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getRecentStickers),
		Attached:    attached,
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getRecentStickers: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_RecentStickers() *mtproto.Messages_RecentStickers
	})
	if !ok {
		t.Fatalf("messages.getRecentStickers returned %T", o)
	}
	return r.To_Messages_RecentStickers()
}

func favedStickers(ctx context.Context, t *testing.T, u *user, hash int64) *mtproto.Messages_FavedStickers {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetFavedStickers{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getFavedStickers),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getFavedStickers: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_FavedStickers() *mtproto.Messages_FavedStickers
	})
	if !ok {
		t.Fatalf("messages.getFavedStickers returned %T", o)
	}
	return r.To_Messages_FavedStickers()
}
