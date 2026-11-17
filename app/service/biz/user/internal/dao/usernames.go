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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/peernames"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// EditUsernames changes the usernames of the account id beside its
// editable one (task 104), as a channel's are changed (task 078,
// pkg/peernames). Its usernames are its rows of the username table: the
// editable one, set by account.updateUsername, and the others, which
// Telegram sells on Fragment and this server has no way to give; the
// methods work on what the rows hold. By action:
//
//   - user.UsernamesToggle shows (active) or hides username, one of the
//     account's: USERNAME_INVALID for another, USERNAME_NOT_MODIFIED for
//     its state, USERNAMES_ACTIVE_TOO_MUCH past peernames.MaxShown shown
//     (core.telegram.org/method/account.toggleUsername). The editable one
//     is always shown: the account drops it by account.updateUsername with
//     "", so hiding it is USERNAME_INVALID; Android does not offer it
//     either, it puts the cursor in the field instead
//     (ChangeUsernameActivity.java:281-284);
//   - user.UsernamesReorder orders the shown usernames as order, which
//     lists each of them once ("all active usernames must be specified",
//     core.telegram.org/method/account.reorderUsernames; Android sends
//     the shown ones only, ChangeUsernameActivity.java:613-640):
//     ORDER_INVALID otherwise, USERNAME_NOT_MODIFIED for their order.
//
// The rows are read and changed in the account's box transaction
// (changeSelf), which pushes updateUserName with seq and the usernames as
// they are now to every device of the account and tells the users who see
// the account (ADR 0020, ADR 0021). A change that changes nothing is one of
// the errors above and writes nothing. An unknown action is
// INPUT_REQUEST_INVALID.
func (d *Dao) EditUsernames(ctx context.Context, id int64, action int32, username string, active bool, order []string) error {
	if action != user.UsernamesToggle && action != user.UsernamesReorder {
		return mtproto.ErrInputRequestInvalid
	}
	_, _, err := d.changeSelf(ctx, id, mtproto.Predicate_updateUserName, nil, func(b *userbox.Box, _ *selfRow) (bool, error) {
		rows, err := peernames.Load(b, mtproto.PEER_USER, id)
		if err != nil {
			return false, err
		}
		if action == user.UsernamesToggle {
			err = peernames.Toggle(b, rows, username, active)
		} else {
			err = peernames.Reorder(b, rows, order)
		}
		if err != nil {
			return false, accountUsernamesError(err)
		}
		return true, nil
	})
	return err
}

// accountUsernamesError is the error of account.toggleUsername or
// account.reorderUsernames for an error of peernames, as
// core.telegram.org/method/account.toggleUsername and
// core.telegram.org/method/account.reorderUsernames name them; another
// error is returned as it is. An order that is not every shown username
// once is ORDER_INVALID here, and the order they have
// USERNAME_NOT_MODIFIED, where a channel answers USERNAME_INVALID and
// CHAT_NOT_MODIFIED.
func accountUsernamesError(err error) error {
	switch {
	case errors.Is(err, peernames.ErrNotFound), errors.Is(err, peernames.ErrEditable):
		return mtproto.ErrUsernameInvalid
	case errors.Is(err, peernames.ErrSame), errors.Is(err, peernames.ErrSameOrder):
		return mtproto.ErrUsernameNotModified
	case errors.Is(err, peernames.ErrTooMany):
		return mtproto.ErrUsernamesActiveTooMuch
	case errors.Is(err, peernames.ErrBadOrder):
		return mtproto.ErrOrderInvalid
	}
	return err
}
