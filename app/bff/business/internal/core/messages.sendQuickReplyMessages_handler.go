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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesSendQuickReplyMessages
// messages.sendQuickReplyMessages#6c750de1 peer:InputPeer shortcut_id:int id:Vector<int> random_id:Vector<long> = Updates;
//
// Sends the messages of a shortcut of the account, all of them when id is
// empty, to a private chat with another user (task 105,
// core.telegram.org/method/messages.sendQuickReplyMessages): Saved
// Messages, a group or a channel is PEER_ID_INVALID, and so is a bot (not
// verified).
//
// The messages reach every device of the account, the calling one too:
// Android sends the request without reading its answer
// (QuickRepliesController.java:810, 820 sendRequest(req, null)) and has no
// local copy of the messages to match, so only the push shows them; tdesktop
// and iOS apply the answer as well (apiwrap.cpp:4492-4493), and the second
// copy of each update carries the same pts, which they pass over.
func (c *BusinessCore) MessagesSendQuickReplyMessages(in *mtproto.TLMessagesSendQuickReplyMessages) (*mtproto.Updates, error) {
	const method = "messages.sendQuickReplyMessages"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId == c.MD.UserId {
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgSendQuickReplyMessages(c.ctx, &msgpb.TLMsgSendQuickReplyMessages{
		UserId:     c.MD.UserId,
		AuthKeyId:  0,
		PeerId:     peer.PeerId,
		ShortcutId: in.ShortcutId,
		Id:         in.Id,
		RandomId:   in.RandomId,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}
