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

// ChatlistsDeleteExportedInvite
// chatlists.deleteExportedInvite#719c5c5e chatlist:InputChatlist slug:string = Bool;
//
// Deletes a link of the folder; its channel invite links stop working.
func (c *FoldersCore) ChatlistsDeleteExportedInvite(in *mtproto.TLChatlistsDeleteExportedInvite) (*mtproto.Bool, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.deleteExportedInvite - %v", err)
		return nil, err
	}
	if in.GetSlug() == "" {
		return nil, mtproto.ErrInviteSlugEmpty
	}
	if _, err = c.svcCtx.Dao.DialogClient.DialogDeleteChatlistInvite(c.ctx, &dialog.TLDialogDeleteChatlistInvite{
		UserId:   c.MD.UserId,
		FilterId: id,
		Slug:     in.GetSlug(),
	}); err != nil {
		c.Logger.Errorf("chatlists.deleteExportedInvite - folder %d: %v", id, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
