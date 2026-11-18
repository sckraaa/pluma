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
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// langpackCoverage compares the keys a built client looks up (a file of
// its built-in English keys, one per line, a plural by its base key) with
// the strings the server has in each language of its pack (task 114). A
// missing key is Telegram's gap when Telegram's snapshot lacks it too
// (-snapshots, the directory of langpack fetch), otherwise the server's;
// Pluma's own keys (patchkeys.json, own.json, override.json) must have a
// text of their own in every language. It reads keys only, never texts, and
// fails when the server lacks a key Telegram has or one of Pluma's.
func langpackCoverage(args []string, configFile string, stdout io.Writer) error {
	flags := flag.NewFlagSet("langpack coverage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pack := flags.String("pack", "", "the client's pack: android, ios, tdesktop or webk")
	keysFile := flags.String("keys", "", "file of the client's keys, one per line")
	langs := flags.String("lang", "", "languages, comma-separated (default: every stored language of the pack)")
	snapshots := flags.String("snapshots", "", "directory of Telegram's snapshots (langpack fetch), to tell its gaps")
	list := flags.Bool("list", false, "list the keys missing by the server's fault")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *pack == "" || *keysFile == "" {
		return errUsage
	}
	p := langpack.CanonicalPack(*pack)
	clientKeys, err := readKeyList(*keysFile)
	if err != nil {
		return err
	}
	if len(clientKeys) == 0 {
		return fmt.Errorf("%s has no keys", *keysFile)
	}
	store, err := openLangpackStore(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	codes := splitList(*langs, langpack.CanonicalLangCode)
	if len(codes) == 0 {
		stored, err := store.Languages(ctx, p)
		if err != nil {
			return err
		}
		for _, l := range stored {
			codes = append(codes, l.LangCode)
		}
	}
	if len(codes) == 0 {
		return fmt.Errorf("no languages of %s are stored", p)
	}
	patchKeys := slices.Sorted(slices.Values(langpack.PatchKeys()[p]))

	fmt.Fprintf(stdout, "%s: %d keys of the client.\n", p, len(clientKeys))
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "LANG\tON SERVER\tMISSING\tTELEGRAM'S GAPS\tSERVER'S\tPLUMA'S")
	problems := 0
	var details []string
	for _, lang := range codes {
		have, err := store.Keys(ctx, p, lang)
		if err != nil {
			return err
		}
		missing := missingKeys(clientKeys, have)
		// Pluma's keys are the server's own, never Telegram's gaps: those
		// the server lacks, and those with the English text in lang.
		var ours []string
		for _, k := range patchKeys {
			if _, ok := slices.BinarySearch(have, k); !ok {
				ours = append(ours, k)
			}
		}
		for _, k := range langpack.OwnMissing(p, lang) {
			if !slices.Contains(ours, k) {
				ours = append(ours, k)
			}
		}
		rest := missingKeys(missing, patchKeys)
		gaps, server := "-", "-"
		var serverKeys []string
		if *snapshots != "" {
			tg, err := snapshotKeys(*snapshots, p, lang)
			if err != nil {
				return err
			}
			serverKeys = presentKeys(rest, tg)
			gaps, server = fmt.Sprint(len(rest)-len(serverKeys)), fmt.Sprint(len(serverKeys))
		}
		problems += len(serverKeys) + len(ours)
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%d\n", lang, len(clientKeys)-len(missing), len(missing), gaps, server, len(ours))
		if *list && len(serverKeys)+len(ours) > 0 {
			line := lang + ":"
			if len(serverKeys) > 0 {
				line += " the server's: " + strings.Join(serverKeys, ", ") + "."
			}
			if len(ours) > 0 {
				line += " Pluma's without a text of their own: " + strings.Join(ours, ", ") + "."
			}
			details = append(details, line)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, d := range details {
		fmt.Fprintln(stdout, d)
	}
	if problems > 0 {
		return fmt.Errorf("%d key(s) missing by the server's fault", problems)
	}
	return nil
}

// readKeyList reads a file of keys, one per line; blank lines and lines
// starting with '#' are skipped, repeats are dropped.
func readKeyList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var keys []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k := strings.TrimSpace(sc.Text())
		if k == "" || strings.HasPrefix(k, "#") || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys, sc.Err()
}

// snapshotKeys returns the keys with a text in Telegram's snapshot of pack
// in lang, none when the directory has no snapshot of it.
func snapshotKeys(dir, pack, lang string) ([]string, error) {
	s, err := langpack.ReadSnapshot(langpack.SnapshotPath(dir, pack, lang))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, str := range s.Strings {
		if str.Value != "" {
			keys = append(keys, str.Key)
		}
	}
	slices.Sort(keys)
	return keys, nil
}

// missingKeys returns the keys of want that sorted have lacks, in the order
// of want.
func missingKeys(want, have []string) []string {
	var out []string
	for _, k := range want {
		if _, ok := slices.BinarySearch(have, k); !ok {
			out = append(out, k)
		}
	}
	return out
}

// presentKeys returns the keys of want that sorted have has, in the order of
// want.
func presentKeys(want, have []string) []string {
	var out []string
	for _, k := range want {
		if _, ok := slices.BinarySearch(have, k); ok {
			out = append(out, k)
		}
	}
	return out
}
