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

// ChatlistsEditExportedInvite
// chatlists.editExportedInvite#653db63d flags:# chatlist:InputChatlist slug:string title:flags.1?string peers:flags.2?Vector<InputPeer> = ExportedChatlistInvite;
//
// Changes the title or the channels of a link. With neither it reads back
// the link as it is.
func (c *FoldersCore) ChatlistsEditExportedInvite(in *mtproto.TLChatlistsEditExportedInvite) (*mtproto.ExportedChatlistInvite, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.editExportedInvite - %v", err)
		return nil, err
	}
	if in.GetSlug() == "" {
		return nil, mtproto.ErrInviteSlugEmpty
	}
	setPeers := in.GetPeers() != nil
	req := &dialog.TLDialogEditChatlistInvite{
		UserId:    c.MD.UserId,
		FilterId:  id,
		Slug:      in.GetSlug(),
		SetTitle:  mtproto.ToBool(in.GetTitle() != nil),
		Title:     in.GetTitle().GetValue(),
		SetPeers:  mtproto.ToBool(setPeers),
		CheckOnly: mtproto.BoolFalse,
	}
	if setPeers {
		check := func() error {
			_, err := c.svcCtx.Dao.DialogClient.DialogEditChatlistInvite(c.ctx, &dialog.TLDialogEditChatlistInvite{
				UserId:    c.MD.UserId,
				FilterId:  id,
				Slug:      in.GetSlug(),
				SetTitle:  mtproto.BoolFalse,
				SetPeers:  mtproto.BoolFalse,
				CheckOnly: mtproto.BoolTrue,
			})
			return err
		}
		s, err := c.resolveShare(in.GetPeers(), check)
		if err != nil {
			c.Logger.Errorf("chatlists.editExportedInvite - folder %d: %v", id, err)
			return nil, err
		}
		req.Peers, req.MigratedChats, req.MigratedChannels, req.Chats = s.peers, s.migrated, s.channels, s.chats
	}
	r, err := c.svcCtx.Dao.DialogClient.DialogEditChatlistInvite(c.ctx, req)
	if err != nil {
		c.Logger.Errorf("chatlists.editExportedInvite - folder %d: %v", id, err)
		return nil, err
	}
	return r, nil
}
