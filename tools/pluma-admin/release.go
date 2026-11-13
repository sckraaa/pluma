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

// releaseCommand runs release list, withdraw, restore, mandatory and
// optional (tasks 054 and 095, docs/ops/releases.md): the current release
// of a platform is its latest build not withdrawn, so withdrawing it rolls
// the site and the apps' update back to the build before; a mandatory
// release is offered with can_not_skip.
func releaseCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var platform string
	var build int32
	switch args[1] {
	case "list":
	case "withdraw", "restore", "mandatory", "optional":
		var err error
		if platform, build, err = releaseTarget(args); err != nil {
			return err
		}
	default:
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
		fmt.Fprintln(tw, "PLATFORM\tCURRENT VERSION\tBUILD\tMANDATORY\tRELEASED")
		for _, p := range appreleases.Platforms {
			rel, err := appreleases.Current(ctx, db, p)
			if err != nil {
				return err
			}
			if rel == nil {
				fmt.Fprintf(tw, "%s\t-\t-\t-\t-\n", p)
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", p, rel.Version, rel.Build, mandatoryColumn(rel),
				time.Unix(rel.CreatedAt, 0).UTC().Format(time.DateTime+" MST"))
		}
		return tw.Flush()
	case "withdraw", "restore":
		found, err := appreleases.SetWithdrawn(ctx, db, platform, build, args[1] == "withdraw")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%s has no build %d", platform, build)
		}
		done := map[string]string{"withdraw": "withdrawn", "restore": "restored"}[args[1]]
		rel, err := appreleases.Current(ctx, db, platform)
		if err != nil {
			return err
		}
		if rel == nil {
			fmt.Fprintf(stdout, "%s: build %d %s; no release is current now.\n", platform, build, done)
			return nil
		}
		fmt.Fprintf(stdout, "%s: build %d %s; current now %s (build %d).\n", platform, build, done, rel.Version, rel.Build)
		return nil
	}
	found, err := appreleases.SetCanNotSkip(ctx, db, platform, build, args[1] == "mandatory")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s has no build %d", platform, build)
	}
	fmt.Fprintf(stdout, "%s: build %d is %s now.\n", platform, build, args[1])
	return nil
}

// releaseTarget is the platform and build of release withdraw, restore,
// mandatory and optional. Only the Android and iOS apps take an update
// from help.getAppUpdate, the one that carries can_not_skip; the desktop
// app updates itself over HTTP, which has no mandatory updates
// (docs/ops/releases.md).
func releaseTarget(args []string) (string, int32, error) {
	if len(args) != 4 || !appreleases.Known(args[2]) {
		return "", 0, errUsage
	}
	build, err := strconv.ParseInt(args[3], 10, 32)
	if err != nil {
		return "", 0, errUsage
	}
	if (args[1] == "mandatory" || args[1] == "optional") && !appreleases.CanBeMandatory(args[2]) {
		return "", 0, fmt.Errorf("%s: the desktop app has no mandatory updates; only android and ios do", args[2])
	}
	return args[2], int32(build), nil
}

// mandatoryColumn is the MANDATORY column of release list: "-" where the
// apps have no mandatory updates.
func mandatoryColumn(rel *appreleases.Release) string {
	switch {
	case !appreleases.CanBeMandatory(rel.Platform):
		return "-"
	case rel.CanNotSkip:
		return "yes"
	}
	return "no"
}
