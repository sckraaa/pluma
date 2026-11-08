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

// ChatlistsLeaveChatlist
// chatlists.leaveChatlist#74fae13a chatlist:InputChatlist peers:Vector<InputPeer> = Updates;
//
// Leaves the chosen channels of the shared folder and deletes the folder:
// the channels first, so that a failure in between can be retried.
func (c *FoldersCore) ChatlistsLeaveChatlist(in *mtproto.TLChatlistsLeaveChatlist) (*mtproto.Updates, error) {
	id, err := chatlistFolderId(in.GetChatlist())
	if err != nil {
		c.Logger.Errorf("chatlists.leaveChatlist - %v", err)
		return nil, err
	}
	folder, _, err := c.chatlistFolder(id)
	if err != nil {
		c.Logger.Errorf("chatlists.leaveChatlist - folder %d: %v", id, err)
		return nil, err
	}
	out, err := c.leaveChatlistChannels(folder, in.GetPeers())
	if err != nil {
		c.Logger.Errorf("chatlists.leaveChatlist - folder %d: %v", id, err)
		return nil, err
	}
	if _, err = c.svcCtx.Dao.DialogClient.DialogDeleteDialogFilter(c.ctx, &dialog.TLDialogDeleteDialogFilter{UserId: c.MD.UserId, Id: id}); err != nil {
		c.Logger.Errorf("chatlists.leaveChatlist - folder %d: %v", id, err)
		return nil, err
	}
	withUpdateDialogFilter(out, id, nil)
	if err = c.addChatlistUsers(out); err != nil {
		c.Logger.Errorf("chatlists.leaveChatlist - folder %d: %v", id, err)
		return nil, err
	}
	return out, nil
}
