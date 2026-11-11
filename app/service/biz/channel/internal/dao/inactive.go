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
)

// InactiveChannelsMax is the most channels channels.getInactiveChannels
// lists: the clients show them for the user to pick some to leave
// (Android TooManyCommunitiesActivity, tdesktop premium_limits_box.cpp
// InactiveController, iOS InactiveChannels.swift), and the least active
// come first.
const InactiveChannelsMax = 100

// ChannelActivity is a channel of a user with the date of the user's last
// activity there.
type ChannelActivity struct {
	ChannelId int64 `db:"channel_id"`
	Date      int64 `db:"active"`
}

// InactiveChannels lists the channels and supergroups userId is in, the
// least active first, at most limit: the date of each is the user's last
// activity there that the server keeps, the latest of its join, of its
// last message (a post of a channel's admin included) and, in a
// supergroup, of its last read (channel_read_marks). The clients show
// "inactive for" the time since that date (Android
// TooManyCommunitiesActivity.java:340-360, tdesktop premium_limits_box.cpp
// InactiveController::createRow).
func (d *Dao) InactiveChannels(ctx context.Context, userId int64, limit int32) ([]ChannelActivity, error) {
	if limit <= 0 || limit > InactiveChannelsMax {
		limit = InactiveChannelsMax
	}
	var rows []ChannelActivity
	err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT p.channel_id, GREATEST(p.`date`,"+
			" COALESCE((SELECT MAX(m.date2) FROM channel_messages m WHERE m.channel_id = p.channel_id AND m.sender_user_id = p.user_id), 0),"+
			" COALESCE((SELECT MAX(r.read_at) FROM channel_read_marks r WHERE r.channel_id = p.channel_id AND r.user_id = p.user_id), 0)) AS active"+
			" FROM channel_participants p JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.user_id = ? AND p.state = 0 AND c.deleted = 0"+
			" ORDER BY active, p.channel_id LIMIT ?",
		userId, limit)
	return rows, err
}
