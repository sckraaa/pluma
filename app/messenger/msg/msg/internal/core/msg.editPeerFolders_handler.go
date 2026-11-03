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
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgEditPeerFolders
// msg.editPeerFolders user_id:long auth_key_id:long peers:Vector<long> folder_id:int = Updates;
//
// It moves dialogs of the user into the archive (1) or back to the main
// list (0) in one box transaction (task 050): the answer and the user's
// other devices get one updateFolderPeers with pts (core.telegram.org/api/
// folders). Dialogs already in the folder, or that do not exist, are left
// out of the update; with none moved the answer carries no update.
func (c *MsgCore) MsgEditPeerFolders(in *msg.TLMsgEditPeerFolders) (*mtproto.Updates, error) {
	if in.FolderId != dao.FolderMain && in.FolderId != dao.FolderArchive {
		return nil, mtproto.ErrFolderIdInvalid
	}
	peers := make([]dao.FolderPeer, 0, len(in.Peers))
	for _, id := range in.Peers {
		peerType, peerId := mtproto.GetPeerUtilByPeerDialogId(id)
		peerType, peerId = selfPeer(in.UserId, peerType, peerId)
		if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
			c.Logger.Errorf("msg.editPeerFolders - error: invalid peer %d/%d", peerType, peerId)
			return nil, mtproto.ErrPeerIdInvalid
		}
		peers = append(peers, dao.FolderPeer{PeerType: peerType, PeerId: peerId})
	}

	var moved *mtproto.Update
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		if moved, err = c.svcCtx.Dao.SetPeerFolders(b, peers, in.FolderId); err != nil || moved == nil {
			return err
		}
		return c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(moved))
	})
	if err != nil {
		c.Logger.Errorf("msg.editPeerFolders - error: %v", err)
		return nil, err
	}
	if moved == nil {
		return mtproto.MakeUpdatesByUpdates(), nil
	}
	return mtproto.MakeUpdatesByUpdates(moved), nil
}
