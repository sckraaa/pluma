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

	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The phone book of a user on the server (contacts.importContacts, task
// 011, ADR 0015). An imported number of a Telegram account the importer may
// find becomes a user_contacts row with the name and the phone the importer
// gave; every other imported number — not registered, or of an account
// that does not let the importer find it by its number — is a row of
// unregistered_contacts (phone, importer, name). A number registered later
// turns its unregistered_contacts rows into contacts of the new user
// (contact_signup.go).
//
// The statements below run in the importer's box transaction (b.Tx()); the
// importer's rows are the ones of the box owner.

// ImportedPhone is a number of a phone book with the name its owner gave.
type ImportedPhone struct {
	Phone     string
	FirstName string
	LastName  string
}

// ImportedContact is a contact an import adds or renames: a user of the
// imported phone.
type ImportedContact struct {
	UserId    int64
	Phone     string
	FirstName string
	LastName  string
}

// ContactChange tells what an import did to a contact row of the importer.
type ContactChange struct {
	UserId  int64
	Changed bool // the row is new, revived, renamed, got the phone or changed mutuality
	Mutual  bool
}

// ImportQuota returns the import window of the box owner: its start (unix
// seconds) and the new numbers imported since; 0, 0 without a row.
func ImportQuota(b *userbox.Box) (windowStart int64, imported int32, err error) {
	var rows []struct {
		WindowStart int64 `db:"window_start"`
		Imported    int32 `db:"imported"`
	}
	if err = b.Tx().QueryRowsPartial(&rows, "SELECT window_start, imported FROM contact_import_quota WHERE user_id = ?", b.UserId()); err != nil || len(rows) == 0 {
		return 0, 0, err
	}
	return rows[0].WindowStart, rows[0].Imported, nil
}

// SetImportQuota writes the import window of the box owner.
func SetImportQuota(b *userbox.Box, windowStart int64, imported int32) error {
	_, err := b.Tx().Exec("INSERT INTO contact_import_quota (user_id, window_start, imported) VALUES (?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE window_start = new.window_start, imported = new.imported",
		b.UserId(), windowStart, imported)
	return err
}

// SavedPhones returns which of phones are in the box owner's phone book
// (unregistered_contacts).
func SavedPhones(b *userbox.Box, phones []string) (map[string]bool, error) {
	saved := make(map[string]bool, len(phones))
	if len(phones) == 0 {
		return saved, nil
	}
	var list []string
	err := b.Tx().QueryRowsPartial(&list,
		"SELECT phone FROM unregistered_contacts WHERE importer_user_id = ? AND phone IN ("+inList(len(phones))+")",
		append([]any{b.UserId()}, stringArgs(phones)...)...)
	for _, p := range list {
		saved[p] = true
	}
	return saved, err
}

// ContactRows returns the box owner's user_contacts rows of ids, deleted
// rows too, by contact user id.
func ContactRows(b *userbox.Box, ids []int64) (map[int64]dataobject.UserContactsDO, error) {
	rows := make(map[int64]dataobject.UserContactsDO, len(ids))
	if len(ids) == 0 {
		return rows, nil
	}
	var list []dataobject.UserContactsDO
	err := b.Tx().QueryRowsPartial(&list,
		"SELECT id, owner_user_id, contact_user_id, contact_phone, contact_first_name, contact_last_name, mutual, close_friend, stories_hidden, date2, is_deleted"+
			" FROM user_contacts WHERE owner_user_id = ? AND contact_user_id IN ("+inList(len(ids))+")",
		append([]any{b.UserId()}, int64Args(ids)...)...)
	for _, r := range list {
		rows[r.ContactUserId] = r
	}
	return rows, err
}

// PutContacts makes contacts the box owner's contacts with the names and
// phones of the import and sets the mutuality of both rows of each pair: a
// contact who has the owner among their contacts makes it mutual.
//
// The owner's rows are written first, then the rows of the contacts that
// point back are read locked: two users importing each other at once each
// wait for the other's row, InnoDB breaks the deadlock and Run repeats the
// victim, which then sees the other's committed row. Reading first would
// let both miss each other (a missing row is not locked in READ COMMITTED).
func PutContacts(b *userbox.Box, contacts []ImportedContact) ([]ContactChange, error) {
	if len(contacts) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(contacts))
	for _, c := range contacts {
		ids = append(ids, c.UserId)
	}
	old, err := ContactRows(b, ids)
	if err != nil {
		return nil, err
	}

	args := make([]any, 0, 6*len(contacts))
	for _, c := range contacts {
		args = append(args, b.UserId(), c.UserId, c.Phone, c.FirstName, c.LastName, b.Date())
	}
	// date2 is the date of the contact: it stays when the row exists.
	if _, err = b.Tx().Exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_phone, contact_first_name, contact_last_name, date2) VALUES "+
		strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?),", len(contacts)), ",")+" AS new"+
		" ON DUPLICATE KEY UPDATE contact_phone = new.contact_phone, contact_first_name = new.contact_first_name, contact_last_name = new.contact_last_name,"+
		" date2 = IF(user_contacts.is_deleted, new.date2, user_contacts.date2), is_deleted = 0",
		args...); err != nil {
		return nil, err
	}

	var back []int64
	if err = b.Tx().QueryRowsPartial(&back,
		"SELECT owner_user_id FROM user_contacts WHERE owner_user_id IN ("+inList(len(ids))+") AND contact_user_id = ? AND is_deleted = 0 ORDER BY owner_user_id FOR UPDATE",
		append(int64Args(ids), b.UserId())...); err != nil {
		return nil, err
	}
	mutual := make(map[int64]bool, len(back))
	for _, id := range back {
		mutual[id] = true
	}
	var mutualIds, oneWayIds []int64
	for _, id := range ids {
		if mutual[id] {
			mutualIds = append(mutualIds, id)
		} else {
			oneWayIds = append(oneWayIds, id)
		}
	}
	if err = setMutual(b, mutualIds, true); err != nil {
		return nil, err
	}
	if err = setMutual(b, oneWayIds, false); err != nil {
		return nil, err
	}

	changes := make([]ContactChange, 0, len(contacts))
	for _, c := range contacts {
		o, ok := old[c.UserId]
		changes = append(changes, ContactChange{
			UserId: c.UserId,
			Mutual: mutual[c.UserId],
			Changed: !ok || o.IsDeleted || o.ContactFirstName != c.FirstName || o.ContactLastName != c.LastName ||
				o.ContactPhone != c.Phone || o.Mutual != mutual[c.UserId],
		})
	}
	return changes, nil
}

// setMutual sets the mutuality of the box owner's rows of ids and of the
// rows of ids that point back to the owner.
func setMutual(b *userbox.Box, ids []int64, mutual bool) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := b.Tx().Exec("UPDATE user_contacts SET mutual = ? WHERE owner_user_id = ? AND contact_user_id IN ("+inList(len(ids))+")",
		append([]any{mutual, b.UserId()}, int64Args(ids)...)...); err != nil {
		return err
	}
	if !mutual {
		// Rows pointing back exist only for mutual pairs.
		return nil
	}
	_, err := b.Tx().Exec("UPDATE user_contacts SET mutual = 1 WHERE owner_user_id IN ("+inList(len(ids))+") AND contact_user_id = ?",
		append(int64Args(ids), b.UserId())...)
	return err
}

// SavePhones puts phones into the box owner's phone book, or renames them.
func SavePhones(b *userbox.Box, phones []ImportedPhone) error {
	if len(phones) == 0 {
		return nil
	}
	args := make([]any, 0, 4*len(phones))
	for _, p := range phones {
		args = append(args, p.Phone, b.UserId(), p.FirstName, p.LastName)
	}
	_, err := b.Tx().Exec("INSERT INTO unregistered_contacts (phone, importer_user_id, import_first_name, import_last_name) VALUES "+
		strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?),", len(phones)), ",")+" AS new"+
		" ON DUPLICATE KEY UPDATE import_first_name = new.import_first_name, import_last_name = new.import_last_name",
		args...)
	return err
}

// ForgetPhones removes phones from the box owner's phone book: their
// numbers became contacts.
func ForgetPhones(b *userbox.Box, phones []string) error {
	if len(phones) == 0 {
		return nil
	}
	_, err := b.Tx().Exec("DELETE FROM unregistered_contacts WHERE phone IN ("+inList(len(phones))+") AND importer_user_id = ?",
		append(stringArgs(phones), b.UserId())...)
	return err
}

// RegisteredPhones returns which of phones belong to an account now, as
// the transaction reads them: a number may be registered while it is
// imported (see the sign-up of a contact).
func RegisteredPhones(b *userbox.Box, phones []string) (map[string]bool, error) {
	found := make(map[string]bool)
	if len(phones) == 0 {
		return found, nil
	}
	var list []string
	err := b.Tx().QueryRowsPartial(&list,
		"SELECT phone FROM users WHERE phone IN ("+inList(len(phones))+")", stringArgs(phones)...)
	for _, p := range list {
		found[p] = true
	}
	return found, err
}

// ImportersCount returns how many users keep each of phones in their phone
// book: popular_invites.importers.
func (d *Dao) ImportersCount(ctx context.Context, phones []string) (map[string]int32, error) {
	counts := make(map[string]int32, len(phones))
	if len(phones) == 0 {
		return counts, nil
	}
	var rows []struct {
		Phone string `db:"phone"`
		N     int32  `db:"n"`
	}
	err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT phone, COUNT(*) AS n FROM unregistered_contacts WHERE phone IN ("+inList(len(phones))+") GROUP BY phone",
		stringArgs(phones)...)
	for _, r := range rows {
		counts[r.Phone] = r.N
	}
	return counts, err
}

func stringArgs(v []string) []any {
	args := make([]any, len(v))
	for i := range v {
		args[i] = v[i]
	}
	return args
}

func int64Args(v []int64) []any {
	args := make([]any, len(v))
	for i := range v {
		args[i] = v[i]
	}
	return args
}
