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

package appconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// strings of a jsonArray.
func stringsOf(v *mtproto.JSONValue) []string {
	var out []string
	for _, s := range v.GetValue_VECTORJSONVALUE() {
		out = append(out, s.GetValue_STRING())
	}
	return out
}

func keysOf(c *AppConfig) []string {
	var out []string
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		out = append(out, v.GetKey())
	}
	return out
}

// The login domains of bots replace url_auth_domains of the file in its
// place, with a hash of their own; no domains leave the file's config.
func TestWithURLAuthDomains(t *testing.T) {
	base := mustParse(t, `{"a": 1, "url_auth_domains": [], "b": true}`)
	if got := base.WithURLAuthDomains(nil); got != base {
		t.Error("no domains changed the config")
	}
	got := base.WithURLAuthDomains([]string{"one.example", "two.example"})
	if d := stringsOf(field(t, got, "url_auth_domains")); len(d) != 2 || d[0] != "one.example" || d[1] != "two.example" {
		t.Errorf("url_auth_domains %v", d)
	}
	if k := keysOf(got); len(k) != 3 || k[0] != "a" || k[1] != "url_auth_domains" || k[2] != "b" {
		t.Errorf("keys %v", k)
	}
	if len(stringsOf(field(t, base, "url_auth_domains"))) != 0 {
		t.Error("the file's config changed")
	}
	other := base.WithURLAuthDomains([]string{"one.example"})
	if got.Hash == base.Hash || other.Hash == got.Hash || other.Hash == base.Hash {
		t.Errorf("hashes: file %d, two domains %d, one %d", base.Hash, got.Hash, other.Hash)
	}
	if again := base.WithURLAuthDomains([]string{"one.example", "two.example"}); again.Hash != got.Hash {
		t.Errorf("the same domains give hash %d, then %d", got.Hash, again.Hash)
	}
	if r := got.Reply(got.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfigNotModified {
		t.Errorf("reply to the same hash: %s", r.GetPredicateName())
	}

	// A file without the key gets it at the end.
	bare := mustParse(t, `{"a": 1}`).WithURLAuthDomains([]string{"one.example"})
	if k := keysOf(bare); len(k) != 2 || k[1] != "url_auth_domains" {
		t.Errorf("keys %v", k)
	}
}

// Live reads the domains once a period; a failed read keeps the config read
// last.
func TestLive(t *testing.T) {
	base := mustParse(t, `{"url_auth_domains": []}`)
	var (
		domains []string
		fail    bool
		reads   int
	)
	now := time.Unix(1_700_000_000, 0)
	l := NewLive(base, func(context.Context) ([]string, error) {
		reads++
		if fail {
			return nil, errors.New("the database is away")
		}
		return domains, nil
	}, time.Minute)
	l.now = func() time.Time { return now }
	ctx := context.Background()

	if l.Current(ctx) != base || reads != 1 {
		t.Fatalf("no domains: reads %d", reads)
	}
	domains = []string{"site.example"}
	if l.Current(ctx) != base || reads != 1 {
		t.Errorf("read again within the period: reads %d", reads)
	}
	now = now.Add(time.Minute)
	got := l.Current(ctx)
	if d := stringsOf(field(t, got, "url_auth_domains")); len(d) != 1 || d[0] != "site.example" || reads != 2 {
		t.Errorf("after the period: %v, reads %d", d, reads)
	}

	fail = true
	now = now.Add(time.Minute)
	if l.Current(ctx) != got || reads != 3 {
		t.Errorf("a failed read: reads %d", reads)
	}
	if l.Current(ctx) != got || reads != 3 {
		t.Errorf("tried again at once: reads %d", reads)
	}
	fail, domains = false, nil
	now = now.Add(domainsRetryAfter)
	if l.Current(ctx) != base || reads != 4 {
		t.Errorf("after the retry: reads %d", reads)
	}

	// The first read failing answers with the file's config.
	first := NewLive(base, func(context.Context) ([]string, error) { return nil, errors.New("away") }, time.Minute)
	if first.Current(ctx) != base {
		t.Error("the first failed read did not answer with the file")
	}
}
