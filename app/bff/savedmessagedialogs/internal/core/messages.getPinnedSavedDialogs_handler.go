// Copyright 2024 Teamgram Authors
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

// MessagesGetPinnedSavedDialogs
// messages.getPinnedSavedDialogs#d63d94e0 = messages.SavedDialogs;
//
// The pinned sub-chats of Saved Messages in their order, all of them.
func (c *SavedMessageDialogsCore) MessagesGetPinnedSavedDialogs(in *mtproto.TLMessagesGetPinnedSavedDialogs) (*mtproto.Messages_SavedDialogs, error) {
	dialogs, err := c.svcCtx.Dao.DialogClient.DialogGetPinnedSavedDialogs(c.ctx, &dialog.TLDialogGetPinnedSavedDialogs{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getPinnedSavedDialogs - error: %v", err)
		return nil, err
	}

	r := &mtproto.Messages_SavedDialogs{
		Dialogs:  []*mtproto.SavedDialog{},
		Messages: []*mtproto.Message{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}
	if err = c.fillSavedDialogs(r, dialogs.GetDialogs()); err != nil {
		c.Logger.Errorf("messages.getPinnedSavedDialogs - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesSavedDialogs(r).To_Messages_SavedDialogs(), nil
}
