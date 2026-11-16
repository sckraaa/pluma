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

//go:build integration

package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/peernames"
)

// addUsernames gives the account id the usernames names beside its
// editable one, hidden, after it in their order: what a purchase on
// Fragment would leave, which this server has no way to make.
func addUsernames(t *testing.T, id int64, names ...string) {
	t.Helper()
	var base int64
	if err := itRaw.QueryRow("SELECT COALESCE(MAX(order2), 0) FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_USER, id).Scan(&base); err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		if _, err := itRaw.Exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 0, 0, ?)",
			n, mtproto.PEER_USER, id, base+int64(i)+1); err != nil {
			t.Fatal(err)
		}
	}
}

// The usernames of an account beside its editable one (task 104): toggle
// and reorder change the rows and push updateUserName with seq and the
// usernames as they are now, in the same box transaction; a request that
// changes nothing, or is refused, writes nothing.
func TestEditUsernamesWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Una")
	main := fmt.Sprintf("una%d", u)
	one, two := fmt.Sprintf("una%d_one", u), fmt.Sprintf("una%d_two", u)
	if _, err := c.UserUpdateUsername(&user.TLUserUpdateUsername{UserId: u, Username: main}); err != nil {
		t.Fatal(err)
	}
	addUsernames(t, u, one, two)

	toggle := func(name string, active bool) error {
		_, err := c.UserEditUsernames(&user.TLUserEditUsernames{
			UserId: u, Action: user.UsernamesToggle, Username: name, Active: mtproto.ToBool(active),
		})
		return err
	}
	reorder := func(order ...string) error {
		_, err := c.UserEditUsernames(&user.TLUserEditUsernames{
			UserId: u, Action: user.UsernamesReorder, Active: mtproto.BoolFalse, Order: order,
		})
		return err
	}
	refused := func(what string, err, want error) {
		t.Helper()
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", what, err, want)
		}
	}

	if err := toggle(strings.ToUpper(one), true); err != nil {
		t.Fatal(err)
	}
	refused("showing it again", toggle(one, true), mtproto.ErrUsernameNotModified)
	refused("hiding the editable one", toggle(main, false), mtproto.ErrUsernameInvalid)
	refused("showing the editable one", toggle(main, true), mtproto.ErrUsernameNotModified)
	refused("another's name", toggle("nobody"+main, true), mtproto.ErrUsernameInvalid)
	refused("the order they have", reorder(main, one), mtproto.ErrUsernameNotModified)
	refused("an order without one", reorder(one), mtproto.ErrOrderInvalid)
	refused("an order with a hidden one", reorder(one, main, two), mtproto.ErrOrderInvalid)
	refused("an order with another's name", reorder(one, "nobody"+main), mtproto.ErrOrderInvalid)
	if err := reorder(one, main); err != nil {
		t.Fatal(err)
	}
	if err := toggle(one, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserEditUsernames(&user.TLUserEditUsernames{UserId: u, Action: 3, Active: mtproto.BoolFalse}); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Errorf("an unknown action: %v, want INPUT_REQUEST_INVALID", err)
	}

	me, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	var served []string
	for _, n := range me.User.GetUsernames() {
		served = append(served, fmt.Sprintf("%s/%v/%v", n.GetUsername(), n.GetEditable(), n.GetActive()))
	}
	if got, want := strings.Join(served, ","), fmt.Sprintf("%s/false/false,%s/true/true,%s/false/false", one, main, two); got != want {
		t.Errorf("served usernames %s, want %s: a stale cache", got, want)
	}

	pushes, users := selfPushes(t, u)
	var desc []string
	for i, p := range pushes {
		desc = append(desc, fmt.Sprintf("%s:[%s]|%d", p.update.GetPredicateName(), usernamesOf(p.update), len(users[i].GetUsernames())))
	}
	want := []string{
		// The user carries one username in user.username alone.
		fmt.Sprintf("updateUserName:[%s/true/true]|0", main),
		fmt.Sprintf("updateUserName:[%s/true/true,%s/false/true,%s/false/false]|3", main, one, two),
		fmt.Sprintf("updateUserName:[%s/false/true,%s/true/true,%s/false/false]|3", one, main, two),
		fmt.Sprintf("updateUserName:[%s/false/false,%s/true/true,%s/false/false]|3", one, main, two),
	}
	if fmt.Sprint(desc) != fmt.Sprint(want) {
		t.Errorf("pushes %v,\nwant %v", desc, want)
	}
}

// An account shows at most peernames.MaxShown usernames:
// USERNAMES_ACTIVE_TOO_MUCH past them, which tdesktop tells of
// (api_user_names.cpp:161-170).
func TestEditUsernamesLimit(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Lim")
	main := fmt.Sprintf("lim%d", u)
	if _, err := c.UserUpdateUsername(&user.TLUserUpdateUsername{UserId: u, Username: main}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := 0; i < peernames.MaxShown; i++ {
		names = append(names, fmt.Sprintf("lim%d_%d", u, i))
	}
	addUsernames(t, u, names...)
	for _, n := range names[:peernames.MaxShown-1] {
		if _, err := c.UserEditUsernames(&user.TLUserEditUsernames{UserId: u, Action: user.UsernamesToggle, Username: n, Active: mtproto.BoolTrue}); err != nil {
			t.Fatalf("showing %s: %v", n, err)
		}
	}
	before := len(eventsOf(t, u))
	last := names[peernames.MaxShown-1]
	if _, err := c.UserEditUsernames(&user.TLUserEditUsernames{UserId: u, Action: user.UsernamesToggle, Username: last, Active: mtproto.BoolTrue}); !errors.Is(err, mtproto.ErrUsernamesActiveTooMuch) {
		t.Errorf("an eleventh shown: %v, want USERNAMES_ACTIVE_TOO_MUCH", err)
	}
	if n := len(eventsOf(t, u)); n != before {
		t.Errorf("the refused toggle wrote %d events", n-before)
	}
	var shown int
	if err := itRaw.QueryRow("SELECT COUNT(*) FROM username WHERE peer_type = ? AND peer_id = ? AND active = 1", mtproto.PEER_USER, u).Scan(&shown); err != nil {
		t.Fatal(err)
	}
	if shown != peernames.MaxShown {
		t.Errorf("%d shown, want %d", shown, peernames.MaxShown)
	}
}
