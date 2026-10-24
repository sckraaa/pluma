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
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// premiumConfig is the part of the bff config premium needs: the database
// to find accounts and list grants, the user service to change them (it
// owns the cache of users and tells the user's devices).
type premiumConfig struct {
	Mysql            sqlx.Config
	BizServiceClient zrpc.RpcClientConf
}

type premiumRow struct {
	Id      int64  `db:"id"`
	Phone   string `db:"phone"`
	Name    string `db:"first_name"`
	Premium bool   `db:"premium"`
	Until   int64  `db:"premium_expire_date"`
}

// premiumCommand runs premium grant, revoke and list (task 014, ADR 0016).
func premiumCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var (
		phone  string
		months int
	)
	switch {
	case args[1] == "list" && len(args) == 2:
	case (args[1] == "grant" || args[1] == "revoke") && len(args) >= 3:
		flags := flag.NewFlagSet("premium", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		if args[1] == "grant" {
			flags.IntVar(&months, "months", 0, "months of Premium; without it, for good")
		}
		if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 || months < 0 {
			return errUsage
		}
		var err error
		if phone, err = normalize(args[2]); err != nil {
			return err
		}
	default:
		return errUsage
	}

	var c premiumConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if args[1] == "list" {
		return listPremium(ctx, db, stdout)
	}
	var rows []premiumRow
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT id, phone, first_name, premium, premium_expire_date FROM users WHERE phone = ? AND deleted = 0", phone); err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("+%s has no account", phone)
	}
	in := &userpb.TLUserUpdatePremium{
		UserId:  rows[0].Id,
		Premium: mtproto.ToBool(args[1] == "grant"),
	}
	if months > 0 {
		in.Months = wrapperspb.Int32(int32(months))
	}
	if _, err := user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)).UserUpdatePremium(ctx, in); err != nil {
		return err
	}
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT id, phone, first_name, premium, premium_expire_date FROM users WHERE id = ?", rows[0].Id); err != nil {
		return err
	}
	r := rows[len(rows)-1]
	switch {
	case !r.Premium:
		fmt.Fprintf(stdout, "+%s (user %d) has no Premium.\n", r.Phone, r.Id)
	default:
		fmt.Fprintf(stdout, "+%s (user %d) has Premium %s.\n", r.Phone, r.Id, premiumUntil(r.Until))
	}
	return nil
}

func listPremium(ctx context.Context, db *sqlx.DB, stdout io.Writer) error {
	var rows []premiumRow
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT id, phone, first_name, premium, premium_expire_date FROM users WHERE premium = 1 AND deleted = 0 ORDER BY id"); err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PHONE\tUSER\tNAME\tPREMIUM")
	for _, r := range rows {
		fmt.Fprintf(w, "+%s\t%d\t%s\t%s\n", r.Phone, r.Id, r.Name, premiumUntil(r.Until))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d user(s).\n", len(rows))
	return nil
}

// premiumUntil describes the end of a Premium.
func premiumUntil(until int64) string {
	switch {
	case until == 0:
		return "for good"
	case until <= time.Now().Unix():
		return "ended " + time.Unix(until, 0).UTC().Format("2006-01-02 15:04") + " UTC, being taken away"
	default:
		return "until " + time.Unix(until, 0).UTC().Format("2006-01-02 15:04") + " UTC"
	}
}
