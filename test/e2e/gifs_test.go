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
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Saved GIFs (task 020): a GIF saved on one device is in the list of the
// account, the other devices get updateSavedGifs with seq, the hash of the
// list answers savedGifsNotModified, and an unsaved GIF leaves the list.
// A document with another access hash cannot be saved.
func TestSavedGifs(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	doc := sendGif(ctx, t, alice)
	input := &mtproto.InputDocument{
		Constructor:   alice.c.Constructor(mtproto.Predicate_inputDocument),
		Id:            doc.GetId(),
		AccessHash:    doc.GetAccessHash(),
		FileReference: doc.GetFileReference(),
	}

	st := getState(ctx, t, alice)
	saveGif(ctx, t, alice, input, false)
	d := differenceUntil(ctx, t, alice, st, seqReached(st.GetSeq()+1))
	found := false
	for _, u := range d.GetOtherUpdates() {
		found = found || u.GetPredicateName() == mtproto.Predicate_updateSavedGifs
	}
	if !found {
		t.Errorf("the difference after messages.saveGif has no updateSavedGifs")
	}

	saved := savedGifs(ctx, t, alice, 0)
	if saved.GetPredicateName() != mtproto.Predicate_messages_savedGifs || len(saved.GetGifs()) != 1 || saved.GetGifs()[0].GetId() != doc.GetId() {
		t.Fatalf("messages.getSavedGifs: %s with %d GIFs", saved.GetPredicateName(), len(saved.GetGifs()))
	}
	if again := savedGifs(ctx, t, alice, saved.GetHash()); again.GetPredicateName() != mtproto.Predicate_messages_savedGifsNotModified {
		t.Errorf("messages.getSavedGifs with the hash of the list: %s", again.GetPredicateName())
	}

	forged := &mtproto.InputDocument{Constructor: input.Constructor, Id: input.Id, AccessHash: input.AccessHash + 1}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSaveGif{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_saveGif),
		Id:          forged,
		Unsave:      mtproto.BoolFalse,
	}); !mtclient.IsRPCError(err, "GIF_ID_INVALID") {
		t.Errorf("messages.saveGif with another access hash: %v", err)
	}

	saveGif(ctx, t, alice, input, true)
	if left := savedGifs(ctx, t, alice, 0); len(left.GetGifs()) != 0 {
		t.Errorf("%d saved GIFs after unsaving", len(left.GetGifs()))
	}
}

// sendGif uploads a GIF, sends it to u's Saved Messages and returns its
// document.
func sendGif(ctx context.Context, t *testing.T, u *user) *mtproto.Document {
	t.Helper()
	pic := image.NewPaletted(image.Rect(0, 0, 32, 32), []color.Color{color.White, color.Black})
	for i := range 32 {
		pic.SetColorIndex(i, i, 1)
	}
	var b bytes.Buffer
	if err := gif.Encode(&b, pic, nil); err != nil {
		t.Fatal(err)
	}
	fileId := randomID()
	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
		FileId:      fileId,
		FilePart:    0,
		Bytes:       b.Bytes(),
	}); err != nil {
		t.Fatalf("upload.saveFilePart: %v", err)
	}
	sum := md5.Sum(b.Bytes())
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: "loop.gif", Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
			MimeType: "image/gif",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "loop.gif"}).To_DocumentAttribute(),
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

func saveGif(ctx context.Context, t *testing.T, u *user, id *mtproto.InputDocument, unsave bool) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSaveGif{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_saveGif),
		Id:          id,
		Unsave:      mtproto.ToBool(unsave),
	}); err != nil {
		t.Fatalf("messages.saveGif (unsave %v): %v", unsave, err)
	}
}

func savedGifs(ctx context.Context, t *testing.T, u *user, hash int64) *mtproto.Messages_SavedGifs {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetSavedGifs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSavedGifs),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getSavedGifs: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_SavedGifs() *mtproto.Messages_SavedGifs
	})
	if !ok {
		t.Fatalf("messages.getSavedGifs returned %T", o)
	}
	return r.To_Messages_SavedGifs()
}
