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
	"slices"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// expirePortion is the most messages of a channel one call deletes: one row
// of the journal holds them.
const expirePortion = 1000

// ExpireMessages deletes up to expirePortion messages of the channel due at
// now (task 063), by the same path as channels.deleteMessages (deleteRows):
// the copy of a post goes with it, a deleted copy takes the comments of its
// post off, the topics move back, the polls leave their authors' unread
// votes. It works in the transaction of the creator's box, which only takes
// the locks: nobody does this, and channelbox has no transaction of its own
// (ADR 0034). The channels of the links are locked in ascending order, as
// any deletion locks them. Another instance of msg's worker finds the rows
// gone. It answers how many messages went; the root of a topic stays, as it
// goes only with its topic.
func (d *Dao) ExpireMessages(ctx context.Context, channelId, now int64) (int, error) {
	var channels []struct {
		CreatorUserId int64 `db:"creator_user_id"`
		Deleted       bool  `db:"deleted"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &channels, "SELECT creator_user_id, deleted FROM channels WHERE id = ?", channelId); err != nil {
		return 0, err
	}
	if len(channels) == 0 || channels[0].Deleted {
		_, err := d.Mysql.Exec(ctx, "DELETE FROM channel_message_expiry WHERE channel_id = ?", channelId)
		return 0, err
	}
	var due []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &due,
		"SELECT message_id FROM channel_message_expiry WHERE channel_id = ? AND expire_at <= ? ORDER BY channel_id, message_id LIMIT ?",
		channelId, now, expirePortion); err != nil || len(due) == 0 {
		return 0, err
	}
	n := 0
	_, err := d.RunBox(ctx, channels[0].CreatorUserId, func(b *userbox.Box) error {
		links, err := discussionLinks(b, channelId, due)
		if err != nil {
			return err
		}
		ch, err := openWithLinked(b, channelId, links)
		if err != nil {
			return err
		}
		args := make([]any, 0, len(due)+1)
		args = append(args, channelId)
		for _, id := range due {
			args = append(args, id)
		}
		in := "(?" + strings.Repeat(", ?", len(due)-1) + ")"
		var taken []int32
		if err = b.Tx().QueryRowsPartial(&taken,
			"SELECT message_id FROM channel_message_expiry WHERE channel_id = ? AND message_id IN "+in+" ORDER BY message_id FOR UPDATE", args...); err != nil || len(taken) == 0 {
			return err
		}
		var alive []int32
		if err = b.Tx().QueryRowsPartial(&alive,
			"SELECT id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN "+in+" ORDER BY id FOR UPDATE", args...); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("DELETE FROM channel_message_expiry WHERE channel_id = ? AND message_id IN "+in, args...); err != nil {
			return err
		}
		roots, err := topicsOf(b, channelId, alive)
		if err != nil {
			return err
		}
		alive = slices.DeleteFunc(alive, func(id int32) bool { return slices.Contains(roots, id) })
		del, err := deleteRows(b, ch, channelId, alive)
		if err != nil {
			return err
		}
		n = len(del.Ids)
		return d.EmitDeletePush(b, 0, del)
	})
	return n, err
}
