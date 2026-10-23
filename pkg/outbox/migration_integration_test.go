//go:build integration

package outbox

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The migration of task 003 (migrate-20261023-delivery.sql) on a database
// in the state production has before it.

const deliveryMigration = "migrate-20261023-delivery.sql"

func startOldDB(t *testing.T) (*testinfra.MySQL, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(deliveryMigration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db, err := sql.Open("mysql", old.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return old, db
}

func applyDelivery(t *testing.T, m *testinfra.MySQL) {
	t.Helper()
	path, err := testinfra.SchemaFile(deliveryMigration)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ApplySQLFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

type msgRow struct {
	user, sender, dmid, random int64
	box                        int32
}

func messageRows(t *testing.T, db *sql.DB) []msgRow {
	t.Helper()
	rows, err := db.Query("SELECT user_id, user_message_box_id, sender_user_id, dialog_message_id, random_id FROM messages ORDER BY user_id, user_message_box_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []msgRow
	for rows.Next() {
		var r msgRow
		if err = rows.Scan(&r.user, &r.box, &r.sender, &r.dmid, &r.random); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func uniqueIndex(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var nonUnique sql.NullInt64
	if err := db.QueryRow("SELECT MIN(non_unique) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'messages' AND index_name = ?", name).Scan(&nonUnique); err != nil {
		t.Fatal(err)
	}
	return nonUnique.Valid && nonUnique.Int64 == 0
}

// On a populated database: copies without a link get one of their own,
// repeated copies and repeated random ids are unlinked (the first stays),
// the columns and unique keys are there; a second run changes nothing; the
// unique keys then refuse a repeat.
func TestDeliveryMigration(t *testing.T) {
	old, db := startOldDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	insert := func(user int64, box int32, sender, dmid, random int64) {
		exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id) VALUES (?, ?, 1, 2, ?, ?, 2, 2, ?)",
			user, box, dmid, sender, random)
	}
	// User 1: two copies without a link, a message delivered three times,
	// two outgoing sends with one random_id, an incoming copy with the
	// sender's random_id and a service message with none.
	insert(1, 1, 2, 0, 0)
	insert(1, 2, 2, 0, 0)
	insert(1, 3, 2, 500, 77)
	insert(1, 4, 2, 500, 77)
	insert(1, 5, 2, 500, 77)
	insert(1, 6, 1, 600, 99)
	insert(1, 7, 1, 601, 99)
	insert(1, 8, 2, 700, 99) // incoming, the sender's random_id
	insert(1, 9, 1, 800, 0)
	insert(1, 10, 1, 801, 0)
	// User 2: the sender of the repeated message; its own copy once.
	insert(2, 1, 2, 500, 77)

	applyDelivery(t, old)

	got := messageRows(t, db)
	byBox := map[[2]int64]msgRow{}
	for _, r := range got {
		byBox[[2]int64{r.user, int64(r.box)}] = r
	}
	for _, c := range []struct {
		user   int64
		box    int32
		linked bool // dialog_message_id kept; unlinked ones get -id
		random int64
	}{
		{1, 1, false, 0},
		{1, 2, false, 0},
		{1, 3, true, 77},
		{1, 4, false, 77},
		{1, 5, false, 77},
		{1, 6, true, 99},
		{1, 7, true, 0},
		{1, 8, true, 99},
		{1, 9, true, 0},
		{2, 1, true, 77},
	} {
		r := byBox[[2]int64{c.user, int64(c.box)}]
		if c.linked && r.dmid <= 0 || !c.linked && r.dmid >= 0 {
			t.Errorf("user %d box %d: dialog_message_id %d, linked %v", c.user, c.box, r.dmid, c.linked)
		}
		if r.random != c.random {
			t.Errorf("user %d box %d: random_id %d, want %d", c.user, c.box, r.random, c.random)
		}
	}
	for _, idx := range []string{"user_dialog_message", "user_out_random"} {
		if !uniqueIndex(t, db, idx) {
			t.Errorf("index %s is not unique", idx)
		}
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE pts <> 0 OR out_random_id IS NOT NULL AND out_random_id <> random_id").Scan(&n); err != nil || n != 0 {
		t.Errorf("pts and out_random_id of old rows: %d, %v", n, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM mq_outbox").Scan(&n); err != nil || n != 0 {
		t.Errorf("mq_outbox: %d rows, %v", n, err)
	}

	// Again: nothing changes.
	applyDelivery(t, old)
	if again := messageRows(t, db); !slices.Equal(again, got) {
		t.Errorf("a second run changed messages:\n%v\n%v", got, again)
	}

	// The keys hold from now on.
	if _, err := db.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id) VALUES (1, 11, 1, 2, 500, 2, 2, 2, 0)"); err == nil {
		t.Error("a second copy of a message was written")
	}
	if _, err := db.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id) VALUES (1, 12, 1, 2, 900, 1, 2, 2, 99)"); err == nil {
		t.Error("a second send with one random_id was written")
	}
}

// The speed of the migration on a larger messages table, to plan the
// window (docs/ops): two unique indexes are built over the whole table.
func TestDeliveryMigrationSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	old, db := startOldDB(t)
	const users, perUser, batch = 100, 1000, 1000
	text := strings.Repeat("x", 200)
	var (
		args []any
		n    int
	)
	flush := func() {
		if n == 0 {
			return
		}
		q := "INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id, message) VALUES " +
			strings.TrimSuffix(strings.Repeat("(?, ?, 1, 2, ?, ?, 2, 2, ?, ?), ", n), ", ")
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
		args, n = args[:0], 0
	}
	for u := 1; u <= users; u++ {
		for b := 1; b <= perUser; b++ {
			sender := int64(u)
			if b%2 == 0 {
				sender = int64(u%users + 1)
			}
			args = append(args, u, b, int64(u)<<20|int64(b), sender, int64(u)<<32|int64(b), text)
			if n++; n == batch {
				flush()
			}
		}
	}
	flush()
	start := time.Now()
	applyDelivery(t, old)
	elapsed := time.Since(start)
	rows := users * perUser
	t.Logf("migration of %d messages rows: %v, %.0f rows/s", rows, elapsed, float64(rows)/elapsed.Seconds())
}
