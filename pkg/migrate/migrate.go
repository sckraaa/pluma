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

// Package migrate applies the SQL files of teamgramd/deploy/sql in order and
// records each one in schema_migrations with its checksum, so every database
// knows which files it has and an applied file cannot change unnoticed
// (ADR 0013).
//
// Files run in lexical order of their names, the order MySQL's init
// directory used before: 1_teamgram.sql, migrate-YYYYMMDD*.sql, z_init.sql.
// A file runs as one multi-statement request: MySQL commits DDL statement by
// statement, so a file that fails halfway is reported and not recorded, and
// the operator finishes or reverts it by hand (docs/ops/user-state.md).
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// File is one migration.
type File struct {
	Name string
	SQL  string
	Sum  string // hex SHA-256 of SQL
}

// Load reads the *.sql files of dir in order.
func Load(dir string) ([]File, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("migrate: no *.sql files in %s", dir)
	}
	sort.Strings(paths)
	files := make([]File, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		files = append(files, NewFile(filepath.Base(p), string(b)))
	}
	return files, nil
}

// NewFile returns a migration with its checksum.
func NewFile(name, sqlText string) File {
	sum := sha256.Sum256([]byte(sqlText))
	return File{Name: name, SQL: sqlText, Sum: hex.EncodeToString(sum[:])}
}

// DSN returns dsn with the settings migrations need: several statements per
// request.
func DSN(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", err
	}
	cfg.MultiStatements = true
	return cfg.FormatDSN(), nil
}

const createTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
  name VARCHAR(255) NOT NULL PRIMARY KEY,
  checksum CHAR(64) NOT NULL,
  applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// lockName serialises migration runs of one database (two services starting
// at once, an operator and the migrate service).
const lockName = "pluma.schema_migrations"

// ErrNoHistory means the database has tables but no record of which files
// built them: an installation from before versioned migrations. Baseline
// records the files it already has.
var ErrNoHistory = errors.New("migrate: the database has tables but no schema_migrations history; record what it has with `pluma-admin migrate baseline -through <last applied file>`")

// Applied returns the recorded files and their checksums.
func Applied(ctx context.Context, db *sql.DB) (map[string]string, error) {
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT name, checksum FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, s string
		if err := rows.Scan(&n, &s); err != nil {
			return nil, err
		}
		out[n] = s
	}
	return out, rows.Err()
}

// Up applies the files not applied yet, in order, and checks that applied
// ones did not change. report is told about each file applied.
func Up(ctx context.Context, db *sql.DB, files []File, report func(name string, took time.Duration)) error {
	return locked(ctx, db, func(conn *sql.Conn) error {
		applied, err := Applied(ctx, db)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fresh, err := emptySchema(ctx, conn)
			if err != nil {
				return err
			}
			if !fresh {
				return ErrNoHistory
			}
		}
		if err := checkSums(files, applied); err != nil {
			return err
		}
		for _, f := range files {
			if _, ok := applied[f.Name]; ok {
				continue
			}
			start := time.Now()
			if err := execFile(ctx, conn, f); err != nil {
				return err
			}
			if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (name, checksum) VALUES (?, ?)", f.Name, f.Sum); err != nil {
				return fmt.Errorf("migrate: %s applied but not recorded: %w", f.Name, err)
			}
			if report != nil {
				report(f.Name, time.Since(start))
			}
		}
		return nil
	})
}

// Baseline records the files up to and including through as applied
// without running them, for a database built before versioned migrations.
func Baseline(ctx context.Context, db *sql.DB, files []File, through string) (int, error) {
	idx := -1
	for i, f := range files {
		if f.Name == through {
			idx = i
		}
	}
	if idx < 0 {
		return 0, fmt.Errorf("migrate: %s is not one of the migration files", through)
	}
	n := 0
	err := locked(ctx, db, func(conn *sql.Conn) error {
		applied, err := Applied(ctx, db)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			return errors.New("migrate: the database already has a migration history; baseline is only for one without it")
		}
		for _, f := range files[:idx+1] {
			if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (name, checksum) VALUES (?, ?)", f.Name, f.Sum); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// Pending returns the files Up would apply, or an error if an applied file
// changed.
func Pending(files []File, applied map[string]string) ([]File, error) {
	if err := checkSums(files, applied); err != nil {
		return nil, err
	}
	var out []File
	for _, f := range files {
		if _, ok := applied[f.Name]; !ok {
			out = append(out, f)
		}
	}
	return out, nil
}

func checkSums(files []File, applied map[string]string) error {
	known := map[string]bool{}
	for _, f := range files {
		known[f.Name] = true
		if sum, ok := applied[f.Name]; ok && sum != f.Sum {
			return fmt.Errorf("migrate: %s changed after it was applied (recorded %s…, now %s…): add a new migration instead of editing an applied one", f.Name, sum[:12], f.Sum[:12])
		}
	}
	var unknown []string
	for name := range applied {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("migrate: the database has migrations this version does not know: %s (a newer version ran here)", strings.Join(unknown, ", "))
	}
	return nil
}

func execFile(ctx context.Context, conn *sql.Conn, f File) error {
	if err := runStatements(ctx, conn, f.SQL); err != nil {
		return fmt.Errorf("migrate: %s: %w", f.Name, err)
	}
	// A file may change the session (USE, SQL_MODE, an open transaction):
	// leave the connection as it was found.
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("migrate: %s: commit: %w", f.Name, err)
	}
	return nil
}

// runStatements runs a multi-statement request and reads every result set:
// an error of a later statement surfaces only there.
func runStatements(ctx context.Context, conn *sql.Conn, text string) (err error) {
	rows, err := conn.QueryContext(ctx, text)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
	}()
	for {
		for rows.Next() {
		}
		if !rows.NextResultSet() {
			break
		}
	}
	return rows.Err()
}

// emptySchema reports whether the current database has no tables besides
// schema_migrations.
func emptySchema(ctx context.Context, conn *sql.Conn) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME <> 'schema_migrations'`).Scan(&n)
	return n == 0, err
}

// locked runs fn on one connection holding the migration lock.
func locked(ctx context.Context, db *sql.DB, fn func(conn *sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT(DATABASE(), '.', ?), 300)", lockName).Scan(&got); err != nil {
		return err
	}
	if got.Int64 != 1 {
		return errors.New("migrate: another migration run holds the lock")
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(CONCAT(DATABASE(), '.', ?))", lockName)
	}()
	return fn(conn)
}
