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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Checklists (task 062). The state of a checklist lives in one place, so
// that two people ticking items at once lose neither tick: a row of
// todo_lists for a private chat or a basic group, under the message's
// dialog_message_id and locked for each change; the one copy of the message
// for a channel, under the channel lock. Each change is written over the
// whole media of the message and journaled as an edit (updateEditMessage,
// updateEditChannelMessage), without edit_date: a tick is not an edit to
// show. A copy of another member applies the current state, so the last
// event applied leaves the last state.

// TodoRef is the checklist of a message as a user addresses it.
type TodoRef struct {
	Peer            boxPeer
	MessageId       int32
	DialogMessageId int64
	AuthorId        int64
	List            *mtproto.TodoList
	Completions     []*mtproto.TodoCompletion
	row             *dataobject.MessagesDO
	channel         *channelbox.Channel
	sender          *sender
	stored          *mtproto.Message
}

// storeNewMedia writes the row a new poll or checklist needs and returns the
// message as its copies keep it (storeNewPoll). Used for a message of a
// private chat or a basic group.
func (d *Dao) storeNewMedia(b *userbox.Box, message *mtproto.Message, peerType int32, peerId int64, id int32, dialogMessageId int64) (*mtproto.Message, error) {
	switch message.GetMedia().GetPredicateName() {
	case mtproto.Predicate_messageMediaPoll:
		return d.storeNewPoll(b, message, peerType, peerId, id, dialogMessageId, dialogMessageId)
	case mtproto.Predicate_messageMediaToDo:
		// A forward is a frozen copy of the list (checkTodoMessage).
		if message.GetFwdFrom() != nil {
			return message, nil
		}
		data, err := jsonx.MarshalToString(message.GetMedia().GetTodo())
		if err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("INSERT INTO todo_lists (dialog_message_id, creator_user_id, data, completions) VALUES (?, ?, ?, '[]')",
			dialogMessageId, b.UserId(), data); err != nil {
			return nil, err
		}
	}
	return message, nil
}

// TodoRefOf reads b's owner's copy msgId of its dialog with the peer, and the
// checklist's state, locked. MESSAGE_ID_INVALID without one.
func (d *Dao) TodoRefOf(b *userbox.Box, peerType int32, peerId int64, msgId int32) (*TodoRef, error) {
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		return nil, mtproto.ErrPeerIdInvalid
	}
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	row, err := messageById(b, msgId)
	if err != nil {
		return nil, err
	}
	did := peer.dialogId(b.UserId())
	if row == nil || row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, mtproto.ErrMessageIdInvalid
	}
	var m *mtproto.Message
	if err = jsonx.UnmarshalFromString(row.MessageData, &m); err != nil {
		return nil, err
	}
	if err = checkTodoMessage(m); err != nil {
		return nil, err
	}
	list, completions, err := lockTodo(b, row.DialogMessageId)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return &TodoRef{Peer: peer, MessageId: row.UserMessageBoxId, DialogMessageId: row.DialogMessageId, AuthorId: row.SenderUserId, List: list, Completions: completions, row: row}, nil
}

// checkTodoMessage: m holds a checklist that may change. A forward of one is
// a frozen copy, which no client ticks or extends (tdesktop TodoList::canComplete,
// Android MessageObject.canCompleteTodo, canAppendToTodo, iOS
// ChatMessageTodoBubbleContentNode).
func checkTodoMessage(m *mtproto.Message) error {
	if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaToDo {
		return mtproto.ErrMessageIdInvalid
	}
	if m.GetFwdFrom() != nil {
		return mtproto.ErrMessageAuthorRequired
	}
	return nil
}

func lockTodo(b *userbox.Box, dialogMessageId int64) (*mtproto.TodoList, []*mtproto.TodoCompletion, error) {
	var rows []struct {
		Data        string `db:"data"`
		Completions string `db:"completions"`
	}
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT data, completions FROM todo_lists WHERE dialog_message_id = ? FOR UPDATE", dialogMessageId); err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	var (
		list        *mtproto.TodoList
		completions []*mtproto.TodoCompletion
	)
	if err := jsonx.UnmarshalFromString(rows[0].Data, &list); err != nil {
		return nil, nil, err
	}
	if err := jsonx.UnmarshalFromString(rows[0].Completions, &completions); err != nil {
		return nil, nil, err
	}
	return list, completions, nil
}

// ChannelTodoRefOf reads message msgId of the channel, locked with the
// channel, and its checklist.
func (d *Dao) ChannelTodoRefOf(b *userbox.Box, channelId int64, msgId int32) (*TodoRef, error) {
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	var rows []channelMessageRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", channelId, msgId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	var m *mtproto.Message
	if err = jsonx.UnmarshalFromString(rows[0].MessageData, &m); err != nil {
		return nil, err
	}
	if err = checkTodoMessage(m); err != nil {
		return nil, err
	}
	return &TodoRef{
		Peer: boxPeer{PeerType: mtproto.PEER_CHANNEL, PeerId: channelId}, MessageId: msgId, AuthorId: rows[0].SenderUserId,
		List: m.GetMedia().GetTodo(), Completions: m.GetMedia().GetCompletions(), channel: ch, sender: s, stored: m,
	}, nil
}

// mayEditTodo: its author, in a channel also an admin who may edit others'
// messages (as EditChannel).
func (ref *TodoRef) mayEditTodo(b *userbox.Box) error {
	if ref.AuthorId == b.UserId() {
		return nil
	}
	if ref.sender != nil && ref.sender.ch.Broadcast && ref.sender.can(channelperm.EditOthers, b.Date()) == nil {
		return nil
	}
	return mtproto.ErrMessageAuthorRequired
}

// ToggleTodo marks items done or not done (messages.toggleTodoCompleted):
// the list's author, others when others_can_complete (api/todo). It returns
// nil when nothing changed, or the service message's action.
func (d *Dao) ToggleTodo(b *userbox.Box, ref *TodoRef, completed, incompleted []int32) (*TodoChange, error) {
	if ref.AuthorId != b.UserId() && !ref.List.GetOthersCanComplete() {
		return nil, mtproto.ErrMessageAuthorRequired
	}
	out, done, undone := polls.Toggle(ref.List, ref.Completions, completed, incompleted, mtproto.MakePeerUser(b.UserId()), int32(b.Date()))
	if len(done) == 0 && len(undone) == 0 {
		return nil, nil
	}
	return d.writeTodo(b, ref, ref.List, out, mtproto.MakeTLMessageActionTodoCompletions(&mtproto.MessageAction{
		Completed:   orEmptyInts(done),
		Incompleted: orEmptyInts(undone),
	}).To_MessageAction())
}

func orEmptyInts(v []int32) []int32 {
	if v == nil {
		return []int32{}
	}
	return v
}

// AppendTodo adds items to the list (messages.appendTodoList): the author,
// others when others_can_append.
func (d *Dao) AppendTodo(b *userbox.Box, ref *TodoRef, items []*mtproto.TodoItem) (*TodoChange, error) {
	if ref.AuthorId != b.UserId() && !ref.List.GetOthersCanAppend() {
		return nil, mtproto.ErrMessageAuthorRequired
	}
	list, err := polls.AppendItems(ref.List, items)
	if err != nil {
		return nil, err
	}
	return d.writeTodo(b, ref, list, ref.Completions, mtproto.MakeTLMessageActionTodoAppendTasks(&mtproto.MessageAction{List: items}).To_MessageAction())
}

// EditTodo replaces the list by its author (messages.editMessage,
// api/todo): no service message.
func (d *Dao) EditTodo(b *userbox.Box, ref *TodoRef, next *mtproto.TodoList) (*TodoChange, error) {
	if err := ref.mayEditTodo(b); err != nil {
		return nil, err
	}
	list, kept, err := polls.EditList(next, ref.Completions)
	if err != nil {
		return nil, err
	}
	return d.writeTodo(b, ref, list, kept, nil)
}

// TodoChange is a checklist after a change: the edit journaled for the
// actor, the box of its copy (private chats and groups) and the service
// message to send with it.
type TodoChange struct {
	Update  *mtproto.Update
	Box     *mtproto.MessageBox
	Message *mtproto.Message
	Pts     int32
	Service *mtproto.MessageAction
}

// todoMedia is the media of a checklist in its current state.
func todoMedia(list *mtproto.TodoList, completions []*mtproto.TodoCompletion) *mtproto.MessageMedia {
	return polls.TodoMedia(list, completions)
}

func (d *Dao) writeTodo(b *userbox.Box, ref *TodoRef, list *mtproto.TodoList, completions []*mtproto.TodoCompletion, service *mtproto.MessageAction) (*TodoChange, error) {
	media := todoMedia(list, completions)
	if ref.channel != nil {
		m := proto.Clone(ref.stored).(*mtproto.Message)
		m.Media = media
		ref.channel.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("UPDATE channel_messages SET message_data = ? WHERE channel_id = ? AND id = ?", data, ref.Peer.PeerId, ref.MessageId); err != nil {
			return nil, err
		}
		return &TodoChange{Message: m, Pts: ref.channel.Pts(), Service: service}, nil
	}
	data, err := jsonx.MarshalToString(list)
	if err != nil {
		return nil, err
	}
	comps, err := jsonx.MarshalToString(orEmptyCompletions(completions))
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE todo_lists SET data = ?, completions = ? WHERE dialog_message_id = ?", data, comps, ref.DialogMessageId); err != nil {
		return nil, err
	}
	box, update, err := d.editRowWith(b, ref.row, func(stored *mtproto.Message) *mtproto.Message {
		stored.Media = media
		return stored
	})
	if err != nil {
		return nil, err
	}
	return &TodoChange{Update: update, Box: box, Service: service}, nil
}

func orEmptyCompletions(v []*mtproto.TodoCompletion) []*mtproto.TodoCompletion {
	if v == nil {
		return []*mtproto.TodoCompletion{}
	}
	return v
}

// ApplyTodo brings b's owner's copy of the checklist dialogMessageId to the
// current state. It returns the journaled edit, nil when the owner has no
// copy or the state is what it has.
func (d *Dao) ApplyTodo(b *userbox.Box, dialogMessageId int64) (*mtproto.Update, error) {
	rows, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	list, completions, err := lockTodo(b, dialogMessageId)
	if err != nil || list == nil {
		return nil, err
	}
	_, update, err := d.editRowWith(b, &rows[0], func(stored *mtproto.Message) *mtproto.Message {
		stored.Media = todoMedia(list, completions)
		return stored
	})
	return update, err
}

// ServiceMessageOf is a service message with action, from fromId in peer,
// replying to replyTo: the message the action is about.
func ServiceMessageOf(fromId int64, peer *mtproto.Peer, replyTo int32, action *mtproto.MessageAction, date int64) *mtproto.Message {
	return mtproto.MakeTLMessageService(&mtproto.Message{
		Out:    true,
		FromId: mtproto.MakePeerUser(fromId),
		PeerId: peer,
		ReplyTo: mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
			ReplyToMsgId:           replyTo,
			ReplyToMsgId_INT32:     replyTo,
			ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(replyTo),
		}).To_MessageReplyHeader(),
		Date:   int32(date),
		Action: action,
	}).To_Message()
}

// PostChannelService writes a service message to the channel of b's locked
// transaction, as a post in a broadcast channel. It returns the message
// stored and its pts.
func (d *Dao) PostChannelService(b *userbox.Box, ch *channelbox.Channel, channelId int64, broadcast bool, replyTo int32, action *mtproto.MessageAction, randomId int64) (*ChannelSent, error) {
	service := ServiceMessageOf(b.UserId(), mtproto.MakePeerChannel(channelId), replyTo, action, b.Date())
	service.Id = ch.NextMessageId()
	service.Out = false
	if broadcast {
		service.Post = true
		service.FromId = nil
	}
	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: service}).To_Update(), 1)
	data, err := jsonx.MarshalToString(service)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, message, reply_top_id, date2) VALUES (?, ?, ?, ?, ?, 0, ?, '', 0, ?)",
		channelId, service.Id, b.UserId(), randomId, ch.Pts(), data, b.Date()); err != nil {
		return nil, err
	}
	if err = serviceMoved(b, channelId, service.Id); err != nil {
		return nil, err
	}
	return &ChannelSent{RandomId: randomId, Message: service, Pts: ch.Pts()}, nil
}

// Channel returns the locked channel a ref was read from.
func (ref *TodoRef) Channel() *channelbox.Channel { return ref.channel }

// Broadcast tells whether the channel of ref is a broadcast channel.
func (ref *TodoRef) Broadcast() bool { return ref.sender != nil && ref.sender.ch.Broadcast }
