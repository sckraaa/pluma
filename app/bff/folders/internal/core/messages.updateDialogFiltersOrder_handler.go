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

// MessagesUpdateDialogFiltersOrder
// messages.updateDialogFiltersOrder#c563c1e4 order:Vector<int> = Bool;
//
// Puts the folders in the order given. "All chats" (id 0) stays first: a
// user without Premium cannot move it, so its place in order is ignored,
// as are ids of folders the user does not have. The other devices get
// updateDialogFilterOrder with the order now stored.
func (c *FoldersCore) MessagesUpdateDialogFiltersOrder(in *mtproto.TLMessagesUpdateDialogFiltersOrder) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.DialogClient.DialogUpdateDialogFiltersOrder(c.ctx, &dialog.TLDialogUpdateDialogFiltersOrder{
		UserId: c.MD.UserId,
		Order:  in.GetOrder(),
	}); err != nil {
		c.Logger.Errorf("messages.updateDialogFiltersOrder - error: %v", err)
		return nil, err
	}

	filters, err := c.dialogFilters()
	if err != nil {
		// The order is stored; the other devices reload it on their next
		// messages.getDialogFilters.
		c.Logger.Errorf("messages.updateDialogFiltersOrder - read the folders: %v", err)
		return mtproto.BoolTrue, nil
	}
	order := make([]int32, 0, len(filters))
	for _, f := range filters {
		order = append(order, f.GetId())
	}
	c.pushToOtherDevices(mtproto.MakeTLUpdateDialogFilterOrder(&mtproto.Update{
		Order_VECTORINT32: order,
	}).To_Update())
	return mtproto.BoolTrue, nil
}
