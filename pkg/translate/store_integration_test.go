//go:build integration

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

package translate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/proto"
)

// The tables of migrate-20261119-translation.sql on the production MySQL.

const itMigration = "migrate-20261119-translation.sql"

var (
	itMySQL *testinfra.MySQL
	itDB    *sql.DB
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if itDB, err = userbox.Open(itMySQL.DSN()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itDB.Close()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// itClock is the time of a store under test.
type itClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *itClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *itClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *itClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func itStore() (*Store, *itClock) {
	c := &itClock{t: time.Now()}
	s := NewStore(itDB)
	s.now = c.now
	return s, c
}

// itUserSeq makes user ids unique across tests and runs (go test -count=N).
var itUserSeq atomic.Int64

func itUser() int64 {
	return time.Now().UnixMilli()%1_000_000_000*1000 + itUserSeq.Add(1)
}

// itKey is a key no other test or run has.
func itKey(t *testing.T) Key {
	t.Helper()
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatal(err)
	}
	return k
}

func itUsedAt(t *testing.T, k Key) int64 {
	t.Helper()
	var usedAt int64
	if err := itDB.QueryRow("SELECT used_at FROM translation_cache WHERE cache_key = ?", k[:]).Scan(&usedAt); err != nil {
		t.Fatal(err)
	}
	return usedAt
}

func itTranslation() *CachedTranslation {
	return &CachedTranslation{
		Text: "Привет, мир",
		Entities: []*mtproto.MessageEntity{
			mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 6}).To_MessageEntity(),
			mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: 8, Length: 3, Url: "https://example.com"}).To_MessageEntity(),
			mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 0, Length: 6, UserId_INT64: 136817688}).To_MessageEntity(),
		},
	}
}

func sameTranslation(t *testing.T, got, want *CachedTranslation) {
	t.Helper()
	if got == nil {
		t.Fatal("no translation")
	}
	if got.Text != want.Text || got.Fallback != want.Fallback || len(got.Entities) != len(want.Entities) {
		t.Fatalf("translation %+v, want %+v", got, want)
	}
	for i := range want.Entities {
		if !proto.Equal(got.Entities[i], want.Entities[i]) {
			t.Fatalf("entity %d: %v, want %v", i, got.Entities[i], want.Entities[i])
		}
	}
}

// Put, then a read of several keys at once: what was put comes back as it
// was, a key that was not put is not in the answer, a key asked twice is
// one entry.
func TestCachePutAndRead(t *testing.T) {
	ctx := context.Background()
	s, _ := itStore()
	kept, fallback, plain, missing := itKey(t), itKey(t), itKey(t), itKey(t)
	wantKept := itTranslation()
	wantFallback := &CachedTranslation{Text: "Просто текст", Fallback: true}
	wantPlain := &CachedTranslation{Text: ""}
	for k, tr := range map[Key]*CachedTranslation{kept: wantKept, fallback: wantFallback, plain: wantPlain} {
		if err := s.PutTranslation(ctx, k, "ru", "hy-mt1.5-1.8b-q8_0", tr); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.CachedTranslations(ctx, []Key{kept, missing, fallback, kept, plain})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d translations, want 3", len(got))
	}
	if _, ok := got[missing]; ok {
		t.Fatal("a key that was not put is found")
	}
	sameTranslation(t, got[kept], wantKept)
	sameTranslation(t, got[fallback], wantFallback)
	sameTranslation(t, got[plain], wantPlain)

	if got, err = s.CachedTranslations(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("no keys: %v %v", got, err)
	}
	var markup int
	if err = itDB.QueryRow("SELECT markup FROM translation_cache WHERE cache_key = ?", fallback[:]).Scan(&markup); err != nil || markup != cacheMarkupFallback {
		t.Fatalf("markup of a fallback: %d %v", markup, err)
	}
}

// More keys than one SELECT reads are read in several.
func TestCacheReadManyKeys(t *testing.T) {
	ctx := context.Background()
	s, _ := itStore()
	keys := make([]Key, 2*cacheReadBatch+7)
	for i := range keys {
		keys[i] = itKey(t)
		if err := s.PutTranslation(ctx, keys[i], "en", "m", &CachedTranslation{Text: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.CachedTranslations(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(keys) {
		t.Fatalf("%d translations of %d", len(got), len(keys))
	}
	for i, k := range keys {
		if got[k] == nil || got[k].Text != fmt.Sprint(i) {
			t.Fatalf("key %d: %+v", i, got[k])
		}
	}
}

// used_at moves forward on a read at most once an hour.
func TestCacheTouch(t *testing.T) {
	ctx := context.Background()
	s, c := itStore()
	k := itKey(t)
	if err := s.PutTranslation(ctx, k, "ru", "m", itTranslation()); err != nil {
		t.Fatal(err)
	}
	put := itUsedAt(t, k)
	c.add(59 * time.Minute)
	if _, err := s.CachedTranslations(ctx, []Key{k}); err != nil {
		t.Fatal(err)
	}
	if got := itUsedAt(t, k); got != put {
		t.Fatalf("a read after 59 minutes moved used_at from %d to %d", put, got)
	}
	c.add(2 * time.Minute)
	if _, err := s.CachedTranslations(ctx, []Key{k}); err != nil {
		t.Fatal(err)
	}
	if got, want := itUsedAt(t, k), c.now().UnixMilli(); got != want {
		t.Fatalf("a read after 61 minutes: used_at %d, want %d", got, want)
	}
}

// Two writes of one key at once: no error, one row, the text of one of
// them; a later write keeps that text and only moves used_at.
func TestCachePutRace(t *testing.T) {
	ctx := context.Background()
	s, c := itStore()
	for range 10 {
		k := itKey(t)
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = s.PutTranslation(ctx, k, "ru", "m", &CachedTranslation{Text: fmt.Sprint("text ", i)})
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.CachedTranslations(ctx, []Key{k})
		if err != nil {
			t.Fatal(err)
		}
		first := got[k]
		if first == nil || !strings.HasPrefix(first.Text, "text ") {
			t.Fatalf("after the race: %+v", first)
		}
		c.add(time.Minute)
		if err = s.PutTranslation(ctx, k, "ru", "m", &CachedTranslation{Text: "later"}); err != nil {
			t.Fatal(err)
		}
		if got, err = s.CachedTranslations(ctx, []Key{k}); err != nil {
			t.Fatal(err)
		}
		if got[k].Text != first.Text {
			t.Fatalf("a later write changed %q to %q", first.Text, got[k].Text)
		}
		if usedAt := itUsedAt(t, k); usedAt != c.now().UnixMilli() {
			t.Fatalf("a later write left used_at at %d", usedAt)
		}
	}
}

func TestCachePutChecks(t *testing.T) {
	ctx := context.Background()
	s, _ := itStore()
	k := itKey(t)
	for name, put := range map[string]func() error{
		"nil":         func() error { return s.PutTranslation(ctx, k, "ru", "m", nil) },
		"no language": func() error { return s.PutTranslation(ctx, k, "", "m", itTranslation()) },
		"long language": func() error {
			return s.PutTranslation(ctx, k, strings.Repeat("x", cacheMaxLang+1), "m", itTranslation())
		},
		"no model": func() error { return s.PutTranslation(ctx, k, "ru", "", itTranslation()) },
		"long model": func() error {
			return s.PutTranslation(ctx, k, "ru", strings.Repeat("m", cacheMaxModel+1), itTranslation())
		},
	} {
		if err := put(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	got, err := s.CachedTranslations(ctx, []Key{k})
	if err != nil || len(got) != 0 {
		t.Fatalf("a refused write is in the cache: %v %v", got, err)
	}
}

// The sweep deletes the rows unused for the ttl, more than one batch of
// them, and nothing else.
func TestCacheSweep(t *testing.T) {
	ctx := context.Background()
	s, c := itStore()
	const ttl = 30 * 24 * time.Hour
	now := c.now()

	// Old rows straight into the table: more than two batches of the sweep.
	old := make([]Key, 2*cacheSweepBatch+500)
	usedAt := now.Add(-ttl - time.Minute).UnixMilli()
	for part := 0; part < len(old); part += 500 {
		var b strings.Builder
		args := make([]any, 0, 500*3)
		for i := part; i < part+500; i++ {
			old[i] = itKey(t)
			if i > part {
				b.WriteString(",")
			}
			b.WriteString("(?, 'ru', 'm', 'old', '', 1, ?, ?)")
			args = append(args, old[i][:], usedAt, usedAt)
		}
		if _, err := itDB.ExecContext(ctx, "INSERT INTO translation_cache (cache_key, to_lang, model, `text`, entities, markup, created_at, used_at) VALUES "+b.String(), args...); err != nil {
			t.Fatal(err)
		}
	}
	// A row put 31 days ago and read 2 days ago is kept; one put just
	// before the ttl is kept too; one put 31 days ago and never read goes.
	readLately, young, unread := itKey(t), itKey(t), itKey(t)
	c.add(-31 * 24 * time.Hour)
	for _, k := range []Key{readLately, unread} {
		if err := s.PutTranslation(ctx, k, "ru", "m", itTranslation()); err != nil {
			t.Fatal(err)
		}
	}
	c.add(29 * 24 * time.Hour)
	if _, err := s.CachedTranslations(ctx, []Key{readLately}); err != nil {
		t.Fatal(err)
	}
	c.set(now.Add(-ttl + time.Minute))
	if err := s.PutTranslation(ctx, young, "ru", "m", itTranslation()); err != nil {
		t.Fatal(err)
	}
	c.set(now)

	before, err := s.CacheRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.SweepTranslations(ctx, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if n < int64(len(old))+1 {
		t.Fatalf("swept %d rows, want at least %d", n, len(old)+1)
	}
	after, err := s.CacheRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before-after != n {
		t.Fatalf("rows %d before and %d after a sweep of %d", before, after, n)
	}
	got, err := s.CachedTranslations(ctx, append([]Key{readLately, young, unread}, old...))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[readLately] == nil || got[young] == nil {
		t.Fatalf("after the sweep: %d rows of ours, read lately %v, young %v", len(got), got[readLately] != nil, got[young] != nil)
	}
	if n, err = s.SweepTranslations(ctx, ttl); err != nil || n != 0 {
		t.Fatalf("a second sweep: %d %v", n, err)
	}
	if _, err = s.SweepTranslations(ctx, 0); err == nil {
		t.Fatal("a sweep with no ttl")
	}
}

// The sweeper sweeps at its start and stops.
func TestCacheSweeper(t *testing.T) {
	ctx := context.Background()
	s, c := itStore()
	k := itKey(t)
	c.add(-2 * time.Hour)
	if err := s.PutTranslation(ctx, k, "ru", "m", itTranslation()); err != nil {
		t.Fatal(err)
	}
	c.add(2 * time.Hour)
	if _, err := StartCacheSweeper(s, 0); err == nil {
		t.Fatal("a sweeper with no ttl")
	}
	w, err := StartCacheSweeper(s, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := s.CachedTranslations(ctx, []Key{k})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweeper did not sweep at its start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	w.Stop()
}

func TestPeerTranslationSettings(t *testing.T) {
	ctx := context.Background()
	s, _ := itStore()
	user, other := itUser(), itUser()
	peerUser := TranslationPeer{Type: mtproto.PEER_USER, Id: 501}
	peerChat := TranslationPeer{Type: mtproto.PEER_CHAT, Id: 501}
	peerChannel := TranslationPeer{Type: mtproto.PEER_CHANNEL, Id: 502}
	self := TranslationPeer{Type: mtproto.PEER_USER, Id: user}

	got, err := s.TranslationsDisabled(ctx, user, peerUser, peerChat, peerChannel)
	if err != nil || len(got) != 0 {
		t.Fatalf("nothing hidden yet: %v %v", got, err)
	}
	for _, p := range []TranslationPeer{peerUser, peerChannel, self} {
		if err = s.SetTranslationsDisabled(ctx, user, p, true); err != nil {
			t.Fatal(err)
		}
	}
	// Hiding again is no error and changes nothing.
	if err = s.SetTranslationsDisabled(ctx, user, peerUser, true); err != nil {
		t.Fatal(err)
	}
	if got, err = s.TranslationsDisabled(ctx, user, peerUser, peerChat, peerChannel, self); err != nil {
		t.Fatal(err)
	}
	// A chat and a user with one id are two peers.
	if len(got) != 3 || !got[peerUser] || got[peerChat] || !got[peerChannel] || !got[self] {
		t.Fatalf("hidden: %v", got)
	}
	if got, err = s.TranslationsDisabled(ctx, other, peerUser, peerChannel); err != nil || len(got) != 0 {
		t.Fatalf("another user: %v %v", got, err)
	}

	// Showing the bar again deletes the row; showing a shown bar is no
	// error.
	if err = s.SetTranslationsDisabled(ctx, user, peerUser, false); err != nil {
		t.Fatal(err)
	}
	if err = s.SetTranslationsDisabled(ctx, user, peerChat, false); err != nil {
		t.Fatal(err)
	}
	if got, err = s.TranslationsDisabled(ctx, user, peerUser, peerChannel); err != nil || len(got) != 1 || !got[peerChannel] {
		t.Fatalf("after showing one: %v %v", got, err)
	}
	var rows int
	if err = itDB.QueryRow("SELECT COUNT(*) FROM peer_translation_settings WHERE user_id = ?", user).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("rows of the user: %d %v", rows, err)
	}

	if got, err = s.TranslationsDisabled(ctx, user); err != nil || len(got) != 0 {
		t.Fatalf("no peers: %v %v", got, err)
	}
	if err = s.SetTranslationsDisabled(ctx, user, TranslationPeer{Type: mtproto.PEER_SELF, Id: user}, true); err == nil {
		t.Fatal("PEER_SELF is accepted")
	}
	if _, err = s.TranslationsDisabled(ctx, user, TranslationPeer{Type: mtproto.PEER_CHANNEL}); err == nil {
		t.Fatal("a channel without an id is accepted")
	}
}

// The migration runs again over its own tables with rows in them: nothing
// changes.
func TestMigrationAgain(t *testing.T) {
	ctx := context.Background()
	s, _ := itStore()
	user, k := itUser(), itKey(t)
	peer := TranslationPeer{Type: mtproto.PEER_CHANNEL, Id: 9}
	if err := s.SetTranslationsDisabled(ctx, user, peer, true); err != nil {
		t.Fatal(err)
	}
	want := itTranslation()
	if err := s.PutTranslation(ctx, k, "ru", "m", want); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(itMigration)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = itMySQL.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("the migration again: %v", err)
		}
	}
	got, err := s.CachedTranslations(ctx, []Key{k})
	if err != nil {
		t.Fatal(err)
	}
	sameTranslation(t, got[k], want)
	disabled, err := s.TranslationsDisabled(ctx, user, peer)
	if err != nil || !disabled[peer] {
		t.Fatalf("hidden after the migration again: %v %v", disabled, err)
	}
}
