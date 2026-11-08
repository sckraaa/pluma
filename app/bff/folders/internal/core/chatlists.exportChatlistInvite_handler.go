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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// ChatlistsExportChatlistInvite
// chatlists.exportChatlistInvite#8472478e chatlist:InputChatlist title:string peers:Vector<InputPeer> = chatlists.ExportedChatlistInvite;
//
// Makes a link of the folder for peers, which become supergroups first if
// they are basic groups (core.telegram.org/api/folders). The folder turns
// shared with has_my_invites; every device gets updateDialogFilter with seq.
func (c *FoldersCore) ChatlistsExportChatlistInvite(in *mtproto.TLChatlistsExportChatlistInvite) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.exportChatlistInvite - %v", err)
		return nil, err
	}
	check := func() error {
		_, err := c.svcCtx.Dao.DialogClient.DialogExportChatlistInvite(c.ctx, &dialog.TLDialogExportChatlistInvite{
			UserId:    c.MD.UserId,
			FilterId:  id,
			CheckOnly: mtproto.BoolTrue,
		})
		return err
	}
	s, err := c.resolveShare(in.GetPeers(), check)
	if err != nil {
		c.Logger.Errorf("chatlists.exportChatlistInvite - folder %d: %v", id, err)
		return nil, err
	}
	r, err := c.svcCtx.Dao.DialogClient.DialogExportChatlistInvite(c.ctx, &dialog.TLDialogExportChatlistInvite{
		UserId:           c.MD.UserId,
		FilterId:         id,
		Title:            in.GetTitle(),
		Peers:            s.peers,
		MigratedChats:    s.migrated,
		MigratedChannels: s.channels,
		Chats:            s.chats,
		CheckOnly:        mtproto.BoolFalse,
	})
	if err != nil {
		c.Logger.Errorf("chatlists.exportChatlistInvite - folder %d: %v", id, err)
		return nil, err
	}
	return r, nil
}
