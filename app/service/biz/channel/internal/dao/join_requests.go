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
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Requests to join a channel (task 041, channel_join_requests): written and
// answered in a box transaction with the channel locked. Those who may
// answer them (creator, admins with invite_users) get the requests pending
// as updatePendingJoinRequests with seq, the actor by its own transaction
// and every other by an inbox event (ADR 0021). The account of the one who
// asks gets nothing: the clients show INVITE_REQUEST_SENT
// (tdesktop api_chat_invite.cpp:182, JoinChannel.swift:41).

const (
	// RecentRequestersMax is how many of the latest requesters
	// updatePendingJoinRequests and channelFull name, as many as the
	// clients show (biz chat's RecentRequestersMax).
	RecentRequestersMax = 3
	// RequestsHideMax is the most requests one
	// messages.hideAllChatJoinRequests answers; the transaction holds the
	// channel. The rest stay pending and the admins see them.
	RequestsHideMax = 1000
)

type querier func(dest any, query string, args ...any) error

// requestsFrom is the requests not answered yet, of accounts that exist.
const requestsFrom = " FROM channel_join_requests r JOIN users u ON u.id = r.user_id WHERE r.channel_id = ? AND u.deleted = 0"

// pendingUpdate is updatePendingJoinRequests of the channel now, by q: the
// count and the latest requesters, newest first.
func pendingUpdate(q querier, channelId int64) (*mtproto.Update, error) {
	var (
		count  []int32
		recent []int64
	)
	if err := q(&count, "SELECT COUNT(*)"+requestsFrom, channelId); err != nil {
		return nil, err
	}
	if err := q(&recent, "SELECT r.user_id"+requestsFrom+" ORDER BY r.date2 DESC, r.user_id DESC LIMIT ?", channelId, RecentRequestersMax); err != nil {
		return nil, err
	}
	if recent == nil {
		recent = []int64{}
	}
	return mtproto.MakeTLUpdatePendingJoinRequests(&mtproto.Update{
		Peer_PEER:        mtproto.MakePeerChannel(channelId),
		RequestsPending:  first(count),
		RecentRequesters: recent,
	}).To_Update(), nil
}

// PendingRequests is updatePendingJoinRequests of the channel now.
func (d *Dao) PendingRequests(ctx context.Context, channelId int64) (*mtproto.Update, error) {
	return pendingUpdate(func(dest any, query string, args ...any) error {
		return d.Mysql.QueryRowsPartial(ctx, dest, query, args...)
	}, channelId)
}

// tellRequests gives those who may answer the requests of the channel the
// requests pending now, and returns them. The actor's own devices get the
// update without the requesters: the box calls no service, its answer
// carries them, and a difference gets them from the fold.
func (d *Dao) tellRequests(b *userbox.Box, channelId int64) (*mtproto.Update, error) {
	u, err := pendingUpdate(b.Tx().QueryRowsPartial, channelId)
	if err != nil {
		return nil, err
	}
	var admins []int64
	if err = b.Tx().QueryRowsPartial(&admins,
		"SELECT user_id FROM channel_participants WHERE channel_id = ? AND state = 0 AND (role = ? OR (role = ? AND admin_rights & ? <> 0)) ORDER BY user_id",
		channelId, channelperm.RoleCreator, channelperm.RoleAdmin, mtproto.ADMIN_INVITE_USERS); err != nil {
		return nil, err
	}
	for _, id := range admins {
		if id == b.UserId() {
			err = d.EmitSeqUpdate(b, u, nil, nil)
		} else {
			err = inbox.EmitSeqInbox(b, d.Topics.Inbox, id, u)
		}
		if err != nil {
			return nil, err
		}
	}
	return u, nil
}

// addRequest writes the request of b's owner to join; a second request
// changes nothing. link is the hash asked through, empty for the public
// username.
func (d *Dao) addRequest(b *userbox.Box, channelId int64, link string) error {
	res, err := b.Tx().Exec("INSERT IGNORE INTO channel_join_requests (channel_id, user_id, link, date2) VALUES (?, ?, ?, ?)",
		channelId, b.UserId(), link, b.Date())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = d.tellRequests(b, channelId)
	return err
}

// dropRequests removes the requests of ids, which no longer stand (added,
// kicked or joined otherwise), and tells the admins when one did.
func (d *Dao) dropRequests(b *userbox.Box, channelId int64, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, channelId)
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := b.Tx().Exec("DELETE FROM channel_join_requests WHERE channel_id = ? AND user_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = d.tellRequests(b, channelId)
	return err
}

// Hidden is what HideRequests wrote: the service messages of the approved
// (one each, of a supergroup), the users approved and the requests left.
type Hidden struct {
	Changes  []*ChannelChange
	Approved []int64
	Pending  *mtproto.Update
}

type requestRow struct {
	UserId int64  `db:"user_id"`
	Link   string `db:"link"`
}

// HideRequests answers requests to join of the channel, by b's owner, who
// must be the creator or an admin with invite_users: of userId, or with all
// every request (of link when given). Approved, each user joins as if it
// had joined by itself: a service message from it in a supergroup, the
// participant row, the count, the use of its link; its dialog and
// updateChannel by an inbox event as for one invited.
//
// A request that is not there is no error and changes nothing: a second
// approval (of another admin, or the same one again) finds the user in the
// channel. iOS sends the answer with retryRequest, which would repeat it
// without end on an error (InvitationLinks.swift:14-16).
//
// With all, the users that cannot join are left with their request; of
// one user it is the error: USER_CHANNELS_TOO_MUCH, USERS_TOO_MUCH.
func (d *Dao) HideRequests(b *userbox.Box, channelId int64, approved, all bool, link string, userId int64) (*Hidden, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = a.linkAdmin(b.Date()); err != nil {
		return nil, err
	}
	if link != "" && !all {
		return nil, mtproto.ErrInputRequestInvalid
	}
	if link != "" {
		var found []int64
		if err = b.Tx().QueryRowsPartial(&found, "SELECT chat_id FROM chat_invites WHERE link = ? AND chat_id = ?", link, channelId); err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, mtproto.ErrInviteHashExpired
		}
	}

	query := "SELECT user_id, link FROM channel_join_requests WHERE channel_id = ?"
	args := []any{channelId}
	switch {
	case !all:
		query += " AND user_id = ?"
		args = append(args, userId)
	case link != "":
		query += " AND link = ?"
		args = append(args, link)
	}
	var rows []requestRow
	if err = b.Tx().QueryRowsPartial(&rows, query+" ORDER BY date2, user_id LIMIT ? FOR UPDATE", append(args, RequestsHideMax)...); err != nil {
		return nil, err
	}

	h := &Hidden{}
	if len(rows) > 0 {
		if approved {
			err = d.approve(b, ch, channelId, rows, all, h)
		} else {
			ids := make([]int64, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.UserId)
			}
			err = d.dropRequests(b, channelId, ids...)
		}
		if err != nil {
			return nil, err
		}
	}
	if h.Pending, err = pendingUpdate(b.Tx().QueryRowsPartial, channelId); err != nil {
		return nil, err
	}
	return h, nil
}

// approve admits the users of rows, the requests of those in, gone or kicked
// being dropped, and tells the admins once.
func (d *Dao) approve(b *userbox.Box, ch *channelbox.Channel, channelId int64, rows []requestRow, all bool, h *Hidden) error {
	c, err := memberChannel(b, channelId)
	if err != nil {
		return err
	}
	top := c.TopMessage
	var drop []int64
	for _, r := range rows {
		t, err := rightsTarget(b, channelId, r.UserId)
		if err != nil {
			return err
		}
		if t.in() || t.kicked(b.Date()) {
			drop = append(drop, r.UserId)
			continue
		}
		var joined []int64
		if err = b.Tx().QueryRowsPartial(&joined, "SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = 0 LIMIT ?", r.UserId, ChannelsLimit); err != nil {
			return err
		}
		if len(joined) >= ChannelsLimit {
			if !all {
				return mtproto.ErrUserChannelsTooMuch
			}
			continue
		}
		if c.ParticipantsCount+int32(len(h.Approved)) >= MembersMax {
			if !all {
				return mtproto.ErrUsersTooMuch
			}
			break
		}

		change := &ChannelChange{ChannelId: channelId}
		if !c.Broadcast {
			// The member's own message: the clients show it as approved
			// to the member and as accepted to the others
			// (MessageObject.java:5711-5716); a channel tells its
			// subscribers nothing.
			if change.Message, change.Pts, err = addServiceMessageFrom(b, ch, channelId, r.UserId, false, mtproto.MakeMessageActionChatJoinedByRequest()); err != nil {
				return err
			}
			top = max(top, change.Message.GetId())
			h.Changes = append(h.Changes, change)
		}
		var availableMinId int32
		if c.HiddenPrehistory && !c.Broadcast {
			availableMinId = change.Message.GetId()
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, state, inviter_user_id, available_min_id, `date`) VALUES (?, ?, 0, 0, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE role = 0, state = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0, kicked_by = 0,"+
			" inviter_user_id = VALUES(inviter_user_id), available_min_id = VALUES(available_min_id), `date` = VALUES(`date`)",
			channelId, r.UserId, b.UserId(), availableMinId, b.Date()); err != nil {
			return err
		}
		if r.Link != "" {
			if err = recordInviteUse(b, channelId, r.Link, r.UserId, b.UserId()); err != nil {
				return err
			}
		}
		ev := &inbox.TLInboxChannelInvitedToInbox{UserId: r.UserId, FromId: b.UserId(), ChannelId: channelId}
		data, err := jsonx.Marshal(ev)
		if err != nil {
			return err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Inbox, Recipient: r.UserId, Method: string(proto.MessageName(ev)), Data: data})
		h.Approved = append(h.Approved, r.UserId)
		drop = append(drop, r.UserId)
	}
	if len(h.Approved) > 0 {
		if _, err = b.Tx().Exec("UPDATE channels SET participants_count = participants_count + ?, top_message = ? WHERE id = ?", len(h.Approved), top, channelId); err != nil {
			return err
		}
	}
	return d.dropRequests(b, channelId, drop...)
}

// recordInviteUse records that userId joined by link (approved by
// approvedBy, 0 by itself): a new user of the link counts once in its
// usage, one coming back does not, so that the usage is the number of
// importers the clients page through (InviteLinkBottomSheet.java:1038-1040).
func recordInviteUse(b *userbox.Box, channelId int64, link string, userId, approvedBy int64) error {
	var seen []int64
	if err := b.Tx().QueryRowsPartial(&seen, "SELECT id FROM chat_invite_participants WHERE link = ? AND user_id = ?", link, userId); err != nil {
		return err
	}
	if len(seen) == 0 {
		if _, err := b.Tx().Exec("UPDATE chat_invites SET usage2 = usage2 + 1 WHERE link = ?", link); err != nil {
			return err
		}
	}
	_, err := b.Tx().Exec("INSERT INTO chat_invite_participants (chat_id, link, user_id, requested, approved_by, date2) VALUES (?, ?, ?, 0, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE chat_id = new.chat_id, requested = 0, approved_by = new.approved_by, date2 = new.date2",
		channelId, link, userId, approvedBy, b.Date())
	return err
}

// Importer is one who joined by a link or asks to join.
type Importer struct {
	UserId     int64 `db:"user_id"`
	Date       int64 `db:"date2"`
	ApprovedBy int64 `db:"approved_by"`
}

// ImportersMax is the most importers of one page.
const ImportersMax = 100

// Importers is a page of those who joined by the links of the channel (of
// link when given) or, with requested, of the requests pending (with q: by
// the beginning of a name or the username), newest first, with the count of
// all that match. The offset is the last row of the page before:
// the date and the user (the clients send both,
// MemberRequestsController.java:49-54).
func (d *Dao) Importers(ctx context.Context, channelId int64, requested bool, link, q string, offsetDate int32, offsetUser int64, limit int32) ([]Importer, int32, error) {
	table, approvedBy := "chat_invite_participants", "r.approved_by"
	where := " WHERE r.chat_id = ? AND r.requested = 0"
	if requested {
		table, approvedBy = "channel_join_requests", "0 AS approved_by"
		where = " WHERE r.channel_id = ?"
	}
	from := " FROM " + table + " r JOIN users u ON u.id = r.user_id" + where + " AND u.deleted = 0"
	args := []any{channelId}
	if link != "" {
		from += " AND r.link = ?"
		args = append(args, link)
	}
	if q = strings.TrimSpace(q); q != "" && requested {
		like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		from += " AND (u.first_name LIKE ? OR u.last_name LIKE ? OR CONCAT(u.first_name, ' ', u.last_name) LIKE ? OR u.username LIKE ?)"
		args = append(args, like, like, like, like)
	}
	var count []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*)"+from, args...); err != nil {
		return nil, 0, err
	}
	if offsetDate != 0 {
		from += " AND (r.date2, r.user_id) < (?, ?)"
		args = append(args, offsetDate, offsetUser)
	}
	if limit <= 0 || limit > ImportersMax {
		limit = ImportersMax
	}
	var rows []Importer
	err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT r.user_id, r.date2, "+approvedBy+from+" ORDER BY r.date2 DESC, r.user_id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	return rows, first(count), nil
}

// LeftChannelsMax is the most channels of a page of channels.getLeftChannels.
const LeftChannelsMax = 100

// LeftChannels is a page of the channels userId left (not those it was
// kicked from), with their count.
func (d *Dao) LeftChannels(ctx context.Context, userId int64, offset int32) ([]int64, int32, error) {
	const from = " FROM channel_participants p JOIN channels c ON c.id = p.channel_id WHERE p.user_id = ? AND p.state = 1 AND c.deleted = 0"
	var count []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*)"+from, userId); err != nil {
		return nil, 0, err
	}
	var ids []int64
	if err := d.Mysql.QueryRowsPartial(ctx, &ids, "SELECT p.channel_id"+from+" ORDER BY p.channel_id LIMIT ? OFFSET ?", userId, LeftChannelsMax, max(offset, 0)); err != nil {
		return nil, 0, err
	}
	return ids, first(count), nil
}
