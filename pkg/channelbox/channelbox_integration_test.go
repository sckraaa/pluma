//go:build integration

package channelbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const migration = "migrate-20261103-channels.sql"

// itDB is MySQL with the whole schema.
var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

// itSeq gives each test its own channels and users, so the tests run in
// any order and again (-count=N) on the same database.
var itSeq atomic.Int64

func itId() int64 { return 20_000_000 + itSeq.Add(1) }

func openPool(t *testing.T, maxConns int) *sql.DB {
	t.Helper()
	dsn := itDB.DSN() + fmt.Sprintf("&innodb_lock_wait_timeout=%d", int(userbox.LockWaitTimeout.Seconds()))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func create(t *testing.T, db *sql.DB, channelId, userId int64) {
	t.Helper()
	_, err := userbox.Run(context.Background(), db, userId, func(b *userbox.Box) error {
		_, err := Create(b, NewChannel{Id: channelId, AccessHash: 1, CreatorUserId: userId, Title: "t", Megagroup: true, Date: b.Date()})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// post writes a message row and its update in the channel, with the
// sender's random_id.
func post(channelId int64, randomId int64) func(b *userbox.Box) error {
	return func(b *userbox.Box) error {
		c, err := Open(b, channelId)
		if err != nil {
			return err
		}
		id := c.NextMessageId()
		c.AddPtsUpdate(newMessage(channelId, id), 1)
		_, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, date2) VALUES (?, ?, ?, ?, ?, ?)",
			channelId, id, b.UserId(), randomId, c.Pts(), b.Date())
		return err
	}
}

func messageIds(t *testing.T, db *sql.DB, channelId int64) []int32 {
	t.Helper()
	rows, err := db.Query("SELECT id FROM channel_messages WHERE channel_id = ? ORDER BY id", channelId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int32
	for rows.Next() {
		var id int32
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// TestMySQLParallel: transactions of many users on one channel at once give
// pts and message ids without gaps or repeats, each message with the pts
// of its journal row.
func TestMySQLParallel(t *testing.T) {
	const users = 30
	ctx := context.Background()
	db := openPool(t, users)
	channel := itId()
	create(t, db, channel, itId())

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []error
	)
	for i := range users {
		user := itId()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := userbox.Run(ctx, db, user, post(channel, int64(i+1))); err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(failed) > 0 {
		t.Fatalf("%d failed: %v", len(failed), failed)
	}
	rep, err := CheckJournal(ctx, db, channel, BasePts)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || rep.Rows != users || rep.State.Pts != BasePts+users || rep.State.LastMessageId != users {
		t.Fatalf("journal %+v", rep)
	}
	ids := messageIds(t, db, channel)
	for i, id := range ids {
		if id != int32(i)+1 {
			t.Fatalf("message ids %v", ids)
		}
	}
	if len(ids) != users {
		t.Fatalf("%d messages", len(ids))
	}
	var mismatched int
	if err = db.QueryRow(`SELECT COUNT(*) FROM channel_messages m JOIN channel_pts_updates p
		ON p.channel_id = m.channel_id AND p.pts = m.pts
		WHERE m.channel_id = ? AND JSON_EXTRACT(p.update_data, '$.message_MESSAGE.id') <> m.id`, channel).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Errorf("%d messages with the pts of another message's row", mismatched)
	}
}

// TestMySQLRollback: a transaction that fails after writing burns nothing.
func TestMySQLRollback(t *testing.T) {
	ctx := context.Background()
	db := openPool(t, 4)
	channel, user := itId(), itId()
	create(t, db, channel, user)
	fail := errors.New("business")
	_, err := userbox.Run(ctx, db, user, func(b *userbox.Box) error {
		if err := post(channel, 1)(b); err != nil {
			return err
		}
		return fail
	})
	if !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if _, err = userbox.Run(ctx, db, user, post(channel, 1)); err != nil {
		t.Fatal(err)
	}
	st, err := Read(ctx, db, channel)
	if err != nil {
		t.Fatal(err)
	}
	if st.Pts != BasePts+1 || st.LastMessageId != 1 {
		t.Fatalf("state %+v after a rollback and a send", st)
	}
	if ids := messageIds(t, db, channel); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("messages %v", ids)
	}
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// TestMySQLKeys: the journal has one row per pts of a channel, and a sender's
// random_id one message per channel; service messages (random_id 0) do not
// collide.
func TestMySQLKeys(t *testing.T) {
	ctx := context.Background()
	db := openPool(t, 4)
	channel, user := itId(), itId()
	create(t, db, channel, user)
	if _, err := userbox.Run(ctx, db, user, post(channel, 42)); err != nil {
		t.Fatal(err)
	}

	_, err := db.Exec("INSERT INTO channel_pts_updates (channel_id, pts, pts_count, update_type, update_data) VALUES (?, ?, 1, 9, '{}')", channel, BasePts+1)
	if !isDuplicate(err) {
		t.Errorf("a second journal row for a pts: %v", err)
	}

	// The same random_id again: the transaction fails on the key and
	// burns nothing.
	_, err = userbox.Run(ctx, db, user, post(channel, 42))
	if !isDuplicate(err) || !userbox.Deterministic(err) {
		t.Errorf("a second message with the random_id: %v", err)
	}
	if st, _ := Read(ctx, db, channel); st.Pts != BasePts+1 || st.LastMessageId != 1 {
		t.Errorf("state %+v after the duplicate", st)
	}
	// Another sender may use it.
	if _, err = userbox.Run(ctx, db, itId(), post(channel, 42)); err != nil {
		t.Errorf("another sender's random_id: %v", err)
	}
	for range 2 {
		if _, err = userbox.Run(ctx, db, user, post(channel, 0)); err != nil {
			t.Errorf("a message without random_id: %v", err)
		}
	}
}

// TestMySQLMigrationAgain: the migration applies again over the tables and
// their rows.
func TestMySQLMigrationAgain(t *testing.T) {
	ctx := context.Background()
	db := openPool(t, 2)
	channel, user := itId(), itId()
	create(t, db, channel, user)
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	if err = itDB.ApplySQLFile(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err = userbox.Run(ctx, db, user, post(channel, 1)); err != nil {
		t.Fatal(err)
	}
	if st, err := Read(ctx, db, channel); err != nil || st.Pts != BasePts+1 {
		t.Fatalf("state %+v, %v", st, err)
	}
}

// TestMySQLCommitOutcomeUnknown: a COMMIT whose reply is lost, in a
// transaction that changes only the channel (no journal of the user's own),
// is decided by the channel's journal on another connection: applied once,
// or not applied and tried again. A proxy breaks the connection at COMMIT.
func TestMySQLCommitOutcomeUnknown(t *testing.T) {
	cfg, err := mysql.ParseDSN(itDB.DSN() + fmt.Sprintf("&innodb_lock_wait_timeout=%d", int(userbox.LockWaitTimeout.Seconds())))
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := testinfra.StartMySQLProxy(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	cfg.Addr = proxy.Addr()
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })

	for _, c := range []struct {
		name     string
		fault    testinfra.CommitFault
		attempts int
	}{
		{"applied", testinfra.CommitReplyLost, 1},
		{"not applied", testinfra.CommitNotSent, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			channel, user := itId(), itId()
			create(t, db, channel, user) // the user's row exists
			fired := proxy.Fired()
			proxy.FailNextCommit(c.fault)
			attempts := 0
			res, err := userbox.Run(ctx, db, user, func(b *userbox.Box) error {
				attempts++
				return post(channel, 7)(b)
			})
			if err != nil {
				t.Fatal(err)
			}
			if proxy.Fired() != fired+1 {
				t.Fatal("the proxy did not break the COMMIT")
			}
			if attempts != c.attempts {
				t.Errorf("%d attempts, want %d", attempts, c.attempts)
			}
			st, err := Read(ctx, db, channel)
			if err != nil {
				t.Fatal(err)
			}
			if r := Results(res); st.Pts != BasePts+1 || st.LastMessageId != 1 || len(r) != 1 || r[0].State != st {
				t.Errorf("state %+v, results %+v", st, r)
			}
			if rep, err := CheckJournal(ctx, db, channel, BasePts); err != nil || !rep.OK() || rep.Rows != 1 {
				t.Errorf("journal %+v, %v", rep, err)
			}
			if ids := messageIds(t, db, channel); len(ids) != 1 || ids[0] != 1 {
				t.Errorf("messages %v", ids)
			}
		})
	}
}
