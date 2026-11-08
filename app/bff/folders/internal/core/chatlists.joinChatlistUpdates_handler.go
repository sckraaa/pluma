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

// ChatlistsJoinChatlistUpdates
// chatlists.joinChatlistUpdates#e089f8f5 chatlist:InputChatlist peers:Vector<InputPeer> = Updates;
//
// Joins the chosen channels new in the link of an imported folder.
func (c *FoldersCore) ChatlistsJoinChatlistUpdates(in *mtproto.TLChatlistsJoinChatlistUpdates) (*mtproto.Updates, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.joinChatlistUpdates - %v", err)
		return nil, err
	}
	ext, err := c.svcCtx.Dao.DialogClient.DialogGetChatlistInvite(c.ctx, &dialog.TLDialogGetChatlistInvite{UserId: c.MD.UserId, FilterId: id})
	if err != nil {
		c.Logger.Errorf("chatlists.joinChatlistUpdates - folder %d: %v", id, err)
		return nil, err
	}
	r, err := c.joinChatlistFrom(ext, "", id, in.GetPeers())
	if err != nil {
		c.Logger.Errorf("chatlists.joinChatlistUpdates - folder %d: %v", id, err)
		return nil, err
	}
	return r, nil
}
