// Copyright 2022 Teamgram Authors
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

// MessagesReorderPinnedDialogs
// messages.reorderPinnedDialogs#3b1adf37 flags:# force:flags.0?true folder_id:int order:Vector<InputDialogPeer> = Bool;
func (c *DialogsCore) MessagesReorderPinnedDialogs(in *mtproto.TLMessagesReorderPinnedDialogs) (*mtproto.Bool, error) {
	var (
		peerDialogIdList []int64
		h                = mtproto.NewIDListHelper()
	)

	for _, peer := range in.GetOrder() {
		switch peer.PredicateName {
		case mtproto.Predicate_inputDialogPeer:
			p := mtproto.FromInputPeer2(c.MD.UserId, peer.Peer)
			if p.PeerType == mtproto.PEER_SELF {
				p.PeerType = mtproto.PEER_USER
			}
			peerDialogIdList = append(peerDialogIdList, mtproto.MakePeerDialogId(p.PeerType, p.PeerId))
			h.PickByPeerUtil(p.PeerType, p.PeerId)
		case mtproto.Predicate_inputDialogPeerFolder:
			c.Logger.Info("messages.reorderPinnedDialogs - inputDialogPeerFolder %s", peer)
		default:
			err := mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.reorderPinnedDialogs - error: %v", err)
			return nil, err
		}
	}

	// biz pushes updatePinnedDialogs with seq, with the order as stored, to
	// every device of the account, this one included, in the transaction of
	// the order (ADR 0020); the peers of the order go with it.
	users, chats, err := c.pinPeers(h)
	if err != nil {
		c.Logger.Errorf("messages.reorderPinnedDialogs - error: %v", err)
		return nil, err
	}

	_, err = c.svcCtx.Dao.DialogClient.DialogReorderPinnedDialogs(c.ctx, &dialog.TLDialogReorderPinnedDialogs{
		UserId:   c.MD.UserId,
		Force:    mtproto.ToBool(in.Force),
		FolderId: in.FolderId,
		IdList:   peerDialogIdList,
		Users:    users,
		Chats:    chats,
	})
	if err != nil {
		c.Logger.Errorf("messages.reorderPinnedDialogs - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
