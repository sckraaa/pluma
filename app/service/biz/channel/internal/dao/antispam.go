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
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/antispam"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The antispam of supergroups (task 082, ADR 0050): msg judges each message
// in its send transaction (pkg/antispam) and, for one that breaks a rule,
// emits an event that deletes it here, in the sender's box, as the antispam
// account; an admin reports a deletion that was wrong.

// AntispamDelete deletes for everyone the messages ids of b's owner in the
// supergroup channelId, which the antispam judged spam by rule, and keeps in
// the recent actions that the antispam account deleted each
// (channelAdminLogEventActionDeleteMessage from antispam.UserId, which the
// clients show as the antispam's). It is the consumer of an event and may
// run again: ids already deleted, of another sender or a topic's root are
// left out, and nothing is done for a channel that is gone or whose
// antispam went off meanwhile.
func (d *Dao) AntispamDelete(b *userbox.Box, channelId int64, ids []int32, rule int32) (*Deleted, error) {
	out := &Deleted{ChannelId: channelId}
	ids = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id int32) bool { return id <= 0 })
	if len(ids) == 0 {
		return out, nil
	}
	links, err := discussionLinks(b, channelId, ids)
	if err != nil {
		return nil, err
	}
	ch, err := openWithLinked(b, channelId, links)
	if errors.Is(err, mtproto.ErrChannelInvalid) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var on []bool
	if err = b.Tx().QueryRowsPartial(&on, "SELECT antispam FROM channels WHERE id = ? AND megagroup = 1 AND deleted = 0", channelId); err != nil {
		return nil, err
	}
	if len(on) == 0 || !on[0] {
		return out, nil
	}
	roots, err := topicsOf(b, channelId, ids)
	if err != nil {
		return nil, err
	}
	args := []any{channelId, b.UserId()}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []deleteRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT id, sender_user_id, COALESCE(message_data, '') AS message_data FROM channel_messages"+
			" WHERE channel_id = ? AND sender_user_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") FOR UPDATE",
		args...); err != nil {
		return nil, err
	}
	var gone []int32
	for _, r := range rows {
		if !slices.Contains(roots, r.Id) {
			gone = append(gone, r.Id)
		}
	}
	if out, err = deleteRows(b, ch, channelId, gone); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if !slices.Contains(gone, r.Id) {
			continue
		}
		var m *mtproto.Message
		if r.MessageData != "" {
			if err = jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
				return nil, err
			}
		}
		if m != nil {
			if err = adminlog.WriteBy(b, channelId, antispam.UserId, mtproto.MakeTLChannelAdminLogEventActionDeleteMessage(&mtproto.ChannelAdminLogEventAction{
				Message: m,
			}).To_ChannelAdminLogEventAction(), m.GetMessage()); err != nil {
				return nil, err
			}
		}
		if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_antispam_deletions (channel_id, message_id, sender_user_id, rule, rules_version, `date`) VALUES (?, ?, ?, ?, ?, ?)",
			channelId, r.Id, b.UserId(), rule, antispam.RulesVersion, b.Date()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type antispamDeletionRow struct {
	SenderUserId int64  `db:"sender_user_id"`
	Reported     bool   `db:"reported"`
	MessageData  string `db:"message_data"`
	Title        string `db:"title"`
}

// AntispamFalsePositive is b's owner, an admin of the supergroup who may
// delete the messages of others, saying the antispam deleted the message
// msgId wrongly: the deletion is marked reported, its sender is no longer
// checked in this group (channel_antispam_exempt), and a report of it goes
// to the moderators (moderation.KindAntispamFalsePositive) with the message
// as it was; all in b's transaction, so that none goes without the others.
// The message is not restored: the clients say the moderators will review
// the report (Android ChannelAntiSpamFalsePositiveReported). false for a
// message the antispam did not delete; a repeated report changes nothing
// more. CHANNEL_INVALID for a broadcast channel, CHAT_ADMIN_REQUIRED for one
// without the right.
func (d *Dao) AntispamFalsePositive(b *userbox.Box, channelId int64, msgId int32) (bool, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return false, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return false, err
	}
	if a.ch.Broadcast {
		return false, mtproto.ErrChannelInvalid
	}
	if err = a.can(channelperm.DeleteOthers, b.Date()); err != nil {
		return false, err
	}
	var rows []antispamDeletionRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT d.sender_user_id, d.reported, COALESCE(m.message_data, '') AS message_data, c.title FROM channel_antispam_deletions d"+
			" JOIN channels c ON c.id = d.channel_id LEFT JOIN channel_messages m ON m.channel_id = d.channel_id AND m.id = d.message_id"+
			" WHERE d.channel_id = ? AND d.message_id = ? FOR UPDATE",
		channelId, msgId); err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	r := rows[0]
	if r.Reported {
		return true, nil
	}
	if _, err = b.Tx().Exec("UPDATE channel_antispam_deletions SET reported = 1 WHERE channel_id = ? AND message_id = ?", channelId, msgId); err != nil {
		return false, err
	}
	if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_antispam_exempt (channel_id, user_id, `date`) VALUES (?, ?, ?)",
		channelId, r.SenderUserId, b.Date()); err != nil {
		return false, err
	}
	snapshot, err := antispamSnapshot(b, &r, msgId)
	if err != nil {
		return false, err
	}
	if err = moderation.AddReportTx(b.Tx(), &moderation.Report{
		ReporterUserId: b.UserId(),
		Kind:           moderation.KindAntispamFalsePositive,
		PeerType:       mtproto.PEER_CHANNEL,
		PeerId:         channelId,
		TargetUserId:   r.SenderUserId,
		Reason:         "not_spam",
		CreatedAt:      b.Date(),
	}, []int32{msgId}, snapshot); err != nil {
		return false, err
	}
	return true, nil
}

// antispamSnapshot is what a moderator sees of a deletion reported as
// wrong: the group's title, the sender (no phone, ADR 0040) and the message
// as it was.
func antispamSnapshot(b *userbox.Box, r *antispamDeletionRow, msgId int32) (*moderation.Snapshot, error) {
	s := &moderation.Snapshot{PeerTitle: r.Title}
	var users []struct {
		FirstName string `db:"first_name"`
		LastName  string `db:"last_name"`
		Username  string `db:"username"`
		About     string `db:"about"`
		Bot       bool   `db:"is_bot"`
	}
	if err := b.Tx().QueryRowsPartial(&users, "SELECT first_name, last_name, username, about, is_bot FROM users WHERE id = ?", r.SenderUserId); err != nil {
		return nil, err
	}
	if len(users) > 0 {
		u := users[0]
		s.Users = []moderation.SnapshotUser{{Id: r.SenderUserId, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username, About: u.About, Bot: u.Bot}}
	}
	if r.MessageData != "" {
		var m *mtproto.Message
		if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, err
		}
		s.Messages = []moderation.SnapshotMessage{{
			Id:     msgId,
			Date:   m.GetDate(),
			FromId: r.SenderUserId,
			Text:   m.GetMessage(),
			Media:  moderation.MediaOf(m.GetMedia()),
		}}
	}
	return s, nil
}
