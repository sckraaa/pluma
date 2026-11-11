// Copyright 2025 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// MessagesGetSavedDialogsByID
// messages.getSavedDialogsByID#6f6f9c96 flags:# parent_peer:flags.1?InputPeer ids:Vector<InputPeer> = messages.SavedDialogs;
//
// The sub-chats of Saved Messages with the peers ids, in their order; a peer
// with no messages there has none and is left out. Desktop asks it without
// parent_peer for the account's own Saved Messages to refresh a cloud draft
// of a sub-chat (apiwrap.cpp:3005, FileOriginCloudDraft); the other calls of
// the clients name a monoforum in parent_peer (Desktop
// data/data_saved_messages.cpp, SavedMessages::requestSomeStale; Android
// TopicsController.java:544; iOS SyncResolve.swift:100), which is not served.
func (c *SavedMessageDialogsCore) MessagesGetSavedDialogsByID(in *mtproto.TLMessagesGetSavedDialogsByID) (*mtproto.Messages_SavedDialogs, error) {
	if err := c.checkParentPeer(in.GetParentPeer()); err != nil {
		c.Logger.Errorf("messages.getSavedDialogsByID - error: %v", err)
		return nil, err
	}

	idList := make([]*mtproto.PeerUtil, 0, len(in.GetIds()))
	for _, id := range in.GetIds() {
		p, err := c.savedPeer(id)
		if err != nil {
			c.Logger.Errorf("messages.getSavedDialogsByID - error: %v", err)
			return nil, err
		}
		idList = append(idList, p)
	}

	r := &mtproto.Messages_SavedDialogs{
		Dialogs:  []*mtproto.SavedDialog{},
		Messages: []*mtproto.Message{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}
	if len(idList) > 0 {
		dialogs, err := c.svcCtx.Dao.DialogClient.DialogGetSavedDialogsByIdList(c.ctx, &dialog.TLDialogGetSavedDialogsByIdList{
			UserId: c.MD.UserId,
			IdList: idList,
		})
		if err != nil {
			c.Logger.Errorf("messages.getSavedDialogsByID - error: %v", err)
			return nil, err
		}
		if err = c.fillSavedDialogs(r, dialogs.GetDialogs()); err != nil {
			c.Logger.Errorf("messages.getSavedDialogsByID - error: %v", err)
			return nil, err
		}
	}
	return mtproto.MakeTLMessagesSavedDialogs(r).To_Messages_SavedDialogs(), nil
}
