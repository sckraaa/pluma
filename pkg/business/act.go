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

package business

import (
	"context"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// What a connected bot does for its account (task 105, part 8; ADR 0067):
// the queries it wraps in invokeWithBusinessConnection, and
// messages.uploadMedia with business_connection_id, run as the account
// once Act has checked them.

// Method is a method a connected bot may call for its account: those of
// core.telegram.org/api/bots/connected-business-bots that Pluma serves (the
// gifts, stars and stories of the list are not, ADR 0067).
type Method int

const (
	MethodSendMessage Method = iota + 1
	MethodSendMedia
	MethodSendMultiMedia
	MethodEditMessage
	MethodDeleteMessages
	MethodReadHistory
	MethodSetTyping
	MethodUpdatePinnedMessage
	MethodUpdateProfile
	// MethodUploadMedia is messages.uploadMedia with
	// business_connection_id: it is used "not by wrapping it", the docs
	// say, but it uploads for a chat of the account all the same.
	MethodUploadMedia
)

// ReplyWindow is how recent an incoming message of a chat must be for the
// bot to write there: "send and edit messages in private chats that had
// incoming messages in the last 24 hours"
// (core.telegram.org/constructor/businessBotRights, reply).
const ReplyWindow = 24 * time.Hour

// methodRule is what a method needs of the connection.
type methodRule struct {
	// chat: the method acts in one private chat, given by the caller.
	chat bool
	// right is the right it needs; 0 when the caller checks the rights by
	// what the request does (account.updateProfile, messages.deleteMessages).
	right Rights
	// recent: the chat must have had an incoming message within
	// ReplyWindow.
	recent bool
}

var methodRules = map[Method]methodRule{
	MethodSendMessage:         {chat: true, right: RightReply, recent: true},
	MethodSendMedia:           {chat: true, right: RightReply, recent: true},
	MethodSendMultiMedia:      {chat: true, right: RightReply, recent: true},
	MethodEditMessage:         {chat: true, right: RightReply, recent: true},
	MethodSetTyping:           {chat: true, right: RightReply, recent: true},
	MethodUpdatePinnedMessage: {chat: true, right: RightReply, recent: true},
	MethodUploadMedia:         {chat: true, right: RightReply, recent: true},
	MethodReadHistory:         {chat: true, right: RightReadMessages},
	MethodDeleteMessages:      {},
	MethodUpdateProfile:       {},
}

// Act is a request of a connected bot, checked: it runs as the account
// of the connection, Connection.UserID.
type Act struct {
	Connection *Connection
	// PeerID is the other side of the private chat the request acts in, 0
	// for a method without one.
	PeerID int64
}

// Owner is the account the request runs as.
func (a *Act) Owner() int64 { return a.Connection.UserID }

// BotID is the bot that makes the request.
func (a *Act) BotID() int64 { return a.Connection.BotID }

// ConnectionID is the connection the request came through.
func (a *Act) ConnectionID() string { return a.Connection.ConnectionID }

// Has tells whether the bot has every right of r.
func (a *Act) Has(r Rights) bool { return a.Connection.Rights.Has(r) }

// Act checks the request of botID through its connection connectionID to
// call m, in the private chat with peer for a method of a chat (nil
// otherwise), at now:
//   - the caller is a bot, else BUSINESS_CONNECTION_NOT_ALLOWED ("a user
//     invoked the method");
//   - the connection is the bot's, else BUSINESS_CONNECTION_INVALID;
//   - the bot is in business mode and m may be called through a
//     connection, else BUSINESS_CONNECTION_NOT_ALLOWED;
//   - the chat is a private chat the bot serves (its recipients take it
//     in, it is not paused there), the bot has the right of m and, for a
//     method of the right to reply, the chat had an incoming message within
//     ReplyWindow; else BOT_ACCESS_FORBIDDEN, "the operation isn't
//     permitted over a business connection".
//
// The errors are of core.telegram.org/api/bots/connected-business-bots.
func (s *Store) Act(ctx context.Context, users Users, botID int64, connectionID string, m Method, peer *mtproto.PeerUtil, now time.Time) (*Act, error) {
	bot, err := users.UserGetImmutableUser(ctx, &userpb.TLUserGetImmutableUser{Id: botID})
	if err != nil {
		if errors.Is(err, mtproto.ErrUserIdInvalid) {
			return nil, mtproto.ErrBusinessConnectionNotAllowed
		}
		return nil, err
	}
	if !bot.IsBot() || bot.Deleted() {
		return nil, mtproto.ErrBusinessConnectionNotAllowed
	}
	c, err := s.ConnectionByID(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.BotID != botID {
		return nil, mtproto.ErrBusinessConnectionInvalid
	}
	rule, ok := methodRules[m]
	if !bot.BotBusiness() || !ok {
		return nil, mtproto.ErrBusinessConnectionNotAllowed
	}
	a := &Act{Connection: c}
	if !rule.chat {
		return a, nil
	}
	if peer == nil || peer.PeerType != mtproto.PEER_USER {
		return nil, mtproto.ErrBotAccessForbidden
	}
	a.PeerID = peer.PeerId
	if err = s.CheckChat(ctx, users, c, peer.PeerId); err != nil {
		return nil, err
	}
	if rule.right != 0 && !c.Rights.Has(rule.right) {
		return nil, mtproto.ErrBotAccessForbidden
	}
	if rule.recent {
		last, err := s.LastIncomingAt(ctx, c.UserID, peer.PeerId)
		if err != nil {
			return nil, err
		}
		if !withinReplyWindow(last, now) {
			return nil, mtproto.ErrBotAccessForbidden
		}
	}
	return a, nil
}

// CheckChat checks that c serves the private chat of its account with
// peerID: its recipients take the chat in and the bot is not paused there,
// else BOT_ACCESS_FORBIDDEN.
func (s *Store) CheckChat(ctx context.Context, users Users, c *Connection, peerID int64) error {
	_, served, err := s.ChatPeer(ctx, users, c, peerID)
	if err != nil {
		return err
	}
	if !served {
		return mtproto.ErrBotAccessForbidden
	}
	paused, err := s.Paused(ctx, c.UserID, peerID)
	if err != nil {
		return err
	}
	if paused {
		return mtproto.ErrBotAccessForbidden
	}
	return nil
}

// withinReplyWindow tells whether an incoming message of date last (unix
// time, 0 for none) lets the bot write at now.
func withinReplyWindow(last int64, now time.Time) bool {
	return last > 0 && now.Sub(time.Unix(last, 0)) <= ReplyWindow
}

const lastIncomingOf = "(SELECT date2 FROM messages" +
	" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = ? AND sender_user_id = ?" +
	" ORDER BY user_message_box_id DESC LIMIT 1)"

// lastIncomingQuery reads the last message peerID sent to userID, of the
// live ones and of the deleted ones: a message the account deleted was
// received all the same.
const lastIncomingQuery = lastIncomingOf + " UNION ALL " + lastIncomingOf

// LastIncomingAt is the date (unix time) of the last message peerID sent
// to userID in their private chat, 0 for none.
func (s *Store) LastIncomingAt(ctx context.Context, userID, peerID int64) (int64, error) {
	did := mtproto.MakeDialogId(userID, mtproto.PEER_USER, peerID)
	var rows []struct {
		Date2 int64 `db:"date2"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, lastIncomingQuery,
		userID, did.A, did.B, 0, peerID, userID, did.A, did.B, 1, peerID); err != nil {
		return 0, err
	}
	var last int64
	for _, r := range rows {
		last = max(last, r.Date2)
	}
	return last, nil
}

// SentByBot tells whether m, a message of the account's box, is one the
// bot of a sent through its connection: an outgoing message with
// via_business_bot_id of that bot.
func (a *Act) SentByBot(m *mtproto.Message) bool {
	return m.GetOut() && m.GetViaBusinessBotId().GetValue() == a.BotID()
}

// CheckEdit checks that the bot may edit m, a message of the account's
// box in the chat of the request: "send and edit messages" of the right to
// reply is of the messages the bot sent, else BOT_ACCESS_FORBIDDEN.
func (a *Act) CheckEdit(m *mtproto.Message) error {
	if !a.SentByBot(m) || m.GetPeerId().GetUserId() != a.PeerID {
		return mtproto.ErrBotAccessForbidden
	}
	return nil
}

// CheckDelete checks that the bot may delete boxes, messages of the
// account's box, each in a private chat the bot serves: the messages it
// sent with delete_sent_messages ("delete messages sent by the bot"), the
// incoming ones with delete_received_messages ("delete received private
// messages in managed chats"). The account's own messages are neither,
// and a message of another chat is not the bot's to delete:
// BOT_ACCESS_FORBIDDEN.
func (s *Store) CheckDelete(ctx context.Context, users Users, a *Act, boxes []*mtproto.MessageBox) error {
	checked := map[int64]bool{}
	for _, box := range boxes {
		m := box.GetMessage()
		if box.GetPeerType() != mtproto.PEER_USER {
			return mtproto.ErrBotAccessForbidden
		}
		peerID := box.GetPeerId()
		if !m.GetOut() {
			peerID = box.GetSenderUserId()
		}
		switch {
		case m.GetOut() && a.SentByBot(m):
			if !a.Has(RightDeleteSentMessages) {
				return mtproto.ErrBotAccessForbidden
			}
		case !m.GetOut() && peerID != a.Owner():
			if !a.Has(RightDeleteReceivedMessages) {
				return mtproto.ErrBotAccessForbidden
			}
		default:
			return mtproto.ErrBotAccessForbidden
		}
		if checked[peerID] {
			continue
		}
		if err := s.CheckChat(ctx, users, a.Connection, peerID); err != nil {
			return err
		}
		checked[peerID] = true
	}
	return nil
}

// BotUpdates is u, the answer to a request the bot made for its account,
// as the bot gets it: the account's new and edited messages as
// updateBotNewBusinessMessage and updateBotEditBusinessMessage of the
// connection, and nothing of the account's state. The pts of the
// account's box is not the bot's, and a bot library applies the updates of
// an answer to its own state; the message is the one the bot is told of
// over qts as well, which the answer gives at once, its qts 0 (not
// verified: what Telegram puts in the answer is not published; Bot API
// returns the sent Message from it). userIDs are the users the answer
// names, for its users as the bot sees them.
func (a *Act) BotUpdates(u *mtproto.Updates) (out *mtproto.Updates, userIDs []int64) {
	h := mtproto.NewIDListHelper(a.Owner(), a.BotID())
	var updates []*mtproto.Update
	add := func(m *mtproto.Message, edit bool) {
		if m.GetPredicateName() != mtproto.Predicate_message && m.GetPredicateName() != mtproto.Predicate_messageService {
			return
		}
		h.PickByMessage(m)
		x := &mtproto.Update{ConnectionId: a.ConnectionID(), Message_MESSAGE: m}
		if edit {
			updates = append(updates, mtproto.MakeTLUpdateBotEditBusinessMessage(x).To_Update())
		} else {
			updates = append(updates, mtproto.MakeTLUpdateBotNewBusinessMessage(x).To_Update())
		}
	}
	date := int32(time.Now().Unix())
	switch u.GetPredicateName() {
	case mtproto.Predicate_updates, mtproto.Predicate_updatesCombined:
		date = u.GetDate()
		for _, x := range u.GetUpdates() {
			switch x.GetPredicateName() {
			case mtproto.Predicate_updateNewMessage:
				add(x.GetMessage_MESSAGE(), false)
			case mtproto.Predicate_updateEditMessage:
				add(x.GetMessage_MESSAGE(), true)
			}
		}
	case mtproto.Predicate_updateShort:
		date = u.GetDate()
		switch x := u.GetUpdate(); x.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			add(x.GetMessage_MESSAGE(), false)
		case mtproto.Predicate_updateEditMessage:
			add(x.GetMessage_MESSAGE(), true)
		}
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{},
		Date:    date,
	}).To_Updates(), h.UserIdList
}
