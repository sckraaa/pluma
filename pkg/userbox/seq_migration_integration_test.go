//go:build integration

package userbox

import (
	"context"
	"database/sql"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The migration of task 010 (migrate-20261026-seq.sql) on a database in
// the state before it: seq starts at BaseSeq, rows already past it stay.
func TestSeqMigration(t *testing.T) {
	const migration = "migrate-20261026-seq.sql"
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(migration)
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
	if _, err = db.Exec("INSERT INTO user_state (user_id, pts, seq) VALUES (1, 5, 0), (2, 7, 5)"); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // safe to run again
		if err = old.ApplySQLFile(ctx, path); err != nil {
			t.Fatal(err)
		}
		for user, want := range map[int64]int32{1: BaseSeq, 2: 5} {
			if st := mustRead(t, db, user); st.Seq != want {
				t.Errorf("user %d: seq %d, want %d", user, st.Seq, want)
			}
		}
	}
	// A row the first write of a box creates (lockStateQuery) gets the base.
	if _, err = db.Exec(lockStateQuery, 3, 0); err != nil {
		t.Fatal(err)
	}
	if st := mustRead(t, db, 3); st.Seq != BaseSeq || st.Pts != BasePts {
		t.Errorf("a new row: %+v", st)
	}
}
