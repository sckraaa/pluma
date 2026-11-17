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
	"crypto/md5"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// sendMusic sends a music file of u to peer and returns its document.
func sendMusic(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) *mtproto.Document {
	t.Helper()
	// An Ogg page header is enough: the server keeps the file as it is.
	data := []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x13OpusHead\x01\x01")
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
		Peer:        peer,
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: "song.ogg", Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
			MimeType: "audio/ogg",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Duration: 180, Title: mtproto.MakeFlagsString("Song"), Performer: mtproto.MakeFlagsString("Band")}).To_DocumentAttribute(),
			},
		}).To_InputMedia(),
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	for _, m := range newMessages(o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()) {
		if d := m.GetMedia().GetDocument(); d.GetPredicateName() == mtproto.Predicate_document {
			return d
		}
	}
	t.Fatal("messages.sendMedia reply without the document")
	return nil
}

// TestStartupNoiseMetrics: what the clients send on their own while chats
// are open, without showing anything (task 098, part D), is answered and
// never ERR_ENTERPRISE_IS_BLOCKED: read metrics of a channel and of a
// private chat and a music listen are true, receivedMessages is an empty
// vector; read metrics of a private channel of others are PEER_ID_INVALID.
// summarizeText is refused with 400; inputMessagePinned of
// channels.getMessages is the pinned post.
func TestStartupNoiseMetrics(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)

	_, ch := createChannel(ctx, t, alice, "metrics", false)
	chPeer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	post := sendMessageID(ctx, t, alice, chPeer(alice), "the post", randomID())
	toBob := userPeer(alice.c, bob)
	dm := sendMessageID(ctx, t, alice, toBob, "hello", randomID())

	metric := func(u *user, id int32, seen int32) *mtproto.InputMessageReadMetric {
		return &mtproto.InputMessageReadMetric{
			Constructor: u.c.Constructor(mtproto.Predicate_inputMessageReadMetric),
			MsgId:       id, ViewId: randomID(), TimeInViewMs: 2300, ActiveTimeInViewMs: 1100,
			HeightToViewportRatioPermille: 1400, // taller than the screen
			SeenRangeRatioPermille:        seen,
		}
	}
	report := func(u *user, peer *mtproto.InputPeer, ms ...*mtproto.InputMessageReadMetric) error {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesReportReadMetrics{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_reportReadMetrics),
			Peer:        peer,
			Metrics:     ms,
		})
		if err != nil {
			return err
		}
		if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
			t.Errorf("messages.reportReadMetrics returned %v", o)
		}
		return nil
	}
	m := metric(alice, post, 1000)
	if err := report(alice, chPeer(alice), m); err != nil {
		t.Errorf("read metrics of the channel: %v", err)
	}
	// The same showing again (a retried batch) is accepted as well.
	if err := report(alice, chPeer(alice), m); err != nil {
		t.Errorf("the same showing again: %v", err)
	}
	if err := report(alice, toBob, metric(alice, dm, 600)); err != nil {
		t.Errorf("read metrics of the private chat: %v", err)
	}
	if err := report(bob, chPeer(bob), metric(bob, post, 1000)); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("read metrics of a private channel of others: %v, want PEER_ID_INVALID", err)
	}
	if err := report(alice, chPeer(alice), metric(alice, post, 1001)); !mtclient.IsRPCError(err, "INPUT_REQUEST_INVALID") {
		t.Errorf("a seen range over the whole message: %v, want INPUT_REQUEST_INVALID", err)
	}

	doc := sendMusic(ctx, t, alice, toBob)
	listen := func(id, hash int64) error {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesReportMusicListen{
			Constructor:      alice.c.Constructor(mtproto.Predicate_messages_reportMusicListen),
			Id:               &mtproto.InputDocument{Constructor: alice.c.Constructor(mtproto.Predicate_inputDocument), Id: id, AccessHash: hash, FileReference: []byte{}},
			ListenedDuration: 95,
		})
		if err != nil {
			return err
		}
		if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
			t.Errorf("messages.reportMusicListen returned %v", o)
		}
		return nil
	}
	if err := listen(doc.GetId(), doc.GetAccessHash()); err != nil {
		t.Errorf("a music listen: %v", err)
	}
	if err := listen(doc.GetId(), doc.GetAccessHash()+1); !mtclient.IsRPCError(err, "DOCUMENT_INVALID") {
		t.Errorf("a listen with a wrong access hash: %v, want DOCUMENT_INVALID", err)
	}

	raw, err := alice.c.InvokeRaw(ctx, &mtproto.TLMessagesReceivedMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_receivedMessages),
		MaxId:       dm,
	})
	if err != nil {
		t.Errorf("messages.receivedMessages: %v", err)
	} else if list, err := mtclient.DecodeVector(raw); err != nil || len(list) != 0 {
		t.Errorf("messages.receivedMessages: %v, %v, want an empty vector", list, err)
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSummarizeText{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_summarizeText),
		Peer:        toBob,
		Id:          dm,
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("messages.summarizeText: %v, want a refusal", err)
	} else if e := (*mtclient.RPCError)(nil); !errors.As(err, &e) || e.Code != 400 {
		t.Errorf("messages.summarizeText: %v, want code 400", err)
	}

	// inputMessagePinned: the channel's pinned post; nothing in
	// messages.getMessages, which names no chat.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesUpdatePinnedMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updatePinnedMessage),
		Peer:        chPeer(alice), Id: post, Silent: true,
	}); err != nil {
		t.Fatalf("messages.updatePinnedMessage: %v", err)
	}
	pinned := &mtproto.InputMessage{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagePinned)}
	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetMessages{
		Constructor:           alice.c.Constructor(mtproto.Predicate_channels_getMessages),
		Channel:               &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()},
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{pinned},
	})
	if err != nil {
		t.Fatalf("channels.getMessages: %v", err)
	}
	got := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
	if len(got) != 1 || got[0].GetId() != post {
		t.Errorf("channels.getMessages inputMessagePinned: %v, want the post %d", got, post)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessages{
		Constructor:           alice.c.Constructor(mtproto.Predicate_messages_getMessages),
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{pinned},
	})
	if err != nil {
		t.Fatalf("messages.getMessages: %v", err)
	}
	if got := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(got) != 0 {
		t.Errorf("messages.getMessages inputMessagePinned: %v, want nothing", got)
	}
}
