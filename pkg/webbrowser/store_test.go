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

package webbrowser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func domains(list []Exception) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Domain
	}
	return out
}

// testStore checks the rules of a Store; newUser gives a user id that no
// other test uses.
func testStore(t *testing.T, newStore func() Store, newUser func() int64) {
	ctx := context.Background()
	get := func(t *testing.T, s Store, user int64) Settings {
		t.Helper()
		v, err := s.Get(ctx, user)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return v
	}
	put := func(t *testing.T, s Store, user int64, domain string, external bool) {
		t.Helper()
		if err := s.PutException(ctx, user, Exception{Domain: domain, URL: "https://" + domain + "/"}, external); err != nil {
			t.Fatalf("PutException %s: %v", domain, err)
		}
	}
	wantLists := func(t *testing.T, got Settings, external, inApp []string) {
		t.Helper()
		if !slices.Equal(domains(got.External), external) || !slices.Equal(domains(got.InApp), inApp) {
			t.Fatalf("lists %v / %v, want %v / %v", domains(got.External), domains(got.InApp), external, inApp)
		}
	}

	t.Run("defaults", func(t *testing.T) {
		s, user := newStore(), newUser()
		got := get(t, s, user)
		if got.OpenExternal || !got.DisplayCloseButton || len(got.External) != 0 || len(got.InApp) != 0 {
			t.Fatalf("settings of a new account %+v, want the defaults", got)
		}
		// A write of the exceptions alone leaves the defaults.
		put(t, s, user, "a.com", true)
		if got = get(t, s, user); got.OpenExternal || !got.DisplayCloseButton {
			t.Fatalf("flags after an exception %+v, want the defaults", got)
		}
	})

	t.Run("flags", func(t *testing.T) {
		s, user := newStore(), newUser()
		put(t, s, user, "a.com", false)
		if err := s.SetFlags(ctx, user, true, false); err != nil {
			t.Fatal(err)
		}
		got := get(t, s, user)
		if !got.OpenExternal || got.DisplayCloseButton {
			t.Fatalf("flags %+v, want open_external and no close button", got)
		}
		wantLists(t, got, nil, []string{"a.com"})
		if err := s.SetFlags(ctx, user, false, true); err != nil {
			t.Fatal(err)
		}
		if got = get(t, s, user); got.OpenExternal || !got.DisplayCloseButton {
			t.Fatalf("flags %+v after setting them back", got)
		}
	})

	t.Run("move between lists", func(t *testing.T) {
		s, user := newStore(), newUser()
		put(t, s, user, "a.com", true)
		put(t, s, user, "b.com", true)
		put(t, s, user, "c.com", false)
		wantLists(t, get(t, s, user), []string{"a.com", "b.com"}, []string{"c.com"})

		// To the other list, at its end.
		put(t, s, user, "a.com", false)
		wantLists(t, get(t, s, user), []string{"b.com"}, []string{"c.com", "a.com"})

		// Again into its own list: to the end, with the new address.
		if err := s.PutException(ctx, user, Exception{Domain: "c.com", URL: "c.com/new"}, false); err != nil {
			t.Fatal(err)
		}
		got := get(t, s, user)
		wantLists(t, got, []string{"b.com"}, []string{"a.com", "c.com"})
		if got.InApp[1].URL != "c.com/new" {
			t.Errorf("address %q, want the new one", got.InApp[1].URL)
		}

		// Other accounts see nothing of it.
		wantLists(t, get(t, s, newUser()), nil, nil)
	})

	t.Run("delete", func(t *testing.T) {
		s, user := newStore(), newUser()
		put(t, s, user, "a.com", true)
		put(t, s, user, "b.com", false)
		external, inApp := true, false

		// From the other list: nothing.
		if deleted, err := s.DeleteException(ctx, user, "a.com", &inApp); err != nil || deleted {
			t.Fatalf("delete from the wrong list: %v, %v", deleted, err)
		}
		if deleted, err := s.DeleteException(ctx, user, "a.com", &external); err != nil || !deleted {
			t.Fatalf("delete from its list: %v, %v", deleted, err)
		}
		// From either list.
		if deleted, err := s.DeleteException(ctx, user, "b.com", nil); err != nil || !deleted {
			t.Fatalf("delete from either list: %v, %v", deleted, err)
		}
		if deleted, err := s.DeleteException(ctx, user, "b.com", nil); err != nil || deleted {
			t.Fatalf("delete again: %v, %v", deleted, err)
		}
		wantLists(t, get(t, s, user), nil, nil)
	})

	t.Run("limit", func(t *testing.T) {
		s, user := newStore(), newUser()
		for i := range MaxExceptions {
			put(t, s, user, fmt.Sprintf("d%d.com", i), true)
		}
		if err := s.PutException(ctx, user, Exception{Domain: "over.com", URL: "over.com"}, true); !errors.Is(err, ErrLimit) {
			t.Fatalf("exception %d: %v, want ErrLimit", MaxExceptions+1, err)
		}
		// One already in the list moves to its end; the other list has
		// room of its own.
		put(t, s, user, "d0.com", true)
		put(t, s, user, "other.com", false)
		got := get(t, s, user)
		if len(got.External) != MaxExceptions || got.External[MaxExceptions-1].Domain != "d0.com" || len(got.InApp) != 1 {
			t.Fatalf("lists of %d / %d, last %q", len(got.External), len(got.InApp), got.External[len(got.External)-1].Domain)
		}
		// A full list does not take a domain from the other one.
		if err := s.PutException(ctx, user, Exception{Domain: "other.com", URL: "other.com"}, true); !errors.Is(err, ErrLimit) {
			t.Fatalf("move into a full list: %v, want ErrLimit", err)
		}
		if got = get(t, s, user); len(got.InApp) != 1 {
			t.Fatalf("a refused move changed the lists: %v", domains(got.InApp))
		}
	})

	t.Run("delete all", func(t *testing.T) {
		s, user := newStore(), newUser()
		if err := s.SetFlags(ctx, user, true, false); err != nil {
			t.Fatal(err)
		}
		put(t, s, user, "a.com", true)
		put(t, s, user, "b.com", false)
		put(t, s, user, "c.com", true)
		removed, err := s.DeleteAllExceptions(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		wantLists(t, removed, []string{"a.com", "c.com"}, []string{"b.com"})
		got := get(t, s, user)
		wantLists(t, got, nil, nil)
		if !got.OpenExternal || got.DisplayCloseButton {
			t.Errorf("flags %+v after deleting the exceptions, want them kept", got)
		}
		if removed, err = s.DeleteAllExceptions(ctx, user); err != nil || len(removed.External)+len(removed.InApp) != 0 {
			t.Errorf("delete all again: %+v, %v", removed, err)
		}
	})
}
