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
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/appreleases"
	"github.com/teamgram/teamgram-server/pkg/conf"
)

// releaseCommand runs release list, withdraw and restore (task 054,
// docs/ops/releases.md): the current release of a platform is its latest
// build not withdrawn, so withdrawing it rolls the site and the apps'
// update back to the build before.
func releaseCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var cfg struct{ Mysql sqlx.Config }
	if err := conf.Load(configFile, &cfg); err != nil {
		return err
	}
	db := sqlx.NewMySQL(&cfg.Mysql)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch args[1] {
	case "list":
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "PLATFORM\tCURRENT VERSION\tBUILD\tRELEASED")
		for _, p := range appreleases.Platforms {
			rel, err := appreleases.Current(ctx, db, p)
			if err != nil {
				return err
			}
			if rel == nil {
				fmt.Fprintf(tw, "%s\t-\t-\t-\n", p)
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", p, rel.Version, rel.Build, time.Unix(rel.CreatedAt, 0).UTC().Format(time.DateTime+" MST"))
		}
		return tw.Flush()
	case "withdraw", "restore":
		if len(args) != 4 || !appreleases.Known(args[2]) {
			return errUsage
		}
		build, err := strconv.ParseInt(args[3], 10, 32)
		if err != nil {
			return errUsage
		}
		found, err := appreleases.SetWithdrawn(ctx, db, args[2], int32(build), args[1] == "withdraw")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%s has no build %d", args[2], build)
		}
		done := map[string]string{"withdraw": "withdrawn", "restore": "restored"}[args[1]]
		rel, err := appreleases.Current(ctx, db, args[2])
		if err != nil {
			return err
		}
		if rel == nil {
			fmt.Fprintf(stdout, "%s: build %d %s; no release is current now.\n", args[2], build, done)
			return nil
		}
		fmt.Fprintf(stdout, "%s: build %d %s; current now %s (build %d).\n", args[2], build, done, rel.Version, rel.Build)
		return nil
	}
	return errUsage
}
