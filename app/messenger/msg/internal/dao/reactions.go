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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The reactions to messages of private chats and basic groups (task 053)
// are rows of message_reactions under the message's dialog_message_id, which
// all copies share. Each user changes only its own rows, in its own box
// transaction; the copy of the message's author has reaction_unread set
// while a reaction of another user is unread (the dialog's
// unread_reactions_count counts those copies).

// ReactionTarget is b's owner's copy of a message reactions are set on.
type ReactionTarget struct {
	Peer            boxPeer // the dialog as the owner sees it
	MessageId       int32   // the owner's copy
	DialogMessageId int64
	AuthorId        int64
	Kind            reactions.Kind
}

// kindOf is the kind of chat of a dialog of owner.
func kindOf(owner int64, peer boxPeer) reactions.Kind {
	switch {
	case peer.PeerType == mtproto.PEER_CHAT:
		return reactions.Group
	case peer.PeerId == owner:
		return reactions.Saved
	}
	return reactions.Private
}

// ReactionTarget returns b's owner's copy msgId of its dialog with peer, as
// a target of reactions: MSG_ID_INVALID for a copy the box does not have,
// of another dialog, deleted, or a service message (Telegram has no
// reactions to those).
func (d *Dao) ReactionTarget(b *userbox.Box, peerType int32, peerId int64, msgId int32) (*ReactionTarget, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		return nil, mtproto.ErrPeerIdInvalid
	}
	row, err := messageById(b, msgId)
	if err != nil {
		return nil, err
	}
	did := peer.dialogId(b.UserId())
	if row == nil || row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, mtproto.ErrMsgIdInvalid
	}
	var m mtproto.Message
	if err = jsonx.UnmarshalFromString(row.MessageData, &m); err != nil {
		return nil, err
	}
	if m.GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMsgIdInvalid
	}
	return &ReactionTarget{
		Peer:            peer,
		MessageId:       row.UserMessageBoxId,
		DialogMessageId: row.DialogMessageId,
		AuthorId:        row.SenderUserId,
		Kind:            kindOf(b.UserId(), peer),
	}, nil
}

// reactionRow is a row of message_reactions as go-zero scans it.
type reactionRow struct {
	DialogMessageId int64  `db:"dialog_message_id"`
	UserId          int64  `db:"user_id"`
	Reaction        string `db:"reaction"`
	ChosenOrder     int32  `db:"chosen_order"`
	Big             bool   `db:"big"`
	Unread          bool   `db:"unread"`
	Date            int64  `db:"date"`
}

func (r reactionRow) row() reactions.Row {
	return reactions.Row{UserId: r.UserId, Reaction: r.Reaction, ChosenOrder: r.ChosenOrder, Big: r.Big, Unread: r.Unread, Date: r.Date}
}

const reactionColumns = "dialog_message_id, user_id, reaction, chosen_order, big, unread, `date`"

// ReactionRows returns the reactions to the messages dialogMessageIds by
// message, in the order of their dates, as b's transaction sees them.
func (d *Dao) ReactionRows(b *userbox.Box, dialogMessageIds []int64) (map[int64][]reactions.Row, error) {
	out := map[int64][]reactions.Row{}
	if len(dialogMessageIds) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(dialogMessageIds))
	for _, id := range dialogMessageIds {
		args = append(args, id)
	}
	var rows []reactionRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+reactionColumns+" FROM message_reactions WHERE dialog_message_id IN ("+inList(len(dialogMessageIds))+") ORDER BY `date`, user_id, chosen_order",
		args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DialogMessageId] = append(out[r.DialogMessageId], r.row())
	}
	return out, nil
}

// SetReactions makes keys, in this order, b's owner's reactions to the
// message of t: the reactions it keeps keep their date and unread flag, the
// new ones are unread for the author when another user puts them (tags of
// Saved Messages never are). changed is false when the list is the same.
func (d *Dao) SetReactions(b *userbox.Box, t *ReactionTarget, keys []string, big bool) (changed bool, err error) {
	var mine []reactionRow
	if err = b.Tx().QueryRowsPartial(&mine,
		"SELECT "+reactionColumns+" FROM message_reactions WHERE dialog_message_id = ? AND user_id = ? ORDER BY chosen_order",
		t.DialogMessageId, b.UserId()); err != nil {
		return false, err
	}
	old := make([]string, 0, len(mine))
	for _, r := range mine {
		old = append(old, r.Reaction)
	}
	if slices.Equal(old, keys) && !big {
		return false, nil
	}
	for _, r := range mine {
		if !slices.Contains(keys, r.Reaction) {
			if _, err = b.Tx().Exec("DELETE FROM message_reactions WHERE dialog_message_id = ? AND user_id = ? AND reaction = ?",
				t.DialogMessageId, b.UserId(), r.Reaction); err != nil {
				return false, err
			}
		}
	}
	unread := b.UserId() != t.AuthorId && t.Kind != reactions.Saved
	for i, k := range keys {
		if slices.Contains(old, k) {
			if _, err = b.Tx().Exec("UPDATE message_reactions SET chosen_order = ? WHERE dialog_message_id = ? AND user_id = ? AND reaction = ?",
				i, t.DialogMessageId, b.UserId(), k); err != nil {
				return false, err
			}
			continue
		}
		if _, err = b.Tx().Exec("INSERT INTO message_reactions ("+reactionColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
			t.DialogMessageId, b.UserId(), k, i, big, unread, b.Date()); err != nil {
			return false, err
		}
	}
	return true, nil
}

// ReactionsUpdate is updateMessageReactions of the owner's copy of t with
// rows, as the owner sees them; an empty object when there are none, so that
// the clients clear theirs.
func ReactionsUpdate(owner int64, t *ReactionTarget, rows []reactions.Row) *mtproto.Update {
	return mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
		Peer_PEER:                  t.Peer.toPeer(),
		MsgId_INT32:                t.MessageId,
		Reactions_MESSAGEREACTIONS: reactions.ViewOrEmpty(rows, owner, t.AuthorId, t.Kind),
	}).To_Update()
}

// OwnCopy returns b's owner's copy of the message dialogMessageId as a target
// of reactions, nil when the box has none (deleted for the owner).
func (d *Dao) OwnCopy(b *userbox.Box, dialogMessageId int64) (*ReactionTarget, error) {
	rows, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	r := rows[0]
	peer := peerOfDialog(b.UserId(), mtproto.DialogID{A: r.DialogId1, B: r.DialogId2})
	return &ReactionTarget{
		Peer:            peer,
		MessageId:       r.UserMessageBoxId,
		DialogMessageId: r.DialogMessageId,
		AuthorId:        r.SenderUserId,
		Kind:            kindOf(b.UserId(), peer),
	}, nil
}

// MarkUnreadReactions sets the reaction_unread flag of the author's copy of
// t from rows, when b's owner is the author, and the dialog's
// unread_reactions_count from the copies flagged.
func (d *Dao) MarkUnreadReactions(b *userbox.Box, t *ReactionTarget, rows []reactions.Row) error {
	if b.UserId() != t.AuthorId {
		return nil
	}
	unread := slices.ContainsFunc(rows, func(r reactions.Row) bool { return r.Unread && r.UserId != t.AuthorId })
	if _, err := b.Tx().Exec("UPDATE messages SET reaction_unread = ? WHERE user_id = ? AND user_message_box_id = ? AND reaction_unread <> ?",
		unread, b.UserId(), t.MessageId, unread); err != nil {
		return err
	}
	return d.recountUnreadReactions(b, t.Peer)
}

// recountUnreadReactions sets the dialog's unread_reactions_count to the
// copies of the dialog with reaction_unread.
func (d *Dao) recountUnreadReactions(b *userbox.Box, peer boxPeer) error {
	did := peer.dialogId(b.UserId())
	n, err := countMessages(b, "dialog_id1 = ? AND dialog_id2 = ? AND reaction_unread = 1", did.A, did.B)
	if err != nil {
		return err
	}
	dlg, err := dialogRow(b, peer)
	if err != nil || dlg == nil || dlg.UnreadReactionsCount == n {
		return err
	}
	return updateDialog(b, peer, "unread_reactions_count = ?", n)
}

// seenReactions clears the unread flag of the reactions of other users to
// b's owner's messages dialogMessageIds.
func seenReactions(b *userbox.Box, dialogMessageIds []int64) error {
	if len(dialogMessageIds) == 0 {
		return nil
	}
	args := make([]any, 0, len(dialogMessageIds)+1)
	for _, id := range dialogMessageIds {
		args = append(args, id)
	}
	args = append(args, b.UserId())
	_, err := b.Tx().Exec("UPDATE message_reactions SET unread = 0 WHERE dialog_message_id IN ("+inList(len(dialogMessageIds))+") AND user_id <> ? AND unread = 1", args...)
	return err
}

// ReadReactions marks the reactions to b's owner's messages of its dialog
// with peer seen: the flags of the copies and of the reactions are cleared
// and the dialog's counter goes to 0. Nothing is journaled: the other
// devices learn it from the dialog (an updateReadMessagesContents of the
// owner's own messages would mark their voice notes listened to).
func (d *Dao) ReadReactions(b *userbox.Box, peerType int32, peerId int64) error {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	did := peer.dialogId(b.UserId())
	var ids []int64
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT dialog_message_id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND reaction_unread = 1 AND deleted = 0",
		b.UserId(), did.A, did.B); err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err := b.Tx().Exec("UPDATE messages SET reaction_unread = 0 WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND reaction_unread = 1",
			b.UserId(), did.A, did.B); err != nil {
			return err
		}
		if err := seenReactions(b, ids); err != nil {
			return err
		}
	}
	return d.recountUnreadReactions(b, peer)
}

// withReactions returns message, b's owner's copy row of a message, with
// its reactions as the owner sees them: a copy of it when there are any.
func (d *Dao) withReactions(b *userbox.Box, row *dataobject.MessagesDO, message *mtproto.Message) (*mtproto.Message, error) {
	peer := peerOfDialog(b.UserId(), mtproto.DialogID{A: row.DialogId1, B: row.DialogId2})
	if peer.PeerType != mtproto.PEER_USER && peer.PeerType != mtproto.PEER_CHAT {
		return message, nil
	}
	rows, err := d.ReactionRows(b, []int64{row.DialogMessageId})
	if err != nil || len(rows[row.DialogMessageId]) == 0 {
		return message, err
	}
	shown := proto.Clone(message).(*mtproto.Message)
	shown.Reactions = reactions.View(rows[row.DialogMessageId], b.UserId(), row.SenderUserId, kindOf(b.UserId(), peer))
	return shown, nil
}

// SetTagTitle gives the tag key of b's owner's Saved Messages the title, or
// takes it away when empty. changed is false when it was so already.
func (d *Dao) SetTagTitle(b *userbox.Box, key, title string) (changed bool, err error) {
	var old []string
	if err = b.Tx().QueryRowsPartial(&old, "SELECT title FROM saved_reaction_tags WHERE user_id = ? AND reaction = ?", b.UserId(), key); err != nil {
		return false, err
	}
	switch {
	case len(old) == 0 && title == "", len(old) > 0 && old[0] == title:
		return false, nil
	case title == "":
		_, err = b.Tx().Exec("DELETE FROM saved_reaction_tags WHERE user_id = ? AND reaction = ?", b.UserId(), key)
	default:
		_, err = b.Tx().Exec("INSERT INTO saved_reaction_tags (user_id, reaction, title) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE title = VALUES(title)",
			b.UserId(), key, title)
	}
	return err == nil, err
}

// readOwnReactions marks the reactions to b's owner's own messages rows seen:
// their reaction_unread flags and the reactions' unread flags are cleared,
// and the dialogs' counters recounted. Nothing is journaled (ReadReactions).
func (d *Dao) readOwnReactions(b *userbox.Box, rows []dataobject.MessagesDO) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int32, 0, len(rows))
	seen := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UserMessageBoxId)
		seen = append(seen, r.DialogMessageId)
	}
	if err := clearUnreadFlags(b, ids, "reaction_unread = 0"); err != nil {
		return err
	}
	if err := seenReactions(b, seen); err != nil {
		return err
	}
	for _, c := range byDialog(b.UserId(), rows) {
		if err := d.recountUnreadReactions(b, c.peer); err != nil {
			return err
		}
	}
	return nil
}
