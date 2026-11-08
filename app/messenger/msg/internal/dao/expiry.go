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
	"errors"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// Auto-deletion of messages (task 063, core.telegram.org/method/
// messages.setHistoryTTL). A message sent while its chat has a period
// carries ttl_period, and a row of message_expiry (of channel_message_expiry
// for a channel) written in the transaction of the message. Each box deletes
// its own copy when the row is due: no event crosses from one user to
// another, and a restart loses nothing. Several instances of msg may run the
// worker: a box is taken under its lock, and what one took the next does
// not find.

const (
	// ExpiryInterval is how often the worker looks for what is due. The
	// clients delete messages on their own at date + ttl_period (Android
	// MessagesStorage, tasks table), so this bounds only what the server
	// still gives for them.
	ExpiryInterval = 3 * time.Second
	// expiryBoxes is the most boxes, and channels, one pass goes through.
	expiryBoxes = 1000
)

// TTLChange returns the period msgs set, and whether they do: the service
// message messageActionSetMessagesTTL. Sending it is how the period of a
// dialog changes (msg.sendMessageV2).
func TTLChange(msgs []Outgoing) (int32, bool) {
	for _, m := range msgs {
		if historyttl.IsChange(m.Message) {
			return m.Message.GetAction().GetPeriod(), true
		}
	}
	return 0, false
}

// ApplyTTL gives msgs the period ttl of their dialog, whatever period they
// came with (a forward). The service message of a change stays.
func ApplyTTL(msgs []Outgoing, ttl int32) {
	for _, m := range msgs {
		m.Message.TtlPeriod = nil
		if !historyttl.IsChange(m.Message) {
			m.Message.TtlPeriod = mtproto.MakeFlagsInt32(ttl)
		}
	}
}

// DialogTTL is the auto-delete period of b's owner's dialog with the peer,
// 0 for no period or no dialog.
func (d *Dao) DialogTTL(b *userbox.Box, peerType int32, peerId int64) (int32, error) {
	var ttl []int32
	err := b.Tx().QueryRowsPartial(&ttl, "SELECT ttl_period FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), peerType, peerId)
	if err != nil || len(ttl) == 0 {
		return 0, err
	}
	return ttl[0], nil
}

// outgoingExists tells whether a sender's copy of a message of userId
// to the peer exists, even a deleted one: whether its next message would be
// the first of the chat.
const outgoingExistsQuery = "SELECT user_message_box_id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND sender_user_id = ? LIMIT 1"

// HasOutgoing tells whether userId ever wrote to the user peerId.
func (d *Dao) HasOutgoing(ctx context.Context, userId, peerId int64) (bool, error) {
	var ids []int32
	did := boxPeer{PeerType: mtproto.PEER_USER, PeerId: peerId}.dialogId(userId)
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &ids, outgoingExistsQuery, userId, did.A, did.B, userId); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// HasOutgoingTx is HasOutgoing in b's transaction.
func (d *Dao) HasOutgoingTx(b *userbox.Box, peerId int64) (bool, error) {
	var ids []int32
	did := boxPeer{PeerType: mtproto.PEER_USER, PeerId: peerId}.dialogId(b.UserId())
	if err := b.Tx().QueryRowsPartial(&ids, outgoingExistsQuery, b.UserId(), did.A, did.B, b.UserId()); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// SetPeerTTL gives b's owner's dialog with the peer the period and journals
// updatePeerHistoryTTL with seq for every device of the account (ADR 0020):
// a device offline now gets the period from getDifference. users and chats
// are the peer as the owner sees it. It returns the update.
func (d *Dao) SetPeerTTL(b *userbox.Box, peerType int32, peerId int64, period int32, users []*mtproto.User, chats []*mtproto.Chat) (*mtproto.Update, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	if err := updateDialog(b, peer, "ttl_period = ?", period); err != nil {
		return nil, err
	}
	u := historyttl.Update(peer.toPeer(), period)
	if err := d.EmitSeqUpdate(b, u, users, chats); err != nil {
		return nil, err
	}
	return u, nil
}

// scheduleExpiry writes the row that deletes message m of b's box when its
// period ends: date + ttl_period, the time the clients count from
// (core.telegram.org/constructor/message).
func scheduleExpiry(b *userbox.Box, m *mtproto.Message) error {
	ttl := m.GetTtlPeriod().GetValue()
	if ttl == 0 {
		return nil
	}
	_, err := b.Tx().Exec("INSERT INTO message_expiry (user_id, user_message_box_id, expire_at) VALUES (?, ?, ?)",
		b.UserId(), m.GetId(), int64(m.GetDate())+int64(ttl))
	return err
}

// RunExpiry deletes the messages that are due, every ExpiryInterval, until
// ctx is done. A failure is logged: the rows stay and the next pass takes
// them.
func (d *Dao) RunExpiry(ctx context.Context) {
	t := time.NewTicker(ExpiryInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := d.ExpireDue(ctx, time.Now().Unix()); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorf("msg: expire messages: %v", err)
			}
		}
	}
}

// ExpireDue deletes the messages due at now and tells how many it deleted.
// One box or channel that fails does not stop the others; the error is the
// first one met.
func (d *Dao) ExpireDue(ctx context.Context, now int64) (int, error) {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	var users []int64
	note(d.Mysql.DB.QueryRowsPartial(ctx, &users, "SELECT DISTINCT user_id FROM message_expiry WHERE expire_at <= ? LIMIT ?", now, expiryBoxes))
	total := 0
	for _, id := range users {
		n, err := d.expireBox(ctx, id, now)
		note(err)
		total += n
	}

	var channels []int64
	note(d.Mysql.DB.QueryRowsPartial(ctx, &channels, "SELECT DISTINCT channel_id FROM channel_message_expiry WHERE expire_at <= ? LIMIT ?", now, expiryBoxes))
	for _, id := range channels {
		n, err := d.expireChannel(ctx, id, now)
		note(err)
		total += n
	}
	return total, firstErr
}

// expireBox deletes up to a portion of userId's messages due at now: the
// deletion, its update with pts, the rows and the push to every device are
// one transaction. A message deleted by hand before has only its row left.
func (d *Dao) expireBox(ctx context.Context, userId, now int64) (int, error) {
	cctx, cancel := context.WithTimeout(ctx, MaxBoxOpTime)
	defer cancel()
	n := 0
	_, err := d.RunBox(cctx, userId, nil, func(b *userbox.Box) error {
		var ids []int32
		if err := b.Tx().QueryRowsPartial(&ids,
			"SELECT user_message_box_id FROM message_expiry WHERE user_id = ? AND expire_at <= ? ORDER BY user_id, user_message_box_id LIMIT ? FOR UPDATE",
			userId, now, maxPortion); err != nil || len(ids) == 0 {
			return err
		}
		deleted, err := d.DeleteMessages(b, ids)
		if err != nil {
			return err
		}
		args := make([]any, 0, len(ids)+1)
		args = append(args, userId)
		for _, id := range ids {
			args = append(args, id)
		}
		if _, err = b.Tx().Exec("DELETE FROM message_expiry WHERE user_id = ? AND user_message_box_id IN ("+inList(len(ids))+")", args...); err != nil {
			return err
		}
		if deleted == nil {
			return nil
		}
		n = len(deleted.Rows)
		return d.EmitUpdatesNotMe(b, userId, 0, mtproto.MakeUpdatesByUpdates(deleted.Update))
	})
	return n, err
}

// expireChannel deletes up to a portion of the channel's messages due at
// now, in the transaction of its creator's box, which only takes the lock:
// no user does this, and channelbox has no transaction of its own (ADR
// 0034). One row of the channel's journal, updateDeleteChannelMessages,
// tells the members.
func (d *Dao) expireChannel(ctx context.Context, channelId, now int64) (int, error) {
	var creators []struct {
		CreatorUserId int64 `db:"creator_user_id"`
		Deleted       bool  `db:"deleted"`
	}
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &creators, "SELECT creator_user_id, deleted FROM channels WHERE id = ?", channelId); err != nil {
		return 0, err
	}
	if len(creators) == 0 || creators[0].Deleted {
		_, err := d.Mysql.DB.Exec(ctx, "DELETE FROM channel_message_expiry WHERE channel_id = ?", channelId)
		return 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, MaxBoxOpTime)
	defer cancel()
	n := 0
	_, err := d.RunBox(cctx, creators[0].CreatorUserId, nil, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, channelId)
		if err != nil {
			if errors.Is(err, channelbox.ErrNoChannel) {
				return nil
			}
			return err
		}
		var ids []int32
		if err = b.Tx().QueryRowsPartial(&ids,
			"SELECT message_id FROM channel_message_expiry WHERE channel_id = ? AND expire_at <= ? ORDER BY channel_id, message_id LIMIT ? FOR UPDATE",
			channelId, now, maxPortion); err != nil || len(ids) == 0 {
			return err
		}
		in := "(?" + strings.Repeat(", ?", len(ids)-1) + ")"
		args := make([]any, 0, len(ids)+1)
		args = append(args, channelId)
		for _, id := range ids {
			args = append(args, id)
		}
		var alive []int32
		if err = b.Tx().QueryRowsPartial(&alive, "SELECT id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN "+in+" ORDER BY id FOR UPDATE", args...); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("DELETE FROM channel_message_expiry WHERE channel_id = ? AND message_id IN "+in, args...); err != nil {
			return err
		}
		if len(alive) == 0 {
			return nil
		}
		n = len(alive)
		in = "(?" + strings.Repeat(", ?", len(alive)-1) + ")"
		args = args[:1]
		for _, id := range alive {
			args = append(args, id)
		}
		if _, err = b.Tx().Exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id IN "+in, args...); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("DELETE FROM channel_message_mentions WHERE channel_id = ? AND message_id IN "+in, args...); err != nil {
			return err
		}
		// The top message is read again, among the messages left.
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = (SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND deleted = 0) WHERE id = ?", channelId, channelId); err != nil {
			return err
		}
		count := int32(len(alive))
		ch.AddPtsUpdate(mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: channelId, Messages: alive}).To_Update(), count)
		return d.EmitChannelUpdatesPush(b, channelId, 0, mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{
			ChannelId: channelId,
			Messages:  alive,
			Pts_INT32: ch.Pts(),
			PtsCount:  count,
		}).To_Update())
	})
	return n, err
}
