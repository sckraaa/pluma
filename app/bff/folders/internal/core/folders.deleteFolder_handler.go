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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// FoldersDeleteFolder
// folders.deleteFolder#1c295881 folder_id:int = Updates;
//
// Empties the archive, the only peer folder there is
// (core.telegram.org/api/folders): its chats go back to the main list with
// one updateFolderPeers, as folders.editPeerFolders moves them. The
// official clients do not call it (Android, tdesktop, Web K, read for task
// 063); that the method empties the folder rather than deleting its chats
// is read from its name and result only, and is unverified.
func (c *FoldersCore) FoldersDeleteFolder(in *mtproto.TLFoldersDeleteFolder) (*mtproto.Updates, error) {
	if in.GetFolderId() != 1 {
		return nil, mtproto.ErrFolderIdInvalid
	}
	archived, err := c.svcCtx.Dao.DialogClient.DialogGetDialogs(c.ctx, &dialog.TLDialogGetDialogs{
		UserId:        c.MD.UserId,
		ExcludePinned: mtproto.BoolFalse,
		FolderId:      1,
	})
	if err != nil {
		c.Logger.Errorf("folders.deleteFolder - error: %v", err)
		return nil, err
	}
	peers := make([]int64, 0, len(archived.GetDatas()))
	for _, ex := range archived.GetDatas() {
		p := mtproto.FromPeer(ex.GetDialog().GetPeer())
		peers = append(peers, mtproto.MakePeerDialogId(p.PeerType, p.PeerId))
	}
	if len(peers) == 0 {
		return mtproto.MakeUpdatesByUpdates(), nil
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgEditPeerFolders(c.ctx, &msgpb.TLMsgEditPeerFolders{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		Peers:     peers,
		FolderId:  0,
	})
	if err != nil {
		c.Logger.Errorf("folders.deleteFolder - error: %v", err)
		return nil, err
	}
	if err = c.clearAutoarchived(r.GetUpdates()); err != nil {
		c.Logger.Errorf("folders.deleteFolder - error: %v", err)
		return nil, err
	}
	return r, nil
}
