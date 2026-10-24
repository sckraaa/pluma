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
	"github.com/teamgram/teamgram-server/pkg/twofa"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// passwordConfig is the part of the bff config password needs: the
// database of accounts and passwords, Redis of the 2FA state.
type passwordConfig struct {
	Mysql sqlx.Config
	KV    kv.KvConf
}

// passwordCommand runs password reset (task 008): it removes the 2FA
// password of an account whose owner forgot it and cannot reset it from a
// logged-in session (account.resetPassword). The administrator establishes
// who asks outside the server first: whoever has the login code then logs
// in without a password.
func passwordCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 3 || args[1] != "reset" {
		return errUsage
	}
	phone, err := normalize(args[2])
	if err != nil {
		return err
	}

	var c passwordConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var ids []struct {
		Id int64 `db:"id"`
	}
	if err := db.QueryRowsPartial(ctx, &ids, "SELECT id FROM users WHERE phone = ? AND deleted = 0", phone); err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("+%s has no account", phone)
	}
	userId := ids[0].Id
	removed, err := twofa.NewService(twofa.NewMySQLStore(db), kv.NewStore(c.KV)).Remove(ctx, userId)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(stdout, "+%s (user %d): the 2FA password is removed. The user logs in with a login code only.\n", phone, userId)
	} else {
		fmt.Fprintf(stdout, "+%s (user %d) has no 2FA password.\n", phone, userId)
	}
	return nil
}
