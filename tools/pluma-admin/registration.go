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
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"
)

// registrationCommand switches registration between open and by list, the
// switch of the panel's Registration page (task 072).
func registrationCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 2 {
		return errUsage
	}
	var open bool
	switch args[1] {
	case "open":
		open = true
	case "list":
	default:
		return errUsage
	}
	var c struct{ Mysql sqlx.Config }
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := phoneallowlist.New(db).SetRegistrationOpen(ctx, open, "pluma-admin"); err != nil {
		return err
	}
	if open {
		fmt.Fprintln(stdout, "Registration is open to every number.")
	} else {
		fmt.Fprintln(stdout, "Registration is by the allowlist.")
	}
	return nil
}
