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

package dao

import (
	"context"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Peer folders (core.telegram.org/api/folders): only the archive, 1, and the
// main list, 0, exist.
const (
	FolderMain    = 0
	FolderArchive = 1
)

// FolderPeer is a peer whose dialog moves between peer folders.
type FolderPeer struct {
	PeerType int32
	PeerId   int64
}

// SetPeerFolders moves the box's dialogs with peers into the peer folder
// folderId (task 050) and journals one updateFolderPeers with a pts for each
// dialog moved, which it returns; nil when none moved (no such dialog, or
// it is there already). A dialog leaves its pin in the folder it leaves, as
// the clients unpin it on the update (Android applyFoldersUpdates, tdesktop
// History::setFolderPointer, tweb onUpdateFolderPeers).
func (d *Dao) SetPeerFolders(b *userbox.Box, peers []FolderPeer, folderId int32) (*mtproto.Update, error) {
	var moved []*mtproto.FolderPeer
	for _, p := range peers {
		peer := boxPeer(p)
		row, err := dialogRow(b, peer)
		if err != nil {
			return nil, err
		}
		if row == nil || row.FolderId == folderId {
			continue
		}
		if _, err = b.Tx().Exec("UPDATE dialogs SET folder_id = ?,"+
			" pinned = IF(? = 1, 0, pinned), folder_pinned = IF(? = 0, 0, folder_pinned)"+
			" WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
			folderId, folderId, folderId, b.UserId(), peer.PeerType, peer.PeerId); err != nil {
			return nil, err
		}
		b.InvalidateCache(dialog.DeleteCacheKeys(b.UserId(), peer.PeerType, peer.PeerId)...)
		moved = append(moved, mtproto.MakeTLFolderPeer(&mtproto.FolderPeer{
			Peer:     mtproto.MakePeer(peer.PeerType, peer.PeerId),
			FolderId: folderId,
		}).To_FolderPeer())
	}
	if len(moved) == 0 {
		return nil, nil
	}
	u := mtproto.MakeTLUpdateFolderPeers(&mtproto.Update{FolderPeers: moved}).To_Update()
	b.AddPtsUpdate(u, int32(len(moved)))
	return u, nil
}

// IncomingArchive is what a private message from fromId does to the
// recipient's archive (task 050).
type IncomingArchive struct {
	// New: the chat is new, from a user who is no contact, and the
	// recipient archives and mutes such chats
	// (globalPrivacySettings.archive_and_mute_new_noncontact_peers).
	New bool
	// Unarchive: the chat is archived and comes back to the main list, as
	// keep_archived_unmuted and keep_archived_folders decide.
	Unarchive bool
}

// IncomingArchiveOf decides IncomingArchive before the message is received.
// The events of one recipient are applied one at a time, so nothing changes
// the dialog in between.
func (d *Dao) IncomingArchiveOf(ctx context.Context, userId, fromId int64) (IncomingArchive, error) {
	if fromId == userId {
		return IncomingArchive{}, nil
	}
	var folders []int32
	if err := d.DB.QueryRowsPartial(ctx, &folders,
		"SELECT folder_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND (deleted = 0 OR top_message > 0)",
		userId, mtproto.PEER_USER, fromId); err != nil {
		return IncomingArchive{}, err
	}
	if len(folders) > 0 && folders[0] != FolderArchive {
		return IncomingArchive{}, nil
	}
	settings, err := d.UserClient.UserGetGlobalPrivacySettings(ctx, &userpb.TLUserGetGlobalPrivacySettings{UserId: userId})
	if err != nil {
		return IncomingArchive{}, err
	}
	if len(folders) == 0 {
		if !settings.GetArchiveAndMuteNewNoncontactPeers_FLAGBOOLEAN() {
			return IncomingArchive{}, nil
		}
		contact, err := d.UserClient.UserCheckContact(ctx, &userpb.TLUserCheckContact{UserId: userId, Id: fromId})
		if err != nil {
			return IncomingArchive{}, err
		}
		return IncomingArchive{New: !mtproto.FromBool(contact)}, nil
	}

	// An archived chat: muted chats stay, so do the unmuted ones when the
	// user keeps them, or keeps those of a chat folder.
	if settings.GetKeepArchivedUnmuted() {
		return IncomingArchive{}, nil
	}
	muted, err := d.mutedChat(ctx, userId, fromId)
	if err != nil || muted {
		return IncomingArchive{}, err
	}
	if settings.GetKeepArchivedFolders() {
		inFolder, err := d.inChatFolder(ctx, userId, fromId)
		if err != nil || inFolder {
			return IncomingArchive{}, err
		}
	}
	return IncomingArchive{Unarchive: true}, nil
}

// mutedChat tells whether the private chat of userId with peerId is muted
// now: by its own setting, or, without one, by the setting of private
// chats (as Telegram-iOS reads it, AccountStateManagementUtils.swift).
func (d *Dao) mutedChat(ctx context.Context, userId, peerId int64) (bool, error) {
	now := int32(time.Now().Unix())
	peer, err := d.UserClient.UserGetNotifySettings(ctx, &userpb.TLUserGetNotifySettings{
		UserId: userId, PeerType: mtproto.PEER_USER, PeerId: peerId,
	})
	if err != nil {
		return false, err
	}
	if mute := peer.GetMuteUntil(); mute != nil {
		return mute.GetValue() > now, nil
	}
	class, err := d.UserClient.UserGetNotifySettings(ctx, &userpb.TLUserGetNotifySettings{
		UserId: userId, PeerType: mtproto.PEER_USERS,
	})
	if err != nil {
		return false, err
	}
	return class.GetMuteUntil().GetValue() > now, nil
}

// inChatFolder tells whether one of the chat folders of userId includes the
// private chat with peerId by name or pins it (keep_archived_folders: "always
// included or pinned in a folder", core.telegram.org/constructor/
// globalPrivacySettings).
func (d *Dao) inChatFolder(ctx context.Context, userId, peerId int64) (bool, error) {
	var filters []string
	if err := d.DB.QueryRowsPartial(ctx, &filters,
		"SELECT dialog_filter FROM dialog_filters WHERE user_id = ? AND deleted = 0", userId); err != nil {
		return false, err
	}
	for _, data := range filters {
		f, err := mtproto.UnmarshalDialogFilter(data)
		if err != nil {
			return false, err
		}
		for _, p := range append(append([]*mtproto.InputPeer{}, f.GetIncludePeers()...), f.GetPinnedPeers()...) {
			if p.GetPredicateName() == mtproto.Predicate_inputPeerUser && p.GetUserId() == peerId {
				return true, nil
			}
		}
	}
	return false, nil
}

// ArchiveNewDialog puts the box's dialog with peer, just created by an
// incoming message, into the archive. The clients learn its folder when
// they load the dialog they do not know (messages.getPeerDialogs), so no
// update goes with it.
func (d *Dao) ArchiveNewDialog(b *userbox.Box, peerType int32, peerId int64) error {
	if _, err := b.Tx().Exec("UPDATE dialogs SET folder_id = ?, pinned = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND folder_id = ?",
		FolderArchive, b.UserId(), peerType, peerId, FolderMain); err != nil {
		return err
	}
	b.InvalidateCache(dialog.DeleteCacheKeys(b.UserId(), peerType, peerId)...)
	return nil
}
