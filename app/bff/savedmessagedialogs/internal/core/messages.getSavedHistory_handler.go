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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessagesGetSavedHistory
// messages.getSavedHistory#998ab009 flags:# parent_peer:flags.0?InputPeer peer:InputPeer offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int hash:long = messages.Messages;
//
// The messages of one sub-chat of Saved Messages: those saved from peer, or
// the account's own ones ("My notes") for itself, with the users and the
// chats, channels among them, the messages name.
func (c *SavedMessageDialogsCore) MessagesGetSavedHistory(in *mtproto.TLMessagesGetSavedHistory) (*mtproto.Messages_Messages, error) {
	monoforumId, err := c.parentMonoforum(in.GetParentPeer())
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - error: %v", err)
		return nil, err
	}
	if monoforumId != 0 {
		return c.getSublistHistory(monoforumId, in)
	}
	peer, err := c.savedPeer(in.GetPeer())
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - error: %v", err)
		return nil, err
	}

	limit := in.GetLimit()
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	boxList, err := c.svcCtx.Dao.MessageClient.MessageGetSavedHistoryMessages(c.ctx, &message.TLMessageGetSavedHistoryMessages{
		UserId:     c.MD.UserId,
		PeerType:   peer.PeerType,
		PeerId:     peer.PeerId,
		OffsetId:   in.GetOffsetId(),
		OffsetDate: in.GetOffsetDate(),
		AddOffset:  in.GetAddOffset(),
		Limit:      limit,
		MaxId:      in.GetMaxId(),
		MinId:      in.GetMinId(),
		Hash:       in.GetHash(),
	})
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - error: %v", err)
		return nil, err
	}

	var (
		messages = []*mtproto.Message{}
		h        = mtproto.NewIDListHelper()
	)
	boxList.Visit(c.MD.UserId,
		func(messageList []*mtproto.Message) {
			messages = messageList
			h.PickByMessages(messageList...)
		},
		nil, nil, nil)
	if len(messages) > 0 {
		// The peer of the sub-chat, which the messages name in saved_peer_id.
		h.PickByPeerUtil(peer.PeerType, peer.PeerId)
	}
	users, chats, err := c.peers(h)
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - error: %v", err)
		return nil, err
	}

	if boxList.Length() == limit {
		return mtproto.MakeTLMessagesMessagesSlice(&mtproto.Messages_Messages{
			Count:    boxList.Count,
			Messages: messages,
			Users:    users,
			Chats:    chats,
		}).To_Messages_Messages(), nil
	}
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: messages,
		Users:    users,
		Chats:    chats,
	}).To_Messages_Messages(), nil
}

// getSublistHistory is messages.getSavedHistory of a monoforum (task 084):
// a page of the sublist of the subscriber peer, messages.channelMessages
// with the monoforum's pts as messages.getHistory of it answers, for an
// admin of the channel's direct messages. The clients read a sublist with
// it (tdesktop data_saved_sublist.cpp, Android
// MessagesController.java:11598-11610).
func (c *SavedMessageDialogsCore) getSublistHistory(monoforumId int64, in *mtproto.TLMessagesGetSavedHistory) (*mtproto.Messages_Messages, error) {
	peerId, err := c.sublistPeer(in.GetPeer())
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - error: %v", err)
		return nil, err
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetSublistHistory(c.ctx, &channelpb.TLChannelGetSublistHistory{
		SelfId:     c.MD.UserId,
		ChannelId:  monoforumId,
		PeerId:     peerId,
		OffsetId:   in.GetOffsetId(),
		OffsetDate: in.GetOffsetDate(),
		AddOffset:  in.GetAddOffset(),
		Limit:      in.GetLimit(),
		MaxId:      in.GetMaxId(),
		MinId:      in.GetMinId(),
	})
	if err != nil {
		c.Logger.Errorf("messages.getSavedHistory - sublist %d of %d: %v", peerId, monoforumId, err)
		return nil, err
	}
	h := mtproto.NewIDListHelper()
	h.AppendChannels(monoforumId)
	h.AppendUsers(peerId)
	h.PickByMessages(r.GetMessages()...)
	if r.Users, r.Chats, err = c.peers(h); err != nil {
		c.Logger.Errorf("messages.getSavedHistory - sublist %d of %d: %v", peerId, monoforumId, err)
		return nil, err
	}
	return r, nil
}
