//go:build integration

package testinfra

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql" // the services' MySQL driver
)

// TestSchemaLoads applies the whole schema to an empty database, as the
// first start of production does, and checks what the code relies on. A
// failing script stops the MySQL image's initialization (migrate-20260309.sql
// used to, so a fresh install never came up).
func TestSchemaLoads(t *testing.T) {
	ctx := context.Background()
	m, err := StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Terminate(ctx)

	db, err := sql.Open("mysql", m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, c := range []struct{ table, column string }{
		{"phone_allowlist", "phone"},              // migrate-20261021.sql
		{"auth_key_infos", "expires_at"},          // migrate-20261021-pfs.sql
		{"chat_invite_participants", "requested"}, // used by the chat DAO
		{"chat_invite_participants", "approved_by"},
		{"users", "main_tab"}, // the last upstream migration of users
		{"user_state", "pts"}, // migrate-20261022.sql
		{"user_state", "last_message_box_id"},
		{"contact_import_quota", "imported"}, // migrate-20261024-contacts.sql
		{"contact_signups", "phone"},
		{"user_top_peer_resets", "reset_at"},
		{"dialog_filters", "from_suggested"}, // migrate-20261024-dialog-filters.sql
		{"user_passwords", "verifier"},       // migrate-20261024-passwords.sql
		{"user_passwords", "pending_delete_date"},
		{"user_seq_updates", "pts"}, // migrate-20261026-seq-journal.sql
		{"user_seq_cursors", "seq"},
	} {
		var n int
		err = db.QueryRow("select count(*) from information_schema.columns where table_schema = ? and table_name = ? and column_name = ?",
			MySQLDatabase, c.table, c.column).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("%s.%s: %d, %v", c.table, c.column, n, err)
		}
	}

	// One journal row per (user_id, pts), as pkg/userbox relies on
	// (migrate-20261022.sql).
	var nonUnique int
	err = db.QueryRow("select min(non_unique) from information_schema.statistics where table_schema = ? and table_name = 'user_pts_updates' and index_name = 'user_id'",
		MySQLDatabase).Scan(&nonUnique)
	if err != nil || nonUnique != 0 {
		t.Errorf("user_pts_updates index user_id: non_unique %d, %v", nonUnique, err)
	}

	// The reads by the other user in the transaction of a change of a name
	// or a photo (migrate-20261027-profile-watchers.sql).
	for _, ix := range []struct{ table, index, columns string }{
		{"user_contacts", "contact_user_id", "contact_user_id"},
		{"dialogs", "peer", "peer_type,peer_id"},
	} {
		var columns sql.NullString
		err = db.QueryRow("select group_concat(column_name order by seq_in_index) from information_schema.statistics where table_schema = ? and table_name = ? and index_name = ?",
			MySQLDatabase, ix.table, ix.index).Scan(&columns)
		if err != nil || columns.String != ix.columns {
			t.Errorf("%s index %s: columns %q, want %q, %v", ix.table, ix.index, columns.String, ix.columns, err)
		}
	}

	// z_init.sql, the last file, ran: the service account exists.
	var name string
	if err = db.QueryRow("select first_name from users where id = 777000").Scan(&name); err != nil {
		t.Fatalf("user 777000: %v", err)
	}

	t.Run("Reset", func(t *testing.T) {
		// What a migration test leaves behind: a new column and rows.
		for _, q := range []string{
			"alter table users add column testinfra_reset int not null default 0",
			"insert into phone_allowlist(phone, note) values ('12015550199', 'reset')",
			"delete from users where id = 777000",
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		if err := m.Reset(ctx); err != nil {
			t.Fatal(err)
		}
		// The pooled connections still work, on the schema as it started.
		var n int
		if err := db.QueryRow("select count(*) from information_schema.columns where table_schema = ? and table_name = 'users' and column_name = 'testinfra_reset'",
			MySQLDatabase).Scan(&n); err != nil || n != 0 {
			t.Errorf("column added before Reset: %d, %v", n, err)
		}
		if err := db.QueryRow("select count(*) from phone_allowlist where phone = '12015550199'").Scan(&n); err != nil || n != 0 {
			t.Errorf("row inserted before Reset: %d, %v", n, err)
		}
		if err := db.QueryRow("select first_name from users where id = 777000").Scan(&name); err != nil {
			t.Errorf("user 777000 after Reset: %v", err)
		}
	})
}
