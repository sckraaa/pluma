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

//go:build integration

package webpage

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The cards are kept by address, survive a new fetch under their id and
// are found by it (migrate-20261102-webpages.sql).
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))

	const link = "https://site.example/post"
	if c, err := s.get(ctx, link); err != nil || c != nil {
		t.Fatalf("before: %+v, %v", c, err)
	}
	u, _ := url.Parse(link)
	photo := mtproto.MakeTLPhoto(&mtproto.Photo{Id: 77, AccessHash: 5, FileReference: []byte{}, Date: 1}).To_Photo()
	page := buildPage(11, u, Meta{Title: "Пост", Description: "About", Image: "/p.png"}, photo)
	if err := s.put(ctx, link, page, 100, 200); err != nil {
		t.Fatal(err)
	}
	c, err := s.get(ctx, link)
	if err != nil || c == nil || c.id != 11 || c.expires != 200 || c.page.GetTitle().GetValue() != "Пост" ||
		c.page.GetPhoto().GetId() != 77 {
		t.Fatalf("stored: %+v, %v", c, err)
	}
	// A new fetch replaces the card and keeps the row's id.
	if err := s.put(ctx, link, emptyPage(99, u), 300, 400); err != nil {
		t.Fatal(err)
	}
	c, err = s.get(ctx, link)
	if err != nil || c.id != 11 || c.expires != 400 || c.page.GetPredicateName() != mtproto.Predicate_webPageEmpty {
		t.Fatalf("replaced: %+v, %v", c, err)
	}
	if p, err := s.byID(ctx, 11); err != nil || p.GetPredicateName() != mtproto.Predicate_webPageEmpty {
		t.Fatalf("by id: %v, %v", p, err)
	}
	if p, err := s.byID(ctx, 99); err != nil || p != nil {
		t.Fatalf("by an unknown id: %v, %v", p, err)
	}
	// An address longer than the column is kept by its hash.
	long := "https://site.example/" + strings.Repeat("a", 3000)
	lu, _ := url.Parse(long)
	if err := s.put(ctx, long, emptyPage(12, lu), 1, 2); err != nil {
		t.Fatal(err)
	}
	if c, err := s.get(ctx, long); err != nil || c == nil || c.id != 12 {
		t.Fatalf("a long address: %+v, %v", c, err)
	}
}
