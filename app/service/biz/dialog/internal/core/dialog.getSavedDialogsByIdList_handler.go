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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// DialogGetSavedDialogsByIdList
// dialog.getSavedDialogsByIdList user_id:long id_list:Vector<PeerUtil> = SavedDialogList;
//
// The sub-chats of the user's Saved Messages named by id_list, in its order
// and once each; a peer with no messages left there has no sub-chat and is
// left out.
func (c *DialogCore) DialogGetSavedDialogsByIdList(in *dialog.TLDialogGetSavedDialogsByIdList) (*dialog.SavedDialogList, error) {
	dialogs, err := c.svcCtx.Dao.SavedDialogsByIdList(c.ctx, in.GetUserId(), in.GetIdList())
	if err != nil {
		c.Logger.Errorf("dialog.getSavedDialogsByIdList - error: %v", err)
		return nil, err
	}

	return dialog.MakeTLSavedDialogList(&dialog.SavedDialogList{
		Count:   int32(len(dialogs)),
		Dialogs: dialogs,
	}).To_SavedDialogList(), nil
}
