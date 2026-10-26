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
	"errors"
	"fmt"
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// The account's own name, username, photo and Premium (ADR 0020): a change
// and the update with seq that tells every device of the account are one
// box transaction of the account. The update goes with the account as it
// sees itself after the change: TDLib takes the data from users, not from
// the update.

// selfRow is what the box transactions of an account change in its users
// row. Each of them reads all of it under the box lock and its push
// carries all of it, so two changes made at once do not undo each other on
// the devices.
type selfRow struct {
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Username  string `db:"username"`
	PhotoId   int64  `db:"photo_id"`
	Premium   bool   `db:"premium"`
	Until     int64  `db:"premium_expire_date"`
}

const selfRowQuery = "SELECT first_name, last_name, username, photo_id, premium, premium_expire_date FROM users WHERE id = ? AND deleted = 0 FOR UPDATE"

// selfAttempts bounds the box transactions of one change of the account:
// one more is run for each photo the transaction found main and the
// caller had not loaded (photos must be loaded before the transaction).
const selfAttempts = 3

// needPhotoError rolls a transaction back when it found a main photo that
// is not loaded: changeSelf loads it and runs the transaction again.
type needPhotoError struct{ id int64 }

func (e *needPhotoError) Error() string {
	return fmt.Sprintf("user: photo %d is not loaded", e.id)
}

// changeSelf runs change as a box transaction of the account id. change
// writes through b.Tx() and sets row to what it wrote, or returns false
// before any write when nothing changes. When it changed something, every
// device of the account gets the update update (updateUserName or
// updateUser) with seq, and the account as it sees itself: the row as
// written, the photo of its photo_id from photos (loaded before the
// transaction, with the cached photo of the account added), the usernames
// read in the transaction and the rest from the cache. It returns the row
// as committed, or as found by a no-op, and whether it changed.
func (d *Dao) changeSelf(ctx context.Context, id int64, update string, photos map[int64]*mtproto.Photo,
	change func(b *userbox.Box, row *selfRow) (bool, error)) (selfRow, bool, error) {
	me, err := d.GetImmutableUser(ctx, id, false)
	if err != nil {
		return selfRow{}, false, err
	}
	if photos == nil {
		photos = make(map[int64]*mtproto.Photo, 1)
	}
	if p := me.Photo(); p.GetId() != 0 {
		if _, ok := photos[p.GetId()]; !ok {
			photos[p.GetId()] = p
		}
	}

	for attempt := 1; ; attempt++ {
		var (
			row     selfRow
			changed bool
		)
		_, err = d.RunBox(ctx, id, []string{genCacheUserDataCacheKey(id)}, func(b *userbox.Box) error {
			changed = false
			var rows []selfRow
			if err := b.Tx().QueryRowsPartial(&rows, selfRowQuery, id); err != nil {
				return err
			}
			if len(rows) == 0 {
				return mtproto.ErrUserIdInvalid
			}
			row = rows[0]
			ok, err := change(b, &row)
			if err != nil || !ok {
				return err
			}
			photo, ok := photos[row.PhotoId]
			if !ok && row.PhotoId != 0 {
				return &needPhotoError{id: row.PhotoId}
			}
			usernames, err := selfUsernames(b, id, row.Username)
			if err != nil {
				return err
			}
			var u *mtproto.Update
			switch update {
			case mtproto.Predicate_updateUserName:
				u = mtproto.MakeTLUpdateUserName(&mtproto.Update{
					UserId:    id,
					FirstName: row.FirstName,
					LastName:  row.LastName,
					Usernames: usernames,
				}).To_Update()
			case mtproto.Predicate_updateUser:
				u = mtproto.MakeTLUpdateUser(&mtproto.Update{UserId: id}).To_Update()
			default:
				return fmt.Errorf("user: %s is not an update of the account itself", update)
			}
			changed = true
			return d.EmitSeqUpdate(b, u, []*mtproto.User{selfUser(me, row, usernames, photo)}, nil)
		})
		var np *needPhotoError
		switch {
		case err == nil:
			return row, changed, nil
		case !errors.As(err, &np) || attempt >= selfAttempts:
			return selfRow{}, false, err
		}
		p, perr := d.MediaClient.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: np.id})
		if perr != nil {
			return selfRow{}, false, perr
		}
		photos[np.id] = p
	}
}

// selfUsernames reads the usernames of the account with username as its
// row has it, as its user shows them: the rows of the username table when
// there are several, otherwise username alone.
func selfUsernames(b *userbox.Box, id int64, username string) ([]*mtproto.Username, error) {
	if username == "" {
		return nil, nil
	}
	var rows []dataobject.UsernameDO
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT username, editable, active FROM username WHERE peer_type = ? AND peer_id = ? ORDER BY order2, id",
		mtproto.PEER_USER, id); err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return []*mtproto.Username{
			mtproto.MakeTLUsername(&mtproto.Username{Editable: true, Active: true, Username: username}).To_Username(),
		}, nil
	}
	usernames := make([]*mtproto.Username, 0, len(rows))
	for _, r := range rows {
		usernames = append(usernames, mtproto.MakeTLUsername(&mtproto.Username{
			Editable: r.Editable,
			Active:   r.Active,
			Username: r.Username,
		}).To_Username())
	}
	return usernames, nil
}

// selfUser is the account me as it sees itself with row, its usernames and
// its main photo.
func selfUser(me *mtproto.ImmutableUser, row selfRow, usernames []*mtproto.Username, photo *mtproto.Photo) *mtproto.User {
	view := proto.Clone(me).(*mtproto.ImmutableUser)
	u := view.User
	u.FirstName = row.FirstName
	u.LastName = row.LastName
	u.Username = row.Username
	u.Usernames = usernames
	u.ProfilePhoto = photo
	u.Premium = row.Premium
	u.PremiumExpireDate = mtproto.MakeFlagsInt64(row.Until)
	return view.ToSelfUser()
}

// UpdateFirstAndLastName sets the name of the account id; its devices get
// updateUserName with seq (ADR 0020). The same name changes nothing.
func (d *Dao) UpdateFirstAndLastName(ctx context.Context, id int64, firstName, lastName string) error {
	_, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUserName, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if row.FirstName == firstName && row.LastName == lastName {
			return false, nil
		}
		if _, err := d.UsersDAO.UpdateFirstAndLastNameTx(b.Tx(), firstName, lastName, id); err != nil {
			return false, err
		}
		row.FirstName, row.LastName = firstName, lastName
		return true, nil
	})
	return err
}

// UpdateUsername sets the username of the account id, "" removes it: the
// editable row of the account in the username table and users.username
// change together, and its devices get updateUserName with seq (ADR 0020).
// The same username changes nothing; one taken by another peer is
// USERNAME_OCCUPIED. The format is checked by the caller.
func (d *Dao) UpdateUsername(ctx context.Context, id int64, username string) error {
	_, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUserName, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if row.Username == username {
			return false, nil
		}
		if username != "" {
			var owners []dataobject.UsernameDO
			if err := b.Tx().QueryRowsPartial(&owners,
				"SELECT peer_type, peer_id FROM username WHERE username = ? FOR UPDATE", username); err != nil {
				return false, err
			}
			// The table compares usernames without case: the account may
			// change the case of its own.
			if len(owners) > 0 && (owners[0].PeerType != mtproto.PEER_USER || owners[0].PeerId != id) {
				return false, mtproto.ErrUsernameOccupied
			}
		}
		// An account has one editable username: the new one replaces it.
		if _, err := d.UsernameDAO.Delete2Tx(b.Tx(), mtproto.PEER_USER, id); err != nil {
			return false, err
		}
		if username != "" {
			if _, _, err := d.UsernameDAO.InsertTx(b.Tx(), &dataobject.UsernameDO{
				Username: username,
				PeerType: mtproto.PEER_USER,
				PeerId:   id,
				Editable: true,
				Active:   true,
				Order2:   b.Date() << 32,
			}); err != nil {
				return false, err
			}
		}
		if _, err := d.UsersDAO.UpdateUsernameTx(b.Tx(), username, id); err != nil {
			return false, err
		}
		row.Username = username
		return true, nil
	})
	if sqlx.IsDuplicate(err) {
		// Taken by another peer since the check.
		return mtproto.ErrUsernameOccupied
	}
	return err
}

// UpdateProfilePhoto makes photoId the main photo of the account id, or with
// photoId 0 removes the main photo and makes the latest of the others main.
// Its devices get updateUser with seq (ADR 0020); the main photo already
// set, or no photo to remove, changes nothing. A photo media does not have
// is PHOTO_ID_INVALID. It returns the main photo now.
func (d *Dao) UpdateProfilePhoto(ctx context.Context, id int64, photoId int64) (int64, error) {
	var photos map[int64]*mtproto.Photo
	if photoId != 0 {
		p, err := d.MediaClient.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: photoId})
		if err != nil {
			return 0, err
		}
		if p.GetPredicateName() != mtproto.Predicate_photo {
			return 0, mtproto.ErrPhotoIdInvalid
		}
		photos = map[int64]*mtproto.Photo{photoId: p}
	}
	row, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, photos, func(b *userbox.Box, row *selfRow) (bool, error) {
		if photoId != 0 {
			if row.PhotoId == photoId {
				return false, nil
			}
			if _, _, err := d.UserProfilePhotosDAO.InsertOrUpdateTx(b.Tx(), &dataobject.UserProfilePhotosDO{
				UserId:  id,
				PhotoId: photoId,
				Date2:   b.Date(),
			}); err != nil {
				return false, err
			}
			if _, err := d.UsersDAO.UpdateProfilePhotoTx(b.Tx(), photoId, id); err != nil {
				return false, err
			}
			row.PhotoId = photoId
			return true, nil
		}
		if row.PhotoId == 0 {
			return false, nil
		}
		return d.deleteProfilePhotos(b, id, row, []int64{row.PhotoId})
	})
	return row.PhotoId, err
}

// DeleteProfilePhotos deletes the photos ids of the account id. When the
// main photo is among them, the latest of the others becomes main. Its
// devices get updateUser with seq (ADR 0020); none of the account's photos
// among ids changes nothing. It returns the main photo now.
func (d *Dao) DeleteProfilePhotos(ctx context.Context, id int64, ids []int64) (int64, error) {
	row, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		return d.deleteProfilePhotos(b, id, row, ids)
	})
	return row.PhotoId, err
}

// deleteProfilePhotos deletes the photos ids of the account id in b and
// moves its main photo, row.PhotoId, to the latest one left when it is
// among them.
func (d *Dao) deleteProfilePhotos(b *userbox.Box, id int64, row *selfRow, ids []int64) (bool, error) {
	var live []int64
	if err := b.Tx().QueryRowsPartial(&live,
		"SELECT photo_id FROM user_profile_photos WHERE user_id = ? AND deleted = 0 ORDER BY date2 DESC, id DESC FOR UPDATE", id); err != nil {
		return false, err
	}
	var (
		gone []int64
		next int64
	)
	for _, p := range live {
		if slices.Contains(ids, p) {
			gone = append(gone, p)
		} else if next == 0 {
			next = p
		}
	}
	mainGone := row.PhotoId != 0 && slices.Contains(ids, row.PhotoId)
	if len(gone) == 0 && !mainGone {
		return false, nil
	}
	if len(gone) > 0 {
		if _, err := d.UserProfilePhotosDAO.DeleteTx(b.Tx(), id, gone); err != nil {
			return false, err
		}
	}
	if mainGone {
		if _, err := d.UsersDAO.UpdateProfilePhotoTx(b.Tx(), next, id); err != nil {
			return false, err
		}
		row.PhotoId = next
	}
	return true, nil
}
