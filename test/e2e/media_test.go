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
	"image/jpeg"
	"maps"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Photos sent to a chat are in its "Photos and videos": the recipient's
// search counters and search find them. Android shows the media tab of a
// profile only when the photo and video counter is above 0
// (DrKLO/Telegram ui/Components/SharedMediaLayout.java:6884), task 020.
func TestSharedPhotos(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)

	sendPhoto(ctx, t, alice, toBob)
	sendPhoto(ctx, t, alice, toBob)

	// Bob's copy arrives with an inbox event after Alice's reply.
	want := map[string]int32{
		mtproto.Predicate_inputMessagesFilterPhotoVideo: 2,
		mtproto.Predicate_inputMessagesFilterPhotos:     2,
		mtproto.Predicate_inputMessagesFilterVideo:      0,
	}
	got := searchCounters(ctx, t, bob, toAlice)
	for deadline := time.Now().Add(30 * time.Second); got[mtproto.Predicate_inputMessagesFilterPhotoVideo] < 2 && time.Now().Before(deadline); got = searchCounters(ctx, t, bob, toAlice) {
		time.Sleep(200 * time.Millisecond)
	}
	if !maps.Equal(got, want) {
		t.Errorf("messages.getSearchCounters %v, want %v", got, want)
	}

	o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSearch{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_search),
		Peer:        toAlice,
		Filter:      mtproto.MakeTLInputMessagesFilterPhotoVideo(nil).To_MessagesFilter(),
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("messages.search: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.search returned %T", o)
	}
	// A page of one of the two photos: a slice with the count of all, by
	// which the media viewer pages.
	page := r.To_Messages_Messages()
	if page.GetPredicateName() != mtproto.Predicate_messages_messagesSlice || page.GetCount() != 2 || len(page.GetMessages()) != 1 {
		t.Fatalf("messages.search for photos and videos: %s, count %d, %d messages", page.GetPredicateName(), page.GetCount(), len(page.GetMessages()))
	}
	if p := page.GetMessages()[0].GetMedia().GetPredicateName(); p != mtproto.Predicate_messageMediaPhoto {
		t.Errorf("messages.search for photos and videos: media %s", p)
	}
}

// sendPhoto uploads a small photo and sends it from u to peer.
func sendPhoto(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) {
	t.Helper()
	pic := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := range 64 {
		for y := range 48 {
			pic.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 120, A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, pic, nil); err != nil {
		t.Fatal(err)
	}
	fileId := randomID()
	if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
		FileId:      fileId,
		FilePart:    0,
		Bytes:       jpg.Bytes(),
	}); err != nil {
		t.Fatalf("upload.saveFilePart: %v", err)
	}
	sum := md5.Sum(jpg.Bytes())
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peer,
		Media: mtproto.MakeTLInputMediaUploadedPhoto(&mtproto.InputMedia{
			File: mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: "photo.jpg", Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
		}).To_InputMedia(),
		RandomId: randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
}

// searchCounters are u's counters of the photo, video and photo and video
// filters of the dialog with peer, by the filter's predicate.
func searchCounters(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) map[string]int32 {
	t.Helper()
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLMessagesGetSearchCounters{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getSearchCounters),
		Peer:        peer,
		Filters: []*mtproto.MessagesFilter{
			mtproto.MakeTLInputMessagesFilterPhotoVideo(nil).To_MessagesFilter(),
			mtproto.MakeTLInputMessagesFilterPhotos(nil).To_MessagesFilter(),
			mtproto.MakeTLInputMessagesFilterVideo(nil).To_MessagesFilter(),
		},
	})
	if err != nil {
		t.Fatalf("messages.getSearchCounters: %v", err)
	}
	list, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int32{}
	for _, o := range list {
		c, ok := o.(interface {
			To_Messages_SearchCounter() *mtproto.Messages_SearchCounter
		})
		if !ok {
			t.Fatalf("messages.getSearchCounters returned %T", o)
		}
		out[c.To_Messages_SearchCounter().GetFilter().GetPredicateName()] = c.To_Messages_SearchCounter().GetCount()
	}
	return out
}
