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

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// A sign-up of an imported number (task 011, ADR 0015). The account and a
// row of contact_signups are written together (CreateNewUserV2); the row
// stays until the sign-up is delivered to everyone who imported the number:
// one box transaction of the new user turns the importers' phone book rows
// into contacts of the new user and writes, as its events, the service
// message messageActionContactSignUp into each importer's box and the peer
// settings of the new contact, with seq, into each importer's account. The
// worker of the service (core.SignUpWorker) runs it right after the sign-up
// and again after a failure or a restart.

// ContactSignUp is a sign-up waiting for its delivery.
type ContactSignUp struct {
	UserId    int64  `db:"user_id"`
	Phone     string `db:"phone"`
	CreatedAt int64  `db:"created_at"`
}

// Importer is a user who keeps a number in the phone book, with the name
// they gave it.
type Importer struct {
	UserId    int64  `db:"importer_user_id"`
	FirstName string `db:"import_first_name"`
	LastName  string `db:"import_last_name"`
}

// PendingContactSignUps returns up to limit sign-ups not yet delivered,
// the oldest first.
func (d *Dao) PendingContactSignUps(ctx context.Context, limit int) ([]ContactSignUp, error) {
	var rows []ContactSignUp
	err := d.DB.QueryRowsPartial(ctx, &rows, "SELECT user_id, phone, created_at FROM contact_signups ORDER BY created_at, user_id LIMIT ?", limit)
	return rows, err
}

// Importers returns who keeps phone in the phone book, by user id.
func (d *Dao) Importers(ctx context.Context, phone string) ([]Importer, error) {
	var rows []Importer
	err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT importer_user_id, import_first_name, import_last_name FROM unregistered_contacts WHERE phone = ? ORDER BY importer_user_id", phone)
	return rows, err
}

// SignUpNotificationsSilenced returns which of ids turned the notifications
// of contacts' sign-ups off (account.setContactSignUpNotification silent).
// Without a setting they are on.
func (d *Dao) SignUpNotificationsSilenced(ctx context.Context, ids []int64) (map[int64]bool, error) {
	silenced := make(map[int64]bool)
	if len(ids) == 0 {
		return silenced, nil
	}
	var rows []struct {
		UserId int64  `db:"user_id"`
		Value  string `db:"value"`
	}
	err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, value FROM user_settings WHERE key2 = ? AND deleted = 0 AND user_id IN ("+inList(len(ids))+")",
		append([]any{ContactSignUpNotificationKey}, int64Args(ids)...)...)
	for _, r := range rows {
		if r.Value == "false" {
			silenced[r.UserId] = true
		}
	}
	return silenced, err
}

// ContactSignUpNotificationKey is the user_settings key of the
// notifications of contacts' sign-ups: "true" when on, "false" when off.
const ContactSignUpNotificationKey = "contactSignUpNotification"

// TakeContactSignUp tells whether the sign-up of the box owner is still
// waiting for its delivery. Only the owner's box transactions change the
// row, so the box lock serializes the deliveries.
func TakeContactSignUp(b *userbox.Box) (bool, error) {
	var ids []int64
	err := b.Tx().QueryRowsPartial(&ids, "SELECT user_id FROM contact_signups WHERE user_id = ?", b.UserId())
	return len(ids) > 0, err
}

// FinishContactSignUp marks the sign-up of the box owner delivered.
func FinishContactSignUp(b *userbox.Box) error {
	_, err := b.Tx().Exec("DELETE FROM contact_signups WHERE user_id = ?", b.UserId())
	return err
}

// LockImporters returns the users who keep phone in the phone book now and
// locks their rows: an import of the number that is not committed yet is
// waited for, so it is either seen here or sees the new account itself
// (RegisteredPhones).
func LockImporters(b *userbox.Box, phone string) ([]int64, error) {
	var ids []int64
	err := b.Tx().QueryRowsPartial(&ids,
		"SELECT importer_user_id FROM unregistered_contacts WHERE phone = ? ORDER BY importer_user_id FOR UPDATE", phone)
	return ids, err
}

// DropImporters removes phone from the phone books of ids: the number is a
// contact of theirs now, or theirs are gone.
func DropImporters(b *userbox.Box, phone string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := b.Tx().Exec("DELETE FROM unregistered_contacts WHERE phone = ? AND importer_user_id IN ("+inList(len(ids))+")",
		append([]any{phone}, int64Args(ids)...)...)
	return err
}

// OwnContacts returns which of ids are among the box owner's contacts.
func OwnContacts(b *userbox.Box, ids []int64) (map[int64]bool, error) {
	found := make(map[int64]bool)
	if len(ids) == 0 {
		return found, nil
	}
	var list []int64
	err := b.Tx().QueryRowsPartial(&list,
		"SELECT contact_user_id FROM user_contacts WHERE owner_user_id = ? AND contact_user_id IN ("+inList(len(ids))+") AND is_deleted = 0",
		append([]any{b.UserId()}, int64Args(ids)...)...)
	for _, id := range list {
		found[id] = true
	}
	return found, err
}

// SignUpContact is an importer who gets the box owner as a contact.
type SignUpContact struct {
	OwnerId   int64 // the importer
	Phone     string
	FirstName string
	LastName  string
	Mutual    bool // the box owner has the importer among their contacts
}

// AddSignUpContacts adds the box owner to the contacts of the importers
// with the names they gave the number. A row of an importer who already
// has the owner as a contact keeps its names; the mutuality of both rows
// of each pair is set.
func AddSignUpContacts(b *userbox.Box, contacts []SignUpContact) error {
	if len(contacts) == 0 {
		return nil
	}
	args := make([]any, 0, 7*len(contacts))
	var mutual []int64
	for _, c := range contacts {
		args = append(args, c.OwnerId, b.UserId(), c.Phone, c.FirstName, c.LastName, c.Mutual, b.Date())
		if c.Mutual {
			mutual = append(mutual, c.OwnerId)
		}
	}
	if _, err := b.Tx().Exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_phone, contact_first_name, contact_last_name, mutual, date2) VALUES "+
		strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?, ?),", len(contacts)), ",")+" AS new"+
		" ON DUPLICATE KEY UPDATE contact_phone = IF(user_contacts.is_deleted, new.contact_phone, user_contacts.contact_phone),"+
		" contact_first_name = IF(user_contacts.is_deleted, new.contact_first_name, user_contacts.contact_first_name),"+
		" contact_last_name = IF(user_contacts.is_deleted, new.contact_last_name, user_contacts.contact_last_name),"+
		" date2 = IF(user_contacts.is_deleted, new.date2, user_contacts.date2), mutual = new.mutual, is_deleted = 0",
		args...); err != nil {
		return err
	}
	if len(mutual) == 0 {
		return nil
	}
	_, err := b.Tx().Exec("UPDATE user_contacts SET mutual = 1 WHERE owner_user_id = ? AND contact_user_id IN ("+inList(len(mutual))+")",
		append([]any{b.UserId()}, int64Args(mutual)...)...)
	return err
}

// SignUpMessage is the inbox event that brings the service message
// messageActionContactSignUp of the box owner into the box of the importer
// to (the event from_id). users go with the message, as the importer sees
// them.
func (d *Dao) SignUpMessage(b *userbox.Box, to, dialogMessageId int64, silent bool, users []*mtproto.User) (userbox.Event, error) {
	message := mtproto.MakeTLMessageService(&mtproto.Message{
		Silent: silent,
		FromId: mtproto.MakePeerUser(b.UserId()),
		PeerId: mtproto.MakePeerUser(to),
		Date:   int32(b.Date()),
		Action: mtproto.MakeMessageActionContactSignUp(),
	}).To_Message()
	return newEvent(d.Topics.Inbox, &inboxpb.TLInboxSendUserMessageToInboxV2{
		UserId:   to,
		FromId:   b.UserId(),
		PeerType: mtproto.PEER_USER,
		PeerId:   to,
		BoxList: []*mtproto.MessageBox{{
			UserId:            b.UserId(),
			SenderUserId:      b.UserId(),
			PeerType:          mtproto.PEER_USER,
			PeerId:            to,
			DialogMessageId:   dialogMessageId,
			MessageFilterType: mtproto.GetMediaType(message),
			Message:           message,
		}},
		Users: users,
	})
}
