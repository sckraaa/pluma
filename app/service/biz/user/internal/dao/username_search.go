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
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// UsernameMatch is a username that starts with the query, and its peer.
type UsernameMatch struct {
	Username string `db:"username"`
	PeerType int32  `db:"peer_type"`
	PeerId   int64  `db:"peer_id"`
}

// likeEscaper makes a query a LIKE prefix: its own % and _ are literal.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// SearchUsernames returns up to limit active usernames that start with q,
// of users and channels, leaving out the users excludedUsers. The excluded
// ids are user ids only: a channel and a user may have the same id (each
// table counts its own), and a channel must not drop out because a contact
// has its number.
func (d *Dao) SearchUsernames(ctx context.Context, q string, excludedUsers []int64, limit int32) ([]UsernameMatch, error) {
	query := "SELECT username, peer_type, peer_id FROM username WHERE username LIKE ? ESCAPE '\\\\' AND active = 1 AND deleted = 0 AND peer_type IN (?, ?)"
	args := []any{likeEscaper.Replace(q) + "%", mtproto.PEER_USER, mtproto.PEER_CHANNEL}
	if len(excludedUsers) > 0 {
		query += " AND NOT (peer_type = ? AND peer_id IN (" + sqlx.InInt64List(excludedUsers) + "))"
		args = append(args, mtproto.PEER_USER)
	}
	query += " ORDER BY LENGTH(username), username LIMIT ?"
	args = append(args, limit)
	var out []UsernameMatch
	if err := d.DB.QueryRowsPartial(ctx, &out, query, args...); err != nil {
		return nil, err
	}
	return out, nil
}
