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
	"math/rand"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The messages of a quick reply shortcut after they are added (task 105,
// ADR 0067; core.telegram.org/api/business#quick-reply-shortcuts): an edit
// (messages.editMessage with quick_reply_shortcut_id), a delete
// (messages.deleteQuickReplyMessages) and the send of the shortcut to a
// chat (messages.sendQuickReplyMessages). The edit and the delete answer
// updateQuickReplyMessage and updateDeleteQuickReplyMessages, which the
// owner's other devices get with seq.

// deleteQuickReplyIdsMax bounds the ids of one delete: iOS deletes in
// parts of 100 (ManagedCloudChatRemoveMessagesOperations.swift:170-172),
// and a shortcut holds far fewer messages.
const deleteQuickReplyIdsMax = 100

// MsgEditQuickReplyMessage
// msg.editQuickReplyMessage user_id:long auth_key_id:long shortcut_id:int message:OutboxMessage = Updates;
//
// Replaces a message of a shortcut of the user with the one messages.
// editMessage made of it. A shortcut of another account, or none, is
// SHORTCUT_INVALID; a message the shortcut does not hold MESSAGE_ID_INVALID.
// The answer is updateQuickReplyMessage, which iOS applies from the answer
// (RequestEditMessage.swift:229, stateManager.addUpdates) and tdesktop too
// (api_editing.cpp:341-356).
func (c *MsgCore) MsgEditQuickReplyMessage(in *msg.TLMsgEditQuickReplyMessage) (*mtproto.Updates, error) {
	r, err := c.editQuickReplyMessage(in)
	if err != nil {
		c.Logger.Errorf("msg.editQuickReplyMessage: user %d, shortcut %d - error: %v", in.UserId, in.ShortcutId, err)
		return nil, err
	}
	return r, nil
}

func (c *MsgCore) editQuickReplyMessage(in *msg.TLMsgEditQuickReplyMessage) (*mtproto.Updates, error) {
	o := in.GetMessage()
	switch {
	case o.GetMessage() == nil || o.GetMessage().GetId() <= 0:
		return nil, mtproto.ErrMessageIdInvalid
	case o.GetScheduleDate() != nil:
		// A shortcut has no time to go out at, as for an add.
		return nil, mtproto.ErrScheduleDateInvalid
	}
	id := o.GetMessage().GetId()
	// The automatic entities and the link preview of the new text, as an
	// add makes them for a message in Saved Messages, which has no bot.
	o.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, o.Message, in.UserId, o.NoWebpage, func() bool { return false })
	edited := prepareQuickReplies([]*msg.OutboxMessage{o}, in.UserId)[0].Message
	users, chats, err := c.quickReplyPeers(in.UserId, []*mtproto.Message{edited})
	if err != nil {
		return nil, err
	}

	var row *dao.QuickReplyRow
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		s, err := c.svcCtx.Dao.LockShortcut(b, in.ShortcutId)
		if err != nil {
			return err
		}
		if s == nil {
			return mtproto.ErrShortcutInvalid
		}
		old, err := c.svcCtx.Dao.LockQuickReplyMessage(b, s.Id, id)
		if err != nil {
			return err
		}
		if old == nil {
			return mtproto.ErrMessageIdInvalid
		}
		if row, err = c.svcCtx.Dao.UpdateQuickReplyMessage(b, old, edited); err != nil {
			return err
		}
		m, err := row.Message()
		if err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitSeqUpdateExcept(b, dao.QuickReplyMessageUpdate(m), users, chats, in.AuthKeyId)
	})
	if err != nil {
		return nil, err
	}
	m, err := row.Message()
	if err != nil {
		return nil, err
	}
	return dao.UpdatesOf([]*mtproto.Update{dao.QuickReplyMessageUpdate(m)}, users, chats, int32(time.Now().Unix())), nil
}

// MsgDeleteQuickReplyMessages
// msg.deleteQuickReplyMessages user_id:long auth_key_id:long shortcut_id:int id:Vector<int> = Updates;
//
// Deletes messages of a shortcut of the user; ids the shortcut does not
// hold are passed over. A shortcut of another account, or none, is
// SHORTCUT_INVALID. The answer is updateDeleteQuickReplyMessages of the
// messages deleted, which tdesktop, Android and iOS apply from the answer
// (data_histories.cpp:1040-1045, MessagesController.java:9466-9469,
// ManagedCloudChatRemoveMessagesOperations.swift:176-181); with none
// deleted it is empty.
func (c *MsgCore) MsgDeleteQuickReplyMessages(in *msg.TLMsgDeleteQuickReplyMessages) (*mtproto.Updates, error) {
	r, err := c.deleteQuickReplyMessages(in)
	if err != nil {
		c.Logger.Errorf("msg.deleteQuickReplyMessages: user %d, shortcut %d - error: %v", in.UserId, in.ShortcutId, err)
		return nil, err
	}
	return r, nil
}

func (c *MsgCore) deleteQuickReplyMessages(in *msg.TLMsgDeleteQuickReplyMessages) (*mtproto.Updates, error) {
	ids := slices.Compact(slices.Sorted(slices.Values(in.GetId())))
	if len(ids) > deleteQuickReplyIdsMax {
		return nil, mtproto.ErrInputRequestInvalid
	}
	var deleted []int32
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		s, err := c.svcCtx.Dao.LockShortcut(b, in.ShortcutId)
		if err != nil {
			return err
		}
		if s == nil {
			return mtproto.ErrShortcutInvalid
		}
		if deleted, err = c.svcCtx.Dao.DeleteQuickReplyMessages(b, s.Id, ids); err != nil || len(deleted) == 0 {
			return err
		}
		return c.svcCtx.Dao.EmitSeqUpdateExcept(b, dao.DeleteQuickReplyMessagesUpdate(s.Id, deleted), []*mtproto.User{}, []*mtproto.Chat{}, in.AuthKeyId)
	})
	if err != nil {
		return nil, err
	}
	var updates []*mtproto.Update
	if len(deleted) > 0 {
		updates = append(updates, dao.DeleteQuickReplyMessagesUpdate(in.ShortcutId, deleted))
	}
	return dao.UpdatesOf(updates, []*mtproto.User{}, []*mtproto.Chat{}, int32(time.Now().Unix())), nil
}

// MsgSendQuickReplyMessages
// msg.sendQuickReplyMessages user_id:long auth_key_id:long peer_id:long shortcut_id:int id:Vector<int> random_id:Vector<long> = Updates;
//
// Sends the messages of a shortcut of the user to the private chat with
// peer_id: all of them when id is empty, as tdesktop and iOS ask
// (apiwrap.cpp:4482-4495, QuickReplyMessages.swift:393), or those with id,
// as Android asks (QuickRepliesController.java:773-830).
func (c *MsgCore) MsgSendQuickReplyMessages(in *msg.TLMsgSendQuickReplyMessages) (*mtproto.Updates, error) {
	r, err := c.sendQuickReplies(quickReplySend{
		userId:     in.UserId,
		authKeyId:  in.AuthKeyId,
		peerId:     in.PeerId,
		shortcutId: in.ShortcutId,
		ids:        in.GetId(),
		randomIds:  in.GetRandomId(),
	})
	if err != nil {
		c.Logger.Errorf("msg.sendQuickReplyMessages: user %d, shortcut %d - error: %v", in.UserId, in.ShortcutId, err)
		return nil, err
	}
	return r, nil
}

// quickReplySend is a send of the messages of a shortcut to a private chat:
// by its owner (messages.sendQuickReplyMessages), or, on the owner's
// behalf, by the server (the automatic replies of Telegram Business).
type quickReplySend struct {
	userId int64
	// authKeyId is the session that gets the answer, 0 for none.
	authKeyId  int64
	peerId     int64
	shortcutId int32
	// ids are the messages to send, every message of the shortcut when
	// empty; randomIds are their random ids in the same order, or one for
	// each message of the shortcut when ids is empty. Without them the
	// server makes them.
	ids       []int32
	randomIds []int64
	// randomIdsFor, when set, gives the random ids of the n messages sent
	// in place of randomIds: the automatic replies derive them from their
	// row, so that a send tried again is found by them (SentBefore).
	randomIdsFor func(n int) []int64
	// offline marks the messages sent by the server on the owner's behalf
	// (message.offline: "sent because of a scheduled action by the
	// sender, e.g. as away, or a greeting service message",
	// core.telegram.org/constructor/message).
	offline bool
	// opt changes the send: its after runs in the owner's transaction once
	// the messages are written.
	opt sendOptions
}

// sendQuickReplies sends a shortcut's messages, in the order they were
// added, through the send of a private chat: the recipient's privacy,
// blocks and the sender's restriction apply as for any message. The
// recipient must be another user, not a bot (core.telegram.org/api/
// business: "only to users, in private chats with other users"; whether a
// bot is refused is not verified): PEER_ID_INVALID otherwise.
func (c *MsgCore) sendQuickReplies(s quickReplySend) (*mtproto.Updates, error) {
	if len(s.ids) > 0 && len(s.randomIds) > 0 && len(s.ids) != len(s.randomIds) {
		return nil, mtproto.ErrRandomIdInvalid
	}
	if s.peerId <= 0 || s.peerId == s.userId {
		return nil, mtproto.ErrPeerIdInvalid
	}
	shortcut, err := c.svcCtx.Dao.ShortcutOf(c.ctx, s.userId, s.shortcutId)
	if err != nil {
		return nil, err
	}
	if shortcut == nil {
		return nil, mtproto.ErrShortcutInvalid
	}
	rows, err := c.svcCtx.Dao.LoadQuickReplyMessages(c.ctx, shortcut.Id, s.ids)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	randomIds := s.randomIds
	if s.randomIdsFor != nil {
		randomIds = s.randomIdsFor(len(rows))
	}
	randomOf, err := quickReplyRandomIds(rows, s.ids, randomIds)
	if err != nil {
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{s.peerId}, To: []int64{s.userId}})
	if err != nil {
		return nil, err
	}
	if peer, _ := users.GetImmutableUser(s.peerId); peer == nil || peer.IsBot() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	outBox, err := quickReplyOutbox(rows, randomOf, s.userId, s.peerId, int32(time.Now().Unix()), rand.Int63) //nolint:gosec // the id of an album, not a secret
	if err != nil {
		return nil, err
	}
	if s.offline {
		for _, o := range outBox {
			o.Message.Offline = true
		}
	}
	in := &msg.TLMsgSendMessageV2{UserId: s.userId, AuthKeyId: s.authKeyId, PeerType: mtproto.PEER_USER, PeerId: s.peerId, Message: outBox}
	if err = c.checkRestriction(in); err != nil {
		return nil, err
	}
	// The messages went through plugin.RemakeMessage when they were added
	// (for Saved Messages, without a bot, as for this chat with a user): a
	// second pass would add their automatic entities again, as for a
	// scheduled message.
	opt := s.opt
	opt.queued = true
	return c.send(in, opt)
}

// quickReplyRandomIds pairs the rows with random ids: by the id each was
// asked with, or, when the send names no ids, in the order of the rows; a
// row without one gets a new one. random_id must match id one to one
// (core.telegram.org/method/messages.sendQuickReplyMessages):
// RANDOM_ID_INVALID otherwise.
func quickReplyRandomIds(rows []*dao.QuickReplyRow, ids []int32, randomIds []int64) ([]int64, error) {
	out := make([]int64, len(rows))
	switch {
	case len(randomIds) == 0:
	case len(ids) > 0:
		of := make(map[int32]int64, len(ids))
		for i, id := range ids {
			of[id] = randomIds[i]
		}
		for i, r := range rows {
			out[i] = of[r.Id]
		}
	case len(randomIds) != len(rows):
		return nil, mtproto.ErrRandomIdInvalid
	default:
		copy(out, randomIds)
	}
	for i := range out {
		for out[i] == 0 {
			out[i] = rand.Int63() //nolint:gosec // G404: a random_id is unique, not secret
		}
	}
	if dao.RepeatedRandomIds(out) {
		return nil, mtproto.ErrRandomIdDuplicate
	}
	return out, nil
}

// quickReplyOutbox makes the messages of rows as fromId sends them now to
// the private chat with peerId: new messages of the chat, without the
// shortcut, the id and the edit date they have in it. A forward added to
// the shortcut goes as the owner's own message, without its header (not
// verified what Telegram keeps). An album goes as a new album: each
// grouped_id of the shortcut gets one from newGroup.
func quickReplyOutbox(rows []*dao.QuickReplyRow, randomIds []int64, fromId, peerId int64, date int32, newGroup func() int64) ([]*msg.OutboxMessage, error) {
	groups := make(map[int64]int64)
	out := make([]*msg.OutboxMessage, len(rows))
	for i, r := range rows {
		m, err := r.Message()
		if err != nil {
			return nil, err
		}
		m.Id = 0
		m.Date = date
		m.EditDate = nil
		m.EditHide = false
		m.QuickReplyShortcutId = nil
		m.Out = true
		m.FromId = mtproto.MakePeerUser(fromId)
		m.PeerId = mtproto.MakePeerUser(peerId)
		m.SavedPeerId = nil
		m.FwdFrom = nil
		m.ReplyTo = nil
		m.Pinned = false
		m.Views, m.Forwards, m.Replies, m.Reactions = nil, nil, nil, nil
		m.GroupedId = nil
		if r.GroupedId != 0 {
			g, ok := groups[r.GroupedId]
			if !ok {
				g = newGroup()
				groups[r.GroupedId] = g
			}
			m.GroupedId = mtproto.MakeFlagsInt64(g)
		}
		out[i] = msg.MakeTLOutboxMessage(&msg.OutboxMessage{RandomId: randomIds[i], Message: m}).To_OutboxMessage()
	}
	return out, nil
}
