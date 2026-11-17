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
	"context"
	"fmt"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/qtsqueue"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// What the connected bot of Telegram Business is told of the private chats
// it serves (task 105, part 7; ADR 0067, p. 6): the box transaction of the
// account that changes a chat, the account's send, edit and deletion and
// the inbox's taking of the other side's, writes the event
// inbox.botBusinessUpdateToInbox to the bot with the change as the account
// sees it; the bot's box transaction queues it with qts for each of its
// authorizations (QueueBotBusinessUpdate).

// BusinessChat is the private chat of an account with PeerId, a person, as
// the events to its connected bot need it, asked of other services before
// the box transaction: whether PeerId is a contact of the account.
type BusinessChat struct {
	PeerId  int64
	Contact bool
}

// BusinessChatsOf returns the chats of owner with peerIds whose changes its
// connected bot may be told, by peer: none when no bot is connected, and
// only the chats a bot serves (business.ServedUser). users are the peers
// already loaded, nil to load them. Whether the bot is told is decided in
// the box transaction (BusinessBotTx), where the connection is locked.
func (d *Dao) BusinessChatsOf(ctx context.Context, owner int64, peerIds []int64, users *userpb.Vector_ImmutableUser) (map[int64]*BusinessChat, error) {
	peerIds = slices.DeleteFunc(slices.Clone(peerIds), func(id int64) bool { return id == owner || id <= 0 })
	if len(peerIds) == 0 {
		return nil, nil
	}
	c, err := d.Business().Connection(ctx, owner)
	if err != nil || c == nil {
		return nil, err
	}
	if users == nil {
		if users, err = d.UserClient.UserGetMutableUsers(ctx, &userpb.TLUserGetMutableUsers{Id: peerIds}); err != nil {
			return nil, err
		}
	}
	out := make(map[int64]*BusinessChat, len(peerIds))
	for _, id := range peerIds {
		peer, _ := users.GetImmutableUser(id)
		if !business.ServedUser(owner, peer) {
			continue
		}
		contact, err := d.UserClient.UserCheckContact(ctx, &userpb.TLUserCheckContact{UserId: owner, Id: id})
		if err != nil {
			return nil, err
		}
		out[id] = &BusinessChat{PeerId: id, Contact: mtproto.FromBool(contact)}
	}
	return out, nil
}

// BusinessChatOf is BusinessChatsOf of one chat, nil when the bot is not
// told of it.
func (d *Dao) BusinessChatOf(ctx context.Context, owner, peerId int64, users *userpb.Vector_ImmutableUser) (*BusinessChat, error) {
	chats, err := d.BusinessChatsOf(ctx, owner, []int64{peerId}, users)
	if err != nil {
		return nil, err
	}
	return chats[peerId], nil
}

// BusinessBotTx returns the connection of b's owner, read and locked in b,
// whose bot is told of the changes of chat: it serves the chat and is not
// paused there (business.ServesTx). nil when there is none; a connection
// made after chat was asked for is told of the chat's next change.
func (d *Dao) BusinessBotTx(b *userbox.Box, chat *BusinessChat) (*business.Connection, error) {
	if chat == nil {
		return nil, nil
	}
	c, err := business.ConnectionTx(b.Tx(), b.UserId())
	if err != nil || c == nil {
		return nil, err
	}
	ok, err := business.ServesTx(b.Tx(), c, chat.PeerId, chat.Contact)
	if err != nil || !ok {
		return nil, err
	}
	return c, nil
}

// EmitBusinessMessages adds to b the events that tell the bot of c of
// messages new (updateBotNewBusinessMessage) or edited
// (updateBotEditBusinessMessage) in the chat with peerId, which it serves,
// as b's owner sees them, each with the message of b's box it replies to
// (reply_to_message).
// Only messages go: a service message is no message of the chat's
// conversation for the bot (not verified: what Telegram does with them is
// not published).
func (d *Dao) EmitBusinessMessages(b *userbox.Box, c *business.Connection, peerId int64, edit bool, messages ...*mtproto.Message) error {
	if c == nil {
		return nil
	}
	for _, m := range messages {
		if m.GetPredicateName() != mtproto.Predicate_message {
			continue
		}
		reply, err := repliedMessage(b, peerId, m)
		if err != nil {
			return err
		}
		u := &mtproto.Update{ConnectionId: c.ConnectionID, Message_MESSAGE: m, ReplyToMessage: reply}
		if edit {
			u = mtproto.MakeTLUpdateBotEditBusinessMessage(u).To_Update()
		} else {
			u = mtproto.MakeTLUpdateBotNewBusinessMessage(u).To_Update()
		}
		if err = inbox.EmitBotBusinessUpdate(b, d.Topics.Inbox, c.BotID, u); err != nil {
			return err
		}
	}
	return nil
}

// EmitBusinessDeleted adds to b the event that tells the bot of c that the
// messages ids of b's box were deleted in its chat with peerId
// (updateBotDeleteBusinessMessage).
func (d *Dao) EmitBusinessDeleted(b *userbox.Box, c *business.Connection, peerId int64, ids []int32) error {
	if c == nil || len(ids) == 0 {
		return nil
	}
	return inbox.EmitBotBusinessUpdate(b, d.Topics.Inbox, c.BotID, mtproto.MakeTLUpdateBotDeleteBusinessMessage(&mtproto.Update{
		ConnectionId: c.ConnectionID,
		Peer_PEER:    mtproto.MakePeerUser(peerId),
		Messages:     ids,
	}).To_Update())
}

// EmitBusinessDeletedChats adds to b, for each private chat of chats the
// deletion touched, the event of EmitBusinessDeleted with its messages.
func (d *Dao) EmitBusinessDeletedChats(b *userbox.Box, deleted *Deleted, chats map[int64]*BusinessChat) error {
	if deleted == nil || len(chats) == 0 {
		return nil
	}
	byPeer := deleted.PrivateIds(b.UserId())
	for _, peerId := range sortedKeys(byPeer) {
		c, err := d.BusinessBotTx(b, chats[peerId])
		if err != nil {
			return err
		}
		if err = d.EmitBusinessDeleted(b, c, peerId, byPeer[peerId]); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[int64][]int32) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// PrivateIds are the box ids of the deleted messages of private chats with
// other users, by the other user, in ascending order.
func (d *Deleted) PrivateIds(userId int64) map[int64][]int32 {
	out := map[int64][]int32{}
	for _, r := range d.Rows {
		p := peerOfDialog(userId, mtproto.DialogID{A: r.DialogId1, B: r.DialogId2})
		if p.PeerType != mtproto.PEER_USER || p.PeerId == userId {
			continue
		}
		out[p.PeerId] = append(out[p.PeerId], r.UserMessageBoxId)
	}
	return out
}

// repliedMessage is the message of b's box m, a message of the chat with
// peerId, replies to in that chat, nil when there is none: no reply, a
// reply to another chat, a message deleted since.
func repliedMessage(b *userbox.Box, peerId int64, m *mtproto.Message) (*mtproto.Message, error) {
	h := m.GetReplyTo()
	if h.GetPredicateName() != mtproto.Predicate_messageReplyHeader || h.GetReplyFrom() != nil || h.GetReplyToPeerId() != nil {
		return nil, nil
	}
	id := h.GetFixedReplyToMsgId()
	if id == 0 {
		return nil, nil
	}
	row, err := messageById(b, id)
	if err != nil || row == nil {
		return nil, err
	}
	if did := mtproto.MakeDialogId(b.UserId(), mtproto.PEER_USER, peerId); row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, nil
	}
	return storedMessage(row)
}

// errNotBusinessUpdate: an event to a bot with an update it is not told
// with qts.
var errNotBusinessUpdate = fmt.Errorf("%w: not a business update of a bot", userbox.ErrMisuse)

// QueueBotBusinessUpdate queues u, an update of a connection of b's owner,
// a bot, for each authorization of the bot with its next qts
// (qtsqueue.Append), and pushes it there alone, with users and chats, the
// peers it names as the bot sees them, and userIds, their ids. It returns
// the number of authorizations. A bot signed in nowhere gets nothing: its
// next authorization starts with getState, as a bot in a group does.
func (d *Dao) QueueBotBusinessUpdate(b *userbox.Box, u *mtproto.Update, userIds []int64, users []*mtproto.User, chats []*mtproto.Chat) (int, error) {
	if !inbox.BotBusinessUpdate(u.GetPredicateName()) {
		return 0, errNotBusinessUpdate
	}
	keys, err := qtsqueue.Devices(b.Tx(), b.UserId())
	if err != nil {
		return 0, err
	}
	now := int32(b.Date())
	for _, key := range keys {
		one := proto.Clone(u).(*mtproto.Update)
		if _, err = qtsqueue.Append(b.Tx(), qtsqueue.Device{UserId: b.UserId(), AuthKey: key}, one, userIds, now); err != nil {
			return 0, err
		}
		ev, err := newEvent(d.Topics.Sync, &sync.TLSyncUpdatesMe{
			UserId:        b.UserId(),
			PermAuthKeyId: key,
			Updates:       UpdatesOf([]*mtproto.Update{one}, users, chats, now),
		})
		if err != nil {
			return 0, err
		}
		b.Emit(ev)
	}
	return len(keys), nil
}
