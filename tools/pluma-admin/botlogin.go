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
	"slices"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// bot login: the OpenID Connect client of a bot on oauth.pluma.chat (task
// 106, ADR 0068, p. 6). Its client_id is the bot's id; the secret is shown
// once, when it is made, and the server keeps only its hash. The login
// domain of the bot's buttons is bot set -domain.

// botLogins is what bot login changes: pkg/botstore.
type botLogins interface {
	botLookup
	LoginDomain(ctx context.Context, botId int64) (string, error)
	SetLoginClient(ctx context.Context, botId int64, redirectURIs, origins []string, now int32) (string, error)
	EditLoginClient(ctx context.Context, botId int64, redirectURIs, origins []string) error
	DeleteLoginClient(ctx context.Context, botId int64) error
	LoginClient(ctx context.Context, botId int64) (*botstore.LoginClient, error)
}

// botLoginCommand: login show|set|edit|rm <bot> [flags...].
func botLoginCommand(ctx context.Context, store botLogins, args []string, now func() time.Time, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	switch args[0] {
	case "show", "rm":
		if len(args) != 2 {
			return errUsage
		}
	case "set", "edit":
	default:
		return errUsage
	}
	flags := flag.NewFlagSet("bot login", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var redirects, origins listFlag
	flags.Var(&redirects, "redirect", "an https address the site may get the login back at (redirect_uri, exact); repeatable")
	flags.Var(&origins, "origin", "an https://host[:port] origin of the site's pages that start a login; repeatable; off for none (edit)")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	given := givenFlags(flags)
	id, err := editableBot(ctx, store, args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "show":
		return showLogin(ctx, store, id, stdout)
	case "rm":
		if err := store.DeleteLoginClient(ctx, id); errors.Is(err, botstore.ErrNoLoginClient) {
			return errors.New("the bot has no login client")
		} else if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "bot %d: login client removed; its sites can no longer log users in.\n", id)
		return nil
	case "set":
		if len(redirects) == 0 || slices.Contains(origins, settingOff) {
			return errUsage
		}
		secret, err := store.SetLoginClient(ctx, id, redirects, origins, int32(now().Unix()))
		if err != nil {
			return describeLoginError(err)
		}
		// The secret has a line of its own, to be copied: it cannot be
		// shown again.
		fmt.Fprintf(stdout, "client_id: %d\nclient_secret: %s\n", id, secret)
		fmt.Fprintln(stdout, "The secret is shown only now; a lost one is replaced by bot login set again.")
		return nil
	}
	// edit: the lists given replace the client's, the secret stays.
	if len(given) == 0 {
		return errUsage
	}
	c, err := store.LoginClient(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("the bot has no login client: bot login set makes one")
	}
	if given["redirect"] {
		c.RedirectURIs = redirects
	}
	if given["origin"] {
		switch {
		case len(origins) == 1 && origins[0] == settingOff:
			c.Origins = nil
		case slices.Contains(origins, settingOff):
			return errUsage
		default:
			c.Origins = origins
		}
	}
	if err := store.EditLoginClient(ctx, id, c.RedirectURIs, c.Origins); err != nil {
		return describeLoginError(err)
	}
	fmt.Fprintf(stdout, "bot %d: login client updated; the secret is the same.\n", id)
	return nil
}

func showLogin(ctx context.Context, store botLogins, id int64, stdout io.Writer) error {
	domain, err := store.LoginDomain(ctx, id)
	if err != nil {
		return err
	}
	c, err := store.LoginClient(ctx, id)
	if err != nil {
		return err
	}
	if domain == "" {
		domain = "none"
	}
	fmt.Fprintf(stdout, "bot %d\n  login domain: %s\n", id, domain)
	if c == nil {
		fmt.Fprintln(stdout, "  login client: none")
		return nil
	}
	fmt.Fprintf(stdout, "  client_id: %d (secret made %s)\n", id, time.Unix(int64(c.CreatedAt), 0).UTC().Format(time.RFC3339))
	fmt.Fprintf(stdout, "  redirects: %s\n", strings.Join(c.RedirectURIs, " "))
	if len(c.Origins) == 0 {
		fmt.Fprintln(stdout, "  origins: none")
	} else {
		fmt.Fprintf(stdout, "  origins: %s\n", strings.Join(c.Origins, " "))
	}
	return nil
}

func describeLoginError(err error) error {
	if errors.Is(err, botstore.ErrLoginClientInvalid) {
		// The store says which address and why.
		return errors.New(strings.TrimPrefix(err.Error(), "botstore: "))
	}
	return describeAppError(err)
}
