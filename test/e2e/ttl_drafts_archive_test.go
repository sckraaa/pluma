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

func setHistoryTTL(ctx context.Context, u *user, peer *mtproto.InputPeer, period int32) (*mtproto.Updates, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSetHistoryTTL{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_setHistoryTTL),
		Peer:        peer,
		Period:      period,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

func waitPeerTTL(ctx context.Context, t *testing.T, u *user, period int32) {
	t.Helper()
	waitUpdate(ctx, t, u, mtproto.Predicate_updatePeerHistoryTTL, func(x *mtproto.Update) bool {
		return x.GetTtlPeriod().GetValue() == period
	})
}

func waitDeleted(ctx context.Context, t *testing.T, u *user, id int32) {
	t.Helper()
	waitUpdate(ctx, t, u, mtproto.Predicate_updateDeleteMessages, func(x *mtproto.Update) bool {
		for _, m := range x.GetMessages() {
			if m == id {
				return true
			}
		}
		return false
	})
}

// The auto-delete period of a private chat (task 063): both sides learn it,
// a later message carries it and goes for both, with updateDeleteMessages,
// by the worker of the server.
func TestHistoryTTLPrivate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, laptop)
	getState(ctx, t, bob)
	toBob := inputPeerOf(ctx, t, alice, bob)

	if _, err := setHistoryTTL(ctx, alice, toBob, 30); !mtclient.IsRPCError(err, "TTL_PERIOD_INVALID") {
		t.Fatalf("a period below a minute: %v", err)
	}
	r, err := setHistoryTTL(ctx, alice, toBob, 60)
	if err != nil {
		t.Fatalf("messages.setHistoryTTL: %v", err)
	}
	var service, peerTTL bool
	for _, u := range r.GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			service = u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionSetMessagesTTL
		case mtproto.Predicate_updatePeerHistoryTTL:
			peerTTL = u.GetTtlPeriod().GetValue() == 60
		}
	}
	if !service || !peerTTL {
		t.Fatalf("answer %v", r.GetUpdates())
	}
	waitPeerTTL(ctx, t, laptop, 60)
	waitPeerTTL(ctx, t, bob, 60)
	if _, err = setHistoryTTL(ctx, alice, toBob, 60); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Fatalf("the same period: %v", err)
	}
	d := peerDialogs(ctx, t, alice, toBob).GetDialogs()
	if len(d) == 0 || d[0].GetTtlPeriod().GetValue() != 60 {
		t.Errorf("dialog %v", d)
	}

	id := sendTo(ctx, t, alice, toBob, "self-destruct")
	bobId := waitMessageId(ctx, t, bob, "self-destruct")
	waitDeleted(ctx, t, alice, id)
	waitDeleted(ctx, t, bob, bobId)
}

// The period of a channel: the admin sets it, the service message and the
// update reach the members; a member may not.
func TestHistoryTTLChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	_, ch := createChannel(ctx, t, alice, "ttl", true)
	inv := channelInviteHash(ctx, t, alice, &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	})
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        inv,
	}); err != nil {
		t.Fatalf("join: %v", err)
	}
	peerFor := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{
			Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		}
	}

	if _, err := setHistoryTTL(ctx, bob, peerFor(bob), 86400); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Fatalf("a member's change: %v", err)
	}
	r, err := setHistoryTTL(ctx, alice, peerFor(alice), 86400)
	if err != nil {
		t.Fatalf("setHistoryTTL: %v", err)
	}
	var service bool
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage &&
			u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionSetMessagesTTL {
			service = true
		}
	}
	if !service {
		t.Fatalf("answer %v", r.GetUpdates())
	}
	waitPeerTTL(ctx, t, bob, 86400)

	sent, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerFor(alice),
		Message:     "posted",
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	var ttl int32
	for _, u := range sent.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			ttl = u.GetMessage_MESSAGE().GetTtlPeriod().GetValue()
		}
	}
	if ttl != 86400 {
		t.Errorf("ttl_period of the post: %d", ttl)
	}
}

func saveDraft(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSaveDraft{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_saveDraft),
		Peer:        peer,
		Message:     text,
	}); err != nil {
		t.Fatalf("messages.saveDraft: %v", err)
	}
}

func allDrafts(ctx context.Context, t *testing.T, u *user) *mtproto.Updates {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetAllDrafts{Constructor: u.c.Constructor(mtproto.Predicate_messages_getAllDrafts)})
	if err != nil {
		t.Fatalf("messages.getAllDrafts: %v", err)
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
}

// Drafts (task 063): the other devices get them at once, getAllDrafts gives
// them, a chat without messages included, a channel's too, and sending with
// clear_draft clears them everywhere.
func TestDraftsSync(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	toBob := inputPeerOf(ctx, t, alice, bob)
	_, ch := createChannel(ctx, t, alice, "drafts", false)
	toChannel := &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	}

	// A chat that has no messages.
	saveDraft(ctx, t, alice, toBob, "to bob")
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDraftMessage, func(x *mtproto.Update) bool {
		return x.GetDraft().GetMessage() == "to bob" && x.GetPeer_PEER().GetUserId() == bob.self.GetId()
	})
	saveDraft(ctx, t, alice, toChannel, "to channel")
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDraftMessage, func(x *mtproto.Update) bool {
		return x.GetDraft().GetMessage() == "to channel" && x.GetPeer_PEER().GetChannelId() == ch.GetId()
	})

	found := map[string]bool{}
	for _, u := range allDrafts(ctx, t, laptop).GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateDraftMessage {
			found[u.GetDraft().GetMessage()] = true
		}
	}
	if !found["to bob"] || !found["to channel"] {
		t.Fatalf("getAllDrafts: %v", found)
	}

	// Sent with clear_draft: the other device learns it is gone.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		ClearDraft:  true,
		Peer:        toBob,
		Message:     "sent",
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDraftMessage, func(x *mtproto.Update) bool {
		return x.GetDraft().GetPredicateName() == mtproto.Predicate_draftMessageEmpty && x.GetPeer_PEER().GetUserId() == bob.self.GetId()
	})
	for _, u := range allDrafts(ctx, t, laptop).GetUpdates() {
		if u.GetDraft().GetMessage() == "to bob" {
			t.Error("the draft outlived its message")
		}
	}

	// clearAllDrafts empties the channel's draft too.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesClearAllDrafts{Constructor: alice.c.Constructor(mtproto.Predicate_messages_clearAllDrafts)}); err != nil {
		t.Fatalf("messages.clearAllDrafts: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDraftMessage, func(x *mtproto.Update) bool {
		return x.GetDraft().GetPredicateName() == mtproto.Predicate_draftMessageEmpty && x.GetPeer_PEER().GetChannelId() == ch.GetId()
	})
	if us := allDrafts(ctx, t, laptop).GetUpdates(); len(us) != 0 {
		t.Errorf("drafts left: %v", us)
	}
}

// The archive with channels and its entry in the main list: a channel moves
// into it with updateFolderPeers, the pinned dialogs give the dialogFolder
// with the channel's unread messages, folders.deleteFolder empties it.
func TestArchiveFolderChannels(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	getState(ctx, t, laptop)
	_, ch := createChannel(ctx, t, alice, "archived", false)
	toChannel := &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
	}

	r := editPeerFolder(ctx, t, alice, toChannel, 1)
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateFolderPeers {
		t.Fatalf("editPeerFolders: %v", r.GetUpdates())
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateFolderPeers, func(x *mtproto.Update) bool {
		for _, fp := range x.GetFolderPeers() {
			if fp.GetPeer().GetChannelId() == ch.GetId() && fp.GetFolderId() == 1 {
				return true
			}
		}
		return false
	})

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetPinnedDialogs{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getPinnedDialogs),
		FolderId:    0,
	})
	if err != nil {
		t.Fatalf("messages.getPinnedDialogs: %v", err)
	}
	var folder *mtproto.Dialog
	for _, d := range o.(interface {
		To_Messages_PeerDialogs() *mtproto.Messages_PeerDialogs
	}).To_Messages_PeerDialogs().GetDialogs() {
		if d.GetPredicateName() == mtproto.Predicate_dialogFolder {
			folder = d
		}
	}
	if folder == nil || folder.GetPeer().GetChannelId() != ch.GetId() || folder.GetTopMessage() == 0 {
		t.Fatalf("no dialogFolder for the channel: %v", folder)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLFoldersDeleteFolder{
		Constructor: alice.c.Constructor(mtproto.Predicate_folders_deleteFolder),
		FolderId:    1,
	})
	if err != nil {
		t.Fatalf("folders.deleteFolder: %v", err)
	}
	if us := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates(); len(us) != 1 || us[0].GetPredicateName() != mtproto.Predicate_updateFolderPeers {
		t.Fatalf("folders.deleteFolder: %v", us)
	}
}
