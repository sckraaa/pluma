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

package dao

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/config"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The folders of a user on MySQL and Redis (task 013), with and without
// Premium (task 014).

var (
	itDao  *Dao
	itUser atomic.Int64
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg    sync.WaitGroup
		mysql *testinfra.MySQL
		redis *testinfra.Redis
		errs  = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); mysql, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); redis, errs[1] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		itDao = New(config.Config{Mysql: mysql.Config(), Cache: redis.CacheConf()})
		itUser.Store(time.Now().UnixNano() % 1_000_000_000 * 1000)
		code = m.Run()
	}
	if mysql != nil {
		_ = mysql.Terminate(ctx)
	}
	if redis != nil {
		_ = redis.Terminate(ctx)
	}
	os.Exit(code)
}

func newUser() int64 { return itUser.Add(1) }

// newPremiumUser returns a user with Premium for good.
func newPremiumUser(t *testing.T) int64 {
	t.Helper()
	u := newUser()
	if _, err := itDao.Mysql.DB.Exec(context.Background(),
		"INSERT INTO users (id, access_hash, phone, country_code, premium) VALUES (?, 1, ?, 'US', 1)", u, fmt.Sprint("1999", u)); err != nil {
		t.Fatal(err)
	}
	return u
}

// setPremium gives userId Premium until the moment given (0 — for good) or
// takes it away.
func setPremium(t *testing.T, userId int64, premium bool, until int64) {
	t.Helper()
	if _, err := itDao.Mysql.DB.Exec(context.Background(),
		"UPDATE users SET premium = ?, premium_expire_date = ? WHERE id = ?", premium, until, userId); err != nil {
		t.Fatal(err)
	}
}

// withChats returns folder id that includes n chats and excludes m.
func withChats(id int32, n, m int) *mtproto.DialogFilter {
	f := folder(id, "Chats")
	for i := range n {
		f.IncludePeers = append(f.IncludePeers, mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: int64(i + 1)}).To_InputPeer())
	}
	for i := range m {
		f.ExcludePeers = append(f.ExcludePeers, mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: int64(n + i + 1)}).To_InputPeer())
	}
	return f
}

func folder(id int32, name string) *mtproto.DialogFilter {
	return mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id: id,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{
			Text: name,
		}).To_TextWithEntities(),
		Contacts: true,
	}).To_DialogFilter()
}

// stored returns the ids of userId's folders in their order and their titles.
func stored(t *testing.T, userId int64) ([]int32, map[int32]string) {
	t.Helper()
	rows, err := itDao.DialogFiltersDAO.SelectList(context.Background(), userId)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int32, 0, len(rows))
	titles := make(map[int32]string, len(rows))
	for _, r := range rows {
		f, err := mtproto.UnmarshalDialogFilter(r.DialogFilter)
		if err != nil {
			t.Fatalf("folder %d: %v", r.DialogFilterId, err)
		}
		ids = append(ids, r.DialogFilterId)
		titles[r.DialogFilterId] = f.GetTitle_TEXTWITHENTITIES().GetText()
	}
	return ids, titles
}

func save(t *testing.T, userId int64, id int32, name string) {
	t.Helper()
	if err := itDao.SaveDialogFilter(context.Background(), userId, id, folder(id, name)); err != nil {
		t.Fatalf("save folder %d: %v", id, err)
	}
}

// A new folder goes last, a changed one keeps its place, a deleted one
// comes back last.
func TestDialogFiltersPlaces(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	save(t, u, 2, "Work")
	save(t, u, 3, "Family")
	save(t, u, 4, "Friends")
	if ids, _ := stored(t, u); !slices.Equal(ids, []int32{2, 3, 4}) {
		t.Fatalf("order %v, want [2 3 4]", ids)
	}

	save(t, u, 2, "Job")
	ids, titles := stored(t, u)
	if !slices.Equal(ids, []int32{2, 3, 4}) || titles[2] != "Job" {
		t.Fatalf("after a change: order %v, titles %v", ids, titles)
	}

	if err := itDao.DeleteDialogFilter(ctx, u, 3); err != nil {
		t.Fatal(err)
	}
	if err := itDao.DeleteDialogFilter(ctx, u, 3); err != nil {
		t.Fatalf("deleting a deleted folder: %v", err)
	}
	if ids, _ = stored(t, u); !slices.Equal(ids, []int32{2, 4}) {
		t.Fatalf("after a deletion: %v", ids)
	}
	save(t, u, 3, "Family")
	if ids, _ = stored(t, u); !slices.Equal(ids, []int32{2, 4, 3}) {
		t.Fatalf("a deleted folder created again: %v, want it last", ids)
	}
}

func TestDialogFiltersLimit(t *testing.T) {
	u := newUser()
	for id := int32(2); id < 2+DialogFiltersLimit; id++ {
		save(t, u, id, fmt.Sprint("Folder ", id))
	}
	err := itDao.SaveDialogFilter(context.Background(), u, 2+DialogFiltersLimit, folder(2+DialogFiltersLimit, "One more"))
	if !errors.Is(err, ErrDialogFiltersTooMuch) {
		t.Fatalf("folder %d: %v, want DIALOG_FILTERS_TOO_MUCH", DialogFiltersLimit+1, err)
	}
	// A change of a folder there is fine at the limit.
	save(t, u, 2, "Renamed")
}

// Devices creating folders at once cannot pass the limit, and every folder
// gets a place of its own.
func TestDialogFiltersConcurrent(t *testing.T) {
	u := newUser()
	const tries = 2 * DialogFiltersLimit
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		failed  []error
	)
	for i := range tries {
		wg.Add(1)
		go func(id int32) {
			defer wg.Done()
			err := itDao.SaveDialogFilter(context.Background(), u, id, folder(id, fmt.Sprint("Folder ", id)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case !errors.Is(err, ErrDialogFiltersTooMuch):
				failed = append(failed, err)
			}
		}(int32(2 + i))
	}
	wg.Wait()
	if len(failed) > 0 {
		t.Fatalf("%d saves failed, first: %v", len(failed), failed[0])
	}
	rows, err := itDao.DialogFiltersDAO.SelectList(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if created != DialogFiltersLimit || len(rows) != DialogFiltersLimit {
		t.Fatalf("%d created, %d stored, want %d", created, len(rows), DialogFiltersLimit)
	}
	orders := make(map[int64]bool, len(rows))
	for _, r := range rows {
		if orders[r.OrderValue] {
			t.Fatalf("two folders at order %d", r.OrderValue)
		}
		orders[r.OrderValue] = true
	}
}

// The order of a reorder; "All chats" (0) and unknown ids are ignored,
// folders left out follow the others.
func TestOrderDialogFilters(t *testing.T) {
	u := newUser()
	save(t, u, 2, "Work")
	save(t, u, 3, "Family")
	save(t, u, 4, "Friends")
	got, err := itDao.OrderDialogFilters(context.Background(), u, []int32{4, 0, 99, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int32{4, 2, 3}) {
		t.Fatalf("stored order %v, want [4 2 3]", got)
	}
	if ids, _ := stored(t, u); !slices.Equal(ids, got) {
		t.Fatalf("read order %v, stored %v", ids, got)
	}
	// A new folder still goes last.
	save(t, u, 5, "News")
	if ids, _ := stored(t, u); !slices.Equal(ids, []int32{4, 2, 3, 5}) {
		t.Fatalf("after a new folder: %v", ids)
	}
}

// With Premium a user has 30 folders; after it ends they stay and can be
// changed, but no new one is made.
func TestDialogFiltersLimitPremium(t *testing.T) {
	ctx := context.Background()
	u := newPremiumUser(t)
	for id := int32(2); id < 2+DialogFiltersLimitPremium; id++ {
		save(t, u, id, fmt.Sprint("Folder ", id))
	}
	last := int32(2 + DialogFiltersLimitPremium)
	if err := itDao.SaveDialogFilter(ctx, u, last, folder(last, "One more")); !errors.Is(err, ErrDialogFiltersTooMuch) {
		t.Fatalf("folder %d with Premium: %v, want DIALOG_FILTERS_TOO_MUCH", DialogFiltersLimitPremium+1, err)
	}

	setPremium(t, u, true, time.Now().Unix()-1)
	if ids, _ := stored(t, u); len(ids) != DialogFiltersLimitPremium {
		t.Fatalf("%d folders after Premium ended, want all %d kept", len(ids), DialogFiltersLimitPremium)
	}
	save(t, u, 2+DialogFiltersLimit, "Renamed")
	if err := itDao.DeleteDialogFilter(ctx, u, 2); err != nil {
		t.Fatal(err)
	}
	if err := itDao.SaveDialogFilter(ctx, u, 2, folder(2, "Back")); !errors.Is(err, ErrDialogFiltersTooMuch) {
		t.Fatalf("a new folder over the limit without Premium: %v, want DIALOG_FILTERS_TOO_MUCH", err)
	}
}

// A folder has 100 chats included and 100 excluded, 200 with Premium.
func TestFilterChatsLimit(t *testing.T) {
	ctx := context.Background()
	free, premium := newUser(), newPremiumUser(t)
	for _, tc := range []struct {
		name    string
		user    int64
		in, out int
		tooMuch bool
	}{
		{"free, included at the limit", free, FilterChatsLimit, FilterChatsLimit, false},
		{"free, one more included", free, FilterChatsLimit + 1, 0, true},
		{"free, one more excluded", free, 1, FilterChatsLimit + 1, true},
		{"Premium, included at the limit", premium, FilterChatsLimitPremium, FilterChatsLimitPremium, false},
		{"Premium, one more included", premium, FilterChatsLimitPremium + 1, 0, true},
	} {
		err := itDao.SaveDialogFilter(ctx, tc.user, 2, withChats(2, tc.in, tc.out))
		if tc.tooMuch != errors.Is(err, ErrFilterChatsTooMuch) || (!tc.tooMuch && err != nil) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// With Premium "All chats" (0) takes the place given; when Premium ends it
// goes back to the top.
func TestOrderDialogFiltersPremium(t *testing.T) {
	ctx := context.Background()
	u := newPremiumUser(t)
	save(t, u, 2, "Work")
	save(t, u, 3, "Family")
	got, err := itDao.OrderDialogFilters(ctx, u, []int32{3, 0, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int32{3, 0, 2}) {
		t.Fatalf("stored order %v, want [3 0 2]", got)
	}
	if ids, _ := stored(t, u); !slices.Equal(ids, got) {
		t.Fatalf("read order %v, stored %v", ids, got)
	}
	// A new folder goes after "All chats" too.
	save(t, u, 4, "News")
	if ids, _ := stored(t, u); !slices.Equal(ids, []int32{3, 0, 2, 4}) {
		t.Fatalf("after a new folder: %v", ids)
	}

	setPremium(t, u, false, 0)
	if got, err = itDao.OrderDialogFilters(ctx, u, []int32{2, 0, 3}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int32{0, 2, 3, 4}) {
		t.Fatalf("order without Premium %v, want [0 2 3 4]", got)
	}
}

func TestDialogFilterTags(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	check := func(want bool) {
		t.Helper()
		if on, err := itDao.DialogFilterTags(ctx, u); err != nil || on != want {
			t.Fatalf("tags %v, %v; want %v", on, err, want)
		}
	}
	set := func(on bool) {
		t.Helper()
		if err := itDao.SetDialogFilterTags(ctx, u, on); err != nil {
			t.Fatal(err)
		}
	}
	check(false)
	set(true)
	check(true)
	set(true)
	check(true)
	set(false)
	check(false)
	set(true)
	check(true)
}
