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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ErrContactMissing answers a personal photo for a user who is not a
// contact (core.telegram.org/method/photos.uploadContactProfilePhoto).
var ErrContactMissing = status.Error(mtproto.ErrBadRequest, "CONTACT_MISSING")

// contactRowForUpdate reads the box owner's live row of contactId locked:
// the contact's own box changes its mutuality (PutContacts) while the
// owner's box changes the rest. ok is false when there is none.
func contactRowForUpdate(b *userbox.Box, contactId int64) (dataobject.UserContactsDO, bool, error) {
	var rows []dataobject.UserContactsDO
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, owner_user_id, contact_user_id, contact_phone, contact_first_name, contact_last_name, mutual, close_friend, stories_hidden, personal_photo_id, personal_photo_has_video, date2, is_deleted"+
			" FROM user_contacts WHERE owner_user_id = ? AND contact_user_id = ? AND is_deleted = 0 FOR UPDATE",
		b.UserId(), contactId)
	if err != nil || len(rows) == 0 {
		return dataobject.UserContactsDO{}, false, err
	}
	return rows[0], true, nil
}

// SetContactPersonalPhoto sets the personal photo the account ownerId sees
// for its contact contactId to photoId, a video when hasVideo, or removes
// it with photoId 0 (task 051). The row and updateUser with seq for every
// device of the account, with the contact as the account sees it now, are
// one box transaction (ADR 0020); nobody else is told, since nobody else
// sees the photo. The same photo changes nothing. A user who is not the
// account's contact is CONTACT_MISSING. It returns the contact as the
// account sees it now.
func (d *Dao) SetContactPersonalPhoto(ctx context.Context, ownerId, contactId, photoId int64, hasVideo bool) (*mtproto.User, error) {
	if contactId == ownerId {
		return nil, ErrContactMissing
	}
	if photoId == 0 {
		hasVideo = false
	}
	users := d.GetMutableUsersV2(ctx, []int64{ownerId, contactId}, true, true, []int64{ownerId})
	var owner, contact *mtproto.ImmutableUser
	for _, u := range users {
		switch u.Id() {
		case ownerId:
			owner = u
		case contactId:
			contact = u
		}
	}
	if owner == nil || contact == nil || contact.Deleted() {
		return nil, mtproto.ErrUserIdInvalid
	}

	var seen *mtproto.User
	_, err := d.RunBox(ctx, ownerId, ContactCacheKeys(ownerId, contactId), func(b *userbox.Box) error {
		row, ok, err := contactRowForUpdate(b, contactId)
		if err != nil {
			return err
		}
		if !ok {
			return ErrContactMissing
		}
		changed := row.PersonalPhotoId != photoId || row.PersonalPhotoHasVideo != hasVideo
		if changed {
			if _, err = b.Tx().Exec("UPDATE user_contacts SET personal_photo_id = ?, personal_photo_has_video = ? WHERE id = ?",
				photoId, hasVideo, row.Id); err != nil {
				return err
			}
			row.PersonalPhotoId, row.PersonalPhotoHasVideo = photoId, hasVideo
		}
		seen = contact.ToUnsafeUser(withContactRow(owner, contactData(&row)))
		if !changed {
			return nil
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateUser(&mtproto.Update{UserId: contactId}).To_Update(),
			[]*mtproto.User{seen}, nil)
	})
	if err != nil {
		return nil, err
	}
	return seen, nil
}

// withContactRow returns owner as it is with c as its row of c's contact,
// in place of the row it had: the view the contact is shown in after a
// change of the row.
func withContactRow(owner *mtproto.ImmutableUser, c *mtproto.ContactData) *mtproto.ImmutableUser {
	view := proto.Clone(owner).(*mtproto.ImmutableUser)
	contacts := make([]*mtproto.ContactData, 0, len(view.Contacts)+1)
	for _, r := range view.Contacts {
		if r.GetContactUserId() != c.GetContactUserId() {
			contacts = append(contacts, r)
		}
	}
	view.Contacts = append(contacts, c)
	return view
}

// AddSuggestedPhoto records that fromUserId suggested the photo photoId to
// the user userId (photos.uploadContactProfilePhoto with suggest, task
// 051): the user may then make it its main photo (UpdateProfilePhoto),
// although another user uploaded it. The same photo suggested again keeps
// one record, with the latest sender and date.
func (d *Dao) AddSuggestedPhoto(ctx context.Context, userId, fromUserId, photoId int64) error {
	_, err := d.DB.Exec(ctx,
		"INSERT INTO user_suggested_photos (user_id, photo_id, from_user_id, date2) VALUES (?, ?, ?, ?) AS new"+
			" ON DUPLICATE KEY UPDATE from_user_id = new.from_user_id, date2 = new.date2",
		userId, photoId, fromUserId, time.Now().Unix())
	return err
}
