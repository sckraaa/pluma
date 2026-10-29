//go:build integration

package dao

import (
	"context"
	"database/sql"
	"math/rand"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// connect stores the initConnection of the session on key, as the session
// service does: its lang_code, active at active.
func connect(t *testing.T, db *sql.DB, key int64, langCode string, active int64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"INSERT INTO auths (auth_key_id, api_id, lang_code, params, date_active) VALUES (?, 1, ?, '{}', ?)",
		key, langCode, active); err != nil {
		t.Fatal(err)
	}
}

// The settings of an authorization (task 019) on a real MySQL and Redis: a
// new login of a user with other sessions is unconfirmed until it is
// confirmed or the autoconfirm period passes; the cached authorization
// follows a change.
func TestAuthorizationSettings(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	userId := rand.Int63n(1<<40) + 1
	first, second := storeKey(t, d, mtproto.AuthKeyTypePerm, 0), storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	db := openSQL(t, itFullDB)
	connect(t, db, first, "en", time.Now().Unix())
	connect(t, db, second, "en", time.Now().Unix())

	cached := func(key int64) *BindUser {
		t.Helper()
		c, err := d.GetCacheAuthData(ctx, key)
		if err != nil || c == nil || c.BindUser == nil {
			t.Fatalf("authorization of key %d: %v %v", key, c, err)
		}
		return c.BindUser
	}

	if h := d.BindAuthKeyUser(ctx, first, userId); h == 0 {
		t.Fatal("the first key was not bound")
	}
	if cached(first).Unconfirmed {
		t.Fatal("the first session of a user is unconfirmed: nobody can confirm it")
	}
	hash := d.BindAuthKeyUser(ctx, second, userId)
	if hash == 0 {
		t.Fatal("the second key was not bound")
	}
	b := cached(second)
	if !b.Unconfirmed {
		t.Fatal("a login next to another session is confirmed")
	}
	c, _ := d.GetCacheAuthData(ctx, second)
	now := time.Now().Unix()
	if !c.Unconfirmed(now) || c.Unconfirmed(b.DateCreated+authsession.AuthorizationAutoconfirmPeriod) {
		t.Fatal("the autoconfirm period does not hold")
	}

	// Unknown hashes change nothing.
	if key, err := d.ChangeAuthorization(ctx, userId, hash+1, AuthorizationChange{Confirm: true}); err != nil || key != 0 {
		t.Fatalf("an unknown hash: key %d, %v", key, err)
	}
	if key, err := d.ChangeAuthorization(ctx, userId+1, hash, AuthorizationChange{Confirm: true}); err != nil || key != 0 {
		t.Fatalf("another user's hash: key %d, %v", key, err)
	}

	yes := true
	if key, err := d.ChangeAuthorization(ctx, userId, hash, AuthorizationChange{CallRequestsDisabled: &yes}); err != nil || key != second {
		t.Fatalf("calls off: key %d, %v", key, err)
	}
	b = cached(second)
	if !b.Unconfirmed || !b.CallRequestsDisabled || b.EncryptedRequestsDisabled {
		t.Fatalf("after calls off: %+v", b)
	}
	if key, err := d.ChangeAuthorization(ctx, userId, hash, AuthorizationChange{Confirm: true}); err != nil || key != second {
		t.Fatalf("confirm: key %d, %v", key, err)
	}
	b = cached(second)
	if b.Unconfirmed || !b.CallRequestsDisabled {
		t.Fatalf("after the confirmation: %+v", b)
	}

	// A new login on the key starts over: a new hash, unconfirmed, calls on.
	if h := d.BindAuthKeyUser(ctx, second, userId); h == 0 || h == hash {
		t.Fatalf("rebound hash %d", h)
	}
	if b = cached(second); !b.Unconfirmed || b.CallRequestsDisabled {
		t.Fatalf("after a new login: %+v", b)
	}
}

// The language of the session that connected last, the new one aside.
func TestRecentLangCode(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)
	userId := rand.Int63n(1<<40) + 1
	if lang, err := d.RecentLangCode(ctx, userId, 0); err != nil || lang != "" {
		t.Fatalf("a user without sessions: %q, %v", lang, err)
	}

	now := time.Now().Unix()
	keys := []int64{rand.Int63(), rand.Int63(), rand.Int63()}
	for i, s := range []struct {
		lang   string
		active int64
	}{{"ru", now - 60}, {"en", now - 3600}, {"cs", now}} {
		if d.BindAuthKeyUser(ctx, keys[i], userId) == 0 {
			t.Fatalf("key %d not bound", i)
		}
		connect(t, db, keys[i], s.lang, s.active)
	}
	// keys[2] is the new login: the one active before it is in Russian.
	if lang, err := d.RecentLangCode(ctx, userId, keys[2]); err != nil || lang != "ru" {
		t.Fatalf("recent language %q, %v", lang, err)
	}
	if lang, err := d.RecentLangCode(ctx, userId, 0); err != nil || lang != "cs" {
		t.Fatalf("recent language of all %q, %v", lang, err)
	}
}
