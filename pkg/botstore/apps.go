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

package botstore

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The direct link mini apps of bots (bot_apps, ADR 0068 p. 5): the admin
// tool makes them, messages.getBotApp and messages.requestAppWebView read
// them, botApp.hash is the version of the row.

// Limits of the columns of bot_apps and bot_games.
const (
	maxShortName       = 64
	MaxAppTitle        = 64
	MaxAppDescription  = 512
	maxAppURL          = maxMenuURL
	randomIdAttempts   = 4
	shortNameMinLength = 1
)

// shortNameRe is the short name of an app or a game. It goes into links
// (pluma.chat/<bot>/<app>), so it keeps to the characters every client
// takes there: tdesktop reads the app of a link with
// ^/([a-zA-Z0-9\.\_\-]+) (core/local_url_handlers.cpp:2299), and this is a
// subset of it.
var shortNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Errors of the admin tool's changes of apps and games.
var (
	ErrShortNameInvalid = errors.New("botstore: invalid short name")
	ErrShortNameTaken   = errors.New("botstore: short name taken")
	ErrNoApp            = errors.New("botstore: no such app")
	ErrAppInvalid       = errors.New("botstore: invalid app")
	ErrNoBot            = errors.New("botstore: no such bot")
)

// ValidShortName tells a short name an app or a game may have.
func ValidShortName(name string) bool {
	return len(name) >= shortNameMinLength && len(name) <= maxShortName && shortNameRe.MatchString(name)
}

// App is a direct link mini app of a bot.
type App struct {
	Id                 int64  `db:"id"`
	AccessHash         int64  `db:"access_hash"`
	BotId              int64  `db:"bot_id"`
	ShortName          string `db:"short_name"`
	Title              string `db:"title"`
	Description        string `db:"description"`
	URL                string `db:"url"`
	PhotoId            int64  `db:"photo_id"`
	DocumentId         int64  `db:"document_id"`
	RequestWriteAccess bool   `db:"request_write_access"`
	Version            int32  `db:"version"`
}

const appColumns = "id, access_hash, bot_id, short_name, title, description, url, photo_id, document_id, request_write_access, version"

func (a *App) validate() error {
	if !ValidShortName(a.ShortName) {
		return ErrShortNameInvalid
	}
	if n := utf8.RuneCountInString(a.Title); n == 0 || n > MaxAppTitle || strings.TrimSpace(a.Title) == "" {
		return fmt.Errorf("%w: title of 1 to %d characters", ErrAppInvalid, MaxAppTitle)
	}
	if utf8.RuneCountInString(a.Description) > MaxAppDescription {
		return fmt.Errorf("%w: description over %d characters", ErrAppInvalid, MaxAppDescription)
	}
	if !httpsURL(a.URL) {
		return fmt.Errorf("%w: the url must be https:// and at most %d characters", ErrAppInvalid, maxAppURL)
	}
	if a.PhotoId == 0 {
		// botApp.photo is a Photo, not optional (TL botApp).
		return fmt.Errorf("%w: an app needs a photo", ErrAppInvalid)
	}
	return nil
}

// randomId63 is a random positive id or access hash.
func randomId63() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("botstore: read random: %w", err)
	}
	v := int64(binary.BigEndian.Uint64(b[:]) >> 1)
	if v == 0 {
		v = 1
	}
	return v, nil
}

// retryable tells an error of a transaction that the same transaction may
// succeed when run again: a key another one took first (a random id, or the
// row two inserts raced for) or a deadlock InnoDB broke by rolling this one
// back (two inserts of a missing row under FOR UPDATE).
func retryable(err error) bool {
	if sqlx.IsDuplicate(err) {
		return true
	}
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1213
}

// bumpPlatform raises the versions of the bot that tell clients its
// platform settings changed: platform_version, in the hashes of the
// attachment menu, and bot_info_version, which makes clients fetch the
// bot's info again. The caller drops the cached user afterwards through
// user.updateBotData, which owns the cache of users (as touchBot does).
func bumpPlatform(tx *sqlx.Tx, botId int64) error {
	r, err := tx.Exec("UPDATE bots SET platform_version = platform_version + 1, bot_info_version = bot_info_version + 1 WHERE bot_id = ?", botId)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNoBot
	}
	return nil
}

// AddApp makes an app of the bot, or brings back a removed app of the same
// short name with the new fields (it keeps its id and access hash, so the
// links and the copies clients hold stay valid, and gets a new version).
func (s *Store) AddApp(ctx context.Context, a App) (App, error) {
	if err := a.validate(); err != nil {
		return App{}, err
	}
	for range randomIdAttempts {
		id, err := randomId63()
		if err != nil {
			return App{}, err
		}
		accessHash, err := randomId63()
		if err != nil {
			return App{}, err
		}
		var out App
		err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			var rows []struct {
				Id      int64 `db:"id"`
				Deleted bool  `db:"deleted"`
			}
			if err := tx.QueryRowsPartial(&rows, "SELECT id, deleted FROM bot_apps WHERE bot_id = ? AND short_name = ? FOR UPDATE", a.BotId, a.ShortName); err != nil {
				return err
			}
			if len(rows) > 0 && !rows[0].Deleted {
				return ErrShortNameTaken
			}
			if err := bumpPlatform(tx, a.BotId); err != nil {
				return err
			}
			if len(rows) > 0 {
				if _, err := tx.Exec(`UPDATE bot_apps SET title = ?, description = ?, url = ?, photo_id = ?, document_id = ?, request_write_access = ?,
					version = version + 1, deleted = 0 WHERE id = ?`,
					a.Title, a.Description, a.URL, a.PhotoId, a.DocumentId, a.RequestWriteAccess, rows[0].Id); err != nil {
					return err
				}
				return tx.QueryRowPartial(&out, "SELECT "+appColumns+" FROM bot_apps WHERE id = ?", rows[0].Id)
			}
			if _, err := tx.Exec(`INSERT INTO bot_apps (id, access_hash, bot_id, short_name, title, description, url, photo_id, document_id, request_write_access, version, deleted)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0)`,
				id, accessHash, a.BotId, a.ShortName, a.Title, a.Description, a.URL, a.PhotoId, a.DocumentId, a.RequestWriteAccess); err != nil {
				return err
			}
			return tx.QueryRowPartial(&out, "SELECT "+appColumns+" FROM bot_apps WHERE id = ?", id)
		})
		if retryable(err) {
			// Another id of ours (2^-63) or the same short name made at
			// the same moment: the next round tells which.
			continue
		}
		return out, err
	}
	return App{}, fmt.Errorf("botstore: no free app id after %d attempts", randomIdAttempts)
}

// EditApp replaces the fields of the bot's app of a.ShortName and raises
// its version, so clients holding the old one fetch it again.
func (s *Store) EditApp(ctx context.Context, a App) (App, error) {
	if err := a.validate(); err != nil {
		return App{}, err
	}
	var out App
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.Exec(`UPDATE bot_apps SET title = ?, description = ?, url = ?, photo_id = ?, document_id = ?, request_write_access = ?, version = version + 1
			WHERE bot_id = ? AND short_name = ? AND deleted = 0`,
			a.Title, a.Description, a.URL, a.PhotoId, a.DocumentId, a.RequestWriteAccess, a.BotId, a.ShortName)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return ErrNoApp
		}
		if err := bumpPlatform(tx, a.BotId); err != nil {
			return err
		}
		return tx.QueryRowPartial(&out, "SELECT "+appColumns+" FROM bot_apps WHERE bot_id = ? AND short_name = ?", a.BotId, a.ShortName)
	})
	return out, err
}

// DeleteApp removes the bot's app. The row stays, marked, so the app's id
// answers BOT_APP_INVALID and AddApp can bring the name back.
func (s *Store) DeleteApp(ctx context.Context, botId int64, shortName string) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.Exec("UPDATE bot_apps SET deleted = 1, version = version + 1 WHERE bot_id = ? AND short_name = ? AND deleted = 0", botId, shortName)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return ErrNoApp
		}
		return bumpPlatform(tx, botId)
	})
}

// Apps returns the bot's apps, by short name.
func (s *Store) Apps(ctx context.Context, botId int64) ([]App, error) {
	var rows []App
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+appColumns+" FROM bot_apps WHERE bot_id = ? AND deleted = 0 ORDER BY short_name", botId)
	return rows, err
}

// AppByShortName returns the bot's app of the short name
// (inputBotAppShortName), nil for none.
func (s *Store) AppByShortName(ctx context.Context, botId int64, shortName string) (*App, error) {
	var rows []App
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+appColumns+" FROM bot_apps WHERE bot_id = ? AND short_name = ? AND deleted = 0", botId, shortName); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// AppById returns the app of inputBotAppID, nil for none or for an access
// hash that is not the app's.
func (s *Store) AppById(ctx context.Context, id, accessHash int64) (*App, error) {
	var rows []App
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+appColumns+" FROM bot_apps WHERE id = ? AND access_hash = ? AND deleted = 0", id, accessHash); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// MarkAppLaunched records that the user opened the app; first tells the
// first time.
func (s *Store) MarkAppLaunched(ctx context.Context, userId, appId int64, now int32) (first bool, err error) {
	r, err := s.db.Exec(ctx, "INSERT IGNORE INTO bot_app_launches (user_id, app_id, launched_at) VALUES (?, ?, ?)", userId, appId, now)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// AppLaunched tells whether the user opened the app (botApp.inactive is
// the opposite).
func (s *Store) AppLaunched(ctx context.Context, userId, appId int64) (bool, error) {
	var rows []int64
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT app_id FROM bot_app_launches WHERE user_id = ? AND app_id = ?", userId, appId); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// SetMainApp sets the main mini app of the bot (messages.requestMainWebView,
// user.bot_has_main_app); "" removes it. The caller drops the cached user
// through user.updateBotData with bot_has_main_app.
func (s *Store) SetMainApp(ctx context.Context, botId int64, url string) error {
	if url != "" && !httpsURL(url) {
		return fmt.Errorf("%w: the url must be https:// and at most %d characters", ErrAppInvalid, maxAppURL)
	}
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec("UPDATE bots SET main_app_url = ?, bot_has_main_app = ? WHERE bot_id = ?", url, url != "", botId); err != nil {
			return err
		}
		return bumpPlatform(tx, botId)
	})
}

// MainApp returns the url of the bot's main mini app, "" for none.
func (s *Store) MainApp(ctx context.Context, botId int64) (string, error) {
	var rows []string
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT main_app_url FROM bots WHERE bot_id = ? AND bot_has_main_app = 1", botId); err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0], nil
}
