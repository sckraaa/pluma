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
)

// sendVoice sends a voice message of u to peer and returns its id.
func sendVoice(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
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
			File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: 1, Name: "voice.ogg", Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile(),
			MimeType: "audio/ogg",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration: 2}).To_DocumentAttribute(),
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
	for _, x := range up.To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			m := x.GetMessage_MESSAGE()
			if !mtproto.IsVoiceMessage(m) {
				t.Fatalf("the message sent is no voice message: %v", m.GetMedia())
			}
			if m.GetMediaUnread() {
				t.Error("the author's own voice post is unread for it")
			}
			return m.GetId()
		}
	}
	t.Fatal("messages.sendMedia reply without the message")
	return 0
}

// historyMediaUnread tells whether message id is media_unread in u's
// history of peer.
func historyMediaUnread(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, id int32) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	h, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getHistory returned %T", o)
	}
	for _, m := range h.To_Messages_Messages().GetMessages() {
		if m.GetId() == id {
			return m.GetMediaUnread()
		}
	}
	t.Fatalf("history without message %d", id)
	return false
}

// TestChannelMediaUnread (task 080): the voice posts a subscriber finds
// when it joins a channel are unread for it, as are the new ones pushed;
// the author never sees its own unread; once the subscriber plays one,
// channels.readMessageContents makes it read for it alone, and its other
// device gets updateChannelReadMessagesContents.
func TestChannelMediaUnread(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobLaptop := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, ch := createChannel(ctx, t, alice, "voices", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	old := sendVoice(ctx, t, alice, peer(alice))
	inviteHash := channelInviteHash(ctx, t, alice, input(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        inviteHash,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	if !historyMediaUnread(ctx, t, bob, peer(bob), old) {
		t.Fatal("a voice post from before the join is played for the newcomer")
	}
	if historyMediaUnread(ctx, t, alice, peer(alice), old) {
		t.Error("the author sees its own voice post unread")
	}

	getState(ctx, t, bob)
	getState(ctx, t, bobLaptop)
	fresh := sendVoice(ctx, t, alice, peer(alice))
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetId() == fresh && u.GetMessage_MESSAGE().GetMediaUnread()
	})

	invokeTrue(ctx, t, bob, "channels.readMessageContents", &mtproto.TLChannelsReadMessageContents{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_readMessageContents),
		Channel:     input(bob),
		Id:          []int32{old},
	})
	waitUpdate(ctx, t, bobLaptop, mtproto.Predicate_updateChannelReadMessagesContents, func(u *mtproto.Update) bool {
		return u.GetChannelId() == ch.GetId() && len(u.GetMessages()) == 1 && u.GetMessages()[0] == old
	})
	if historyMediaUnread(ctx, t, bobLaptop, peer(bobLaptop), old) {
		t.Error("the played voice post is still unread")
	}
	if !historyMediaUnread(ctx, t, bob, peer(bob), fresh) {
		t.Error("the voice post bob did not play became played")
	}
	if !historyMediaUnread(ctx, t, carol, peer(carol), old) {
		t.Error("bob's play made the post played for carol")
	}
}
