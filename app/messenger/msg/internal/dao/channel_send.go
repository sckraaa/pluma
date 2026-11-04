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
	"errors"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Messages of channels and supergroups (task 039, ADR 0034): one copy in
// channel_messages, one row of the channel's journal each, in the sender's
// box transaction with the channel locked after the sender.

// Rights of chatBannedRights and chatAdminRights the send checks; the full
// rules of rights are task 042.
const (
	bannedSendMessages = 1 << 1
	adminPostMessages  = mtproto.ADMIN_POST_MESSAGES
)

// ChannelSent is a message sent to a channel.
type ChannelSent struct {
	RandomId int64
	Message  *mtproto.Message // as stored: out not set
	Pts      int32
}

type senderRow struct {
	Broadcast           bool  `db:"broadcast"`
	Deleted             bool  `db:"deleted"`
	DefaultBannedRights int32 `db:"default_banned_rights"`
	Role                int32 `db:"role"`
	State               int32 `db:"state"`
	AdminRights         int32 `db:"admin_rights"`
	BannedRights        int32 `db:"banned_rights"`
	UntilDate           int32 `db:"until_date"`
	Member              bool  `db:"member"`
}

// channelSender checks that b's owner may post to the channel: a member in
// good standing; in a channel (broadcast) its creator or an admin who may
// post; in a supergroup one not banned from sending.
func channelSender(b *userbox.Box, channelId int64) (*senderRow, error) {
	var rows []senderRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT c.broadcast, c.deleted, c.default_banned_rights, COALESCE(p.role, 0) AS role, COALESCE(p.state, 1) AS state,"+
			" COALESCE(p.admin_rights, 0) AS admin_rights, COALESCE(p.banned_rights, 0) AS banned_rights, COALESCE(p.until_date, 0) AS until_date,"+
			" p.user_id IS NOT NULL AS member"+
			" FROM channels c LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ?",
		b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Deleted {
		return nil, mtproto.ErrChannelInvalid
	}
	s := &rows[0]
	switch {
	case !s.Member || s.State != 0:
		return nil, mtproto.ErrChannelPrivate
	case s.Role == 2:
		return s, nil
	case s.Broadcast:
		if s.Role == 1 && s.AdminRights&adminPostMessages != 0 {
			return s, nil
		}
		return nil, mtproto.ErrChatWriteForbidden
	case s.Role == 1:
		return s, nil
	case s.DefaultBannedRights&bannedSendMessages != 0, s.BannedRights&bannedSendMessages != 0 && (s.UntilDate == 0 || int64(s.UntilDate) > b.Date()):
		return nil, mtproto.ErrChatWriteForbidden
	}
	return s, nil
}

type channelSentRow struct {
	Id          int32  `db:"id"`
	RandomId    int64  `db:"random_id"`
	Pts         int32  `db:"pts"`
	MessageData string `db:"message_data"`
}

// ChannelSentBefore returns the messages b's owner already sent to the
// channel with randomIds, all of them, or nil when it sent none of them:
// a repeated send gets the first answer, a partial repeat is
// RANDOM_ID_DUPLICATE (ADR 0034, p. 11; the unique key of out_random_id
// keeps it beyond any cache).
func (d *Dao) ChannelSentBefore(b *userbox.Box, channelId int64, randomIds []int64) ([]ChannelSent, error) {
	if len(randomIds) == 0 {
		return nil, nil
	}
	args := []any{channelId, b.UserId()}
	for _, id := range randomIds {
		args = append(args, id)
	}
	var rows []channelSentRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, random_id, pts, message_data FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND out_random_id IN (?"+strings.Repeat(", ?", len(randomIds)-1)+") ORDER BY id",
		args...); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, nil
	case len(rows) != len(randomIds):
		return nil, mtproto.ErrRandomIdDuplicate
	}
	out := make([]ChannelSent, 0, len(rows))
	for _, r := range rows {
		var m mtproto.Message
		if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, err
		}
		out = append(out, ChannelSent{RandomId: r.RandomId, Message: &m, Pts: r.Pts})
	}
	return out, nil
}

// SendChannel writes the messages of b's owner to the channel: each gets the
// channel's next id and pts, as a post in a channel (no author) or from the
// owner in a supergroup. mentioned are the users the messages mention or
// reply to, other than the owner, who get rows of channel_message_mentions.
// The owner's dialog moves to the last message, read.
func (d *Dao) SendChannel(b *userbox.Box, channelId int64, outgoing []Outgoing) (sent []ChannelSent, mentioned []int64, err error) {
	s, err := channelSender(b, channelId)
	if err != nil {
		return nil, nil, err
	}
	ch, err := channelbox.Open(b, channelId)
	if err != nil {
		if errors.Is(err, channelbox.ErrNoChannel) {
			return nil, nil, mtproto.ErrChannelInvalid
		}
		return nil, nil, err
	}
	for _, o := range outgoing {
		m := proto.Clone(o.Message).(*mtproto.Message)
		m.Id = ch.NextMessageId()
		m.Out = false
		m.PeerId = mtproto.MakePeerChannel(channelId)
		m.Date = int32(b.Date())
		if s.Broadcast {
			m.Post = true
			m.FromId = nil
		} else {
			m.FromId = mtproto.MakePeerUser(b.UserId())
		}
		// Mentions: the users of the text's mentions, the author of the
		// message replied to.
		var who []int64
		for _, e := range m.GetEntities() {
			if id := e.GetUserId_INT64(); id != 0 {
				who = append(who, id)
			}
		}
		if r := m.GetReplyTo().GetReplyToMsgId(); r != 0 {
			var authors []int64
			if err = b.Tx().QueryRowsPartial(&authors,
				"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0", channelId, r); err != nil {
				return nil, nil, err
			}
			if len(authors) == 0 {
				m.ReplyTo = nil
			} else if !s.Broadcast {
				who = append(who, authors[0])
			}
		}
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, nil, err
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, message, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			channelId, m.Id, b.UserId(), o.RandomId, ch.Pts(), mtproto.GetMediaType(m), data, m.GetMessage(), b.Date()); err != nil {
			return nil, nil, err
		}
		for _, id := range who {
			if id == b.UserId() {
				continue
			}
			if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?)", channelId, id, m.Id); err != nil {
				return nil, nil, err
			}
			if !slices.Contains(mentioned, id) {
				mentioned = append(mentioned, id)
			}
		}
		sent = append(sent, ChannelSent{RandomId: o.RandomId, Message: m, Pts: ch.Pts()})
	}
	last := sent[len(sent)-1].Message.Id
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", last, channelId); err != nil {
		return nil, nil, err
	}
	b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), mtproto.PEER_CHANNEL, channelId))
	res, err := b.Tx().Exec("UPDATE dialogs SET top_message = ?, read_inbox_max_id = ?, date2 = ?, deleted = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		last, last, b.Date(), b.UserId(), mtproto.PEER_CHANNEL, channelId)
	if err != nil {
		return nil, nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, nil, err
	}
	if n == 0 {
		if _, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)",
			b.UserId(), mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId), last, last, b.Date()); err != nil {
			return nil, nil, err
		}
	}
	return sent, mentioned, nil
}

// EmitChannelPush adds the push of the new messages of the channel to its
// members (sync.pushChannelUpdates, keyed by the channel).
func (d *Dao) EmitChannelPush(b *userbox.Box, channelId, permAuthKeyId int64, sent []ChannelSent, mentioned []int64) error {
	updates := make([]*mtproto.Update, 0, len(sent))
	for _, s := range sent {
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: s.Message, Pts_INT32: s.Pts, PtsCount: 1}).To_Update())
	}
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Mentioned:     mentioned,
		Updates:       mtproto.MakeUpdatesByUpdates(updates...),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}
