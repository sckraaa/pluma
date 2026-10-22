//go:build integration

package phoneallowlist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// MySQL with the whole schema, and with the schema up to the allowlist
// migration (migrate-20261021.sql).
var itFullDB, itOldDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itFullDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() {
		defer wg.Done()
		files, err := testinfra.SchemaFilesBefore("migrate-20261021.sql")
		if err != nil {
			errs[1] = err
			return
		}
		itOldDB, errs[1] = testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	}()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	_ = itFullDB.Terminate(ctx)
	_ = itOldDB.Terminate(ctx)
	os.Exit(code)
}

func newDAO(db *testinfra.MySQL) *DAO {
	c := db.Config()
	return New(sqlx.NewMySQL(&c))
}

// TestAllowlistDAO works with its own two phones only: it removes them
// before and after, and ignores other entries of the list.
func TestAllowlistDAO(t *testing.T) {
	ctx := context.Background()
	d := newDAO(itFullDB)

	const (
		alice = "12015550101"
		bob   = "12015550102"
	)
	removeAll := func() {
		for _, p := range []string{alice, bob} {
			if _, err := d.Remove(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	removeAll()
	t.Cleanup(removeAll)
	if ok, err := d.Contains(ctx, alice); err != nil || ok {
		t.Fatalf("Contains of a phone not listed: %v %v", ok, err)
	}

	before := time.Now().Add(-5 * time.Second).Unix()
	if added, err := d.Add(ctx, bob, "Bob"); err != nil || !added {
		t.Fatalf("Add: %v %v", added, err)
	}
	if added, err := d.Add(ctx, alice, "Alice, ünïcode note"); err != nil || !added {
		t.Fatalf("Add: %v %v", added, err)
	}
	if added, err := d.Add(ctx, alice, "again"); err != nil || added {
		t.Fatalf("Add of a listed phone: added %v, %v", added, err)
	}
	if ok, err := d.Contains(ctx, alice); err != nil || !ok {
		t.Fatalf("Contains after Add: %v %v", ok, err)
	}

	all, err := d.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var list []Entry
	for _, e := range all {
		if e.Phone == alice || e.Phone == bob {
			list = append(list, e)
		}
	}
	if len(list) != 2 || list[0].Phone != alice || list[1].Phone != bob {
		t.Fatalf("List = %+v, want alice and bob by phone", list)
	}
	if list[0].Note != "Alice, ünïcode note" {
		t.Fatalf("note %q: the first Add's note is kept, in utf8mb4", list[0].Note)
	}
	if now := time.Now().Add(5 * time.Second).Unix(); list[0].CreatedAt < before || list[0].CreatedAt > now {
		t.Fatalf("created_at %d is not the insert time", list[0].CreatedAt)
	}

	if removed, err := d.Remove(ctx, alice); err != nil || !removed {
		t.Fatalf("Remove: %v %v", removed, err)
	}
	if removed, err := d.Remove(ctx, alice); err != nil || removed {
		t.Fatalf("Remove of a missing phone: removed %v, %v", removed, err)
	}
	if ok, _ := d.Contains(ctx, alice); ok {
		t.Fatal("Contains after Remove")
	}
}

// TestAllowlistMigration applies migrate-20261021.sql to a database with
// accounts: regular accounts keep their access, bots, service, deleted
// accounts and accounts without a phone are not listed.
func TestAllowlistMigration(t *testing.T) {
	ctx := context.Background()
	if err := itOldDB.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", itOldDB.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	users := []struct {
		phone           string
		userType, isBot int
		deleted         int
	}{
		{"12015550111", 2, 0, 0}, // regular: listed
		{"12015550112", 2, 1, 0}, // bot
		{"12015550113", 2, 0, 1}, // deleted
		{"12015550114", 4, 0, 0}, // service account
		{"", 2, 0, 0},            // no phone
	}
	for i, u := range users {
		if _, err = db.Exec("insert into users(user_type, access_hash, phone, country_code, is_bot, deleted) values (?, ?, ?, 'US', ?, ?)",
			u.userType, int64(i+1)*7919, u.phone, u.isBot, u.deleted); err != nil {
			t.Fatal(err)
		}
	}

	migration, err := testinfra.SchemaFile("migrate-20261021.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = itOldDB.ApplySQLFile(ctx, migration); err != nil {
		t.Fatal(err)
	}

	list, err := newDAO(itOldDB).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Phone != "12015550111" || list[0].Note != "existing account" {
		t.Fatalf("allowlist after the migration: %+v, want only the regular account", list)
	}
}
