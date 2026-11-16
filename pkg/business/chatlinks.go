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

package business

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	// ErrChatLinksTooMuch: the account has ChatLinksLimit links already.
	ErrChatLinksTooMuch = errors.New("business: too many chat links")
	// ErrChatLinkNotFound: no link of the slug, or not of the account.
	ErrChatLinkNotFound = errors.New("business: no such chat link")
)

// chatLinkAttempts bounds the rounds of CreateChatLink: a new slug equal
// to a kept one (62^16 of them) or a deadlock of two creations at once
// sends it to the next round, after a short random pause so that the
// creations that met do not meet again.
const chatLinkAttempts = 10

// ChatLink is a business chat link (businessChatLink): the slug of
// https://pluma.chat/m/<slug>, the text with its entities the link puts in
// the input field of the chat with the owner, the title the owner sees in
// the list, and the times it was opened.
type ChatLink struct {
	Slug     string
	UserID   int64
	Message  string
	Entities []*mtproto.MessageEntity
	Title    string
	Views    int32
	// CreatedAt is unix ms: the order of the list.
	CreatedAt int64
}

type chatLinkRow struct {
	Slug      string `db:"slug"`
	UserID    int64  `db:"user_id"`
	Message   string `db:"message"`
	Entities  string `db:"entities"`
	Title     string `db:"title"`
	Views     int32  `db:"views"`
	CreatedAt int64  `db:"created_at"`
}

const chatLinkColumns = "slug, user_id, message, entities, title, views, created_at"

func (r chatLinkRow) link() (ChatLink, error) {
	l := ChatLink{Slug: r.Slug, UserID: r.UserID, Message: r.Message, Title: r.Title, Views: r.Views, CreatedAt: r.CreatedAt}
	if r.Entities != "" {
		var t mtproto.TextWithEntities
		if err := protojson.Unmarshal([]byte(r.Entities), &t); err != nil {
			return ChatLink{}, fmt.Errorf("business: the entities of a chat link of %d: %w", r.UserID, err)
		}
		l.Entities = t.GetEntities()
	}
	return l, nil
}

// marshalEntities is the column of entities, empty for none.
func marshalEntities(entities []*mtproto.MessageEntity) (string, error) {
	if len(entities) == 0 {
		return "", nil
	}
	b, err := protojson.Marshal(&mtproto.TextWithEntities{Entities: entities})
	if err != nil {
		return "", fmt.Errorf("business: the entities of a chat link: %w", err)
	}
	return string(b), nil
}

// CreateChatLink keeps a new chat link of l.UserID with l.Message,
// l.Entities and l.Title under a new slug, made at now (unix ms), unless
// the account has limit links already (ErrChatLinksTooMuch). The count and
// the insert are one transaction: the count locks the account's range of
// the index, so two creations at once cannot both pass the limit (one of
// them deadlocks and goes again).
func (s *Store) CreateChatLink(ctx context.Context, l ChatLink, limit int, now int64) (ChatLink, error) {
	entities, err := marshalEntities(l.Entities)
	if err != nil {
		return ChatLink{}, err
	}
	for range chatLinkAttempts {
		slug, err := NewSlug()
		if err != nil {
			return ChatLink{}, err
		}
		err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			var n struct {
				N int `db:"n"`
			}
			if err := tx.QueryRowPartial(&n, "SELECT COUNT(*) AS n FROM business_chat_links WHERE user_id = ? FOR UPDATE", l.UserID); err != nil {
				return err
			}
			if n.N >= limit {
				return ErrChatLinksTooMuch
			}
			_, err := tx.Exec("INSERT INTO business_chat_links ("+chatLinkColumns+") VALUES (?, ?, ?, ?, ?, 0, ?)",
				slug, l.UserID, l.Message, entities, l.Title, now)
			return err
		})
		if retryable(err) {
			if err := pause(ctx); err != nil {
				return ChatLink{}, err
			}
			continue
		}
		if err != nil {
			return ChatLink{}, err
		}
		out := l
		out.Slug, out.Views, out.CreatedAt = slug, 0, now
		return out, nil
	}
	return ChatLink{}, fmt.Errorf("business: no chat link of %d after %d attempts", l.UserID, chatLinkAttempts)
}

// EditChatLink replaces the text, the entities and the title of the link
// of slug of l.UserID, keeping its slug, views and date; another account's
// slug or none is ErrChatLinkNotFound.
func (s *Store) EditChatLink(ctx context.Context, slug string, l ChatLink) (ChatLink, error) {
	entities, err := marshalEntities(l.Entities)
	if err != nil {
		return ChatLink{}, err
	}
	var out ChatLink
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var rows []chatLinkRow
		if err := tx.QueryRowsPartial(&rows, "SELECT "+chatLinkColumns+" FROM business_chat_links WHERE slug = ? AND user_id = ? FOR UPDATE", slug, l.UserID); err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrChatLinkNotFound
		}
		if _, err := tx.Exec("UPDATE business_chat_links SET message = ?, entities = ?, title = ? WHERE slug = ?", l.Message, entities, l.Title, slug); err != nil {
			return err
		}
		r := rows[0]
		r.Message, r.Entities, r.Title = l.Message, entities, l.Title
		out, err = r.link()
		return err
	})
	return out, err
}

// DeleteChatLink removes the link of slug of userID; another account's
// slug or none is ErrChatLinkNotFound.
func (s *Store) DeleteChatLink(ctx context.Context, userID int64, slug string) error {
	r, err := s.db.Exec(ctx, "DELETE FROM business_chat_links WHERE slug = ? AND user_id = ?", slug, userID)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrChatLinkNotFound
	}
	return nil
}

// ChatLinks returns the links of userID, the oldest first.
func (s *Store) ChatLinks(ctx context.Context, userID int64) ([]ChatLink, error) {
	var rows []chatLinkRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+chatLinkColumns+" FROM business_chat_links WHERE user_id = ? ORDER BY created_at, slug", userID); err != nil {
		return nil, err
	}
	out := make([]ChatLink, 0, len(rows))
	for _, r := range rows {
		l, err := r.link()
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// ChatLink returns the link of slug, nil when there is none.
func (s *Store) ChatLink(ctx context.Context, slug string) (*ChatLink, error) {
	var rows []chatLinkRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+chatLinkColumns+" FROM business_chat_links WHERE slug = ?", slug); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	l, err := rows[0].link()
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ChatLinkOwner returns the account of the link of slug, 0 when there is
// none: what the page of the link (the service links) shows.
func (s *Store) ChatLinkOwner(ctx context.Context, slug string) (int64, error) {
	var rows []struct {
		UserID int64 `db:"user_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT user_id FROM business_chat_links WHERE slug = ?", slug); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].UserID, nil
}

// CountChatLinkView adds one to the views of the link of slug, opened by
// viewer: "views are counted when resolving the link"
// (core.telegram.org/api/business#business-chat-links); the owner's own
// opening is not a view. One UPDATE, so views opened at once all count.
func (s *Store) CountChatLinkView(ctx context.Context, slug string, viewer int64) error {
	_, err := s.db.Exec(ctx, "UPDATE business_chat_links SET views = views + 1 WHERE slug = ? AND user_id <> ?", slug, viewer)
	return err
}

// SlugOf is the slug a client names a link by. The official clients send
// the whole link they were given as the slug of
// account.editBusinessChatLink and account.deleteBusinessChatLink
// (tdesktop api/api_chat_links.cpp:82-84, 110-111; Android
// ui/Business/BusinessLinksController.java:277-278; iOS
// SyncCore_CachedUserData.swift:2056, 2081; Web K
// sidebarLeft/tabs/businessChatLinks.tsx:183), and the bare slug of a
// link they open to account.resolveBusinessChatLink (Android
// ui/LaunchActivity.java:5564-5565). So https://<host>/m/<slug>,
// <host>/m/<slug> and tg://message?slug=<slug> (Android
// BusinessLinksController.findLink, :306-317) all name <slug>; the host
// is not checked, the slug alone names the link.
func SlugOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "?slug="); i >= 0 {
		s = s[i+len("?slug="):]
		if j := strings.IndexByte(s, '&'); j >= 0 {
			s = s[:j]
		}
		return s
	}
	if i := strings.LastIndex(s, "/m/"); i >= 0 {
		s = s[i+len("/m/"):]
	}
	if j := strings.IndexAny(s, "?#"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSuffix(s, "/")
}

// pause waits 5 to 25 ms, or until ctx is done.
func pause(ctx context.Context) error {
	t := time.NewTimer(time.Duration(5+mrand.IntN(21)) * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryable tells an error of a transaction that the same transaction may
// succeed when run again: a key another one took first, or a deadlock
// InnoDB broke by rolling this one back.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if sqlx.IsDuplicate(err) {
		return true
	}
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1213
}
