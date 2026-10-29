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

// tgservicetexts writes the texts of the messages that Telegram's service
// account (777000) sent to the account of a saved session, as templates
// (conformance.ServiceTexts): Pluma's server sends the same texts (task 017).
// Telegram ends the sessions of third-party clients within a minute, so
// usually the login reads them itself: tglogin -qr -service-texts FILE.
//
//	set -a; . ~/pluma-secrets/telegram-api.env; set +a
//	go run ./test/conformance/cmd/tgservicetexts [-name main] [-out file.md]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/test/conformance"
)

func main() {
	name := flag.String("name", "main", "name of the saved session of production")
	out := flag.String("out", "", "file to write the templates to (default: standard output)")
	limit := flag.Int("limit", 200, "how many of the latest messages of the service account to read")
	flag.Parse()
	if err := run(*name, *out, *limit); err != nil {
		fmt.Fprintln(os.Stderr, "tgservicetexts:", err)
		os.Exit(1)
	}
}

func run(name, out string, limit int) error {
	s, err := conformance.LoadSession(false, name)
	if err != nil {
		return fmt.Errorf("no saved session %q of production (go run ./test/conformance/cmd/tglogin -qr): %w", name, err)
	}
	tg, err := conformance.TelegramFromEnv(false)
	if err != nil {
		return err
	}
	cfg, err := tg.Config(s.DC, s.Addr, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := mtclient.DialKey(ctx, cfg, s.AuthKey)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err = c.InitConnection(ctx); err != nil {
		return fmt.Errorf("the session does not work (sign in again: tglogin -qr -service-texts FILE): %w", err)
	}
	doc, messages, texts, err := conformance.ServiceTexts(ctx, c, limit)
	if err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.WriteString(doc)
		return err
	}
	if err = os.WriteFile(out, []byte(doc), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "tgservicetexts: %d messages, %d different texts, in %s\n", messages, texts, out)
	return nil
}
