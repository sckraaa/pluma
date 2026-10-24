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

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
)

// SetGlobalPrivacySettings stores the global privacy settings of a user.
// The cached user data carries new_noncontact_peers_require_premium (the
// users others see show it), so it is dropped with the change.
func (d *Dao) SetGlobalPrivacySettings(ctx context.Context, do *dataobject.UserGlobalPrivacySettingsDO) error {
	_, _, err := d.CachedConn.Exec(
		ctx,
		func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
			return d.UserGlobalPrivacySettingsDAO.InsertOrUpdate(ctx, do)
		},
		genCacheUserDataCacheKey(do.UserId))
	return err
}
