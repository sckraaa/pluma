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
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// SavedContacts returns the phone book of userId on the server
// (contacts.getSaved): the contacts that came with a phone number and the
// imported numbers without an account the user may see, the contacts
// first, each part by date.
func (d *Dao) SavedContacts(ctx context.Context, userId int64) ([]*mtproto.SavedContact, error) {
	var rows []struct {
		Phone     string `db:"phone"`
		FirstName string `db:"first_name"`
		LastName  string `db:"last_name"`
		Date      int64  `db:"date"`
	}
	err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT contact_phone AS phone, contact_first_name AS first_name, contact_last_name AS last_name, date2 AS date, 0 AS part, id"+
			" FROM user_contacts WHERE owner_user_id = ? AND is_deleted = 0 AND contact_phone <> ''"+
			" UNION ALL"+
			" SELECT phone, import_first_name, import_last_name, UNIX_TIMESTAMP(created_at), 1, id"+
			" FROM unregistered_contacts WHERE importer_user_id = ?"+
			" ORDER BY part, date, id",
		userId, userId)
	if err != nil {
		return nil, err
	}
	list := make([]*mtproto.SavedContact, 0, len(rows))
	for _, r := range rows {
		list = append(list, mtproto.MakeTLSavedPhoneContact(&mtproto.SavedContact{
			Phone:     r.Phone,
			FirstName: r.FirstName,
			LastName:  r.LastName,
			Date:      int32(r.Date),
		}).To_SavedContact())
	}
	return list, nil
}

// ResetSavedPhones removes the imported numbers of the box owner that are
// not contacts (contacts.resetSaved): the numbers without an account and
// those of accounts that do not let the owner find them. The owner's
// contacts stay. It returns how many it removed.
func ResetSavedPhones(b *userbox.Box) (int64, error) {
	var ids []int64
	if err := b.Tx().QueryRowsPartial(&ids, "SELECT id FROM unregistered_contacts WHERE importer_user_id = ? ORDER BY id", b.UserId()); err != nil || len(ids) == 0 {
		return 0, err
	}
	res, err := b.Tx().Exec("DELETE FROM unregistered_contacts WHERE id IN ("+inList(len(ids))+")", int64Args(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
