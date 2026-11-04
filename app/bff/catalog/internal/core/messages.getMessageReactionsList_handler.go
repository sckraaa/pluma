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

	"github.com/teamgram/proto/mtproto"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetMessageReactionsList
// messages.getMessageReactionsList#461b3f48 flags:# peer:InputPeer id:int reaction:flags.0?Reaction offset:flags.1?string limit:int = messages.MessageReactionsList;
//
// Who reacted to the account's message id, the newest first, with one
// reaction or all; offset is where the previous page ended (task 053).
func (c *CatalogCore) MessagesGetMessageReactionsList(in *mtproto.TLMessagesGetMessageReactionsList) (*mtproto.Messages_MessageReactionsList, error) {
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
	var only string
	if r := in.GetReaction_FLAGREACTION(); r != nil {
		if only, err = reactions.Key(r); err != nil {
			return nil, err
		}
	}
	rows := slices.DeleteFunc(slices.Clone(byMessage[box.GetDialogMessageId()]), func(r reactions.Row) bool { return only != "" && r.Reaction != only })
	slices.SortStableFunc(rows, func(a, b reactions.Row) int { return cmp.Compare(b.Date, a.Date) })

	start := 0
	if o := in.GetOffset().GetValue(); o != "" {
		if start, err = strconv.Atoi(o); err != nil || start < 0 {
			return nil, mtproto.ErrOffsetInvalid
		}
	}
	limit := int(min(max(in.Limit, 1), 100))
	end := min(start+limit, len(rows))
	start = min(start, end)

	out := &mtproto.Messages_MessageReactionsList{Count: int32(len(rows)), Reactions: []*mtproto.MessagePeerReaction{}}
	var ids []int64
	for _, r := range rows[start:end] {
		ids = append(ids, r.UserId)
		out.Reactions = append(out.Reactions, mtproto.MakeTLMessagePeerReaction(&mtproto.MessagePeerReaction{
			Big:               r.Big,
			Unread:            r.Unread && c.MD.UserId == box.GetSenderUserId() && r.UserId != c.MD.UserId,
			My:                r.UserId == c.MD.UserId,
			PeerId:            mtproto.MakePeerUser(r.UserId),
			Date:              int32(r.Date),
			Reaction_REACTION: reactions.FromKey(r.Reaction),
		}).To_MessagePeerReaction())
	}
	if end < len(rows) {
		out.NextOffset = mtproto.MakeFlagsString(strconv.Itoa(end))
	}
	if out.Users, out.Chats, err = c.peersOf(peer, ids); err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessageReactionsList(out).To_Messages_MessageReactionsList(), nil
}
