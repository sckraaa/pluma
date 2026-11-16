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

// Package peernames keeps the usernames of a peer beside its editable one:
// which of them it shows and in what order (tasks 078 and 104). A peer's
// usernames are its rows of the username table, (peer_type, peer_id): the
// editable one, its public link, and the others, which Telegram sells on
// Fragment. The functions work in the box transaction of the account that
// changes them, on the rows Load read there FOR UPDATE, and answer with
// their own errors, which each caller turns into the errors of its method:
// channels.toggleUsername and account.toggleUsername differ in them
// (core.telegram.org/method/channels.reorderUsernames,
// core.telegram.org/method/account.reorderUsernames).
package peernames

import (
	"errors"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MaxShown is how many usernames a peer shows at most; the clients tell of
// it on USERNAMES_ACTIVE_TOO_MUCH (tdesktop kMaxUsernames,
// edit_peer_usernames_list.cpp:338).
const MaxShown = 10

var (
	// ErrNotFound: the username is none of the peer's.
	ErrNotFound = errors.New("peernames: not a username of the peer")
	// ErrEditable: hiding the editable username, which is always shown;
	// the peer drops it by setting no username instead
	// (account.updateUsername, channels.updateUsername).
	ErrEditable = errors.New("peernames: the editable username is always shown")
	// ErrSame: the username is already shown, or hidden.
	ErrSame = errors.New("peernames: the username is in that state")
	// ErrTooMany: showing one more than MaxShown.
	ErrTooMany = errors.New("peernames: too many usernames shown")
	// ErrBadOrder: the order does not list each shown username once.
	ErrBadOrder = errors.New("peernames: the order does not list each shown username once")
	// ErrSameOrder: the shown usernames are in that order already.
	ErrSameOrder = errors.New("peernames: the usernames are in that order")
)

// Row is a row of the username table of a peer.
type Row struct {
	Id       int64  `db:"id"`
	Username string `db:"username"`
	Editable bool   `db:"editable"`
	Active   bool   `db:"active"`
}

// Load reads the usernames of the peer in b in their order, locked until
// the commit, so that two changes made at once apply one after the other.
func Load(b *userbox.Box, peerType int32, peerId int64) ([]Row, error) {
	var rows []Row
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, username, editable, active FROM username WHERE peer_type = ? AND peer_id = ? ORDER BY order2, id FOR UPDATE",
		peerType, peerId); err != nil {
		return nil, err
	}
	return rows, nil
}

// Active are the usernames of rows shown, in their order.
func Active(rows []Row) []string {
	names := []string{}
	for _, r := range rows {
		if r.Active {
			names = append(names, r.Username)
		}
	}
	return names
}

// Toggle shows (active) or hides username, one of rows, in b. The table
// compares usernames without case, and so does the match. The state the
// username has is ErrSame, before anything else: showing the editable one
// asks for nothing; hiding it is ErrEditable. Showing one more than
// MaxShown is ErrTooMany (core.telegram.org/method/account.toggleUsername:
// USERNAMES_ACTIVE_TOO_MUCH); a username not in rows is ErrNotFound.
func Toggle(b *userbox.Box, rows []Row, username string, active bool) error {
	r, err := toggled(rows, username, active)
	if err != nil {
		return err
	}
	_, err = b.Tx().Exec("UPDATE username SET active = ? WHERE id = ?", active, r.Id)
	return err
}

// toggled finds the row of username that Toggle changes, or why it does
// not change it.
func toggled(rows []Row, username string, active bool) (Row, error) {
	for _, r := range rows {
		if !strings.EqualFold(r.Username, username) {
			continue
		}
		switch {
		case r.Active == active:
			return Row{}, ErrSame
		case r.Editable:
			return Row{}, ErrEditable
		case active && len(Active(rows)) >= MaxShown:
			return Row{}, ErrTooMany
		}
		return r, nil
	}
	return Row{}, ErrNotFound
}

// Order places the shown usernames of rows as order: the index in order of
// each, by row id. order must list each of them once, in any case of the
// letters ("all active usernames must be specified",
// core.telegram.org/method/account.reorderUsernames), or it is ErrBadOrder;
// their order as it is is ErrSameOrder.
func Order(rows []Row, order []string) (map[int64]int, error) {
	shown := 0
	for _, r := range rows {
		if r.Active {
			shown++
		}
	}
	if shown != len(order) {
		return nil, ErrBadOrder
	}
	at := make(map[int64]int, len(order))
	same := true
	i := 0
	for _, r := range rows {
		if !r.Active {
			continue
		}
		found := false
		for j, name := range order {
			if strings.EqualFold(r.Username, name) {
				if found {
					return nil, ErrBadOrder
				}
				at[r.Id], found = j, true
			}
		}
		if !found {
			return nil, ErrBadOrder
		}
		same = same && at[r.Id] == i
		i++
	}
	if same {
		return nil, ErrSameOrder
	}
	return at, nil
}

// Reorder orders the shown usernames of rows as order (Order) in b; the
// hidden ones follow them, in their order. The positions start from the
// box's time, so that they stay after those written before.
func Reorder(b *userbox.Box, rows []Row, order []string) error {
	at, err := Order(rows, order)
	if err != nil {
		return err
	}
	base := b.Date() << 32
	hidden := int64(len(order))
	for _, r := range rows {
		pos := hidden
		if r.Active {
			pos = int64(at[r.Id])
		} else {
			hidden++
		}
		if _, err := b.Tx().Exec("UPDATE username SET order2 = ? WHERE id = ?", base+pos, r.Id); err != nil {
			return err
		}
	}
	return nil
}
