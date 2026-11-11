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

// Package sendas decides as whom a member may write to a supergroup
// (task 079): channels.getSendAs, the send_as of the sending methods,
// messages.saveDefaultSendAs and channelFull.default_send_as.
//
// The peers of a supergroup (core.telegram.org/api/channel, "Sending
// messages on behalf of owned channels and groups"):
//   - the member itself, unless it is an admin with the anonymous right;
//   - the supergroup itself, for an admin with the anonymous right;
//   - the broadcast channels the member created, for an admin of the
//     supergroup, and in a public supergroup or a discussion group, where
//     Android asks for the list (ChatObject.canSendAsPeers,
//     ChatObject.java:1979: megagroup && (public || has_geo || has_link)).
//
// Only these may be send_as. A broadcast channel offers itself alone, its
// default ("The current channel (default)", core.telegram.org/api/channel):
// its posts come from the channel, signed by channels.toggleSignatures.
// Android names it as send_as of every post of a channel without
// signatures (ChatObject.getSendAsPeerId, ChatObject.java:2268-2269;
// SendMessagesHelper.java:5420-5421), so it must pass.
package sendas

import (
	"context"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// Querier reads rows: a transaction (*sqlx.Tx), or a database with OnDB.
type Querier interface {
	QueryRowsPartial(v any, query string, args ...any) error
}

type onDB struct {
	ctx context.Context
	db  *sqlx.DB
}

func (q onDB) QueryRowsPartial(v any, query string, args ...any) error {
	return q.db.QueryRowsPartial(q.ctx, v, query, args...)
}

// OnDB is a Querier of the database outside a transaction.
func OnDB(ctx context.Context, db *sqlx.DB) Querier {
	return onDB{ctx: ctx, db: db}
}

// Member is what the choice reads of a user in a supergroup.
type Member struct {
	// Megagroup: a supergroup, not a broadcast channel.
	Megagroup bool
	// Public: the supergroup has a username.
	Public bool
	// Discussion: the supergroup discusses a broadcast channel.
	Discussion bool
	// Admin: an admin or the creator of the supergroup; Anonymous: one
	// with the anonymous right.
	Admin, Anonymous bool
}

// Choices are the peers userId may write to the supergroup groupId as, the
// default first: the user itself or, for an anonymous admin, the group
// (tdesktop falls back to the group then, SendAsPeers::ResolveChosen,
// send_as_peers.cpp:139). owned are the broadcast channels userId created.
// A broadcast channel groupId offers itself alone.
func Choices(userId, groupId int64, m Member, owned []int64) []*mtproto.Peer {
	if !m.Megagroup {
		return []*mtproto.Peer{mtproto.MakePeerChannel(groupId)}
	}
	out := make([]*mtproto.Peer, 0, 1+len(owned))
	if m.Anonymous {
		out = append(out, mtproto.MakePeerChannel(groupId))
	} else {
		out = append(out, mtproto.MakePeerUser(userId))
	}
	if m.Admin || m.Public || m.Discussion {
		for _, id := range owned {
			if id != groupId {
				out = append(out, mtproto.MakePeerChannel(id))
			}
		}
	}
	return out
}

// Allowed tells whether as is one of choices. A peer is compared by its
// kind and id.
func Allowed(choices []*mtproto.Peer, as *mtproto.Peer) bool {
	for _, p := range choices {
		if p.GetPredicateName() == as.GetPredicateName() && p.GetUserId() == as.GetUserId() && p.GetChannelId() == as.GetChannelId() {
			return true
		}
	}
	return false
}

type memberRow struct {
	Megagroup    bool  `db:"megagroup"`
	Deleted      bool  `db:"deleted"`
	LinkedChatId int64 `db:"linked_chat_id"`
	Public       bool  `db:"public"`
	Role         int32 `db:"role"`
	State        int32 `db:"state"`
	AdminRights  int32 `db:"admin_rights"`
}

// Peers are the peers userId may write to channelId as (Choices).
// CHANNEL_INVALID for a channel that does not exist, CHANNEL_PRIVATE for
// one userId may not write to: not in it and no guest of a discussion
// group (core.telegram.org/method/channels.getSendAs).
func Peers(q Querier, userId, channelId int64) ([]*mtproto.Peer, error) {
	var rows []memberRow
	if err := q.QueryRowsPartial(&rows,
		"SELECT c.megagroup, c.deleted, c.linked_chat_id,"+
			" EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1) AS public,"+
			" COALESCE(p.role, 0) AS role, COALESCE(p.state, ?) AS state, COALESCE(p.admin_rights, 0) AS admin_rights"+
			" FROM channels c LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ?",
		mtproto.PEER_CHANNEL, channelperm.StateLeft, userId, channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Deleted {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	m := Member{Megagroup: r.Megagroup, Public: r.Public, Discussion: r.Megagroup && r.LinkedChatId != 0}
	switch r.State {
	case channelperm.StateIn:
		m.Admin = r.Role == channelperm.RoleAdmin || r.Role == channelperm.RoleCreator
		m.Anonymous = m.Admin && r.AdminRights&mtproto.ADMIN_ANONYMOUS != 0
	case channelperm.StateKicked:
		return nil, mtproto.ErrChannelPrivate
	default:
		guest, err := discussionGuest(q, userId, channelId)
		if err != nil {
			return nil, err
		}
		if !guest {
			return nil, mtproto.ErrChannelPrivate
		}
	}
	if !m.Megagroup {
		return Choices(userId, channelId, m, nil), nil
	}
	owned, err := ownedChannels(q, userId)
	if err != nil {
		return nil, err
	}
	return Choices(userId, channelId, m, owned), nil
}

// Check answers whether userId may write to channelId as as: nil, or
// SEND_AS_PEER_INVALID (core.telegram.org/method/messages.saveDefaultSendAs),
// or the error of Peers.
func Check(q Querier, userId, channelId int64, as *mtproto.Peer) error {
	peers, err := Peers(q, userId, channelId)
	if err != nil {
		return err
	}
	if !Allowed(peers, as) {
		return mtproto.ErrSendAsPeerInvalid
	}
	return nil
}

// discussionGuest tells whether userId, not in the supergroup channelId,
// may write to it as a member of the broadcast channel it discusses, which
// does not ask commenters to join (join_to_send).
func discussionGuest(q Querier, userId, channelId int64) (bool, error) {
	var ok []int64
	err := q.QueryRowsPartial(&ok,
		"SELECT b.id FROM channels g JOIN channels b ON b.id = g.linked_chat_id AND b.broadcast = 1 AND b.deleted = 0 AND b.linked_chat_id = g.id"+
			" JOIN channel_participants p ON p.channel_id = b.id AND p.user_id = ? AND p.state = ?"+
			" WHERE g.id = ? AND g.megagroup = 1 AND g.join_to_send = 0",
		userId, channelperm.StateIn, channelId)
	return len(ok) > 0, err
}

// ownedChannels are the broadcast channels userId created and still has,
// ascending.
func ownedChannels(q Querier, userId int64) ([]int64, error) {
	var ids []int64
	err := q.QueryRowsPartial(&ids,
		"SELECT c.id FROM channel_participants p JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.user_id = ? AND p.state = ? AND p.role = ? AND c.broadcast = 1 AND c.deleted = 0 AND c.blocked = 0 ORDER BY c.id",
		userId, channelperm.StateIn, channelperm.RoleCreator)
	return ids, err
}

// Store keeps the peer each user chose to write to a supergroup as by
// default (messages.saveDefaultSendAs).
type Store struct {
	db *sqlx.DB
}

// New is a Store over the database.
func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Peers is Peers outside a transaction.
func (s *Store) Peers(ctx context.Context, userId, channelId int64) ([]*mtproto.Peer, error) {
	return Peers(OnDB(ctx, s.db), userId, channelId)
}

// Check is Check outside a transaction.
func (s *Store) Check(ctx context.Context, userId, channelId int64, as *mtproto.Peer) error {
	return Check(OnDB(ctx, s.db), userId, channelId, as)
}

// SaveDefault remembers as as the default of userId in channelId, one of
// Peers. The first of them, what the clients fall back to, is kept as no
// choice.
func (s *Store) SaveDefault(ctx context.Context, userId, channelId int64, as *mtproto.Peer) error {
	peers, err := s.Peers(ctx, userId, channelId)
	if err != nil {
		return err
	}
	if !Allowed(peers, as) {
		return mtproto.ErrSendAsPeerInvalid
	}
	if Allowed(peers[:1], as) {
		_, err = s.db.Exec(ctx, "DELETE FROM channel_default_send_as WHERE user_id = ? AND channel_id = ?", userId, channelId)
		return err
	}
	_, err = s.db.Exec(ctx, "INSERT INTO channel_default_send_as (user_id, channel_id, send_as_id) VALUES (?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE send_as_id = VALUES(send_as_id)", userId, channelId, as.GetChannelId())
	return err
}

// Default is the peer userId chose for channelId, nil for none or for one
// it may no longer write as (channelFull.default_send_as: the clients fall
// back without it, tdesktop data_channel.cpp:1561-1565).
func (s *Store) Default(ctx context.Context, userId, channelId int64) (*mtproto.Peer, error) {
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids,
		"SELECT send_as_id FROM channel_default_send_as WHERE user_id = ? AND channel_id = ?", userId, channelId); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	peers, err := s.Peers(ctx, userId, channelId)
	if err != nil {
		if errors.Is(err, mtproto.ErrChannelInvalid) || errors.Is(err, mtproto.ErrChannelPrivate) {
			return nil, nil
		}
		return nil, err
	}
	as := mtproto.MakePeerChannel(ids[0])
	if !Allowed(peers, as) {
		return nil, nil
	}
	return as, nil
}

// Author is the peer a message of a supergroup comes from: the channel it
// was written as (channel_messages.send_as_id), or its sender.
func Author(senderUserId, sendAsId int64) *mtproto.Peer {
	if sendAsId != 0 {
		return mtproto.MakePeerChannel(sendAsId)
	}
	return mtproto.MakePeerUser(senderUserId)
}

// AuthorRow is a row of authors read by AuthorsQuery.
type AuthorRow struct {
	SenderUserId int64 `db:"sender_user_id"`
	SendAsId     int64 `db:"send_as_id"`
}

// Peer is the author of the row.
func (r AuthorRow) Peer() *mtproto.Peer {
	return Author(r.SenderUserId, r.SendAsId)
}
