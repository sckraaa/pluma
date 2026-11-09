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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesGetUnreadReactions
// messages.getUnreadReactions#bd7f90ac flags:# peer:InputPeer top_msg_id:flags.0?int saved_peer_id:flags.1?InputPeer offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// The account's messages of the dialog with reactions it has not seen,
// newest first, paged as the history (task 053); of a channel with biz
// channel.getUnreadReactions, top_msg_id narrowing it to a topic (task 067).
// Saved dialogs of monoforums do not exist here.
func (c *CatalogCore) MessagesGetUnreadReactions(in *mtproto.TLMessagesGetUnreadReactions) (*mtproto.Messages_Messages, error) {
	if p := mtproto.FromInputPeer2(c.MD.UserId, in.Peer); p.PeerType == mtproto.PEER_CHANNEL {
		return c.channelUnreadReactions(in, p.PeerId)
	}
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	boxes, err := c.svcCtx.Message.MessageGetUnreadReactions(c.ctx, &messagepb.TLMessageGetUnreadReactions{
		UserId:    c.MD.UserId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		OffsetId:  in.OffsetId,
		AddOffset: in.AddOffset,
		Limit:     in.Limit,
		MinId:     in.MinId,
		MaxId:     in.MaxId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getUnreadReactions - error: %v", err)
		return nil, err
	}
	messages := make([]*mtproto.Message, 0, len(boxes.GetDatas()))
	for _, box := range boxes.GetDatas() {
		messages = append(messages, box.ToMessage(c.MD.UserId))
	}
	h := mtproto.NewIDListHelper()
	h.PickByMessages(messages...)
	for _, m := range messages {
		for _, r := range m.GetReactions().GetRecentReactions() {
			h.PickByPeer(r.GetPeerId())
		}
	}
	users, chats, err := c.peersOf(peer, h.UserIdList)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: messages,
		Chats:    chats,
		Users:    users,
	}).To_Messages_Messages(), nil
}

// channelUnreadReactions is messages.getUnreadReactions of a channel, or of
// a topic of a forum with top_msg_id (biz channel.getUnreadReactions).
func (c *CatalogCore) channelUnreadReactions(in *mtproto.TLMessagesGetUnreadReactions, channelId int64) (*mtproto.Messages_Messages, error) {
	r, err := c.svcCtx.Channel.ChannelGetUnreadReactions(c.ctx, &channelpb.TLChannelGetUnreadReactions{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
		TopMsgId:  in.GetTopMsgId().GetValue(),
		OffsetId:  in.OffsetId,
		AddOffset: in.AddOffset,
		Limit:     min(in.Limit, channelUnreadReactionsMax),
		MaxId:     in.MaxId,
		MinId:     in.MinId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getUnreadReactions - error: %v", err)
		return nil, err
	}
	return r, c.fillChannelReactionPeers(r, channelId)
}

// channelUnreadReactionsMax is the most messages of one page, as of the
// history.
const channelUnreadReactionsMax = 100

// fillChannelReactionPeers sets the channel and the users of a page of its
// messages, the reacting users and the creators of its topics among them.
func (c *CatalogCore) fillChannelReactionPeers(r *mtproto.Messages_Messages, channelId int64) error {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	for _, m := range r.GetMessages() {
		for _, p := range m.GetReactions().GetRecentReactions() {
			h.PickByPeer(p.GetPeerId())
		}
	}
	for _, t := range r.GetTopics() {
		h.PickByPeer(t.GetFromId())
	}
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{c.MD.UserId}})
	if err != nil {
		return err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	channels, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: append(h.ChannelIdList, channelId)})
	if err != nil {
		return err
	}
	r.Chats = channels.GetChannelListByIdList(c.MD.UserId)
	return nil
}
