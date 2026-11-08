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

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dal/dataobject"
)

// ChannelParticipantsDAO reads channel_participants.
type ChannelParticipantsDAO struct {
	db *sqlx.DB
}

func NewChannelParticipantsDAO(db *sqlx.DB) *ChannelParticipantsDAO {
	return &ChannelParticipantsDAO{
		db: db,
	}
}

const channelParticipantsColumns = "channel_id, user_id, role, state, admin_rights, `rank`, banned_rights, until_date, " +
	"inviter_user_id, promoted_by, kicked_by, available_min_id, last_send_date, `date`"

// SelectListByUserIdList
// select <channelParticipantsColumns> from channel_participants where channel_id = :channel_id and user_id in (:idList)
func (dao *ChannelParticipantsDAO) SelectListByUserIdList(ctx context.Context, channelId int64, idList []int64) ([]dataobject.ChannelParticipantsDO, error) {
	if len(idList) == 0 {
		return []dataobject.ChannelParticipantsDO{}, nil
	}
	var values []dataobject.ChannelParticipantsDO
	query := "select " + channelParticipantsColumns + " from channel_participants where channel_id = ? and user_id in (" + placeholders(len(idList)) + ")"
	if err := dao.db.QueryRowsPartial(ctx, &values, query, int64Args(idList, channelId)...); err != nil {
		return nil, err
	}
	return values, nil
}

// SelectListByChannelIdList is the rows of userId in the channels of idList.
// select <channelParticipantsColumns> from channel_participants where user_id = :user_id and channel_id in (:idList)
func (dao *ChannelParticipantsDAO) SelectListByChannelIdList(ctx context.Context, userId int64, idList []int64) ([]dataobject.ChannelParticipantsDO, error) {
	if len(idList) == 0 {
		return []dataobject.ChannelParticipantsDO{}, nil
	}
	var values []dataobject.ChannelParticipantsDO
	query := "select " + channelParticipantsColumns + " from channel_participants where user_id = ? and channel_id in (" + placeholders(len(idList)) + ")"
	if err := dao.db.QueryRowsPartial(ctx, &values, query, int64Args(idList, userId)...); err != nil {
		return nil, err
	}
	return values, nil
}
