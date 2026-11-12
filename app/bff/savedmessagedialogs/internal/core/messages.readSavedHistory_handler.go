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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// MessagesReadSavedHistory
// messages.readSavedHistory#ba4a3b5b parent_peer:InputPeer peer:InputPeer max_id:int = Bool;
//
// An admin of a channel's direct messages read the sublist of the
// subscriber peer in the monoforum parent_peer up to max_id (task 084;
// core.telegram.org/api/monoforum): the mark is one for all the admins, who
// get updateReadMonoForumInbox, and the subscriber learns that its messages
// were read. The clients call it for monoforums alone (Android
// MessagesController.java:14608-14612, tdesktop data_saved_sublist.cpp:660,
// iOS ReplyThreadHistory.swift:392): the sub-chats of Saved Messages have
// nothing to read. PARENT_PEER_INVALID for a parent_peer that is no
// monoforum, CHAT_ADMIN_REQUIRED for anyone but an admin of it
// (core.telegram.org/method/messages.readSavedHistory).
func (c *SavedMessageDialogsCore) MessagesReadSavedHistory(in *mtproto.TLMessagesReadSavedHistory) (*mtproto.Bool, error) {
	monoforumId, err := c.parentMonoforum(in.GetParentPeer())
	if err != nil || monoforumId == 0 {
		c.Logger.Errorf("messages.readSavedHistory - parent_peer is no monoforum: %v", err)
		return nil, errParentPeerInvalid
	}
	peerId, err := c.sublistPeer(in.GetPeer())
	if err != nil {
		c.Logger.Errorf("messages.readSavedHistory - error: %v", err)
		return nil, err
	}
	if _, err = c.svcCtx.Dao.Channel.ChannelReadSublist(c.ctx, &channelpb.TLChannelReadSublist{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     monoforumId,
		PeerId:        peerId,
		MaxId:         in.GetMaxId(),
	}); err != nil {
		c.Logger.Errorf("messages.readSavedHistory - sublist %d of %d: %v", peerId, monoforumId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
