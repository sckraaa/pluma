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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/antispam"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// antispamCheck judges the messages b's owner has just written to the
// supergroup channelId (task 082, ADR 0050), in the send's transaction with
// the channel locked. Every send to a supergroup is noted by the hash of
// its content (antispam_recent_posts, no text), so that the same post to
// several groups is seen whether those groups have the antispam or not;
// the owner's notes older than the window go. In a group with the antispam
// on, the messages that break a rule are deleted by
// inbox.channelAntispamToInbox, written with the send: they are sent and
// pushed as any, and the deletion follows once the transaction commits,
// as many times as it takes.
func (d *Dao) antispamCheck(b *userbox.Box, channelId int64, s *sender, sent []ChannelSent) error {
	if len(sent) == 0 {
		return nil
	}
	if _, err := b.Tx().Exec("DELETE FROM antispam_recent_posts WHERE user_id = ? AND `date` < ?",
		b.UserId(), b.Date()-antispam.CrossPostWindow); err != nil {
		return err
	}
	hashes := make([][]byte, len(sent))
	for i, m := range sent {
		if hashes[i] = antispam.ContentHash(m.Message); hashes[i] == nil {
			continue
		}
		if _, err := b.Tx().Exec("INSERT INTO antispam_recent_posts (user_id, content_hash, channel_id, `date`) VALUES (?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE `date` = VALUES(`date`)",
			b.UserId(), hashes[i], channelId, b.Date()); err != nil {
			return err
		}
	}
	if !s.antispam {
		return nil
	}
	who, err := antispamSender(b, channelId, s, sent[0].Message.Id)
	if err != nil {
		return err
	}
	if who.Exempt {
		return nil
	}
	var (
		ids  []int32
		rule antispam.Rule
	)
	for i, m := range sent {
		// A message sent as the group is its admin's (pkg/sendas).
		if m.Message.GetFromId().GetChannelId() == channelId {
			continue
		}
		crossPosts := 0
		if hashes[i] != nil {
			var n []int
			if err = b.Tx().QueryRowsPartial(&n, "SELECT COUNT(*) FROM antispam_recent_posts WHERE user_id = ? AND content_hash = ? AND `date` >= ?",
				b.UserId(), hashes[i], b.Date()-antispam.CrossPostWindow); err != nil {
				return err
			}
			if len(n) > 0 {
				crossPosts = n[0]
			}
		}
		r, hit := antispam.Evaluate(antispam.Input{ChannelId: channelId, Sender: who, Message: m.Message, CrossPosts: crossPosts, Now: b.Date()})
		if !hit {
			continue
		}
		if rule == antispam.RuleNone {
			rule = r
		}
		ids = append(ids, m.Message.Id)
	}
	if len(ids) == 0 {
		return nil
	}
	return d.EmitInbox(b, &inbox.TLInboxChannelAntispamToInbox{
		UserId:    antispam.UserId,
		FromId:    b.UserId(),
		ChannelId: channelId,
		Ids:       ids,
		Rule:      int32(rule),
	})
}

// antispamSender reads b's owner as the antispam of the group judges it:
// exempt as an admin, a bot, a ghost (ADR 0048) or a service account, or
// after an admin reported a deletion of its message as wrong; else since
// when it is in the group and how many of its messages before firstSent
// the group keeps, up to antispam.NewMessages.
func antispamSender(b *userbox.Box, channelId int64, s *sender, firstSent int32) (antispam.Sender, error) {
	if !s.guest && (s.p.Role == channelperm.RoleCreator || s.p.Role == channelperm.RoleAdmin) {
		return antispam.Sender{Exempt: true}, nil
	}
	var users []struct {
		Bot      bool  `db:"is_bot"`
		UserType int32 `db:"user_type"`
		Ghost    bool  `db:"ghost"`
	}
	if err := b.Tx().QueryRowsPartial(&users, "SELECT is_bot, user_type, ghost FROM users WHERE id = ?", b.UserId()); err != nil {
		return antispam.Sender{}, err
	}
	if len(users) == 0 || users[0].Bot || users[0].Ghost || users[0].UserType == user.UserTypeService {
		return antispam.Sender{Exempt: true}, nil
	}
	var exempt []int64
	if err := b.Tx().QueryRowsPartial(&exempt, "SELECT user_id FROM channel_antispam_exempt WHERE channel_id = ? AND user_id = ?",
		channelId, b.UserId()); err != nil {
		return antispam.Sender{}, err
	}
	if len(exempt) > 0 {
		return antispam.Sender{Exempt: true}, nil
	}
	var kept []int32
	if err := b.Tx().QueryRowsPartial(&kept, "SELECT id FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND deleted = 0 AND id < ? LIMIT ?",
		channelId, b.UserId(), firstSent, antispam.NewMessages); err != nil {
		return antispam.Sender{}, err
	}
	return antispam.Sender{Joined: s.joined, Guest: s.guest, Kept: len(kept)}, nil
}
