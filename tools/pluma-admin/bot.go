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
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// botConfig is the part of the bff config bot needs: the database of bots,
// the user service for the flags (it owns the cache of users).
type botConfig struct {
	Mysql            sqlx.Config
	BizServiceClient zrpc.RpcClientConf
}

// botCommand runs bot create, list, token and set (task 070, ADR 0041).
func botCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var c botConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	db := sqlx.NewMySQL(&c.Mysql)
	store := botstore.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch args[1] {
	case "list":
		if len(args) != 2 {
			return errUsage
		}
		return listBots(ctx, store, stdout)
	case "create":
		return createBot(ctx, db, store, args[2:], stdout)
	case "token":
		if len(args) != 3 {
			return errUsage
		}
		id, err := botByUsername(ctx, store, args[2])
		if err != nil {
			return err
		}
		token, err := store.NewToken(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, token)
		return nil
	case "set":
		return setBot(ctx, c, store, args[2:], stdout)
	}
	return errUsage
}

func botByUsername(ctx context.Context, store *botstore.Store, username string) (int64, error) {
	id, err := store.ByUsername(ctx, username)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("no bot %s", username)
	}
	return id, nil
}

func listBots(ctx context.Context, store *botstore.Store, stdout io.Writer) error {
	bots, err := store.List(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSERNAME\tNAME\tOWNER")
	for _, b := range bots {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\n", b.Id, b.Username, b.Name, b.Creator)
	}
	return w.Flush()
}

func createBot(ctx context.Context, db *sqlx.DB, store *botstore.Store, args []string, stdout io.Writer) error {
	// Flags after the name.
	var rest, nameParts []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && i > 0 {
			rest = args[i:]
			break
		}
		nameParts = append(nameParts, args[i])
	}
	if len(nameParts) < 2 {
		return errUsage
	}
	username, name := nameParts[0], strings.Join(nameParts[1:], " ")
	flags := flag.NewFlagSet("bot create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	owner := flags.String("owner", "", "the phone of the account that may edit the bot")
	if err := flags.Parse(rest); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	if name == "" || len([]rune(name)) > botstore.MaxName {
		return errors.New("the name must be 1 to 64 characters")
	}
	creator := int64(0)
	if *owner != "" {
		phone, err := normalize(*owner)
		if err != nil {
			return err
		}
		var ids []int64
		if err = db.QueryRowsPartial(ctx, &ids, "SELECT id FROM users WHERE phone = ? AND deleted = 0", phone); err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("+%s has no account", phone)
		}
		creator = ids[0]
	}
	id, token, err := store.Create(ctx, username, name, creator)
	switch {
	case errors.Is(err, botstore.ErrUsernameTaken):
		return fmt.Errorf("username %s is taken", username)
	case errors.Is(err, botstore.ErrUsernameInvalid):
		return fmt.Errorf("username %s: 5 to 32 letters, digits and _, starts with a letter, ends in bot", username)
	case err != nil:
		return err
	}
	fmt.Fprintf(stdout, "bot %d: %s\n", id, token)
	return nil
}

func setBot(ctx context.Context, c botConfig, store *botstore.Store, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	flags := flag.NewFlagSet("bot set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	privacy := flags.String("privacy", "", "on: the bot gets only the messages meant for it in groups")
	groups := flags.String("groups", "", "off: the bot cannot be added to groups")
	inlineGeo := flags.String("inline-geo", "", "on: the bot gets the location of the user of an inline query")
	inline := flags.String("inline", "", "the placeholder of the inline mode; a space turns inline off")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	id, err := botByUsername(ctx, store, args[0])
	if err != nil {
		return err
	}
	in := &userpb.TLUserUpdateBotData{BotId: id}
	on := func(v string) (*mtproto.Bool, error) {
		switch v {
		case "":
			return nil, nil
		case "on":
			return mtproto.BoolTrue, nil
		case "off":
			return mtproto.BoolFalse, nil
		}
		return nil, errUsage
	}
	flagsSet := false
	if *privacy != "" {
		// Privacy mode on is bot_chat_history off.
		v, err := on(*privacy)
		if err != nil {
			return err
		}
		in.BotChatHistory = mtproto.ToBool(!mtproto.FromBool(v))
		flagsSet = true
	}
	if *groups != "" {
		v, err := on(*groups)
		if err != nil {
			return err
		}
		in.BotNochats = mtproto.ToBool(!mtproto.FromBool(v))
		flagsSet = true
	}
	if *inlineGeo != "" {
		if in.BotInlineGeo, err = on(*inlineGeo); err != nil {
			return err
		}
		flagsSet = true
	}
	if *inline != "" {
		placeholder := *inline
		if strings.TrimSpace(placeholder) == "" {
			placeholder = ""
		}
		in.BotInlinePlaceholder = wrapperspb.String(placeholder)
		flagsSet = true
	}
	if !flagsSet {
		return errUsage
	}
	if _, err := user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)).UserUpdateBotData(ctx, in); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bot %d updated.\n", id)
	return nil
}
