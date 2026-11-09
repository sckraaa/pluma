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

package dao

import (
	"database/sql"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Reactions to messages of channels (task 044): one row per reaction of a
// user in channel_message_reactions, written in the user's box transaction
// with the channel locked, so that the number of different reactions of a
// message holds. The channel's pts does not move: the members get
// updateMessageReactions by push and poll messages.getMessagesReactions,
// as all four clients do for channels.

// ChannelReactions is a message's reactions after a change.
type ChannelReactions struct {
	Rows      []reactions.Row
	AuthorId  int64
	TopicId   int32 // the topic of a forum, 0 for General and elsewhere
	Broadcast bool
	Changed   bool
}

// SetChannelReactions makes keys, in this order, b's owner's reactions to
// message msgId of the channel. Any member reacts in a broadcast channel; in
// a supergroup a member whom neither the default rights nor its own
// restriction ban send_reactions (CHAT_WRITE_FORBIDDEN otherwise).
// MESSAGE_ID_INVALID for a message that does not exist, is deleted or is a
// service message; REACTIONS_TOO_MANY past reactions.UniqMax different
// reactions on the message.
func (d *Dao) SetChannelReactions(b *userbox.Box, channelId int64, msgId int32, keys []string, big bool) (*ChannelReactions, error) {
	_, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if !s.ch.Broadcast && s.p.Role == channelperm.RoleMember {
		banned := s.ch.Default
		if channelperm.Restricted(s.p, int32(b.Date())) {
			banned |= s.p.BannedRights
		}
		if banned&mtproto.BANNED_SEND_REACTIONS != 0 {
			return nil, mtproto.ErrChatWriteForbidden
		}
	}
	if err = channelAllows(b, channelId, keys); err != nil {
		return nil, err
	}
	var msgs []struct {
		SenderUserId int64 `db:"sender_user_id"`
		TopicId      int32 `db:"topic_id"`
		Service      bool  `db:"service"`
	}
	if err = b.Tx().QueryRowsPartial(&msgs,
		"SELECT sender_user_id, topic_id, JSON_UNQUOTE(JSON_EXTRACT(message_data, '$.predicate_name')) <> ? AS service FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE",
		mtproto.Predicate_message, channelId, msgId); err != nil {
		return nil, err
	}
	if len(msgs) == 0 || msgs[0].Service {
		return nil, mtproto.ErrMessageIdInvalid
	}
	out := &ChannelReactions{Broadcast: s.ch.Broadcast, TopicId: msgs[0].TopicId}
	if !s.ch.Broadcast {
		out.AuthorId = msgs[0].SenderUserId
	}
	// The author alone sees a reaction of another as unread (task 067), a
	// post has nobody to show it to.
	unread := !s.ch.Broadcast && msgs[0].SenderUserId != b.UserId()
	rows, err := channelReactionRows(b, channelId, msgId)
	if err != nil {
		return nil, err
	}
	var old []string
	others := map[string]bool{}
	for _, r := range rows {
		if r.UserId == b.UserId() {
			old = append(old, r.Reaction)
		} else {
			others[r.Reaction] = true
		}
	}
	slices.SortStableFunc(old, func(a, c string) int {
		return chosenOrder(rows, b.UserId(), a) - chosenOrder(rows, b.UserId(), c)
	})
	if slices.Equal(old, keys) && !big {
		out.Rows = rows
		return out, nil
	}
	for _, k := range keys {
		others[k] = true
	}
	if len(others) > reactions.UniqMax {
		return nil, mtproto.ErrReactionsTooMany
	}
	for _, k := range old {
		if !slices.Contains(keys, k) {
			if _, err = b.Tx().Exec("DELETE FROM channel_message_reactions WHERE channel_id = ? AND message_id = ? AND user_id = ? AND reaction = ?",
				channelId, msgId, b.UserId(), k); err != nil {
				return nil, err
			}
		}
	}
	for i, k := range keys {
		if slices.Contains(old, k) {
			if _, err = b.Tx().Exec("UPDATE channel_message_reactions SET chosen_order = ? WHERE channel_id = ? AND message_id = ? AND user_id = ? AND reaction = ?",
				i, channelId, msgId, b.UserId(), k); err != nil {
				return nil, err
			}
			continue
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_message_reactions (channel_id, message_id, user_id, reaction, chosen_order, big, unread, `date`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			channelId, msgId, b.UserId(), k, i, big, unread, b.Date()); err != nil {
			return nil, err
		}
	}
	if out.Rows, err = channelReactionRows(b, channelId, msgId); err != nil {
		return nil, err
	}
	out.Changed = true
	return out, nil
}

func chosenOrder(rows []reactions.Row, userId int64, key string) int {
	for _, r := range rows {
		if r.UserId == userId && r.Reaction == key {
			return int(r.ChosenOrder)
		}
	}
	return 0
}

// channelReactionRows are the reactions to message msgId of the channel in
// the order of their dates, read in b's transaction.
func channelReactionRows(b *userbox.Box, channelId int64, msgId int32) ([]reactions.Row, error) {
	var rows []struct {
		UserId      int64  `db:"user_id"`
		Reaction    string `db:"reaction"`
		ChosenOrder int32  `db:"chosen_order"`
		Big         bool   `db:"big"`
		Unread      bool   `db:"unread"`
		Date        int64  `db:"date"`
	}
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT user_id, reaction, chosen_order, big, unread, `date` FROM channel_message_reactions WHERE channel_id = ? AND message_id = ? ORDER BY `date`, user_id, chosen_order",
		channelId, msgId); err != nil {
		return nil, err
	}
	out := make([]reactions.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, reactions.Row{UserId: r.UserId, Reaction: r.Reaction, ChosenOrder: r.ChosenOrder, Big: r.Big, Unread: r.Unread, Date: r.Date})
	}
	return out, nil
}

// ChannelReactionsUpdate is updateMessageReactions of message msgId of the
// channel with rows as viewer sees them; viewer 0 is the view every member
// gets by push, min: the clients keep their own chosen reactions
// (core.telegram.org/constructor/messageReactions).
func ChannelReactionsUpdate(viewer, channelId int64, msgId int32, r *ChannelReactions) *mtproto.Update {
	view := reactions.ViewOrEmpty(r.Rows, viewer, r.AuthorId, reactions.ChannelKind(r.Broadcast))
	if viewer == 0 {
		view = reactions.WithUnreadOf(view, r.Rows, r.AuthorId)
		view.Min = true
	}
	u := mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
		Peer_PEER:                  mtproto.MakePeerChannel(channelId),
		MsgId_INT32:                msgId,
		Reactions_MESSAGEREACTIONS: view,
	}).To_Update()
	if r.TopicId != 0 {
		u.TopMsgId_FLAGINT32 = wrapperspb.Int32(r.TopicId)
	}
	return u
}

// EmitChannelReactionsPush pushes the reactions r of a message to the
// members in their min view. The unread marks are for the author (the
// mentioned of the push): sync takes them off everyone else's copy, and it
// is by them that the clients count the unread reactions of a message
// (Android MessagesController.java:19509 hasUnreadReactions, tdesktop
// data_message_reactions.cpp:2169).
func (d *Dao) EmitChannelReactionsPush(b *userbox.Box, channelId, permAuthKeyId int64, msgId int32, r *ChannelReactions) error {
	var author []int64
	if r.AuthorId != 0 && r.AuthorId != b.UserId() {
		author = []int64{r.AuthorId}
	}
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Mentioned:     author,
		Updates:       mtproto.MakeUpdatesByUpdates(ChannelReactionsUpdate(0, channelId, msgId, r)),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

// channelAllows checks keys against the reactions the channel allows
// (task 044, chatReactions): none, all but custom emoji, all, or some;
// REACTION_INVALID for one it does not. A channel that never chose allows
// the standard ones.
func channelAllows(b *userbox.Box, channelId int64, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	var rows []struct {
		Type int32          `db:"available_reactions_type"`
		List sql.NullString `db:"available_reactions"`
	}
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT available_reactions_type, available_reactions FROM channels WHERE id = ?", channelId); err != nil {
		return err
	}
	if len(rows) == 0 {
		return mtproto.ErrChannelInvalid
	}
	var some map[string]bool
	if rows[0].Type == mtproto.ChatReactionsTypeSome {
		var list []string
		if rows[0].List.Valid {
			if err := jsonx.UnmarshalFromString(rows[0].List.String, &list); err != nil {
				return err
			}
		}
		some = map[string]bool{}
		for _, r := range list {
			if k, err := reactions.Key(mtproto.FromReaction(r)); err == nil {
				some[k] = true
			}
		}
	}
	for _, k := range keys {
		custom := strings.HasPrefix(k, "c")
		switch rows[0].Type {
		case mtproto.ChatReactionsTypeNone:
			return mtproto.ErrReactionInvalid
		case mtproto.ChatReactionsTypeNotDefined, mtproto.ChatReactionsTypeAllNoAllowCustom:
			if custom {
				return mtproto.ErrReactionInvalid
			}
		case mtproto.ChatReactionsTypeSome:
			if !some[k] {
				return mtproto.ErrReactionInvalid
			}
		}
	}
	return nil
}
