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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/emojistatus"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// The account's own name, username and photo (ADR 0020): a change
// and the update with seq that tells every device of the account are one
// box transaction of the account. The update goes with the account as it
// sees itself after the change: TDLib takes the data from users, not from
// the update. The same transaction writes the inbox events that give the
// update to the users who see the account (ADR 0021).

// selfRow is what the box transactions of an account change in its users
// row. Each of them reads all of it under the box lock and its push
// carries all of it, so two changes made at once do not undo each other on
// the devices.
type selfRow struct {
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Username  string `db:"username"`
	PhotoId   int64  `db:"photo_id"`
	// FallbackPhotoId is the public photo (task 051): not in the account's
	// own user, so the push does not carry it.
	FallbackPhotoId int64 `db:"fallback_photo_id"`
	// Verified is the verification badge, set by the administrator
	// (UpdateVerified).
	Verified bool `db:"verified"`
	// Ghost is the support ghost's flag (SetGhost, ADR 0048).
	Ghost bool `db:"ghost"`
	// The look of the account (task 084): the color of its name and of
	// its profile, user.NoColor for none, each with its pattern of custom
	// emoji (0 for none), and its emoji status (0 for none) with its end
	// (0 for no end).
	Color                         int32 `db:"color"`
	ColorBackgroundEmojiId        int64 `db:"color_background_emoji_id"`
	ProfileColor                  int32 `db:"profile_color"`
	ProfileColorBackgroundEmojiId int64 `db:"profile_color_background_emoji_id"`
	EmojiStatusDocumentId         int64 `db:"emoji_status_document_id"`
	EmojiStatusUntil              int32 `db:"emoji_status_until"`
}

const selfRowQuery = "SELECT first_name, last_name, username, photo_id, fallback_photo_id, verified, ghost," +
	" color, color_background_emoji_id, profile_color, profile_color_background_emoji_id, emoji_status_document_id, emoji_status_until" +
	" FROM users WHERE id = ? AND deleted = 0 FOR UPDATE"

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
// device of the account gets the update update (updateUserName,
// updateUser or updateUserEmojiStatus) with seq, and the account as it
// sees itself: the row as
// written, the photo of its photo_id from photos (loaded before the
// transaction, with the cached photo of the account added), the usernames
// read in the transaction and the rest from the cache. The users who see
// the account in their lists get the same update about another user
// (tellWatchers). It returns the row as committed, or as found by a no-op,
// and whether it changed.
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
			case mtproto.Predicate_updateUserEmojiStatus:
				status := user.MakeEmojiStatus(row.EmojiStatusDocumentId, row.EmojiStatusUntil)
				if status == nil {
					status = mtproto.MakeTLEmojiStatusEmpty(nil).To_EmojiStatus()
				}
				u = mtproto.MakeTLUpdateUserEmojiStatus(&mtproto.Update{UserId: id, EmojiStatus: status}).To_Update()
			default:
				return fmt.Errorf("user: %s is not an update of the account itself", update)
			}
			changed = true
			if err = d.EmitSeqUpdate(b, u, []*mtproto.User{selfUser(me, row, usernames, photo)}, nil); err != nil {
				return err
			}
			return d.tellWatchers(b, id, u)
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
	u.Verified = row.Verified
	u.Ghost = row.Ghost
	u.Color = user.MakePeerColor(row.Color, row.ColorBackgroundEmojiId)
	u.ProfileColor = user.MakePeerColor(row.ProfileColor, row.ProfileColorBackgroundEmojiId)
	u.EmojiStatus = user.MakeEmojiStatus(row.EmojiStatusDocumentId, row.EmojiStatusUntil)
	return view.ToSelfUser()
}

// UpdateEmojiStatus sets the emoji status of the account id: the custom
// emoji documentId until the date until (0 for no end), or none with
// documentId 0. An end that has passed is no status. Its devices and the
// users who see the account get updateUserEmojiStatus with seq ("other
// users will receive an updateUserEmojiStatus",
// core.telegram.org/api/emoji-status; Android MessagesController.java:20042
// and tdesktop api_updates.cpp:2862 set the status of the user from it). A
// status set joins the recent statuses of the account in the same
// transaction (emojistatus.Use), and the devices are told to load them
// again. The same status changes nothing. The caller checks that the
// document is a custom emoji.
func (d *Dao) UpdateEmojiStatus(ctx context.Context, id, documentId int64, until int32) error {
	if documentId == 0 {
		until = 0
	}
	_, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUserEmojiStatus, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if until != 0 && int64(until) <= b.Date() {
			documentId, until = 0, 0
		}
		if row.EmojiStatusDocumentId == documentId && row.EmojiStatusUntil == until {
			return false, nil
		}
		if _, err := d.UsersDAO.UpdateEmojiStatusTx(b.Tx(), documentId, until, id); err != nil {
			return false, err
		}
		row.EmojiStatusDocumentId, row.EmojiStatusUntil = documentId, until
		if documentId != 0 {
			listed, err := emojistatus.Use(b, documentId)
			if err != nil {
				return false, err
			}
			if listed {
				if err = d.EmitSeqUpdate(b, emojistatus.Changed(), nil, nil); err != nil {
					return false, err
				}
			}
		}
		return true, nil
	})
	return err
}

// UpdateColor sets the color of the name of the account id or, with
// forProfile, of its profile, with its pattern of custom emoji: color
// user.NoColor and emojiId 0 are none. Its devices and the users who see
// the account get updateUser with seq and the account with its colors: the
// clients take them from the user (tdesktop data_session.cpp:905-911
// applies user.color and user.profile_color of every user it gets). The same color
// changes nothing. The caller checks that the color and the emoji exist.
func (d *Dao) UpdateColor(ctx context.Context, id int64, forProfile bool, color int32, emojiId int64) error {
	if color < 0 {
		color = user.NoColor
	}
	_, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if forProfile {
			if row.ProfileColor == color && row.ProfileColorBackgroundEmojiId == emojiId {
				return false, nil
			}
			if _, err := d.UsersDAO.UpdateProfileColorTx(b.Tx(), color, emojiId, id); err != nil {
				return false, err
			}
			row.ProfileColor, row.ProfileColorBackgroundEmojiId = color, emojiId
			return true, nil
		}
		if row.Color == color && row.ColorBackgroundEmojiId == emojiId {
			return false, nil
		}
		if _, err := d.UsersDAO.UpdateColorTx(b.Tx(), color, emojiId, id); err != nil {
			return false, err
		}
		row.Color, row.ColorBackgroundEmojiId = color, emojiId
		return true, nil
	})
	return err
}

// UpdateVerified sets or clears the verification badge of the account id
// (user.verified). Its devices get updateUser with seq (ADR 0020) and the
// users who see the account get it as well (tellWatchers): the clients take
// the flag from the user that comes with the update or fetch the account
// again. The same value changes nothing; it returns whether it changed.
func (d *Dao) UpdateVerified(ctx context.Context, id int64, verified bool) (bool, error) {
	_, changed, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if row.Verified == verified {
			return false, nil
		}
		if _, err := b.Tx().Exec("UPDATE users SET verified = ? WHERE id = ?", verified, id); err != nil {
			return false, err
		}
		row.Verified = verified
		return true, nil
	})
	return changed, err
}

// SetGhost makes the account id the support ghost or an ordinary account
// again (users.ghost, ADR 0048): the services that read the flag from the
// table (pkg/ghost) see it at once, the cached user is dropped, and the
// account's devices and the users who see it get updateUser with seq, so
// that a client which kept the account's phone fetches the account without
// it. It tells whether the flag changed.
func (d *Dao) SetGhost(ctx context.Context, id int64, ghost bool) (bool, error) {
	_, changed, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if row.Ghost == ghost {
			return false, nil
		}
		if _, err := b.Tx().Exec("UPDATE users SET ghost = ? WHERE id = ?", ghost, id); err != nil {
			return false, err
		}
		row.Ghost = ghost
		return true, nil
	})
	return changed, err
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
// The same username changes nothing, which it tells by false; one taken by
// another peer is USERNAME_OCCUPIED. The format is checked by the caller.
func (d *Dao) UpdateUsername(ctx context.Context, id int64, username string) (bool, error) {
	_, changed, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUserName, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
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
		return false, mtproto.ErrUsernameOccupied
	}
	return changed, err
}

// ownPhotoQuery tells whether a photo is one of the account's photos.
const ownPhotoQuery = "SELECT COUNT(*) FROM user_profile_photos WHERE user_id = ? AND photo_id = ? AND deleted = 0"

// suggestedPhotoQuery tells whether another user suggested the photo to the
// account (photos.uploadContactProfilePhoto with suggest, task 051).
const suggestedPhotoQuery = "SELECT COUNT(*) FROM user_suggested_photos WHERE user_id = ? AND photo_id = ?"

// UpdateProfilePhoto makes photoId the main photo of the account id, or with
// photoId 0 removes the main photo and makes the latest of the others main.
// photoId is one of the account's photos, its public photo or one suggested
// to it, or uploaded for it just now; the main photo becomes one of its
// photos. Its devices get updateUser with seq (ADR 0020); the main photo
// already set, or no photo to remove, changes nothing. A photo media does
// not have, or that is none of those, is PHOTO_ID_INVALID. It returns the
// main photo now.
//
// With fallback it sets the public photo instead (task 051): photoId, one
// of the account's photos or uploaded just now, or none with photoId 0.
// The main photo and the album stay. The account's devices and the users
// who see it get updateUser with seq as for the main photo: what those the
// privacy hides the main photo from see changes. It returns the public
// photo now.
func (d *Dao) UpdateProfilePhoto(ctx context.Context, id int64, photoId int64, uploaded, fallback bool) (int64, error) {
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
	if fallback {
		return d.updateFallbackPhoto(ctx, id, photoId, uploaded)
	}
	row, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, photos, func(b *userbox.Box, row *selfRow) (bool, error) {
		if photoId != 0 {
			if row.PhotoId == photoId {
				return false, nil
			}
			if !uploaded && photoId != row.FallbackPhotoId {
				// The access hash bff checked tells only that the caller
				// sees the photo: another user's photo is not the
				// account's to make main, unless that user suggested
				// it. The box lock keeps the account's photos as read
				// until the commit.
				ok, err := isOwnPhoto(b, id, photoId, true)
				if err != nil {
					return false, err
				}
				if !ok {
					return false, mtproto.ErrPhotoIdInvalid
				}
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
		return d.deleteProfilePhotos(b, id, row, []int64{row.PhotoId}, false)
	})
	return row.PhotoId, err
}

// isOwnPhoto tells, in b, whether photoId is one of the account id's photos
// or, with suggested, one another user suggested to it.
func isOwnPhoto(b *userbox.Box, id, photoId int64, suggested bool) (bool, error) {
	var n int
	if err := b.Tx().QueryRow(&n, ownPhotoQuery, id, photoId); err != nil || n > 0 {
		return n > 0, err
	}
	if !suggested {
		return false, nil
	}
	if err := b.Tx().QueryRow(&n, suggestedPhotoQuery, id, photoId); err != nil {
		return false, err
	}
	return n > 0, nil
}

// updateFallbackPhoto sets the public photo of the account id to photoId,
// or removes it with photoId 0 (UpdateProfilePhoto with fallback). A photo
// not uploaded just now must be one of the account's photos: not one
// suggested to it, since the clients set the public photo from a new upload.
func (d *Dao) updateFallbackPhoto(ctx context.Context, id, photoId int64, uploaded bool) (int64, error) {
	row, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		if row.FallbackPhotoId == photoId {
			return false, nil
		}
		if photoId != 0 && !uploaded && photoId != row.PhotoId {
			ok, err := isOwnPhoto(b, id, photoId, false)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, mtproto.ErrPhotoIdInvalid
			}
		}
		if _, err := b.Tx().Exec("UPDATE users SET fallback_photo_id = ? WHERE id = ?", photoId, id); err != nil {
			return false, err
		}
		row.FallbackPhotoId = photoId
		return true, nil
	})
	return row.FallbackPhotoId, err
}

// DeleteProfilePhotos deletes the photos ids of the account id. When the
// main photo is among them, the latest of the others becomes main; the
// public photo among them is removed (Android removes it so, task 051). Its
// devices get updateUser with seq (ADR 0020); none of the account's photos
// among ids changes nothing. It returns the main photo now.
func (d *Dao) DeleteProfilePhotos(ctx context.Context, id int64, ids []int64) (int64, error) {
	row, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUser, nil, func(b *userbox.Box, row *selfRow) (bool, error) {
		return d.deleteProfilePhotos(b, id, row, ids, true)
	})
	return row.PhotoId, err
}

// deleteProfilePhotos deletes the photos ids of the account id in b and
// moves its main photo, row.PhotoId, to the latest one left when it is
// among them. With fallback the public photo, row.FallbackPhotoId, is
// removed too when it is among them.
func (d *Dao) deleteProfilePhotos(b *userbox.Box, id int64, row *selfRow, ids []int64, fallback bool) (bool, error) {
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
	fallbackGone := fallback && row.FallbackPhotoId != 0 && slices.Contains(ids, row.FallbackPhotoId)
	if len(gone) == 0 && !mainGone && !fallbackGone {
		return false, nil
	}
	if fallbackGone {
		if _, err := b.Tx().Exec("UPDATE users SET fallback_photo_id = 0 WHERE id = ?", id); err != nil {
			return false, err
		}
		row.FallbackPhotoId = 0
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
