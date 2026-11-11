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
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
)

// The support ghost (task 080, ADR 0048): an account that takes part in
// supergroups and channels without being seen among their members.
//
//	ghost set <@username|user id> on|off   the account becomes a ghost, or stops
//	ghost join <account> <chat>            hidden into a chat nobody called it to
//	ghost leave <account> <chat>
//	ghost list <account>                   the chats where it is hidden
//
// A chat is named by its t.me link (t.me/name, t.me/+hash,
// t.me/joinchat/hash, t.me/c/<id>/...), @name or id. The account is
// named by @username or id, never by phone, and so is it in the output.

// ghostConfig is the part of the bff config the command needs: the
// database to find the account and the chats, biz (its channel service)
// to change them.
type ghostConfig struct {
	Mysql            sqlx.Config
	BizServiceClient zrpc.RpcClientConf
}

// ghostAccount is an account as the command finds and names it.
type ghostAccount struct {
	Id        int64  `db:"id"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Username  string `db:"username"`
	Ghost     bool   `db:"ghost"`
}

// String names the account by id, username and name; never by phone.
func (a ghostAccount) String() string {
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

// ghostChat is a supergroup or channel as the command names it.
type ghostChat struct {
	Id        int64  `db:"id"`
	Title     string `db:"title"`
	Broadcast bool   `db:"broadcast"`
}

func (c ghostChat) String() string {
	kind := "supergroup"
	if c.Broadcast {
		kind = "channel"
	}
	return fmt.Sprintf("%s %d %q", kind, c.Id, c.Title)
}

// chatRef is how a chat was named: an id, a public username or the hash of
// an invite link; exactly one is set.
type chatRef struct {
	Id       int64
	Username string
	Invite   string
}

// ghostStore finds accounts and chats and changes them.
type ghostStore interface {
	// Account returns nil for no such account.
	Account(ctx context.Context, ref string) (*ghostAccount, error)
	// Chat returns nil for no such supergroup or channel.
	Chat(ctx context.Context, ref chatRef) (*ghostChat, error)
	// SetGhost writes the account's flag and tells whether it changed.
	SetGhost(ctx context.Context, userId int64, ghost bool) (bool, error)
	// SetHidden hides or shows the account's member rows, returning in
	// how many chats.
	SetHidden(ctx context.Context, userId int64, hidden bool) (int32, error)
	// Visible counts the chats where the account is an admin or the
	// owner, whose rows stay shown.
	Visible(ctx context.Context, userId int64) (int, error)
	Join(ctx context.Context, userId, chatId int64) (bool, error)
	Leave(ctx context.Context, userId, chatId int64) (bool, error)
	Hidden(ctx context.Context, userId int64) ([]ghostChat, error)
}

func ghostCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 3 {
		return errUsage
	}
	var c ghostConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	store := &dbGhosts{
		db:      sqlx.NewMySQL(&c.Mysql),
		channel: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return runGhost(ctx, store, args, stdout)
}

// runGhost runs ghost set|join|leave|list on store.
func runGhost(ctx context.Context, store ghostStore, args []string, stdout io.Writer) error {
	if len(args) < 3 {
		return errUsage
	}
	sub := args[1]
	switch {
	case sub == "set" && len(args) == 4:
	case (sub == "join" || sub == "leave") && len(args) == 4:
	case sub == "list" && len(args) == 3:
	default:
		return errUsage
	}
	a, err := findGhostAccount(ctx, store, args[2])
	if err != nil {
		return err
	}
	switch sub {
	case "set":
		var on bool
		switch args[3] {
		case "on":
			on = true
		case "off":
		default:
			return errUsage
		}
		return setGhost(ctx, store, a, on, stdout)
	case "list":
		chats, err := store.Hidden(ctx, a.Id)
		if err != nil {
			return err
		}
		if len(chats) == 0 {
			fmt.Fprintf(stdout, "%s is hidden in no chat.\n", a)
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tKIND\tTITLE")
		for _, c := range chats {
			kind := "supergroup"
			if c.Broadcast {
				kind = "channel"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\n", c.Id, kind, c.Title)
		}
		return tw.Flush()
	}
	if !a.Ghost {
		return fmt.Errorf("%s is no ghost: run ghost set %s on first", a, args[2])
	}
	ref, err := parseChatRef(args[3])
	if err != nil {
		return err
	}
	chat, err := store.Chat(ctx, ref)
	if err != nil {
		return err
	}
	if chat == nil {
		return fmt.Errorf("no supergroup or channel %s (basic groups are not served)", args[3])
	}
	if sub == "join" {
		joined, err := store.Join(ctx, a.Id, chat.Id)
		if err != nil {
			return err
		}
		if !joined {
			fmt.Fprintf(stdout, "%s is in %s already.\n", a, chat)
			return nil
		}
		fmt.Fprintf(stdout, "%s joined %s hidden; its devices show the chat, nobody else sees it.\n", a, chat)
		return nil
	}
	left, err := store.Leave(ctx, a.Id, chat.Id)
	if err != nil {
		return err
	}
	if !left {
		fmt.Fprintf(stdout, "%s is not hidden in %s: nothing to do.\n", a, chat)
		return nil
	}
	fmt.Fprintf(stdout, "%s left %s.\n", a, chat)
	return nil
}

// setGhost makes the account a ghost or not, and its member rows follow:
// the flag first, so that a join meanwhile is already as the account will
// be.
func setGhost(ctx context.Context, store ghostStore, a *ghostAccount, on bool, stdout io.Writer) error {
	changed, err := store.SetGhost(ctx, a.Id, on)
	if err != nil {
		return err
	}
	n, err := store.SetHidden(ctx, a.Id, on)
	if err != nil {
		return err
	}
	state := "a ghost"
	if !on {
		state = "no ghost"
	}
	if changed {
		fmt.Fprintf(stdout, "%s is %s now.\n", a, state)
	} else {
		fmt.Fprintf(stdout, "%s is %s already.\n", a, state)
	}
	what := "hidden in"
	if !on {
		what = "shown again in"
	}
	fmt.Fprintf(stdout, "Its membership was %s %d chats; their counters were recounted.\n", what, n)
	if on {
		visible, err := store.Visible(ctx, a.Id)
		if err != nil {
			return err
		}
		if visible > 0 {
			fmt.Fprintf(stdout, "It stays shown in %d chats where it is an admin or the owner.\n", visible)
		}
	}
	return nil
}

// findGhostAccount finds the account ref names, a user id or a username
// with or without @.
func findGhostAccount(ctx context.Context, store ghostStore, ref string) (*ghostAccount, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "" || ref == "@":
		return nil, errUsage
	case strings.HasPrefix(ref, "+"):
		// ParseInt takes a sign, so a phone number would pass for an id.
		return nil, errors.New("an account is named by @username or user id, not by phone")
	}
	a, err := store.Account(ctx, ref)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no account %s", ref)
	}
	return a, nil
}

// parseChatRef reads how a chat is named: t.me/name, t.me/+hash,
// t.me/joinchat/hash, t.me/c/<id>[/<message>], @name, name or an id.
func parseChatRef(s string) (chatRef, error) {
	s = strings.TrimSpace(s)
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		// A bot API style id of a channel, -100<id>, is taken too.
		if id < 0 {
			id = -id
			if t := strconv.FormatInt(id, 10); strings.HasPrefix(t, "100") && len(t) > 3 {
				id, _ = strconv.ParseInt(t[3:], 10, 64)
			}
		}
		if id <= 0 {
			return chatRef{}, fmt.Errorf("invalid chat id %s", s)
		}
		return chatRef{Id: id}, nil
	}
	if strings.HasPrefix(s, "@") {
		return usernameRef(s[1:])
	}
	if !strings.Contains(s, "/") {
		return usernameRef(s)
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return chatRef{}, fmt.Errorf("invalid chat link %s: %w", s, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) == 0 || parts[0] == "":
		return chatRef{}, fmt.Errorf("no chat in the link %s", s)
	case strings.HasPrefix(parts[0], "+") && len(parts[0]) > 1:
		return chatRef{Invite: parts[0][1:]}, nil
	case parts[0] == "joinchat" && len(parts) > 1 && parts[1] != "":
		return chatRef{Invite: parts[1]}, nil
	case parts[0] == "c" && len(parts) > 1:
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id <= 0 {
			return chatRef{}, fmt.Errorf("invalid chat id in the link %s", s)
		}
		return chatRef{Id: id}, nil
	}
	return usernameRef(parts[0])
}

func usernameRef(name string) (chatRef, error) {
	if name == "" {
		return chatRef{}, errors.New("empty chat username")
	}
	return chatRef{Username: name}, nil
}

// dbGhosts is ghostStore over the database and biz channel.
type dbGhosts struct {
	db      *sqlx.DB
	channel channel_client.ChannelClient
}

// ghostAccountQuery reads a live account; by username it finds any of the
// account's usernames (the username table) as well as the main one.
const ghostAccountQuery = "SELECT id, first_name, last_name, username, ghost FROM users WHERE deleted = 0 AND "

func (s *dbGhosts) Account(ctx context.Context, ref string) (*ghostAccount, error) {
	var (
		rows []ghostAccount
		err  error
	)
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		if id <= 0 {
			return nil, fmt.Errorf("invalid user id %s", ref)
		}
		err = s.db.QueryRowsPartial(ctx, &rows, ghostAccountQuery+"id = ? LIMIT 2", id)
	} else {
		name := strings.TrimPrefix(ref, "@")
		err = s.db.QueryRowsPartial(ctx, &rows, ghostAccountQuery+"(username = ? OR id IN (SELECT peer_id FROM username WHERE username = ? AND peer_type = ? AND deleted = 0)) LIMIT 2",
			name, name, mtproto.PEER_USER)
	}
	if err != nil {
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

func (s *dbGhosts) Chat(ctx context.Context, ref chatRef) (*ghostChat, error) {
	var (
		rows []ghostChat
		err  error
	)
	const columns = "SELECT c.id, c.title, c.broadcast FROM channels c "
	switch {
	case ref.Id != 0:
		err = s.db.QueryRowsPartial(ctx, &rows, columns+"WHERE c.id = ? AND c.deleted = 0", ref.Id)
	case ref.Username != "":
		err = s.db.QueryRowsPartial(ctx, &rows, columns+"JOIN username u ON u.peer_type = ? AND u.peer_id = c.id AND u.deleted = 0 WHERE u.username = ? AND c.deleted = 0",
			mtproto.PEER_CHANNEL, ref.Username)
	default:
		err = s.db.QueryRowsPartial(ctx, &rows, columns+"JOIN chat_invites i ON i.chat_id = c.id WHERE i.link = ? AND c.deleted = 0", ref.Invite)
	}
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s *dbGhosts) SetGhost(ctx context.Context, userId int64, ghost bool) (bool, error) {
	r, err := s.db.Exec(ctx, "UPDATE users SET ghost = ? WHERE id = ? AND ghost <> ?", ghost, userId, ghost)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

func (s *dbGhosts) SetHidden(ctx context.Context, userId int64, hidden bool) (int32, error) {
	r, err := s.channel.ChannelSetUserHidden(ctx, &channel.TLChannelSetUserHidden{UserId: userId, Hidden: mtproto.ToBool(hidden)})
	if err != nil {
		return 0, err
	}
	return r.GetV(), nil
}

func (s *dbGhosts) Visible(ctx context.Context, userId int64) (int, error) {
	var n []int
	if err := s.db.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM channel_participants WHERE user_id = ? AND state = 0 AND role <> 0", userId); err != nil || len(n) == 0 {
		return 0, err
	}
	return n[0], nil
}

func (s *dbGhosts) Join(ctx context.Context, userId, chatId int64) (bool, error) {
	r, err := s.channel.ChannelJoinHidden(ctx, &channel.TLChannelJoinHidden{UserId: userId, ChannelId: chatId})
	if err != nil {
		return false, err
	}
	return mtproto.FromBool(r), nil
}

func (s *dbGhosts) Leave(ctx context.Context, userId, chatId int64) (bool, error) {
	r, err := s.channel.ChannelLeaveHidden(ctx, &channel.TLChannelLeaveHidden{UserId: userId, ChannelId: chatId})
	if err != nil {
		return false, err
	}
	return mtproto.FromBool(r), nil
}

func (s *dbGhosts) Hidden(ctx context.Context, userId int64) ([]ghostChat, error) {
	var rows []ghostChat
	err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT c.id, c.title, c.broadcast FROM channel_participants p JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.user_id = ? AND p.state = 0 AND p.hidden = 1 AND c.deleted = 0 ORDER BY c.id", userId)
	return rows, err
}
