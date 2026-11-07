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
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetMessageReactionsList
// messages.getMessageReactionsList#461b3f48 flags:# peer:InputPeer id:int reaction:flags.0?Reaction offset:flags.1?string limit:int = messages.MessageReactionsList;
//
// Who reacted to the account's message id, the newest first, with one
// reaction or all; offset is where the previous page ended (task 053).
func (c *CatalogCore) MessagesGetMessageReactionsList(in *mtproto.TLMessagesGetMessageReactionsList) (*mtproto.Messages_MessageReactionsList, error) {
	if p := mtproto.FromInputPeer2(c.MD.UserId, in.Peer); p.PeerType == mtproto.PEER_CHANNEL {
		return c.channelReactionsList(p, in)
	}
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	box, err := c.svcCtx.Message.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: in.Id})
	if err != nil {
		// biz message answers a message the box does not have with
		// MESSAGE_ID_INVALID; anything else is a failure of its own.
		c.Logger.Errorf("messages.getMessageReactionsList - error: %v", err)
		return nil, err
	}
	if box.GetMessage() == nil || box.GetPeerType() != peer.PeerType || box.GetPeerId() != peer.PeerId {
		return nil, mtproto.ErrMsgIdInvalid
	}
	byMessage, err := reactions.Load(c.ctx, c.svcCtx.Reactions, []int64{box.GetDialogMessageId()})
	if err != nil {
		c.Logger.Errorf("messages.getMessageReactionsList - error: %v", err)
		return nil, err
	}
	out, ids, err := reactionsPage(byMessage[box.GetDialogMessageId()], in, c.MD.UserId, box.GetSenderUserId())
	if err != nil {
		return nil, err
	}
	if out.Users, out.Chats, err = c.peersOf(peer, ids); err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessageReactionsList(out).To_Messages_MessageReactionsList(), nil
}

// reactionsPage is a page of who reacted, of rows: the newest first, with
// one reaction or all, from the offset of the previous page; ids are the
// users of the page.
func reactionsPage(all []reactions.Row, in *mtproto.TLMessagesGetMessageReactionsList, viewer, author int64) (out *mtproto.Messages_MessageReactionsList, ids []int64, err error) {
	var only string
	if r := in.GetReaction_FLAGREACTION(); r != nil {
		if only, err = reactions.Key(r); err != nil {
			return nil, nil, err
		}
	}
	rows := slices.DeleteFunc(slices.Clone(all), func(r reactions.Row) bool { return only != "" && r.Reaction != only })
	slices.SortStableFunc(rows, func(a, b reactions.Row) int { return cmp.Compare(b.Date, a.Date) })

	start := 0
	if o := in.GetOffset().GetValue(); o != "" {
		if start, err = strconv.Atoi(o); err != nil || start < 0 {
			return nil, nil, mtproto.ErrOffsetInvalid
		}
	}
	limit := int(min(max(in.Limit, 1), 100))
	end := min(start+limit, len(rows))
	start = min(start, end)

	out = &mtproto.Messages_MessageReactionsList{Count: int32(len(rows)), Reactions: []*mtproto.MessagePeerReaction{}}
	for _, r := range rows[start:end] {
		ids = append(ids, r.UserId)
		out.Reactions = append(out.Reactions, mtproto.MakeTLMessagePeerReaction(&mtproto.MessagePeerReaction{
			Big:               r.Big,
			Unread:            r.Unread && viewer == author && r.UserId != viewer,
			My:                r.UserId == viewer,
			PeerId:            mtproto.MakePeerUser(r.UserId),
			Date:              int32(r.Date),
			Reaction_REACTION: reactions.FromKey(r.Reaction),
		}).To_MessagePeerReaction())
	}
	if end < len(rows) {
		out.NextOffset = mtproto.MakeFlagsString(strconv.Itoa(end))
	}
	return out, ids, nil
}

// channelReactionsList is messages.getMessageReactionsList in a supergroup
// (task 044): who reacted to a message the account may see. A broadcast
// channel lists no one (its reactions are counts only):
// BROADCAST_FORBIDDEN.
func (c *CatalogCore) channelReactionsList(peer *mtproto.PeerUtil, in *mtproto.TLMessagesGetMessageReactionsList) (*mtproto.Messages_MessageReactionsList, error) {
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetBroadcast() {
		return nil, mtproto.ErrBroadcastForbidden
	}
	messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{in.Id}})
	if err != nil {
		return nil, err
	}
	if len(messages.GetDatas()) == 0 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMsgIdInvalid
	}
	byMessage, err := reactions.LoadChannel(c.ctx, c.svcCtx.Reactions, peer.PeerId, []int32{in.Id})
	if err != nil {
		c.Logger.Errorf("messages.getMessageReactionsList - error: %v", err)
		return nil, err
	}
	out, ids, err := reactionsPage(byMessage[in.Id], in, c.MD.UserId, messages.GetDatas()[0].GetFromId().GetUserId())
	if err != nil {
		return nil, err
	}
	if out.Users, _, err = c.peersOf(peer, ids); err != nil {
		return nil, err
	}
	out.Chats = []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}
	return mtproto.MakeTLMessagesMessageReactionsList(out).To_Messages_MessageReactionsList(), nil
}
