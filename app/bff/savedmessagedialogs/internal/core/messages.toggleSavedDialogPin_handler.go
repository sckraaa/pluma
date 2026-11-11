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

// MessagesToggleSavedDialogPin
// messages.toggleSavedDialogPin#ac81bbde flags:# pinned:flags.0?true peer:InputDialogPeer = Bool;
func (c *SavedMessageDialogsCore) MessagesToggleSavedDialogPin(in *mtproto.TLMessagesToggleSavedDialogPin) (*mtproto.Bool, error) {
	var (
		peer *mtproto.PeerUtil
		err  error
	)

	switch in.GetPeer().GetPredicateName() {
	case mtproto.Predicate_inputDialogPeer:
		if peer, err = c.savedPeer(in.GetPeer().GetPeer()); err != nil {
			c.Logger.Errorf("messages.toggleSavedDialogPin - error: %v", err)
			return nil, err
		}
	case mtproto.Predicate_inputDialogPeerFolder:
		// error
		c.Logger.Errorf("messages.toggleSavedDialogPin - error: client not send inputDialogPeerFolder: %v", in.GetPeer())
		return mtproto.BoolFalse, nil
	default:
		err = mtproto.ErrInputConstructorInvalid
		c.Logger.Errorf("messages.toggleSavedDialogPin - error: %v", err)
		return nil, err
	}

	// biz pushes updateSavedDialogPinned with seq to every device of the
	// account, this one included, in the transaction of the pin (ADR 0020);
	// the peer goes with it.
	h := mtproto.NewIDListHelper()
	h.PickByPeerUtil(peer.PeerType, peer.PeerId)
	users, chats, err := c.peers(h)
	if err != nil {
		c.Logger.Errorf("messages.toggleSavedDialogPin - error: %v", err)
		return nil, err
	}

	_, err = c.svcCtx.Dao.DialogClient.DialogToggleSavedDialogPin(c.ctx, &dialog.TLDialogToggleSavedDialogPin{
		UserId: c.MD.UserId,
		Peer:   peer,
		Pinned: mtproto.ToBool(in.Pinned),
		Users:  users,
		Chats:  chats,
	})
	if err != nil {
		c.Logger.Errorf("messages.toggleSavedDialogPin - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
