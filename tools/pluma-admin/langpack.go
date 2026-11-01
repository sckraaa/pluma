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
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// apiIDEnv names the environment variable with the api_id fetch sends to
// Telegram: the owner's application (my.telegram.org), never stored in the
// repository.
const apiIDEnv = "TELEGRAM_API_ID"

// langpackStore is what import and list need of pkg/langpack.Store.
type langpackStore interface {
	Import(ctx context.Context, snap *langpack.Snapshot) (*langpack.ImportResult, error)
	Languages(ctx context.Context, pack string) ([]langpack.Language, error)
}

// telegramConn is a connection to Telegram (langpack.TelegramClient).
type telegramConn interface {
	langpack.Invoker
	Close() error
}

// Replaced by the tests.
var (
	openLangpackStore = func(configFile string) (langpackStore, error) {
		var c struct{ Mysql sqlx.Config }
		if err := conf.Load(configFile, &c); err != nil {
			return nil, fmt.Errorf("config %s: %w", configFile, err)
		}
		return langpack.NewStore(sqlx.NewMySQL(&c.Mysql)), nil
	}
	dialTelegram = func(ctx context.Context, cfg langpack.TelegramConfig) (telegramConn, error) {
		return langpack.DialTelegram(ctx, cfg)
	}
)

// langpackCommand runs langpack fetch, import and list (task 021, ADR 0025;
// docs/ops/langpacks.md).
func langpackCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	switch args[1] {
	case "fetch":
		return langpackFetch(args[2:], stdout)
	case "import":
		return langpackImport(args[2:], configFile, stdout)
	case "list":
		return langpackList(args[2:], configFile, stdout)
	}
	return errUsage
}

// langpackFetch reads the packs from Telegram into snapshot files.
func langpackFetch(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("langpack fetch", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	out := flags.String("out", "", "directory of the snapshot files (<pack>/<lang>.json)")
	packs := flags.String("pack", strings.Join(langpack.Packs, ","), "packs, comma-separated")
	langs := flags.String("lang", "", "languages, comma-separated (default: every language of each pack)")
	apiID := flags.Int("api-id", 0, "api_id of the application (default: $"+apiIDEnv+")")
	addr := flags.String("addr", langpack.TelegramAddr, "Telegram's datacenter, host:port")
	dc := flags.Int("dc", langpack.TelegramDC, "the number of that datacenter")
	delay := flags.Duration("delay", time.Second, "pause between requests")
	timeout := flags.Duration("timeout", time.Hour, "time for the whole fetch")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *out == "" || *delay < 0 || *timeout <= 0 {
		return errUsage
	}
	id := int64(*apiID)
	if id == 0 {
		v := strings.TrimSpace(os.Getenv(apiIDEnv))
		if v == "" {
			return fmt.Errorf("an api_id is needed: -api-id or $%s (the application's, from my.telegram.org)", apiIDEnv)
		}
		var err error
		if id, err = strconv.ParseInt(v, 10, 32); err != nil {
			return fmt.Errorf("$%s is not a number", apiIDEnv)
		}
	}
	if id <= 0 || id > 1<<31-1 || *dc <= 0 || *dc > 1<<15 {
		return errUsage
	}
	packList := splitList(*packs, langpack.CanonicalPack)
	if len(packList) == 0 {
		return errUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	tg, err := dialTelegram(ctx, langpack.TelegramConfig{Addr: *addr, DC: int32(*dc), APIID: int32(id)})
	if err != nil {
		return fmt.Errorf("connect to Telegram at %s: %w", *addr, err)
	}
	defer tg.Close()
	f := &langpack.Fetcher{
		Invoker:        tg,
		Delay:          *delay,
		MaxFloodWait:   5 * time.Minute,
		RequestTimeout: time.Minute,
	}
	total := 0
	for _, pack := range packList {
		err := f.FetchPack(ctx, pack, splitList(*langs, langpack.CanonicalLangCode), func(s *langpack.Snapshot) error {
			path, err := langpack.WriteSnapshot(*out, s)
			if err != nil {
				return err
			}
			total++
			fmt.Fprintf(stdout, "%s/%s: version %d, %d strings -> %s\n", s.LangPack, s.LangCode, s.Version, len(s.Strings), path)
			return nil
		})
		if err != nil {
			return fmt.Errorf("pack %s: %w", pack, err)
		}
	}
	fmt.Fprintf(stdout, "%d snapshot(s) in %s. Import them with: pluma-admin langpack import %s\n", total, *out, *out)
	return nil
}

// langpackImport puts snapshot files into the database.
func langpackImport(args []string, configFile string, stdout io.Writer) error {
	flags := flag.NewFlagSet("langpack import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pack := flags.String("pack", "", "only this pack")
	lang := flags.String("lang", "", "only this language")
	keys := flags.Bool("keys", false, "list every key that still names Telegram")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		return errUsage
	}
	dir := flags.Arg(0)
	files, err := langpack.SnapshotFiles(dir, langpack.CanonicalPack(*pack), langpack.CanonicalLangCode(*lang))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no snapshot files (<pack>/<lang>.json) in %s", dir)
	}
	store, err := openLangpackStore(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	changed := 0
	for _, path := range files {
		snap, err := langpack.ReadSnapshot(path)
		if err != nil {
			return err
		}
		r, err := store.Import(ctx, snap)
		if err != nil {
			return err
		}
		if r.Changes() > 0 {
			changed++
		}
		printImport(stdout, r, *keys)
	}
	fmt.Fprintf(stdout, "%d of %d language(s) changed.\n", changed, len(files))
	return nil
}

// printImport describes what an import did and what still names Telegram.
func printImport(w io.Writer, r *langpack.ImportResult, keys bool) {
	switch {
	case r.Changes() == 0:
		fmt.Fprintf(w, "%s/%s: no changes, version %d (%d strings)\n", r.LangPack, r.LangCode, r.Version, r.Unchanged)
	case r.OldVersion == 0:
		fmt.Fprintf(w, "%s/%s: new, version %d: %d strings\n", r.LangPack, r.LangCode, r.Version, r.Added)
	default:
		fmt.Fprintf(w, "%s/%s: version %d -> %d: %d added, %d changed, %d deleted, %d unchanged\n",
			r.LangPack, r.LangCode, r.OldVersion, r.Version, r.Added, r.Changed, r.Deleted, r.Unchanged)
	}
	if len(r.Leftovers) == 0 {
		return
	}
	words := map[string]int{}
	for _, ws := range r.Leftovers {
		for _, word := range ws {
			words[word]++
		}
	}
	list := make([]string, 0, len(words))
	for word := range words {
		list = append(list, word)
	}
	sort.Slice(list, func(i, j int) bool {
		if words[list[i]] != words[list[j]] {
			return words[list[i]] > words[list[j]]
		}
		return list[i] < list[j]
	})
	var parts []string
	for _, word := range list {
		parts = append(parts, fmt.Sprintf("%s ×%d", word, words[word]))
	}
	fmt.Fprintf(w, "  %d string(s) still name Telegram: %s\n", len(r.Leftovers), strings.Join(parts, ", "))
	if keys {
		ks := make([]string, 0, len(r.Leftovers))
		for k := range r.Leftovers {
			ks = append(ks, k)
		}
		slices.Sort(ks)
		for _, k := range ks {
			fmt.Fprintf(w, "    %s: %s\n", k, strings.Join(r.Leftovers[k], ", "))
		}
	}
}

// langpackList prints the stored languages.
func langpackList(args []string, configFile string, stdout io.Writer) error {
	flags := flag.NewFlagSet("langpack list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pack := flags.String("pack", "", "only this pack")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	store, err := openLangpackStore(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	langs, err := store.Languages(ctx, langpack.CanonicalPack(*pack))
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PACK\tLANG\tNAME\tVERSION\tTELEGRAM VERSION\tSTRINGS\tUPDATED")
	for _, l := range langs {
		updated := "-"
		if l.UpdatedAt > 0 {
			updated = time.Unix(l.UpdatedAt, 0).UTC().Format("2006-01-02 15:04") + " UTC"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d/%d\t%s\n", l.LangPack, l.LangCode, l.NativeName, l.Version, l.UpstreamVersion,
			l.TranslatedCount, l.StringsCount, updated)
	}
	if err = w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d language(s).\n", len(langs))
	return nil
}

// splitList splits a comma-separated list, normalizing and dropping empty
// and repeated items.
func splitList(s string, norm func(string) string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = norm(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
