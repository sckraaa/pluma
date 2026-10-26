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
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The account's own name, username, photo and Premium with seq (ADR 0020):
// the change, its row of the seq journal and the push to every device of
// the account are one box transaction; a change that changes nothing
// writes none of them.

// seqContainers returns userId's pushes of updates with seq whole, in order.
func seqContainers(t *testing.T, userId int64) []*mtproto.Updates {
	t.Helper()
	var out []*mtproto.Updates
	for _, ev := range eventsOf(t, userId) {
		if ev.method != "sync.TL_sync_pushUpdates" {
			continue
		}
		in := &syncpb.TLSyncPushUpdates{}
		if err := jsonx.Unmarshal(ev.payload, in); err != nil {
			t.Fatal(err)
		}
		if in.GetUpdates().GetSeq() != 0 {
			out = append(out, in.GetUpdates())
		}
	}
	return out
}

// selfPushes checks that every event userId's changes wrote is a push with
// seq (seqPushes checks each one against its row of the seq journal), that
// they took the seqs after the base one by one, and returns them with the
// user each carries: the account as it sees itself.
func selfPushes(t *testing.T, userId int64) ([]seqPush, []*mtproto.User) {
	t.Helper()
	pushes := seqPushes(t, userId)
	if n := len(eventsOf(t, userId)); n != len(pushes) {
		t.Errorf("%d events, %d of them pushes with seq", n, len(pushes))
	}
	var users []*mtproto.User
	for i, c := range seqContainers(t, userId) {
		if pushes[i].seq != userbox.BaseSeq+int32(i)+1 {
			t.Errorf("push %d has seq %d", i, pushes[i].seq)
		}
		if len(c.GetUsers()) != 1 || len(c.GetChats()) != 0 {
			t.Fatalf("push of seq %d: %d users, %d chats; want the account alone", c.GetSeq(), len(c.GetUsers()), len(c.GetChats()))
		}
		u := c.GetUsers()[0]
		if u.GetId() != userId || !u.GetSelf() {
			t.Errorf("push of seq %d: user %d, self %v", c.GetSeq(), u.GetId(), u.GetSelf())
		}
		users = append(users, u)
	}
	if s := stateSeq(t, userId); s != userbox.BaseSeq+int32(len(pushes)) {
		t.Errorf("seq %d after %d pushes", s, len(pushes))
	}
	return pushes, users
}

func usernamesOf(u *mtproto.Update) string {
	var s []string
	for _, n := range u.GetUsernames() {
		s = append(s, fmt.Sprintf("%s/%v/%v", n.GetUsername(), n.GetEditable(), n.GetActive()))
	}
	return strings.Join(s, ",")
}

func TestOwnNameWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Alice")
	set := func(first, last string) {
		t.Helper()
		if _, err := c.UserUpdateFirstAndLastName(&user.TLUserUpdateFirstAndLastName{UserId: u, FirstName: first, LastName: last}); err != nil {
			t.Fatal(err)
		}
	}
	set("Alicia", "Smith")
	set("Alicia", "Smith") // changes nothing
	set("Alicia", "")

	var first, last string
	if err := itRaw.QueryRow("SELECT first_name, last_name FROM users WHERE id = ?", u).Scan(&first, &last); err != nil || first != "Alicia" || last != "" {
		t.Fatalf("stored name %q %q, %v", first, last, err)
	}
	me, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	if me.FirstName() != "Alicia" || me.LastName() != "" {
		t.Fatalf("served name %q %q: a stale cache", me.FirstName(), me.LastName())
	}

	pushes, users := selfPushes(t, u)
	var desc []string
	for i, p := range pushes {
		desc = append(desc, fmt.Sprintf("%s:%d:%s:%s:[%s]|%s:%s",
			p.update.GetPredicateName(), p.update.GetUserId(), p.update.GetFirstName(), p.update.GetLastName(), usernamesOf(p.update),
			users[i].GetFirstName().GetValue(), users[i].GetLastName().GetValue()))
	}
	want := fmt.Sprintf("[updateUserName:%d:Alicia:Smith:[]|Alicia:Smith updateUserName:%d:Alicia::[]|Alicia:]", u, u)
	if fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
}

func TestOwnUsernameWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Uma")
	v, _ := newUser(t, "Vic")
	name := fmt.Sprintf("uma%d", u)
	set := func(id int64, username string) error {
		t.Helper()
		_, err := c.UserUpdateUsername(&user.TLUserUpdateUsername{UserId: id, Username: username})
		return err
	}
	stored := func(id int64) string {
		t.Helper()
		var (
			column string
			rows   []string
		)
		if err := itRaw.QueryRow("SELECT username FROM users WHERE id = ?", id).Scan(&column); err != nil {
			t.Fatal(err)
		}
		r, err := itRaw.Query("SELECT username, editable, active FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_USER, id)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		for r.Next() {
			var (
				n                string
				editable, active bool
			)
			if err = r.Scan(&n, &editable, &active); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, fmt.Sprintf("%s/%v/%v", n, editable, active))
		}
		if err = r.Err(); err != nil {
			t.Fatal(err)
		}
		return column + " " + strings.Join(rows, ",")
	}

	if err := set(u, name); err != nil {
		t.Fatal(err)
	}
	if err := set(u, name); err != nil { // changes nothing
		t.Fatal(err)
	}
	if got, want := stored(u), name+" "+name+"/true/true"; got != want {
		t.Fatalf("stored %q, want %q", got, want)
	}
	// Taken, whatever the case; the one who tried changes nothing.
	for _, taken := range []string{name, strings.ToUpper(name)} {
		if err := set(v, taken); !errors.Is(err, mtproto.ErrUsernameOccupied) {
			t.Fatalf("%s for another user: %v, want USERNAME_OCCUPIED", taken, err)
		}
	}
	if got := stored(v); got != " " {
		t.Errorf("stored for the other user %q", got)
	}
	if n := len(eventsOf(t, v)); n != 0 || stateSeq(t, v) != userbox.BaseSeq {
		t.Errorf("the other user: %d events, seq %d", n, stateSeq(t, v))
	}
	// The account changes the case of its own.
	upper := strings.ToUpper(name)
	if err := set(u, upper); err != nil {
		t.Fatal(err)
	}
	if got, want := stored(u), upper+" "+upper+"/true/true"; got != want {
		t.Fatalf("stored %q, want %q", got, want)
	}
	if err := set(u, ""); err != nil {
		t.Fatal(err)
	}
	if got := stored(u); got != " " {
		t.Fatalf("stored after the removal %q", got)
	}
	me, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	if me.Username() != "" {
		t.Fatalf("served username %q: a stale cache", me.Username())
	}

	pushes, users := selfPushes(t, u)
	var desc []string
	for i, p := range pushes {
		desc = append(desc, fmt.Sprintf("%s:%s:[%s]|%s:%s",
			p.update.GetPredicateName(), p.update.GetFirstName(), usernamesOf(p.update),
			users[i].GetFirstName().GetValue(), users[i].GetUsername().GetValue()))
	}
	want := fmt.Sprintf("[updateUserName:Uma:[%s/true/true]|Uma:%s updateUserName:Uma:[%s/true/true]|Uma:%s updateUserName:Uma:[]|Uma:]",
		name, name, upper, upper)
	if fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
}

// fakeMedia serves the photos of the tests: there is no media server.
type fakeMedia struct {
	media_client.MediaClient
	mu     sync.Mutex
	photos map[int64]*mtproto.Photo
}

func newFakeMedia(ids ...int64) *fakeMedia {
	f := &fakeMedia{photos: make(map[int64]*mtproto.Photo)}
	for _, id := range ids {
		f.photos[id] = mtproto.MakeTLPhoto(&mtproto.Photo{
			Id:         id,
			AccessHash: id,
			Date:       1,
			DcId:       1,
			Sizes: []*mtproto.PhotoSize{
				mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "a", W: 160, H: 160, Size2: 100}).To_PhotoSize(),
			},
		}).To_Photo()
	}
	return f
}

func (f *fakeMedia) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.photos[in.GetPhotoId()]
	if !ok {
		return nil, fmt.Errorf("no photo %d", in.GetPhotoId())
	}
	return p, nil
}

// mediaCore is a UserCore whose media is f.
func mediaCore(t *testing.T, f *fakeMedia) *UserCore {
	t.Helper()
	d := *itSvc.Dao
	d.MediaClient = f
	s := *itSvc
	s.Dao = &d
	return New(context.Background(), &s)
}

func TestOwnPhotoWithSeq(t *testing.T) {
	const p1, p2, p3 = 7001, 7002, 7003
	c := mediaCore(t, newFakeMedia(p1, p2, p3))
	u, _ := newUser(t, "Pia")
	set := func(id int64) int64 {
		t.Helper()
		r, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: u, Id: id})
		if err != nil {
			t.Fatal(err)
		}
		return r.GetV()
	}
	del := func(ids ...int64) int64 {
		t.Helper()
		r, err := c.UserDeleteProfilePhotos(&user.TLUserDeleteProfilePhotos{UserId: u, Id: ids})
		if err != nil {
			t.Fatal(err)
		}
		return r.GetV()
	}
	stored := func() string {
		t.Helper()
		var main int64
		if err := itRaw.QueryRow("SELECT photo_id FROM users WHERE id = ?", u).Scan(&main); err != nil {
			t.Fatal(err)
		}
		r, err := itRaw.Query("SELECT photo_id FROM user_profile_photos WHERE user_id = ? AND deleted = 0 ORDER BY photo_id", u)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		var live []int64
		for r.Next() {
			var id int64
			if err = r.Scan(&id); err != nil {
				t.Fatal(err)
			}
			live = append(live, id)
		}
		if err = r.Err(); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%d %v", main, live)
	}

	steps := []struct {
		name   string
		do     func() int64
		main   int64
		stored string
	}{
		{"set p1", func() int64 { return set(p1) }, p1, fmt.Sprintf("%d [%d]", p1, p1)},
		{"set p1 again", func() int64 { return set(p1) }, p1, fmt.Sprintf("%d [%d]", p1, p1)},
		{"set p2", func() int64 { return set(p2) }, p2, fmt.Sprintf("%d [%d %d]", p2, p1, p2)},
		// The main photo goes: the other becomes main.
		{"delete p2", func() int64 { return del(p2) }, p1, fmt.Sprintf("%d [%d]", p1, p1)},
		{"delete p2 again", func() int64 { return del(p2) }, p1, fmt.Sprintf("%d [%d]", p1, p1)},
		{"delete a photo of nobody", func() int64 { return del(9999) }, p1, fmt.Sprintf("%d [%d]", p1, p1)},
		{"remove the main photo", func() int64 { return set(0) }, 0, "0 []"},
		{"remove when there is none", func() int64 { return set(0) }, 0, "0 []"},
	}
	for _, s := range steps {
		if main := s.do(); main != s.main {
			t.Errorf("%s: main %d, want %d", s.name, main, s.main)
		}
		if got := stored(); got != s.stored {
			t.Errorf("%s: stored %s, want %s", s.name, got, s.stored)
		}
	}

	// A photo set where the cache does not see it yet: the push carries
	// the one under the lock.
	set(p1)
	if _, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, u, false); err != nil {
		t.Fatal(err)
	}
	if _, err := itRaw.Exec("UPDATE users SET photo_id = ? WHERE id = ?", p3, u); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserUpdateFirstAndLastName(&user.TLUserUpdateFirstAndLastName{UserId: u, FirstName: "Pia", LastName: "Lind"}); err != nil {
		t.Fatal(err)
	}
	me, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	if me.Photo().GetId() != p3 {
		t.Fatalf("served photo %d: a stale cache", me.Photo().GetId())
	}

	pushes, users := selfPushes(t, u)
	var desc []string
	for i, p := range pushes {
		photo := "none"
		if ph := users[i].GetPhoto(); ph.GetPredicateName() == mtproto.Predicate_userProfilePhoto {
			photo = fmt.Sprint(ph.GetPhotoId())
		}
		desc = append(desc, fmt.Sprintf("%s:%d|%s", p.update.GetPredicateName(), p.update.GetUserId(), photo))
	}
	want := fmt.Sprintf("[updateUser:%[1]d|%[2]d updateUser:%[1]d|%[3]d updateUser:%[1]d|%[2]d updateUser:%[1]d|none updateUser:%[1]d|%[2]d updateUserName:%[1]d|%[4]d]",
		u, p1, p2, p3)
	if fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
}

func TestOwnPremiumWithSeq(t *testing.T) {
	ctx := context.Background()
	u, _ := newUser(t, "Pete")
	for _, change := range []dao.PremiumChange{{Grant: true}, {Grant: true}, {}, {}} {
		if _, err := ChangePremium(ctx, itSvc, u, change); err != nil {
			t.Fatal(err)
		}
	}
	pushes, users := selfPushes(t, u)
	var desc []string
	for i, p := range pushes {
		desc = append(desc, fmt.Sprintf("%s:%d|%v", p.update.GetPredicateName(), p.update.GetUserId(), users[i].GetPremium()))
	}
	if want := fmt.Sprintf("[updateUser:%[1]d|true updateUser:%[1]d|false]", u); fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
}
