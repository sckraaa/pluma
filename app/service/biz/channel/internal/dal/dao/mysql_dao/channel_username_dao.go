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

// ChannelUsernameDAO reads the public usernames of channels from username,
// the table of all usernames, which biz user writes
// (user.updateUsernameByPeer with peer_type PEER_CHANNEL).
type ChannelUsernameDAO struct {
	db *sqlx.DB
}

func NewChannelUsernameDAO(db *sqlx.DB) *ChannelUsernameDAO {
	return &ChannelUsernameDAO{
		db: db,
	}
}

// SelectListByChannelIdList
// select peer_id, username from username where peer_type = 4 and peer_id in (:idList) and editable = 1
func (dao *ChannelUsernameDAO) SelectListByChannelIdList(ctx context.Context, idList []int64) ([]dataobject.ChannelUsernameDO, error) {
	if len(idList) == 0 {
		return []dataobject.ChannelUsernameDO{}, nil
	}
	var values []dataobject.ChannelUsernameDO
	query := "select peer_id, username from username where peer_type = 4 and peer_id in (" + placeholders(len(idList)) + ") and editable = 1"
	if err := dao.db.QueryRowsPartial(ctx, &values, query, int64Args(idList)...); err != nil {
		return nil, err
	}
	return values, nil
}
