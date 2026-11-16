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

package business

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The chat links on the production MySQL with the whole schema: the
// limit holds under creations at once, a link of another account is not
// found, views opened at once all count and the owner's do not, entities
// read back as kept.
func TestChatLinkStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	mention := mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 0, Length: 3, UserId_INT64: 7}).To_MessageEntity()
	l, err := s.CreateChatLink(ctx, ChatLink{UserID: 1, Message: "Bob, hi", Entities: []*mtproto.MessageEntity{mention}, Title: "Sales"}, 3, 1000)
	must(err)
	if !ValidSlug(l.Slug) || l.Views != 0 || l.CreatedAt != 1000 {
		t.Fatalf("created %+v", l)
	}
	got, err := s.ChatLink(ctx, l.Slug)
	must(err)
	if got == nil || got.UserID != 1 || got.Message != "Bob, hi" || got.Title != "Sales" || len(got.Entities) != 1 ||
		got.Entities[0].GetPredicateName() != mtproto.Predicate_messageEntityMentionName || got.Entities[0].GetUserId_INT64() != 7 {
		t.Fatalf("read back %+v", got)
	}

	// Another account neither edits nor deletes it.
	if _, err = s.EditChatLink(ctx, l.Slug, ChatLink{UserID: 2, Message: "x"}); !errors.Is(err, ErrChatLinkNotFound) {
		t.Errorf("edit by another account: %v", err)
	}
	if err = s.DeleteChatLink(ctx, 2, l.Slug); !errors.Is(err, ErrChatLinkNotFound) {
		t.Errorf("delete by another account: %v", err)
	}
	edited, err := s.EditChatLink(ctx, l.Slug, ChatLink{UserID: 1, Message: "Hello"})
	must(err)
	if edited.Slug != l.Slug || edited.Message != "Hello" || edited.Title != "" || edited.Entities != nil || edited.CreatedAt != 1000 {
		t.Errorf("edited %+v", edited)
	}

	// Views at once all count; the owner's do not.
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.CountChatLinkView(ctx, l.Slug, int64(100+i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	must(s.CountChatLinkView(ctx, l.Slug, 1))
	links, err := s.ChatLinks(ctx, 1)
	must(err)
	if len(links) != 1 || links[0].Views != 20 {
		t.Fatalf("after 20 views and the owner's: %+v", links)
	}

	// Creations at once never pass the limit: 1 kept, 3 allowed, 6 tried.
	var made, refused atomic.Int32
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateChatLink(ctx, ChatLink{UserID: 1, Message: "m"}, 3, 2000)
			switch {
			case err == nil:
				made.Add(1)
			case errors.Is(err, ErrChatLinksTooMuch):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if made.Load() != 2 || refused.Load() != 4 {
		t.Errorf("made %d, refused %d; want 2 and 4", made.Load(), refused.Load())
	}
	links, err = s.ChatLinks(ctx, 1)
	must(err)
	if len(links) != 3 || links[0].Slug != l.Slug {
		t.Errorf("the list: %d links, first %q", len(links), links[0].Slug)
	}

	owner, err := s.ChatLinkOwner(ctx, l.Slug)
	must(err)
	if owner != 1 {
		t.Errorf("owner %d", owner)
	}
	must(s.DeleteChatLink(ctx, 1, l.Slug))
	if got, err = s.ChatLink(ctx, l.Slug); err != nil || got != nil {
		t.Errorf("after delete: %+v, %v", got, err)
	}
	if owner, err = s.ChatLinkOwner(ctx, l.Slug); err != nil || owner != 0 {
		t.Errorf("owner after delete: %d, %v", owner, err)
	}
}
