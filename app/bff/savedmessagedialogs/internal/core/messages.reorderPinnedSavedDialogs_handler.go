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

// MessagesReorderPinnedSavedDialogs
// messages.reorderPinnedSavedDialogs#8b716587 flags:# force:flags.0?true order:Vector<InputDialogPeer> = Bool;
func (c *SavedMessageDialogsCore) MessagesReorderPinnedSavedDialogs(in *mtproto.TLMessagesReorderPinnedSavedDialogs) (*mtproto.Bool, error) {
	if len(in.GetOrder()) == 0 {
		c.Logger.Errorf("messages.reorderPinnedDialogs - len(order) == 0")
		return mtproto.BoolTrue, nil
	}

	var (
		order []*mtproto.PeerUtil
		h     = mtproto.NewIDListHelper()
	)
	for _, peer := range in.GetOrder() {
		if peer.GetPredicateName() != mtproto.Predicate_inputDialogPeer {
			err := mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.reorderPinnedSavedDialogs - error: %v", err)
			return nil, err
		}
		p, err := c.savedPeer(peer.GetPeer())
		if err != nil {
			c.Logger.Errorf("messages.reorderPinnedSavedDialogs - error: %v", err)
			return nil, err
		}
		order = append(order, p)
		h.PickByPeerUtil(p.PeerType, p.PeerId)
	}

	// biz pushes updatePinnedSavedDialogs with seq, with the order as stored,
	// to every device of the account, this one included, in the transaction
	// of the order (ADR 0020); the peers of the order go with it.
	users, chats, err := c.peers(h)
	if err != nil {
		c.Logger.Errorf("messages.reorderPinnedSavedDialogs - error: %v", err)
		return nil, err
	}

	_, err = c.svcCtx.Dao.DialogClient.DialogReorderPinnedSavedDialogs(c.ctx, &dialog.TLDialogReorderPinnedSavedDialogs{
		UserId: c.MD.UserId,
		Force:  mtproto.ToBool(in.Force),
		Order:  order,
		Users:  users,
		Chats:  chats,
	})
	if err != nil {
		c.Logger.Errorf("messages.reorderPinnedSavedDialogs - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
