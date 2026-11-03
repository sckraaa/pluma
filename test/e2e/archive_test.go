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
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func inputPeerOf(ctx context.Context, t *testing.T, from, to *user) *mtproto.InputPeer {
	t.Helper()
	u := inputUserOf(ctx, t, from, to)
	return &mtproto.InputPeer{
		Constructor: from.c.Constructor(mtproto.Predicate_inputPeerUser),
		UserId:      u.GetUserId(),
		AccessHash:  u.GetAccessHash(),
	}
}

func editPeerFolder(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, folderId int32) *mtproto.Updates {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLFoldersEditPeerFolders{
		Constructor: u.c.Constructor(mtproto.Predicate_folders_editPeerFolders),
		FolderPeers: []*mtproto.InputFolderPeer{{
			Constructor: u.c.Constructor(mtproto.Predicate_inputFolderPeer),
			Peer:        peer,
			FolderId:    folderId,
		}},
	})
	if err != nil {
		t.Fatalf("folders.editPeerFolders: %v", err)
	}
	r, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("folders.editPeerFolders returned %T", o)
	}
	return r.To_Updates()
}

// peerFolder is the peer folder of u's dialog with peer.
func peerFolder(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
	t.Helper()
	d := peerDialogs(ctx, t, u, peer).GetDialogs()
	if len(d) == 0 {
		t.Fatal("no dialog")
	}
	return d[0].GetFolderId().GetValue()
}

// waitFolderPeers waits on u for updateFolderPeers moving userId to folderId.
func waitFolderPeers(ctx context.Context, t *testing.T, u *user, userId int64, folderId int32) {
	t.Helper()
	waitUpdate(ctx, t, u, mtproto.Predicate_updateFolderPeers, func(x *mtproto.Update) bool {
		for _, fp := range x.GetFolderPeers() {
			if fp.GetPeer().GetUserId() == userId && fp.GetFolderId() == folderId {
				return true
			}
		}
		return false
	})
}

func setArchiveSettings(ctx context.Context, t *testing.T, u *user, archiveNew, keepUnmuted bool) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountSetGlobalPrivacySettings{
		Constructor: u.c.Constructor(mtproto.Predicate_account_setGlobalPrivacySettings),
		Settings: &mtproto.GlobalPrivacySettings{
			Constructor: u.c.Constructor(mtproto.Predicate_globalPrivacySettings),
			ArchiveAndMuteNewNoncontactPeers_FLAGBOOLEAN: archiveNew,
			KeepArchivedUnmuted:                          keepUnmuted,
		},
	}); err != nil {
		t.Fatalf("account.setGlobalPrivacySettings: %v", err)
	}
}

// The archive (task 050): a dialog moves into it and back with one
// updateFolderPeers that the other devices get; a new message brings an
// archived, unmuted chat back unless the user keeps such chats archived.
func TestArchive(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toAlice := inputPeerOf(ctx, t, bob, alice)
	sendTo(ctx, t, bob, toAlice, "hi")
	toBob := inputPeerOf(ctx, t, alice, bob)
	getState(ctx, t, laptop)
	getState(ctx, t, alice)
	bobId := bob.self.GetId()

	r := editPeerFolder(ctx, t, alice, toBob, 1)
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateFolderPeers || r.GetUpdates()[0].GetPtsCount() != 1 {
		t.Fatalf("editPeerFolders answered %v, want one updateFolderPeers with one pts", r.GetUpdates())
	}
	waitFolderPeers(ctx, t, laptop, bobId, 1)
	if f := peerFolder(ctx, t, alice, toBob); f != 1 {
		t.Fatalf("folder %d after archiving, want 1", f)
	}
	if r := editPeerFolder(ctx, t, alice, toBob, 1); len(r.GetUpdates()) != 0 {
		t.Errorf("archiving again changed something: %v", r.GetUpdates())
	}

	// A new message brings the unmuted chat back.
	sendTo(ctx, t, bob, toAlice, "back")
	waitFolderPeers(ctx, t, laptop, bobId, 0)
	if f := peerFolder(ctx, t, alice, toBob); f != 0 {
		t.Fatalf("folder %d after a new message, want 0", f)
	}

	// Kept: unmuted chats stay archived.
	setArchiveSettings(ctx, t, alice, false, true)
	editPeerFolder(ctx, t, alice, toBob, 1)
	sendTo(ctx, t, bob, toAlice, "still archived")
	waitMessageId(ctx, t, alice, "still archived")
	if f := peerFolder(ctx, t, alice, toBob); f != 1 {
		t.Errorf("folder %d with keep_archived_unmuted, want 1", f)
	}

	// A muted chat stays archived without the setting too.
	setArchiveSettings(ctx, t, alice, false, false)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
		Peer: &mtproto.InputNotifyPeer{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputNotifyPeer),
			Peer:        toBob,
		},
		Settings: mtproto.MakeTLInputPeerNotifySettings(&mtproto.InputPeerNotifySettings{
			MuteUntil: mtproto.MakeFlagsInt32(int32(time.Now().Unix()) + 3600),
		}).To_InputPeerNotifySettings(),
	}); err != nil {
		t.Fatalf("account.updateNotifySettings: %v", err)
	}
	sendTo(ctx, t, bob, toAlice, "muted")
	waitMessageId(ctx, t, alice, "muted")
	if f := peerFolder(ctx, t, alice, toBob); f != 1 {
		t.Errorf("folder %d of a muted chat, want 1", f)
	}
}

// Archive and mute new chats from non-contacts: the first message of a user
// who is no contact puts the chat in the archive, muted, with the bar of an
// automatically archived chat, which goes when the user takes it out.
func TestArchiveNewNoncontacts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	carol := login(ctx, t, phones[1], "Carol") // no contact
	dan := login(ctx, t, phones[2], "Dan")     // a contact
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, phones[2], "Dan", ""))
	setArchiveSettings(ctx, t, alice, true, false)
	getState(ctx, t, alice)

	for _, u := range []*user{carol, dan} {
		text := fmt.Sprintf("first from %s %d", u.self.GetFirstName().GetValue(), randomID())
		sendTo(ctx, t, u, inputPeerOf(ctx, t, u, alice), text)
		waitMessageId(ctx, t, alice, text)
	}
	toCarol := inputPeerOf(ctx, t, alice, carol)
	if f := peerFolder(ctx, t, alice, toCarol); f != 1 {
		t.Errorf("a non-contact's new chat in folder %d, want 1", f)
	}
	if f := peerFolder(ctx, t, alice, inputPeerOf(ctx, t, alice, dan)); f != 0 {
		t.Errorf("a contact's new chat in folder %d, want 0", f)
	}
	notify := peerDialogs(ctx, t, alice, toCarol).GetDialogs()[0].GetNotifySettings()
	if notify.GetMuteUntil().GetValue() <= int32(time.Now().Unix()) {
		t.Errorf("the archived chat is not muted: %v", notify)
	}
	settings := func() *mtproto.PeerSettings {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetPeerSettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getPeerSettings),
			Peer:        toCarol,
		})
		if err != nil {
			t.Fatalf("messages.getPeerSettings: %v", err)
		}
		r, ok := o.(interface {
			To_Messages_PeerSettings() *mtproto.Messages_PeerSettings
		})
		if !ok {
			t.Fatalf("messages.getPeerSettings returned %T", o)
		}
		return r.To_Messages_PeerSettings().GetSettings()
	}
	if !settings().GetAutoarchived() {
		t.Error("no autoarchived bar")
	}
	editPeerFolder(ctx, t, alice, toCarol, 0)
	if settings().GetAutoarchived() {
		t.Error("the autoarchived bar stays after Unarchive")
	}
}
