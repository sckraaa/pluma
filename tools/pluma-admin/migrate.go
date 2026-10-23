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

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/migrate"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// migrateCommand runs migrate up, status or baseline (pkg/migrate).
func migrateCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	flags := flag.NewFlagSet("migrate "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "teamgramd/deploy/sql", "directory of the migration files")
	wait := flags.Duration("wait", 0, "wait this long for MySQL to accept connections")
	through := flags.String("through", "", "baseline: the last file the database already has")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}

	var c struct{ Mysql sqlx.Config }
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	dsn, err := migrate.DSN(c.Mysql.DSN)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(2)

	ctx, cancel := context.WithTimeout(context.Background(), *wait+30*time.Minute)
	defer cancel()
	if err := waitForDB(ctx, db, *wait); err != nil {
		return err
	}
	files, err := migrate.Load(*dir)
	if err != nil {
		return err
	}

	switch args[1] {
	case "up":
		n := 0
		err := migrate.Up(ctx, db, files, func(name string, took time.Duration) {
			n++
			fmt.Fprintf(stdout, "applied %s (%s)\n", name, took.Round(time.Millisecond))
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "schema up to date: %d files, %d applied now\n", len(files), n)
		return nil
	case "status":
		applied, err := migrate.Applied(ctx, db)
		if err != nil {
			return err
		}
		pending, err := migrate.Pending(files, applied)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%d applied, %d pending\n", len(applied), len(pending))
		for _, f := range pending {
			fmt.Fprintf(stdout, "pending %s\n", f.Name)
		}
		return nil
	case "baseline":
		if *through == "" {
			return errUsage
		}
		n, err := migrate.Baseline(ctx, db, files, *through)
		if err != nil {
			return err
		}
		audit("migrate baseline through %s: %d files recorded as applied", *through, n)
		fmt.Fprintf(stdout, "recorded %d files up to %s as applied\n", n, *through)
		return nil
	}
	return errUsage
}

// waitForDB retries until MySQL answers or wait is over: the migrate service
// may start before MySQL is ready.
func waitForDB(ctx context.Context, db *sql.DB, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := db.PingContext(pctx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("MySQL: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
