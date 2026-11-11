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
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
)

// userConfig is the part of the bff config user needs: the database to find
// the account, the user service to change it (it owns the cache of users
// and tells the clients).
type userConfig struct {
	Mysql            sqlx.Config
	BizServiceClient zrpc.RpcClientConf
}

// account is an account as the command finds and names it.
type account struct {
	Id        int64  `db:"id"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Username  string `db:"username"`
}

// String names the account by id, username and name; never by phone.
func (a account) String() string {
	s := fmt.Sprintf("user %d", a.Id)
	name := strings.TrimSpace(a.FirstName + " " + a.LastName)
	switch {
	case a.Username != "" && name != "":
		s += fmt.Sprintf(" (@%s, %s)", a.Username, name)
	case a.Username != "":
		s += fmt.Sprintf(" (@%s)", a.Username)
	case name != "":
		s += fmt.Sprintf(" (%s)", name)
	}
	return s
}

// accounts finds accounts and sets their verification badge.
type accounts interface {
	// ById and ByUsername return nil for no such account.
	ById(ctx context.Context, id int64) (*account, error)
	ByUsername(ctx context.Context, username string) (*account, error)
	SetVerified(ctx context.Context, id int64, verified bool) (bool, error)
}

// accountQuery reads a live account; by username it finds any of the
// account's usernames (the username table) as well as the main one, in the
// case-insensitive collation of both.
const accountQuery = "SELECT id, first_name, last_name, username FROM users WHERE deleted = 0 AND "

type dbAccounts struct {
	db   *sqlx.DB
	user user_client.UserClient
}

func (s *dbAccounts) one(ctx context.Context, where string, args ...interface{}) (*account, error) {
	var rows []account
	if err := s.db.QueryRowsPartial(ctx, &rows, accountQuery+where+" LIMIT 2", args...); err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, nil
	case 1:
		return &rows[0], nil
	}
	return nil, fmt.Errorf("more than one account matches: users %d and %d", rows[0].Id, rows[1].Id)
}

func (s *dbAccounts) ById(ctx context.Context, id int64) (*account, error) {
	return s.one(ctx, "id = ?", id)
}

func (s *dbAccounts) ByUsername(ctx context.Context, username string) (*account, error) {
	return s.one(ctx, "(username = ? OR id IN (SELECT peer_id FROM username WHERE username = ? AND peer_type = ? AND deleted = 0))",
		username, username, mtproto.PEER_USER)
}

func (s *dbAccounts) SetVerified(ctx context.Context, id int64, verified bool) (bool, error) {
	r, err := s.user.UserUpdateVerified(ctx, &userpb.TLUserUpdateVerified{
		UserId:   id,
		Verified: mtproto.ToBool(verified),
	})
	if err != nil {
		return false, err
	}
	return mtproto.FromBool(r), nil
}

// userCommand runs user verify <@username|user id> on|off: it sets or
// clears the verification badge of an account, the check the clients
// draw next to its name. The user service sends updateUser to the
// account's devices and to the users who see it, so their clients take the
// change without a restart. Run it in the bff container, where etcd finds
// the services.
func userCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 4 || args[1] != "verify" {
		return errUsage
	}
	var verified bool
	switch args[3] {
	case "on":
		verified = true
	case "off":
	default:
		return errUsage
	}
	ref := strings.TrimSpace(args[2])
	if ref == "" || ref == "@" {
		return errUsage
	}

	var c userConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	store := &dbAccounts{
		db:   sqlx.NewMySQL(&c.Mysql),
		user: user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return verifyUser(ctx, store, ref, verified, stdout)
}

// verifyUser finds the account ref names, a user id or a username with or
// without @, and sets its badge.
func verifyUser(ctx context.Context, store accounts, ref string, verified bool, stdout io.Writer) error {
	var (
		a   *account
		err error
	)
	if strings.HasPrefix(ref, "+") {
		// ParseInt takes a sign, so a phone number would pass for an id.
		return errors.New("an account is named by @username or user id, not by phone")
	}
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		if id <= 0 {
			return fmt.Errorf("invalid user id %s", ref)
		}
		a, err = store.ById(ctx, id)
	} else {
		a, err = store.ByUsername(ctx, strings.TrimPrefix(ref, "@"))
	}
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no account %s", ref)
	}
	changed, err := store.SetVerified(ctx, a.Id, verified)
	if err != nil {
		return err
	}
	state := "verified"
	if !verified {
		state = "not verified"
	}
	if !changed {
		fmt.Fprintf(stdout, "%s is already %s.\n", a, state)
		return nil
	}
	fmt.Fprintf(stdout, "%s is %s now; its devices and those who see it get updateUser.\n", a, state)
	return nil
}
