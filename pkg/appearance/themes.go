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

package appearance

import (
	"context"
	"fmt"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// A theme row of a user: saved in the list, or a preinstalled one taken out
// of the list.
const (
	themeNone    = 0
	themeSaved   = 1
	themeRemoved = 2
)

type themeRow struct {
	CreatorID     int64  `db:"creator_id"`
	InstallsCount int32  `db:"installs_count"`
	UpdatedAt     int64  `db:"updated_at"`
	Data          []byte `db:"data"`
}

// ThemeOf is a theme as this server keeps it, for a viewer: Creator is whether
// the viewer made it and installs_count is its current count.
func ThemeOf(t *mtproto.Theme, creatorID, viewer int64, installs int32) *mtproto.Theme {
	shown := proto.Clone(t).(*mtproto.Theme)
	shown.Creator = creatorID == viewer
	shown.InstallsCount = mtproto.MakeFlagsInt32(installs)
	return shown
}

func unmarshalTheme(data []byte) (*mtproto.Theme, error) {
	t := &mtproto.Theme{}
	if err := proto.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("appearance: unmarshal a theme: %w", err)
	}
	return t, nil
}

// CreateTheme records a new theme and returns the error
// mtproto.ErrThemeSlugOccupied for a slug another theme has.
func (s *Store) CreateTheme(ctx context.Context, creatorID int64, t *mtproto.Theme) error {
	data, err := marshal(t)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO themes (id, access_hash, slug, creator_id, title, data, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		t.GetId(), t.GetAccessHash(), t.GetSlug(), creatorID, t.GetTitle(), data, time.Now().Unix())
	if sqlx.IsDuplicate(err) {
		return mtproto.ErrThemeSlugOccupied
	}
	return err
}

// ThemeByID returns a theme of this table, its creator and its count of
// installs; nil if there is none.
func (s *Store) ThemeByID(ctx context.Context, id int64) (*mtproto.Theme, int64, int32, error) {
	return s.theme(ctx, "id = ?", id)
}

// ThemeBySlug is ThemeByID by the slug of the theme.
func (s *Store) ThemeBySlug(ctx context.Context, slug string) (*mtproto.Theme, int64, int32, error) {
	return s.theme(ctx, "slug = ?", slug)
}

func (s *Store) theme(ctx context.Context, where string, arg any) (*mtproto.Theme, int64, int32, error) {
	var rows []themeRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT creator_id, installs_count, updated_at, data FROM themes WHERE "+where, arg); err != nil {
		return nil, 0, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, 0, nil
	}
	t, err := unmarshalTheme(rows[0].Data)
	return t, rows[0].CreatorID, rows[0].InstallsCount, err
}

// UpdateTheme replaces a theme and tells the devices of those who saved or
// installed it and of its creator (core.telegram.org/api/themes):
// mtproto.ErrThemeSlugOccupied for a slug another theme has. The change is
// recorded first; a recipient that could not be told is logged and still has
// the new theme from account.getThemes or getTheme.
func (s *Store) UpdateTheme(ctx context.Context, creatorID int64, t *mtproto.Theme) error {
	data, err := marshal(t)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx,
		"UPDATE themes SET slug = ?, title = ?, data = ?, updated_at = ? WHERE id = ?",
		t.GetSlug(), t.GetTitle(), data, time.Now().Unix(), t.GetId()); err != nil {
		if sqlx.IsDuplicate(err) {
			return mtproto.ErrThemeSlugOccupied
		}
		return err
	}
	var users []int64
	if err = s.db.QueryRowsPartial(ctx, &users,
		"SELECT user_id FROM user_themes WHERE theme_id = ? AND (state = ? OR installed = 1)", t.GetId(), themeSaved); err != nil {
		return err
	}
	seen := map[int64]bool{creatorID: true}
	s.pushTheme(ctx, creatorID, creatorID, t)
	for _, u := range users {
		if seen[u] {
			continue
		}
		seen[u] = true
		s.pushTheme(ctx, u, creatorID, t)
	}
	return nil
}

func (s *Store) pushTheme(ctx context.Context, userID, creatorID int64, t *mtproto.Theme) {
	u := mtproto.MakeTLUpdateTheme(&mtproto.Update{Theme: ThemeOf(t, creatorID, userID, t.GetInstallsCount().GetValue())}).To_Update()
	if err := s.run(ctx, userID, func(b *userbox.Box) error {
		return sync_client.EmitSeqUpdate(b, s.topic, u, nil, nil)
	}); err != nil {
		logx.WithContext(ctx).Errorf("appearance: tell user %d of theme %d: %v", userID, t.GetId(), err)
	}
}

// SaveTheme puts a theme into the account's list; UnsaveTheme takes it out.
func (s *Store) SaveTheme(ctx context.Context, userID, themeID int64) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO user_themes (user_id, theme_id, state, saved_at) VALUES (?, ?, ?, ?)"+
			" AS new ON DUPLICATE KEY UPDATE state = new.state, saved_at = new.saved_at",
		userID, themeID, themeSaved, time.Now().UnixMilli())
	return err
}

// UnsaveTheme takes a theme out of the list: a preinstalled one stays out,
// as a removed preinstalled wallpaper does (inCatalog). A row of a theme the
// account installed stays, without saved, so that updateTheme still reaches
// it and installs_count is not counted twice.
func (s *Store) UnsaveTheme(ctx context.Context, userID, themeID int64, inCatalog bool) error {
	state := themeNone
	if inCatalog {
		state = themeRemoved
	}
	_, err := s.db.Exec(ctx,
		"INSERT INTO user_themes (user_id, theme_id, state, saved_at) VALUES (?, ?, ?, 0)"+
			" AS new ON DUPLICATE KEY UPDATE state = new.state",
		userID, themeID, state)
	return err
}

// InstallTheme marks the theme as installed by the account and counts the
// account's first install.
func (s *Store) InstallTheme(ctx context.Context, userID, themeID int64) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		// The row is created first, so that a second install of the account
		// waits on its lock and sees installed.
		if _, err := tx.Exec("INSERT IGNORE INTO user_themes (user_id, theme_id) VALUES (?, ?)", userID, themeID); err != nil {
			return err
		}
		var installed []bool
		if err := tx.QueryRowsPartial(&installed, "SELECT installed FROM user_themes WHERE user_id = ? AND theme_id = ? FOR UPDATE", userID, themeID); err != nil {
			return err
		}
		if len(installed) > 0 && installed[0] {
			return nil
		}
		if _, err := tx.Exec("UPDATE user_themes SET installed = 1 WHERE user_id = ? AND theme_id = ?", userID, themeID); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE themes SET installs_count = installs_count + 1 WHERE id = ?", themeID)
		return err
	})
}

// SavedTheme is a theme of an account's list, as the viewer sees it, and the
// time it last changed.
type SavedTheme struct {
	Theme     *mtproto.Theme
	UpdatedAt int64
}

// ThemeEntry is a row of an account's list of themes.
type ThemeEntry struct {
	ThemeID int64 `db:"theme_id"`
	State   int32 `db:"state"`
}

// Removed says a preinstalled theme was taken out of the list.
func (e ThemeEntry) Removed() bool { return e.State == themeRemoved }

// ThemeEntries returns the account's rows that say something about its list.
func (s *Store) ThemeEntries(ctx context.Context, userID int64) ([]ThemeEntry, error) {
	var rows []ThemeEntry
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT theme_id, state FROM user_themes WHERE user_id = ? AND state <> 0", userID)
	return rows, err
}

// SavedThemes returns the themes the account saved, the last saved first.
func (s *Store) SavedThemes(ctx context.Context, userID int64) ([]SavedTheme, error) {
	var rows []themeRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT t.creator_id, t.installs_count, t.updated_at, t.data FROM user_themes u JOIN themes t ON t.id = u.theme_id"+
			" WHERE u.user_id = ? AND u.state = ? ORDER BY u.saved_at DESC, t.id DESC", userID, themeSaved); err != nil {
		return nil, err
	}
	out := make([]SavedTheme, 0, len(rows))
	for _, r := range rows {
		t, err := unmarshalTheme(r.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, SavedTheme{Theme: ThemeOf(t, r.CreatorID, userID, r.InstallsCount), UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}
