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

	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The rows of the support ghost (task 080, ADR 0048) that pluma-admin
// changes. Joining and leaving hidden by the ghost itself are in
// membership.go.

// LeaveHidden takes the ghost b's owner out of the channel where its row is
// hidden (pluma-admin ghost leave). The change has no Self when it was not
// in, or is in visibly: an admin or the owner leaves by itself.
func (d *Dao) LeaveHidden(b *userbox.Box, channelId int64) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	if !me.participant().Member() || !me.Hidden {
		return &ChannelChange{ChannelId: channelId}, nil
	}
	return leaveHidden(b, channelId, c)
}

// SetUserHidden makes the live rows of userId hidden or shown again, as its
// account became a ghost or stopped being one (pluma-admin ghost set), and
// tells in how many channels a row changed. Each channel is its own
// transaction with the channel locked, its counters recounted from the
// rows: nobody gets a service message. The rows of an admin or the owner
// stay shown: those are shown to every member (ADR 0048, p. 2). Running it
// again changes nothing.
func (d *Dao) SetUserHidden(ctx context.Context, userId int64, hidden bool) (int32, error) {
	var ids []int64
	if err := d.Mysql.QueryRowsPartial(ctx, &ids,
		"SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = ? AND role = ? AND hidden <> ? ORDER BY channel_id",
		userId, channelperm.StateIn, channelperm.RoleMember, hidden); err != nil {
		return 0, fmt.Errorf("channels of user %d: %w", userId, err)
	}
	var changed int32
	for _, id := range ids {
		var n int64
		if _, err := d.RunBox(ctx, userId, func(b *userbox.Box) error {
			if _, err := openChannel(b, id); err != nil {
				return err
			}
			r, err := b.Tx().Exec("UPDATE channel_participants SET hidden = ? WHERE channel_id = ? AND user_id = ? AND state = ? AND role = ? AND hidden <> ?",
				hidden, id, userId, channelperm.StateIn, channelperm.RoleMember, hidden)
			if err != nil {
				return err
			}
			if n, err = r.RowsAffected(); err != nil || n == 0 {
				return err
			}
			_, err = b.Tx().Exec(recountQuery, id, channelperm.StateIn, id, channelperm.StateIn, channelperm.RoleMember, id)
			return err
		}); err != nil {
			return changed, fmt.Errorf("user %d in channel %d: %w", userId, id, err)
		}
		if n > 0 {
			changed++
		}
	}
	return changed, nil
}

// recountQuery sets the counters of a channel from its rows: the members
// and the admins (with the owner) shown to the others.
const recountQuery = "UPDATE channels SET" +
	" participants_count = (SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = ? AND hidden = 0)," +
	" admins_count = (SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = ? AND hidden = 0 AND role <> ?)" +
	" WHERE id = ?"
