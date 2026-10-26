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

// MessagesUpdateDialogFilter
// messages.updateDialogFilter#1ad4a04a flags:# id:int filter:flags.0?DialogFilter = Bool;
//
// Creates folder id or changes it; without filter, deletes it. Every device
// of the account, this one included, gets updateDialogFilter with seq, with
// the folder or without it: the dialog service pushes it in the transaction
// of the change (ADR 0020), with the folder's peers resolved here.
func (c *FoldersCore) MessagesUpdateDialogFilter(in *mtproto.TLMessagesUpdateDialogFilter) (*mtproto.Bool, error) {
	id := in.GetId()
	if id < filterIdMin || id > filterIdMax {
		err := mtproto.ErrFilterIdInvalid
		c.Logger.Errorf("messages.updateDialogFilter - folder %d: %v", id, err)
		return nil, err
	}

	if in.GetFilter() == nil {
		if _, err := c.svcCtx.Dao.DialogClient.DialogDeleteDialogFilter(c.ctx, &dialog.TLDialogDeleteDialogFilter{
			UserId: c.MD.UserId,
			Id:     id,
		}); err != nil {
			c.Logger.Errorf("messages.updateDialogFilter - delete folder %d: %v", id, err)
			return nil, err
		}
		return mtproto.BoolTrue, nil
	}

	filter, peers, err := c.checkFilter(id, in.GetFilter())
	if err != nil {
		c.Logger.Errorf("messages.updateDialogFilter - folder %d: %v", id, err)
		return nil, err
	}
	if _, err = c.svcCtx.Dao.DialogClient.DialogInsertOrUpdateDialogFilter(c.ctx, &dialog.TLDialogInsertOrUpdateDialogFilter{
		UserId:       c.MD.UserId,
		Id:           id,
		DialogFilter: filter,
		Users:        peers.users,
		Chats:        peers.chats,
	}); err != nil {
		c.Logger.Errorf("messages.updateDialogFilter - save folder %d: %v", id, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
