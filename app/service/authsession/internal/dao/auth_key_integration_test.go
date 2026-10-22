//go:build integration

package dao

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The stores of this package's integration tests: MySQL with the whole
// schema, MySQL with the schema up to the PFS migration, and Redis.
var (
	itFullDB *testinfra.MySQL
	itOldDB  *testinfra.MySQL
	itRedis  *testinfra.Redis
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 3)
	)
	wg.Add(3)
	go func() { defer wg.Done(); itFullDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() {
		defer wg.Done()
		files, err := testinfra.SchemaFilesBefore("migrate-20261021-pfs.sql")
		if err != nil {
			errs[1] = err
			return
		}
		itOldDB, errs[1] = testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	}()
	go func() { defer wg.Done(); itRedis, errs[2] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	_ = itFullDB.Terminate(ctx)
	_ = itOldDB.Terminate(ctx)
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// newIntegrationDao returns a Dao on db and the Redis cache, as dao.New
// builds it in the service.
func newIntegrationDao(t *testing.T, db *testinfra.MySQL) *Dao {
	t.Helper()
	c := db.Config()
	conn := sqlx.NewMySQL(&c) // connections are pooled per DSN by go-zero
	return &Dao{
		Mysql:      newMysqlDao(conn),
		CachedConn: sqlc.NewConn(conn, itRedis.CacheConf()),
	}
}

func openSQL(t *testing.T, db *testinfra.MySQL) *sql.DB {
	t.Helper()
	conn, err := sql.Open("mysql", db.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newKeyID(t *testing.T) int64 {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return int64(binary.LittleEndian.Uint64(b[:]) >> 1)
}

// storeKey stores a key as the handshake does and returns its id.
func storeKey(t *testing.T, d *Dao, keyType int, expiresIn int32) int64 {
	t.Helper()
	id := newKeyID(t)
	key := make([]byte, 256)
	_, _ = rand.Read(key)
	if err := d.SetAuthKeyV2(context.Background(), mtproto.NewAuthKeyInfo(id, key, keyType), expiresIn); err != nil {
		t.Fatalf("SetAuthKeyV2: %v", err)
	}
	return id
}

type keyRow struct {
	keyType, perm, temp, mediaTemp, expiresAt int64
}

func readKeyRow(t *testing.T, db *sql.DB, id int64) keyRow {
	t.Helper()
	var r keyRow
	err := db.QueryRow("select auth_key_type, perm_auth_key_id, temp_auth_key_id, media_temp_auth_key_id, expires_at from auth_key_infos where auth_key_id = ?", id).
		Scan(&r.keyType, &r.perm, &r.temp, &r.mediaTemp, &r.expiresAt)
	if err != nil {
		t.Fatalf("auth_key_infos %d: %v", id, err)
	}
	return r
}

func TestBindTempAuthKeyStoresBinding(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)

	perm := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	other := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	temp := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	media := storeKey(t, d, mtproto.AuthKeyTypeMediaTemp, 3600)

	handshakeExpiry := readKeyRow(t, db, temp).expiresAt
	if now := time.Now().Unix(); handshakeExpiry < now+3590 || handshakeExpiry > now+3610 {
		t.Fatalf("temp key expires_at %d, want about now+3600", handshakeExpiry)
	}

	// An unbound temporary key is read from MySQL every time (never cached),
	// so the binding is visible right after it.
	if k, err := d.QueryAuthKeyV2(ctx, temp); err != nil || k.GetPermAuthKeyId() != 0 {
		t.Fatalf("unbound temp key: %v %v", k, err)
	}

	// expires_at never moves past the handshake's expiry.
	if err := d.BindTempAuthKey(ctx, perm, temp, time.Now().Unix()+7200); err != nil {
		t.Fatalf("BindTempAuthKey: %v", err)
	}
	if r := readKeyRow(t, db, temp); r.perm != perm || r.expiresAt != handshakeExpiry {
		t.Fatalf("temp row after binding: %+v, want perm %d expires_at %d", r, perm, handshakeExpiry)
	}
	if r := readKeyRow(t, db, perm); r.temp != temp || r.mediaTemp != 0 {
		t.Fatalf("perm row after binding: %+v", r)
	}
	k, expiresAt, err := d.QueryAuthKeyWithExpiry(ctx, temp)
	if err != nil || k.GetPermAuthKeyId() != perm || expiresAt != handshakeExpiry {
		t.Fatalf("bound temp key: %v, expires %d, %v", k, expiresAt, err)
	}

	// A shorter expires_at from the client is kept.
	shorter := time.Now().Unix() + 600
	if err = d.BindTempAuthKey(ctx, perm, temp, shorter); err != nil {
		t.Fatalf("binding again to the same key: %v", err)
	}
	if r := readKeyRow(t, db, temp); r.expiresAt != shorter {
		t.Fatalf("expires_at %d, want %d", r.expiresAt, shorter)
	}
	// The cached copy was dropped: the new expiry is visible.
	if _, expiresAt, _ = d.QueryAuthKeyWithExpiry(ctx, temp); expiresAt != shorter {
		t.Fatalf("cached expires_at %d, want %d", expiresAt, shorter)
	}

	if err = d.BindTempAuthKey(ctx, other, temp, shorter); !errors.Is(err, mtproto.ErrTempAuthKeyAlreadyBound) {
		t.Fatalf("binding to another permanent key: %v, want TEMP_AUTH_KEY_ALREADY_BOUND", err)
	}
	if r := readKeyRow(t, db, temp); r.perm != perm {
		t.Fatalf("temp key moved to %d", r.perm)
	}

	if err = d.BindTempAuthKey(ctx, perm, media, shorter); err != nil {
		t.Fatalf("binding a media temp key: %v", err)
	}
	if r := readKeyRow(t, db, perm); r.temp != temp || r.mediaTemp != media {
		t.Fatalf("perm row after binding the media key: %+v", r)
	}

	if err = d.BindTempAuthKey(ctx, perm, newKeyID(t), shorter); !errors.Is(err, mtproto.ErrTempAuthKeyEmpty) {
		t.Fatalf("unknown temp key: %v, want TEMP_AUTH_KEY_EMPTY", err)
	}
	if err = d.BindTempAuthKey(ctx, perm, other, shorter); !errors.Is(err, mtproto.ErrTempAuthKeyEmpty) {
		t.Fatalf("a permanent key as the temp key: %v, want TEMP_AUTH_KEY_EMPTY", err)
	}
}

// TestBindTempAuthKeyConcurrent: requests binding one temporary key to
// different permanent keys at once. The row lock (select ... for update)
// lets exactly one win; without it two could read "unbound" and both write.
// The rejected ones must not count as database failures: they used to open
// go-zero's circuit breaker within a few rounds, after which every query of
// the service failed with INTERNAL_SERVER_ERROR.
func TestBindTempAuthKeyConcurrent(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)

	const contenders = 8
	perms := make([]int64, contenders)
	for i := range perms {
		perms[i] = storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	}
	expiresAt := time.Now().Unix() + 600

	for round := 0; round < 20; round++ {
		temp := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			errs  = make([]error, contenders)
		)
		for i := range perms {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = d.BindTempAuthKey(ctx, perms[i], temp, expiresAt)
			}()
		}
		close(start)
		wg.Wait()

		winner := int64(0)
		for i, err := range errs {
			switch {
			case err == nil:
				if winner != 0 {
					t.Fatalf("round %d: temp key bound to %d and %d", round, winner, perms[i])
				}
				winner = perms[i]
			case !errors.Is(err, mtproto.ErrTempAuthKeyAlreadyBound):
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if winner == 0 {
			t.Fatalf("round %d: nobody bound the key", round)
		}
		if r := readKeyRow(t, db, temp); r.perm != winner {
			t.Fatalf("round %d: row says perm %d, the winner is %d", round, r.perm, winner)
		}
	}
	if _, err := d.QueryAuthKeyV2(ctx, perms[0]); err != nil {
		t.Fatalf("a query after the races: %v", err)
	}
}

// TestRetireOldTempKeysMigration applies migrate-20261021-pfs.sql to a
// database with temporary keys and bindings written by the old
// auth.bindTempAuthKey: they are expired and unbound, permanent keys forget
// them and keep working.
func TestRetireOldTempKeysMigration(t *testing.T) {
	ctx := context.Background()
	if err := itOldDB.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	db := openSQL(t, itOldDB)

	perm, temp, media := newKeyID(t), newKeyID(t), newKeyID(t)
	for _, k := range []struct {
		id, keyType, perm, temp, media int64
	}{
		{perm, mtproto.AuthKeyTypePerm, 0, temp, media},
		{temp, mtproto.AuthKeyTypeTemp, perm, 0, 0},
		{media, mtproto.AuthKeyTypeMediaTemp, perm, 0, 0},
	} {
		if _, err := db.Exec("insert into auth_keys(auth_key_id, body) values (?, ?)", k.id, "AAAA"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("insert into auth_key_infos(auth_key_id, auth_key_type, perm_auth_key_id, temp_auth_key_id, media_temp_auth_key_id) values (?, ?, ?, ?, ?)",
			k.id, k.keyType, k.perm, k.temp, k.media); err != nil {
			t.Fatal(err)
		}
	}

	migration, err := testinfra.SchemaFile("migrate-20261021-pfs.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Unix()
	if err = itOldDB.ApplySQLFile(ctx, migration); err != nil {
		t.Fatal(err)
	}

	// expires_at is the database's UNIX_TIMESTAMP() at the migration; the
	// container's clock may be a second off the host's.
	var expiredAt int64
	for _, id := range []int64{temp, media} {
		r := readKeyRow(t, db, id)
		if r.perm != 0 || r.expiresAt < before-5 || r.expiresAt > time.Now().Unix()+5 {
			t.Fatalf("old temp key %d after the migration: %+v, want unbound and expired", id, r)
		}
		expiredAt = max(expiredAt, r.expiresAt)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Unix() < expiredAt && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	if r := readKeyRow(t, db, perm); r.temp != 0 || r.mediaTemp != 0 || r.expiresAt != 0 {
		t.Fatalf("perm key after the migration: %+v, want no temp keys and no expiry", r)
	}

	d := newIntegrationDao(t, itOldDB)
	if _, _, err = d.QueryAuthKeyWithExpiry(ctx, temp); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
		t.Fatalf("old temp key: %v, want AUTH_KEY_UNREGISTERED (-404 for the client)", err)
	}
	if k, err := d.QueryAuthKeyV2(ctx, perm); err != nil || k.GetAuthKeyType() != mtproto.AuthKeyTypePerm {
		t.Fatalf("perm key after the migration: %v %v", k, err)
	}
}
