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

	"github.com/teamgram/proto/mtproto"
)

// HasPrivateChat tells whether userId has a private chat with peerId: its
// dialog with the user (task 014, pkg/premium).
func (d *Dao) HasPrivateChat(ctx context.Context, userId, peerId int64) (bool, error) {
	var ids []int64
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &ids,
		"SELECT peer_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		userId, mtproto.PEER_USER, peerId); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}
