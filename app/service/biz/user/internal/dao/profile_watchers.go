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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// profileWatchersQuery reads, by the user's id, the users who see the user
// in their lists: whose contact the user is and who have a private dialog
// with the user, without the user and without deleted accounts. The
// indexes user_contacts (contact_user_id) and dialogs (peer_type, peer_id)
// serve it (migrate-20261027-profile-watchers.sql).
const profileWatchersQuery = "SELECT c.owner_user_id FROM user_contacts c JOIN users u ON u.id = c.owner_user_id" +
	" WHERE c.contact_user_id = ? AND c.is_deleted = 0 AND c.owner_user_id <> ? AND u.deleted = 0" +
	" UNION SELECT d.user_id FROM dialogs d JOIN users u ON u.id = d.user_id" +
	" WHERE d.peer_type = ? AND d.peer_id = ? AND d.deleted = 0 AND d.user_id <> ? AND u.deleted = 0" +
	" ORDER BY 1"

// profileWatchers returns, by id, the users who see the account id in their
// lists (profileWatchersQuery), as b reads them. The read takes no locks: a
// contact or a dialog another transaction adds at the same time may be
// missed, and that transaction's own reply carries the account as it read
// it then.
func profileWatchers(b *userbox.Box, id int64) ([]int64, error) {
	var ids []int64
	err := b.Tx().QueryRowsPartial(&ids, profileWatchersQuery, id, id, mtproto.PEER_USER, id, id)
	return ids, err
}

// tellWatchers gives u, a change of the account id's name and usernames
// (updateUserName) or of its photo or Premium (updateUser), to the users who
// see the account in their lists (profileWatchers), with seq in each of
// their accounts: one inbox event each in b, the transaction of the change
// (EmitSeqInbox, ADR 0021). msg adds the account as each of them sees it
// when it applies the event; updateUserName carries the account's own
// names, and clients keep the names a contact has in their phone book. Bots
// among them get nothing (msg).
func (d *Dao) tellWatchers(b *userbox.Box, id int64, u *mtproto.Update) error {
	watchers, err := profileWatchers(b, id)
	if err != nil {
		return err
	}
	for _, to := range watchers {
		if err = d.EmitSeqInbox(b, to, u); err != nil {
			return err
		}
	}
	return nil
}
