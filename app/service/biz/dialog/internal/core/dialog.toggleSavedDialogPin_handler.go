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

// DialogToggleSavedDialogPin
// dialog.toggleSavedDialogPin user_id:long peer:PeerUtil pinned:Bool users:Vector<User> chats:Vector<Chat> = Bool;
//
// The pin and updateSavedDialogPinned with seq for every device of the
// account are one box transaction (ADR 0020); users and chats are the peer
// for the push.
func (c *DialogCore) DialogToggleSavedDialogPin(in *dialog.TLDialogToggleSavedDialogPin) (*mtproto.Bool, error) {
	err := c.svcCtx.Dao.ToggleSavedDialogPin(
		c.ctx,
		in.GetUserId(),
		in.GetPeer(),
		mtproto.FromBool(in.GetPinned()),
		in.GetUsers(),
		in.GetChats())
	if err != nil {
		c.Logger.Errorf("dialog.toggleSavedDialogPin - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
