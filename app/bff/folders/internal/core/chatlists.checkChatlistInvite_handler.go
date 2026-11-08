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

// ChatlistsCheckChatlistInvite
// chatlists.checkChatlistInvite#41c10fff slug:string = chatlists.ChatlistInvite;
//
// What the link shows: the channels as a guest sees them, chatlistInviteAlready
// to whoever has its folder; INVITE_SLUG_EXPIRED for a link that is gone.
func (c *FoldersCore) ChatlistsCheckChatlistInvite(in *mtproto.TLChatlistsCheckChatlistInvite) (*mtproto.Chatlists_ChatlistInvite, error) {
	if in.GetSlug() == "" {
		return nil, mtproto.ErrInviteSlugEmpty
	}
	ext, err := c.svcCtx.Dao.DialogClient.DialogGetChatlistInvite(c.ctx, &dialog.TLDialogGetChatlistInvite{UserId: c.MD.UserId, Slug: in.GetSlug()})
	if err != nil {
		c.Logger.Errorf("chatlists.checkChatlistInvite - %v", err)
		return nil, err
	}
	r, err := c.checkChatlistInviteOf(ext)
	if err != nil {
		c.Logger.Errorf("chatlists.checkChatlistInvite - %v", err)
		return nil, err
	}
	return r, nil
}
