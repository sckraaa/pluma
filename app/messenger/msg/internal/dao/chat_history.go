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
	"fmt"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/ghost"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// MentionedGhostsJoinChat makes the ghosts among mentioned, users the
// messages b's owner sends to the basic group chatId mention who are not in
// it, hidden members of the group (ADR 0048, p. 2a), in the transaction of
// the send, with the chat's row locked (ghost.JoinChat): the caller adds
// the ones it returns to the recipients of the messages, sent, the boxes of
// this send. Each that joined gets inbox.chatHistoryToInbox first, so that
// the group's history lands in its box before the message that mentions it
// (the events of one transaction to one recipient keep their order, ADR
// 0012); the history leaves out the messages of sent, which come by their
// own event, unread. A ghost another send brought in after the caller read
// the chat (two mentions at once) is a member already: it gets the
// messages too, with no history of its own. A ghost an admin removed from
// the group stays out.
func (d *Dao) MentionedGhostsJoinChat(b *userbox.Box, chatId int64, mentioned []int64, sent []*mtproto.MessageBox) ([]int64, error) {
	ghosts, err := ghost.Ghosts(b.Tx(), mentioned)
	if err != nil {
		return nil, err
	}
	var (
		joined []int64
		skip   = make([]int64, 0, len(sent))
	)
	for _, box := range sent {
		skip = append(skip, box.GetDialogMessageId())
	}
	for _, id := range ghosts {
		if id == b.UserId() {
			continue
		}
		out, err := ghost.JoinChat(b.Tx(), chatId, id, b.Date(), true)
		if err != nil {
			return nil, err
		}
		if out == ghost.Member {
			joined = append(joined, id)
			continue
		}
		if out != ghost.Joined {
			continue
		}
		if err = d.EmitInbox(b, &inbox.TLInboxChatHistoryToInbox{UserId: id, FromId: b.UserId(), ChatId: chatId, Mention: true, Skip: skip}); err != nil {
			return nil, err
		}
		b.InvalidateCache(chatpb.CacheKey(chatId), chatpb.ParticipantCacheKey(chatId, id))
		joined = append(joined, id)
	}
	return joined, nil
}

// HiddenInChat tells whether b's owner is a hidden member (the ghost, ADR
// 0048) of the basic group peerId; any other peer is no basic group.
func (d *Dao) HiddenInChat(b *userbox.Box, peerType int32, peerId int64) (bool, error) {
	if peerType != mtproto.PEER_CHAT {
		return false, nil
	}
	var hidden []bool
	err := b.Tx().QueryRowsPartial(&hidden, "SELECT hidden FROM chat_participants WHERE chat_id = ? AND user_id = ? AND state = ?",
		peerId, b.UserId(), mtproto.ChatMemberStateNormal)
	return len(hidden) > 0 && hidden[0], err
}

// ChatHistoryPortion is how many rows of the other members' boxes one
// transaction of a history copy reads: a basic group's history comes into
// the ghost's box in portions, as a whole history is deleted (maxPortion).
const ChatHistoryPortion = 500

// chatHistoryQuery reads the next copies of the basic group's messages the
// box of user_id (the ghost) has no copy of, deleted or not, dated no later
// than its join: the live copies in the boxes of the group's other
// participants, of any state (one who left keeps what it saw), by
// dialog_message_id, each message first in the box of the participant who
// came earliest. A copy's date2 is the message's date in its sender's box
// and the delivery's in the others', so a message sent after the join has
// none dated before it.
var chatHistoryQuery = "SELECT " + prefixed("m.", messageColumns) + " FROM messages m" +
	" JOIN chat_participants p ON p.chat_id = ? AND p.user_id = m.user_id" +
	" WHERE m.dialog_id1 = ? AND m.dialog_id2 = ? AND m.peer_type = ? AND m.peer_id = ? AND m.deleted = 0 AND m.user_id <> ? AND m.date2 <= ?" +
	" AND NOT EXISTS (SELECT 1 FROM messages g WHERE g.user_id = ? AND g.dialog_message_id = m.dialog_message_id)" +
	" %s ORDER BY m.dialog_message_id, p.invited_at, p.user_id LIMIT ?"

// prefixed qualifies each column of a list with prefix.
func prefixed(prefix, columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// CopyChatHistory copies the next portion of the history of the basic group
// chatId into b's box, the box of a ghost that joined the group hidden (ADR
// 0048, p. 4), and tells whether the whole history is there. Each message
// gets the next id of the box, in the order of the messages, without a
// journal row (pts 0): the client reads it with messages.getHistory, not
// with getDifference. A copy is the reader's: out only for the ghost's own
// messages, no mention, no unread media, no reaction of another as its
// own, the reply to the ghost's copy of the message replied to. The dialog
// is read up to the last copy, so that what comes next is unread; the
// messages skip names (the send that brought the ghost in) are left to
// their own delivery. The history ends at the ghost's join (invited_at):
// what was sent after it comes by its own delivery, unread, with its
// mention, even while the inbox runs behind. A ghost no longer in the
// group gets nothing.
func (d *Dao) CopyChatHistory(b *userbox.Box, chatId int64, skip []int64) (bool, error) {
	userId := b.UserId()
	var joinedAt []int64
	if err := b.Tx().QueryRowsPartial(&joinedAt, "SELECT invited_at FROM chat_participants WHERE chat_id = ? AND user_id = ? AND state = ?",
		chatId, userId, mtproto.ChatMemberStateNormal); err != nil || len(joinedAt) == 0 {
		return true, err
	}
	did := mtproto.MakeDialogId(userId, mtproto.PEER_CHAT, chatId)
	args := []any{chatId, did.A, did.B, mtproto.PEER_CHAT, chatId, userId, joinedAt[0], userId}
	skipped := ""
	if len(skip) > 0 {
		skipped = "AND m.dialog_message_id NOT IN (" + inList(len(skip)) + ")"
		for _, id := range skip {
			args = append(args, id)
		}
	}
	var rows []dataobject.MessagesDO
	if err := b.Tx().QueryRowsPartial(&rows, fmt.Sprintf(chatHistoryQuery, skipped), append(args, ChatHistoryPortion)...); err != nil {
		return false, err
	}
	var (
		peer = boxPeer{PeerType: mtproto.PEER_CHAT, PeerId: chatId}
		top  int32
		last int64
	)
	for i := range rows {
		src := &rows[i]
		// The first copy of each message is the one to take.
		if src.DialogMessageId == last {
			continue
		}
		last = src.DialogMessageId
		id, err := d.copyHistoryMessage(b, did, peer, src)
		if err != nil {
			return false, err
		}
		top = id
	}
	if top != 0 {
		if err := upsertIncomingDialog(b, peer, top, 0, 0, b.Date()); err != nil {
			return false, err
		}
		if err := updateDialog(b, peer, "read_inbox_max_id = GREATEST(read_inbox_max_id, ?)", top); err != nil {
			return false, err
		}
	}
	return len(rows) < ChatHistoryPortion, nil
}

// copyHistoryMessage writes b's owner's copy of src, a copy of a basic
// group's message in another member's box, and returns its id.
func (d *Dao) copyHistoryMessage(b *userbox.Box, did mtproto.DialogID, peer boxPeer, src *dataobject.MessagesDO) (int32, error) {
	userId := b.UserId()
	message := &mtproto.Message{}
	if err := jsonx.UnmarshalFromString(src.MessageData, message); err != nil {
		return 0, err
	}
	id := b.NextMessageId()
	message.Id = id
	message.Out = src.SenderUserId == userId
	message.Mentioned = false
	message.MediaUnread = false
	message.FromScheduled = false
	ownReactions(message.GetReactions())

	if message.GetReplyTo().GetPredicateName() == mtproto.Predicate_messageReplyHeader && message.GetReplyTo().GetReplyFrom() == nil {
		// The reply names the message in src's owner's box; the copy names
		// the ghost's copy of it, copied before (the order of the messages)
		// or kept from before.
		var replied []int32
		if err := b.Tx().QueryRowsPartial(&replied,
			"SELECT g.user_message_box_id FROM messages s JOIN messages g ON g.user_id = ? AND g.dialog_message_id = s.dialog_message_id AND g.deleted = 0"+
				" WHERE s.user_id = ? AND s.user_message_box_id = ? LIMIT 1",
			userId, src.UserId, message.GetReplyTo().GetFixedReplyToMsgId()); err != nil {
			return 0, err
		}
		if len(replied) == 0 {
			message.ReplyTo = nil
		} else {
			r := replied[0]
			message.ReplyTo.ReplyToMsgId = r
			message.ReplyTo.ReplyToMsgId_INT32 = r
			message.ReplyTo.ReplyToMsgId_FLAGINT32 = mtproto.MakeFlagsInt32(r)
		}
	}

	data, err := jsonx.Marshal(message)
	if err != nil {
		return 0, err
	}
	if err = insertMessage(b, &dataobject.MessagesDO{
		UserId:            userId,
		UserMessageBoxId:  id,
		DialogId1:         did.A,
		DialogId2:         did.B,
		DialogMessageId:   src.DialogMessageId,
		SenderUserId:      src.SenderUserId,
		PeerType:          peer.PeerType,
		PeerId:            peer.PeerId,
		MessageFilterType: src.MessageFilterType,
		MessageData:       string(data),
		Message:           src.Message,
		Pinned:            src.Pinned,
		Date2:             src.Date2,
		TtlPeriod:         src.TtlPeriod,
	}, 0); err != nil {
		return 0, err
	}
	if err = scheduleExpiry(b, message); err != nil {
		return 0, err
	}
	if err = d.replaceHashTags(b, peer, id, message, false); err != nil {
		return 0, err
	}
	return id, nil
}

// ownReactions clears from reactions what was the reactions of the box's
// owner the copy came from: its own choice and its unread marks.
func ownReactions(reactions *mtproto.MessageReactions) {
	for _, r := range reactions.GetResults() {
		r.ChosenOrder, r.Chosen = nil, false
	}
	for _, r := range reactions.GetRecentReactions() {
		r.My = false
		r.Unread = false
	}
}
