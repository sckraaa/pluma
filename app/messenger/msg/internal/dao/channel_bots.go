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

import "context"

// ChannelHasBots tells whether a bot is among the members of channel
// channelId: the /commands of its messages are bot commands then, as in a
// chat with a bot (plugin.RemakeMessage).
func (d *Dao) ChannelHasBots(ctx context.Context, channelId int64) (bool, error) {
	var ids []int64
	err := d.Mysql.DB.QueryRowsPartial(ctx, &ids,
		"SELECT p.user_id FROM channel_participants p JOIN users u ON u.id = p.user_id"+
			" WHERE p.channel_id = ? AND p.state = 0 AND u.is_bot = 1 LIMIT 1",
		channelId)
	return len(ids) > 0, err
}
