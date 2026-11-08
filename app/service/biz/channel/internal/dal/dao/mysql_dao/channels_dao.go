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

package mysql_dao

import (
	"context"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dal/dataobject"
)

// ChannelsDAO reads channels. Only pkg/channelbox inserts its rows and
// writes its counters (tools/boxaudit); the service only reads here.
type ChannelsDAO struct {
	db *sqlx.DB
}

func NewChannelsDAO(db *sqlx.DB) *ChannelsDAO {
	return &ChannelsDAO{
		db: db,
	}
}

// runningBan is a restriction or a kick of p not over yet: until_date 0 or
// Int32.max is for good.
const runningBan = "(p.until_date = 0 OR p.until_date = 2147483647 OR p.until_date > UNIX_TIMESTAMP())"

// kicked_count and banned_count are counted, not stored (task 042): a
// stored count would keep the bans that ran out.
const channelsColumns = "id, access_hash, creator_user_id, title, about, photo_id, broadcast, megagroup, forum, forum_tabs, signatures, signature_profiles, pts, top_message, " +
	"participants_count, admins_count, " +
	"(SELECT COUNT(*) FROM channel_participants p WHERE p.channel_id = channels.id AND p.state = 2 AND " + runningBan + ") AS kicked_count, " +
	"(SELECT COUNT(*) FROM channel_participants p WHERE p.channel_id = channels.id AND p.state = 0 AND p.role = 0 AND p.banned_rights <> 0 AND " + runningBan + ") AS banned_count, " +
	"default_banned_rights, slowmode_seconds, hidden_prehistory, " +
	"linked_chat_id, migrated_from_chat_id, migrated_from_max_id, noforwards, join_to_send, join_request, participants_hidden, " +
	"deleted, version, `date`, available_reactions_type, available_reactions"

// SelectByIdList
// select <channelsColumns> from channels where id in (:idList)
func (dao *ChannelsDAO) SelectByIdList(ctx context.Context, idList []int64) ([]dataobject.ChannelsDO, error) {
	if len(idList) == 0 {
		return []dataobject.ChannelsDO{}, nil
	}
	var values []dataobject.ChannelsDO
	query := "select " + channelsColumns + " from channels where id in (" + placeholders(len(idList)) + ")"
	if err := dao.db.QueryRowsPartial(ctx, &values, query, int64Args(idList)...); err != nil {
		return nil, err
	}
	return values, nil
}

// placeholders is n comma-separated ?.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func int64Args(ids []int64, head ...any) []any {
	args := make([]any, 0, len(head)+len(ids))
	args = append(args, head...)
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}
