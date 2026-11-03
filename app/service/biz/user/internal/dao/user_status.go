// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package dao

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
)

const (
	userPresencesKeyPrefix = "user_presences"

	// signUpOnline is the online window of a user who has just signed up,
	// the one account.updateStatus opens (app/bff/userchannelprofiles).
	signUpOnline = 60
)

func genUserPresencesKey(userId int64) string {
	return fmt.Sprintf("%s_%d", userPresencesKeyPrefix, userId)
}

func isUserPresencesKey(k string) bool {
	return strings.HasPrefix(k, userPresencesKeyPrefix)
}

func parseUserPresencesKey(k string) int64 {
	if strings.HasPrefix(k, userPresencesKeyPrefix+"_") {
		v, _ := strconv.ParseInt(k[len(userPresencesKeyPrefix)+1:], 10, 64)
		return v
	}

	return 0
}

func (d *Dao) GetLastSeenAt(ctx context.Context, id int64) (*dataobject.UserPresencesDO, error) {
	var (
		do = &dataobject.UserPresencesDO{}
	)

	err := d.CachedConn.QueryRow(
		ctx,
		do,
		genUserPresencesKey(id),
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			do2, err := d.UserPresencesDAO.Select(ctx, id)
			if err != nil {
				return err
			}
			if do2 != nil {
				*v.(*dataobject.UserPresencesDO) = *do2
			} else {
				return sqlc.ErrNotFound
			}
			return nil
		})
	if err != nil {
		return nil, err
	}

	return do, nil
}

// PutLastSeenAt stores the time of the user's last account.updateStatus and
// the online window it opened (zero after offline). MySQL is written first,
// so the cache never holds a value the table lost.
func (d *Dao) PutLastSeenAt(ctx context.Context, userId int64, lastSeenAt int64, expires int32) error {
	do := &dataobject.UserPresencesDO{
		UserId:     userId,
		LastSeenAt: lastSeenAt,
		Expires:    expires,
	}

	if _, _, err := d.UserPresencesDAO.InsertOrUpdate(ctx, do); err != nil {
		return err
	}
	return d.CachedConn.SetCache(ctx, genUserPresencesKey(userId), do)
}

// seenUntil is the moment the user is seen until, the value
// ImmutableUser.LastSeenAt carries (mtproto.MakeUserStatus).
func seenUntil(do *dataobject.UserPresencesDO) int64 {
	return do.LastSeenAt + int64(do.Expires)
}
