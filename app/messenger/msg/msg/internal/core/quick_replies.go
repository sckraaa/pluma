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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Quick replies of Telegram Business (task 105, ADR 0067;
// core.telegram.org/api/business#quick-reply-shortcuts). A message goes to a
// shortcut as it would go to Saved Messages, with quick_reply_shortcut; the
// answer and the other devices get updateQuickReplyMessage, and
// updateNewQuickReply for a shortcut the send created. Renaming and
// ordering tell the other devices updateQuickReplies, deleting
// updateDeleteQuickReply. All of them go with seq (userbox.SeqUpdate).

// shortcutOf reads the shortcut a send names: by name (tdesktop always,
// data_shortcut_messages.cpp:783-790 ShortcutIdToMTP; iOS for a shortcut it
// has no id for yet, PendingMessageManager.swift:1339-1345), which creates
// a shortcut by that name, or by id (iOS for a known one; Android
// SendMessagesHelper.java:5010-5019). A name the clients would not let one
// type, or no shortcut, is SHORTCUT_INVALID.
func shortcutOf(in *mtproto.InputQuickReplyShortcut) (name string, id int32, err error) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputQuickReplyShortcut:
		if !dao.ValidShortcutName(in.GetShortcut()) {
			return "", 0, mtproto.ErrShortcutInvalid
		}
		return in.GetShortcut(), 0, nil
	case mtproto.Predicate_inputQuickReplyShortcutId:
		if in.GetShortcutId() <= 0 {
			return "", 0, mtproto.ErrShortcutInvalid
		}
		return "", in.GetShortcutId(), nil
	}
	return "", 0, mtproto.ErrShortcutInvalid
}

// prepareQuickReplies makes the messages as a shortcut keeps them: the
// owner's outgoing messages in its Saved Messages, without a sublist and
// without a reply (a reply means nothing once the message goes to another
// chat). The id, the date and the shortcut are set as they are read
// (dao.QuickReplyRow.Message).
func prepareQuickReplies(list []*msg.OutboxMessage, userId int64) []dao.Outgoing {
	out := make([]dao.Outgoing, len(list))
	for i, o := range list {
		m := o.Message
		m.Out = true
		m.PeerId = mtproto.MakePeerUser(userId)
		m.FromId = mtproto.MakePeerUser(userId)
		m.SavedPeerId = nil
		m.ReplyTo = nil
		m.ScheduleRepeatPeriod = nil
		m.QuickReplyShortcutId = nil
		out[i] = dao.Outgoing{RandomId: o.RandomId, Message: m}
	}
	return out
}

// quickReplyPeers are the users and the chats msgs name, as userId sees
// them, the owner among the users.
func (c *MsgCore) quickReplyPeers(userId int64, msgs []*mtproto.Message) ([]*mtproto.User, []*mtproto.Chat, error) {
	h := mtproto.NewIDListHelper(userId)
	for _, m := range msgs {
		h.PickByMessage(m)
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{userId}})
	if err != nil {
		return nil, nil, err
	}
	chats, err := c.namedChats(userId, h)
	if err != nil {
		return nil, nil, err
	}
	return users.GetUserListByIdList(userId, h.UserIdList...), chats, nil
}

func findQuickReply(list []*dao.QuickReply, id int32) *dao.QuickReply {
	if i := slices.IndexFunc(list, func(q *dao.QuickReply) bool { return q.Id == id }); i >= 0 {
		return list[i]
	}
	return nil
}

// MsgAddQuickReplyMessages
// msg.addQuickReplyMessages user_id:long auth_key_id:long shortcut:InputQuickReplyShortcut message:Vector<OutboxMessage> = Updates;
//
// Adds messages to a shortcut of the user, creating it when a name names
// none (core.telegram.org/api/business: "if the shortcut doesn't exist, it
// will be created"). The answer is updateMessageID and
// updateQuickReplyMessage of each message, after updateNewQuickReply of a
// shortcut created: iOS matches its pending message by the first two
// (ApplyUpdateMessage.swift:304-355), tdesktop moves its local shortcut to
// the id of updateNewQuickReply with the same name
// (data_shortcut_messages.cpp:283-305). The other devices get the same
// with seq. A send repeated with the same random ids gets the first answer.
func (c *MsgCore) MsgAddQuickReplyMessages(in *msg.TLMsgAddQuickReplyMessages) (*mtproto.Updates, error) {
	r, err := c.addQuickReplyMessages(in)
	if err != nil {
		c.Logger.Errorf("msg.addQuickReplyMessages: user %d - error: %v", in.UserId, err)
		return nil, err
	}
	return r, nil
}

func (c *MsgCore) addQuickReplyMessages(in *msg.TLMsgAddQuickReplyMessages) (*mtproto.Updates, error) {
	list := in.GetMessage()
	switch {
	case len(list) == 0:
		return nil, mtproto.ErrInputRequestInvalid
	case len(list) > business.QuickReplyMessagesLimit:
		return nil, mtproto.ErrReplyMessagesTooMuch
	}
	for _, o := range list {
		if o.GetMessage() == nil {
			return nil, mtproto.ErrInputRequestInvalid
		}
		// A shortcut has no time to go out at. Telegram names no error for
		// both together (not verified).
		if o.GetScheduleDate() != nil {
			return nil, mtproto.ErrScheduleDateInvalid
		}
	}
	name, id, err := shortcutOf(in.GetShortcut())
	if err != nil {
		return nil, err
	}
	repeat, err := randomIds(list)
	if err != nil {
		return nil, err
	}
	// The automatic entities and the link preview, as for a message to
	// Saved Messages, which has no bot.
	for _, o := range list {
		o.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, o.Message, in.UserId, o.NoWebpage, func() bool { return false })
	}
	msgs := prepareQuickReplies(list, in.UserId)
	prepared := make([]*mtproto.Message, len(msgs))
	for i, m := range msgs {
		prepared[i] = m.Message
	}
	users, chats, err := c.quickReplyPeers(in.UserId, prepared)
	if err != nil {
		return nil, err
	}

	var (
		rows    []*dao.QuickReplyRow
		created *dao.QuickReply
	)
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		rows, created = nil, nil
		if rows, err = c.svcCtx.Dao.QuickRepliesBefore(b, repeat); err != nil {
			return err
		}
		if rows != nil {
			// The first answer had updateNewQuickReply if that send created
			// the shortcut: its first message is one of these.
			all, err := c.svcCtx.Dao.QuickRepliesIn(b)
			if err != nil {
				return err
			}
			if q := findQuickReply(all, rows[0].ShortcutId); q != nil && slices.ContainsFunc(rows, func(r *dao.QuickReplyRow) bool { return r.Id == q.Top.Id }) {
				created = q
			}
			return nil
		}
		var s *dao.QuickReplyShortcut
		if id != 0 {
			s, err = c.svcCtx.Dao.LockShortcut(b, id)
		} else {
			s, err = c.svcCtx.Dao.LockShortcutByName(b, name)
		}
		if err != nil {
			return err
		}
		isNew := false
		if s == nil {
			if id != 0 {
				return mtproto.ErrShortcutInvalid
			}
			if s, err = c.svcCtx.Dao.CreateShortcut(b, name); err != nil {
				return err
			}
			isNew = true
		}
		if rows, err = c.svcCtx.Dao.InsertQuickReplyMessages(b, s.Id, msgs); err != nil {
			return err
		}
		messages, err := dao.QuickReplyMessages(rows)
		if err != nil {
			return err
		}
		if isNew {
			created = &dao.QuickReply{Id: s.Id, Shortcut: s.Shortcut, Count: int32(len(rows)), Top: rows[0]}
			if err = c.svcCtx.Dao.EmitSeqUpdateExcept(b, dao.NewQuickReplyUpdate(created), []*mtproto.User{}, []*mtproto.Chat{}, in.AuthKeyId); err != nil {
				return err
			}
		}
		for _, u := range dao.QuickReplyMessageUpdates(messages) {
			if err = c.svcCtx.Dao.EmitSeqUpdateExcept(b, u, users, chats, in.AuthKeyId); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	messages, err := dao.QuickReplyMessages(rows)
	if err != nil {
		return nil, err
	}
	updates := make([]*mtproto.Update, 0, 2*len(messages)+1)
	if created != nil {
		updates = append(updates, dao.NewQuickReplyUpdate(created))
	}
	for i, u := range dao.QuickReplyMessageUpdates(messages) {
		if rows[i].RandomId != 0 {
			updates = append(updates, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: rows[i].Id, RandomId: rows[i].RandomId}).To_Update())
		}
		updates = append(updates, u)
	}
	return dao.UpdatesOf(updates, users, chats, int32(time.Now().Unix())), nil
}

// MsgGetQuickReplies
// msg.getQuickReplies user_id:long hash:long = messages.QuickReplies;
//
// The shortcuts in the user's order with their first messages; the same
// hash as the clients count is quickRepliesNotModified.
func (c *MsgCore) MsgGetQuickReplies(in *msg.TLMsgGetQuickReplies) (*mtproto.Messages_QuickReplies, error) {
	list, err := c.svcCtx.Dao.LoadQuickReplies(c.ctx, in.UserId)
	if err != nil {
		c.Logger.Errorf("msg.getQuickReplies: user %d - error: %v", in.UserId, err)
		return nil, err
	}
	if in.Hash != 0 && in.Hash == dao.QuickRepliesHash(list) {
		return mtproto.MakeTLMessagesQuickRepliesNotModified(nil).To_Messages_QuickReplies(), nil
	}
	tops := make([]*dao.QuickReplyRow, len(list))
	for i, q := range list {
		tops[i] = q.Top
	}
	messages, err := dao.QuickReplyMessages(tops)
	if err != nil {
		return nil, err
	}
	users, chats, err := c.quickReplyPeers(in.UserId, messages)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesQuickReplies(&mtproto.Messages_QuickReplies{
		QuickReplies: dao.QuickRepliesOf(list),
		Messages:     messages,
		Chats:        chats,
		Users:        users,
	}).To_Messages_QuickReplies(), nil
}

// MsgGetQuickReplyMessages
// msg.getQuickReplyMessages user_id:long shortcut_id:int id:Vector<int> hash:long = messages.Messages;
//
// The messages of a shortcut of the user, newest first as a history is
// answered, or those with id. Another account's shortcut, or none, is
// SHORTCUT_INVALID (Android MessagesController.java:11591 takes it for an
// empty shortcut).
func (c *MsgCore) MsgGetQuickReplyMessages(in *msg.TLMsgGetQuickReplyMessages) (*mtproto.Messages_Messages, error) {
	s, err := c.svcCtx.Dao.ShortcutOf(c.ctx, in.UserId, in.ShortcutId)
	if err != nil {
		c.Logger.Errorf("msg.getQuickReplyMessages: user %d - error: %v", in.UserId, err)
		return nil, err
	}
	if s == nil {
		return nil, mtproto.ErrShortcutInvalid
	}
	rows, err := c.svcCtx.Dao.LoadQuickReplyMessages(c.ctx, s.Id, in.Id)
	if err != nil {
		return nil, err
	}
	if len(in.Id) == 0 && in.Hash != 0 && in.Hash == dao.QuickReplyMessagesHash(rows) {
		return mtproto.MakeTLMessagesMessagesNotModified(&mtproto.Messages_Messages{Count: int32(len(rows))}).To_Messages_Messages(), nil
	}
	slices.Reverse(rows)
	messages, err := dao.QuickReplyMessages(rows)
	if err != nil {
		return nil, err
	}
	users, chats, err := c.quickReplyPeers(in.UserId, messages)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: messages,
		Chats:    chats,
		Users:    users,
	}).To_Messages_Messages(), nil
}

// MsgCheckQuickReplyShortcut
// msg.checkQuickReplyShortcut user_id:long shortcut:string = Bool;
//
// Whether a message can be added to the shortcut named so: a valid name of
// no shortcut while the user has fewer than quick_replies_limit, or of a
// shortcut with fewer than quick_reply_messages_limit messages. The
// documentation tells only that the method checks "whether the specified
// shortcut name can be used" (not verified what boolFalse means to
// Telegram); no official client calls it.
func (c *MsgCore) MsgCheckQuickReplyShortcut(in *msg.TLMsgCheckQuickReplyShortcut) (*mtproto.Bool, error) {
	if !dao.ValidShortcutName(in.Shortcut) {
		return mtproto.BoolFalse, nil
	}
	s, err := c.svcCtx.Dao.ShortcutByName(c.ctx, in.UserId, in.Shortcut)
	if err != nil {
		c.Logger.Errorf("msg.checkQuickReplyShortcut: user %d - error: %v", in.UserId, err)
		return nil, err
	}
	if s == nil {
		n, err := c.svcCtx.Dao.CountShortcuts(c.ctx, in.UserId)
		if err != nil {
			return nil, err
		}
		return mtproto.ToBool(n < business.QuickRepliesLimit), nil
	}
	n, err := c.svcCtx.Dao.CountQuickReplyMessages(c.ctx, s.Id)
	if err != nil {
		return nil, err
	}
	return mtproto.ToBool(n < business.QuickReplyMessagesLimit), nil
}

// emitQuickReplies tells the user's other devices the whole list as the
// transaction leaves it (updateQuickReplies, api/business: renaming and
// reordering).
func (c *MsgCore) emitQuickReplies(b *userbox.Box, authKeyId int64) error {
	list, err := c.svcCtx.Dao.QuickRepliesIn(b)
	if err != nil {
		return err
	}
	return c.svcCtx.Dao.EmitSeqUpdateExcept(b, dao.QuickRepliesUpdate(list), []*mtproto.User{}, []*mtproto.Chat{}, authKeyId)
}

// MsgEditQuickReplyShortcut
// msg.editQuickReplyShortcut user_id:long auth_key_id:long shortcut_id:int shortcut:string = Bool;
//
// Renames a shortcut of the user: an invalid name or another account's
// shortcut is SHORTCUT_INVALID (/method/messages.editQuickReplyShortcut), a
// name another shortcut has, case aside, SHORTCUT_OCCUPIED.
func (c *MsgCore) MsgEditQuickReplyShortcut(in *msg.TLMsgEditQuickReplyShortcut) (*mtproto.Bool, error) {
	if !dao.ValidShortcutName(in.Shortcut) {
		return nil, mtproto.ErrShortcutInvalid
	}
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		changed, err := c.svcCtx.Dao.RenameShortcut(b, in.ShortcutId, in.Shortcut)
		if err != nil || !changed {
			return err
		}
		return c.emitQuickReplies(b, in.AuthKeyId)
	})
	if err != nil {
		c.Logger.Errorf("msg.editQuickReplyShortcut: user %d, shortcut %d - error: %v", in.UserId, in.ShortcutId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// MsgReorderQuickReplies
// msg.reorderQuickReplies user_id:long auth_key_id:long order:Vector<int> = Bool;
//
// Orders the user's shortcuts as order lists them; ids that are not its
// shortcuts are passed over, those order leaves out keep their order after
// the others.
func (c *MsgCore) MsgReorderQuickReplies(in *msg.TLMsgReorderQuickReplies) (*mtproto.Bool, error) {
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		changed, err := c.svcCtx.Dao.ReorderShortcuts(b, in.Order)
		if err != nil || !changed {
			return err
		}
		return c.emitQuickReplies(b, in.AuthKeyId)
	})
	if err != nil {
		c.Logger.Errorf("msg.reorderQuickReplies: user %d - error: %v", in.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// MsgDeleteQuickReplyShortcut
// msg.deleteQuickReplyShortcut user_id:long auth_key_id:long shortcut_id:int = Bool;
//
// Deletes a shortcut of the user with its messages; the other devices get
// updateDeleteQuickReply alone (api/business: "only an
// updateDeleteQuickReply"). Another account's shortcut, or none, is
// SHORTCUT_INVALID.
func (c *MsgCore) MsgDeleteQuickReplyShortcut(in *msg.TLMsgDeleteQuickReplyShortcut) (*mtproto.Bool, error) {
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		found, err := c.svcCtx.Dao.DeleteShortcut(b, in.ShortcutId)
		if err != nil {
			return err
		}
		if !found {
			return mtproto.ErrShortcutInvalid
		}
		return c.svcCtx.Dao.EmitSeqUpdateExcept(b, dao.DeleteQuickReplyUpdate(in.ShortcutId), []*mtproto.User{}, []*mtproto.Chat{}, in.AuthKeyId)
	})
	if err != nil {
		c.Logger.Errorf("msg.deleteQuickReplyShortcut: user %d, shortcut %d - error: %v", in.UserId, in.ShortcutId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
