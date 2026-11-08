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

// ChatlistsGetExportedInvites
// chatlists.getExportedInvites#ce03da83 chatlist:InputChatlist = chatlists.ExportedInvites;
//
// The links of the folder, with their channels.
func (c *FoldersCore) ChatlistsGetExportedInvites(in *mtproto.TLChatlistsGetExportedInvites) (*mtproto.Chatlists_ExportedInvites, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.getExportedInvites - %v", err)
		return nil, err
	}
	r, err := c.svcCtx.Dao.DialogClient.DialogGetChatlistInvites(c.ctx, &dialog.TLDialogGetChatlistInvites{UserId: c.MD.UserId, FilterId: id})
	if err != nil {
		c.Logger.Errorf("chatlists.getExportedInvites - folder %d: %v", id, err)
		return nil, err
	}
	chats, err := c.exportedChatlistChats(r.GetInvites())
	if err != nil {
		c.Logger.Errorf("chatlists.getExportedInvites - folder %d: %v", id, err)
		return nil, err
	}
	r.Chats = chats
	return r, nil
}
