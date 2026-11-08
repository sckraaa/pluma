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

package core

import (
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// FoldersEditPeerFolders
// folders.editPeerFolders#6847d0ab folder_peers:Vector<InputFolderPeer> = Updates;
//
// Moves dialogs into the archive (folder 1) or back to the main list
// (folder 0), the only peer folders there are (core.telegram.org/api/
// folders, task 050): msg moves them in the user's box with one
// updateFolderPeers per folder.
func (c *FoldersCore) FoldersEditPeerFolders(in *mtproto.TLFoldersEditPeerFolders) (*mtproto.Updates, error) {
	if len(in.GetFolderPeers()) == 0 {
		return nil, mtproto.ErrPeerIdInvalid
	}
	byFolder := map[int32][]int64{}
	var folders []int32
	for _, fp := range in.GetFolderPeers() {
		folderId := fp.GetFolderId()
		if folderId != 0 && folderId != 1 {
			return nil, mtproto.ErrFolderIdInvalid
		}
		peer := mtproto.FromInputPeer2(c.MD.UserId, fp.GetPeer())
		switch peer.PeerType {
		case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		default:
			c.Logger.Errorf("folders.editPeerFolders - error: invalid peer %v", fp.GetPeer())
			return nil, mtproto.ErrPeerIdInvalid
		}
		if _, ok := byFolder[folderId]; !ok {
			folders = append(folders, folderId)
		}
		byFolder[folderId] = append(byFolder[folderId], mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId))
	}

	var updates []*mtproto.Update
	for _, folderId := range folders {
		r, err := c.svcCtx.Dao.MsgClient.MsgEditPeerFolders(c.ctx, &msgpb.TLMsgEditPeerFolders{
			UserId:    c.MD.UserId,
			AuthKeyId: c.MD.PermAuthKeyId,
			Peers:     byFolder[folderId],
			FolderId:  folderId,
		})
		if err != nil {
			c.Logger.Errorf("folders.editPeerFolders - error: %v", err)
			return nil, err
		}
		updates = append(updates, r.GetUpdates()...)
	}
	if err := c.clearAutoarchived(updates); err != nil {
		c.Logger.Errorf("folders.editPeerFolders - error: %v", err)
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdates(updates...), nil
}

// clearAutoarchived hides the bar of an automatically archived chat the
// user took out of the archive: every client clears it at Unarchive
// (Android ChatActivity, tdesktop ContactStatus, iOS PeerContactSettings),
// and Web K reads it again, so the server clears it for all of them.
func (c *FoldersCore) clearAutoarchived(updates []*mtproto.Update) error {
	for _, u := range updates {
		for _, fp := range u.GetFolderPeers() {
			if fp.GetFolderId() != 0 || fp.GetPeer().GetPredicateName() != mtproto.Predicate_peerUser {
				continue
			}
			peerId := fp.GetPeer().GetUserId()
			settings, err := c.svcCtx.Dao.UserClient.UserGetPeerSettings(c.ctx, &userpb.TLUserGetPeerSettings{
				UserId:   c.MD.UserId,
				PeerType: mtproto.PEER_USER,
				PeerId:   peerId,
			})
			if err != nil {
				return err
			}
			if !settings.GetAutoarchived() {
				continue
			}
			if _, err = c.svcCtx.Dao.UserClient.UserDeletePeerSettings(c.ctx, &userpb.TLUserDeletePeerSettings{
				UserId:   c.MD.UserId,
				PeerType: mtproto.PEER_USER,
				PeerId:   peerId,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
