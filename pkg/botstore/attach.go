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
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The attachment menu (bot_attach_menus, bot_attach_menu_installs, ADR 0068
// p. 5): the admin tool gives a bot its entry, users add it with
// messages.toggleBotInAttachMenu, messages.getAttachMenuBots lists what a
// user added. A change of the entry raises the bot's platform_version, so
// the hash of every list with the bot changes.

// The peer types of an entry, bits of bot_attach_menus.peer_types, one per
// constructor of AttachMenuPeerType (core.telegram.org/api/bots/attach).
const (
	AttachPeerSameBotPM = 1 << iota // attachMenuPeerTypeSameBotPM
	AttachPeerBotPM                 // attachMenuPeerTypeBotPM
	AttachPeerPM                    // attachMenuPeerTypePM
	AttachPeerChat                  // attachMenuPeerTypeChat
	AttachPeerBroadcast             // attachMenuPeerTypeBroadcast

	AttachPeerAll = AttachPeerSameBotPM | AttachPeerBotPM | AttachPeerPM | AttachPeerChat | AttachPeerBroadcast
)

// maxAttachIcons bounds the icons of an entry: one of each name below.
const maxAttachIcons = 16

// attachIconNames are the icon names the clients read: tdesktop
// default_static (inline_bots/bot_attach_web_view.cpp:178), iOS
// (TelegramCore/Sources/TelegramEngine/Messages/AttachMenuBots.swift:27-43),
// Android (messenger/MediaDataController.java:118-128).
var attachIconNames = []string{
	"default_static", "placeholder_static",
	"ios_static", "ios_animated", "ios_side_menu_static", "macos_side_menu_static", "macos_animated",
	"android_animated", "android_active_animated", "android_side_menu_static",
}

// attachColorNames are the colors of an icon Android reads
// (messenger/MediaDataController.java:118-128).
var attachColorNames = []string{"light_icon", "light_text", "dark_icon", "dark_text"}

// Errors of the attachment menu.
var (
	ErrNoAttachMenu      = errors.New("botstore: the bot has no attachment menu entry")
	ErrAttachMenuInvalid = errors.New("botstore: invalid attachment menu entry")
)

// AttachIconColor is a color of an icon (TL attachMenuBotIconColor).
type AttachIconColor struct {
	Name  string `json:"name"`
	Color int32  `json:"color"`
}

// AttachIcon is an icon of an entry (TL attachMenuBotIcon): the document of
// the media service with the picture.
type AttachIcon struct {
	Name       string            `json:"name"`
	DocumentId int64             `json:"document_id"`
	Colors     []AttachIconColor `json:"colors,omitempty"`
}

// AttachMenu is the attachment menu entry of a bot.
type AttachMenu struct {
	BotId              int64
	ShortName          string
	URL                string
	ShowInAttachMenu   bool
	ShowInSideMenu     bool
	RequestWriteAccess bool
	HasSettings        bool
	PeerTypes          int32
	Icons              []AttachIcon
	// PlatformVersion is bots.platform_version as the entry was read.
	PlatformVersion int32
}

type attachMenuRow struct {
	BotId              int64  `db:"bot_id"`
	ShortName          string `db:"short_name"`
	URL                string `db:"url"`
	ShowInAttachMenu   bool   `db:"show_in_attach_menu"`
	ShowInSideMenu     bool   `db:"show_in_side_menu"`
	RequestWriteAccess bool   `db:"request_write_access"`
	HasSettings        bool   `db:"has_settings"`
	PeerTypes          int32  `db:"peer_types"`
	Icons              string `db:"icons"`
	PlatformVersion    int32  `db:"platform_version"`
}

const attachMenuSelect = `SELECT m.bot_id, m.short_name, m.url, m.show_in_attach_menu, m.show_in_side_menu, m.request_write_access, m.has_settings,
	m.peer_types, m.icons, b.platform_version FROM bot_attach_menus m JOIN bots b ON b.bot_id = m.bot_id`

func (r *attachMenuRow) menu() (AttachMenu, error) {
	m := AttachMenu{
		BotId: r.BotId, ShortName: r.ShortName, URL: r.URL,
		ShowInAttachMenu: r.ShowInAttachMenu, ShowInSideMenu: r.ShowInSideMenu,
		RequestWriteAccess: r.RequestWriteAccess, HasSettings: r.HasSettings,
		PeerTypes: r.PeerTypes, PlatformVersion: r.PlatformVersion,
	}
	if err := json.Unmarshal([]byte(r.Icons), &m.Icons); err != nil {
		return AttachMenu{}, fmt.Errorf("botstore: icons of the attachment menu of %d: %w", r.BotId, err)
	}
	return m, nil
}

func (m *AttachMenu) validate() error {
	if !ValidShortName(m.ShortName) {
		return ErrShortNameInvalid
	}
	if !httpsURL(m.URL) {
		return fmt.Errorf("%w: the url must be https:// and at most %d characters", ErrAttachMenuInvalid, maxMenuURL)
	}
	if !m.ShowInAttachMenu && !m.ShowInSideMenu {
		return fmt.Errorf("%w: shown neither in the attachment menu nor in the side menu", ErrAttachMenuInvalid)
	}
	if m.ShowInAttachMenu && m.PeerTypes&AttachPeerAll == 0 {
		return fmt.Errorf("%w: no peer type for the attachment menu", ErrAttachMenuInvalid)
	}
	if m.PeerTypes&^AttachPeerAll != 0 {
		return fmt.Errorf("%w: unknown peer types %#x", ErrAttachMenuInvalid, m.PeerTypes&^AttachPeerAll)
	}
	if len(m.Icons) > maxAttachIcons {
		return fmt.Errorf("%w: over %d icons", ErrAttachMenuInvalid, maxAttachIcons)
	}
	names := make(map[string]bool, len(m.Icons))
	for _, icon := range m.Icons {
		if !slices.Contains(attachIconNames, icon.Name) || names[icon.Name] {
			return fmt.Errorf("%w: icon %q unknown or repeated", ErrAttachMenuInvalid, icon.Name)
		}
		names[icon.Name] = true
		if icon.DocumentId == 0 {
			return fmt.Errorf("%w: icon %q has no document", ErrAttachMenuInvalid, icon.Name)
		}
		colors := make(map[string]bool, len(icon.Colors))
		for _, c := range icon.Colors {
			if !slices.Contains(attachColorNames, c.Name) || colors[c.Name] {
				return fmt.Errorf("%w: color %q of icon %q unknown or repeated", ErrAttachMenuInvalid, c.Name, icon.Name)
			}
			colors[c.Name] = true
		}
	}
	// tdesktop takes the picture of the entry only from default_static, an
	// SVG (inline_bots/bot_attach_web_view.cpp:170-178, 916).
	if m.ShowInAttachMenu && !names["default_static"] {
		return fmt.Errorf("%w: the attachment menu needs the default_static icon", ErrAttachMenuInvalid)
	}
	return nil
}

// installedOf returns the users who added the bot, inside tx.
func installedOf(tx *sqlx.Tx, botId int64) ([]int64, error) {
	var users []int64
	err := tx.QueryRowsPartial(&users, "SELECT user_id FROM bot_attach_menu_installs WHERE bot_id = ? ORDER BY user_id", botId)
	return users, err
}

// SetAttachMenu gives the bot its entry or replaces it, and returns the
// users who added the bot, whose sessions get updateAttachMenuBots. The
// user.bot_attach_menu flag is biz user's: the caller sets it through
// user.updateBotData, which also drops the cached user.
func (s *Store) SetAttachMenu(ctx context.Context, m AttachMenu) (installed []int64, err error) {
	if err = m.validate(); err != nil {
		return nil, err
	}
	icons, err := json.Marshal(m.Icons)
	if err != nil {
		return nil, err
	}
	if m.Icons == nil {
		icons = []byte("[]")
	}
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if err := bumpPlatform(tx, m.BotId); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO bot_attach_menus (bot_id, short_name, url, show_in_attach_menu, show_in_side_menu, request_write_access, has_settings, peer_types, icons)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE short_name = VALUES(short_name), url = VALUES(url), show_in_attach_menu = VALUES(show_in_attach_menu),
				show_in_side_menu = VALUES(show_in_side_menu), request_write_access = VALUES(request_write_access), has_settings = VALUES(has_settings),
				peer_types = VALUES(peer_types), icons = VALUES(icons)`,
			m.BotId, m.ShortName, m.URL, m.ShowInAttachMenu, m.ShowInSideMenu, m.RequestWriteAccess, m.HasSettings, m.PeerTypes, string(icons)); err != nil {
			return err
		}
		installed, err = installedOf(tx, m.BotId)
		return err
	})
	return installed, err
}

// DeleteAttachMenu takes the bot's entry away, with every user's addition
// of it, and returns those users, whose sessions get updateAttachMenuBots.
// The caller clears user.bot_attach_menu through user.updateBotData.
func (s *Store) DeleteAttachMenu(ctx context.Context, botId int64) (installed []int64, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.Exec("DELETE FROM bot_attach_menus WHERE bot_id = ?", botId)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return ErrNoAttachMenu
		}
		if installed, err = installedOf(tx, botId); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM bot_attach_menu_installs WHERE bot_id = ?", botId); err != nil {
			return err
		}
		return bumpPlatform(tx, botId)
	})
	return installed, err
}

// AttachMenu returns the bot's entry, nil for none.
func (s *Store) AttachMenu(ctx context.Context, botId int64) (*AttachMenu, error) {
	var rows []attachMenuRow
	if err := s.db.QueryRowsPartial(ctx, &rows, attachMenuSelect+" WHERE m.bot_id = ?", botId); err != nil || len(rows) == 0 {
		return nil, err
	}
	m, err := rows[0].menu()
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// InstallAttachMenu adds the bot's entry to the user's menu; added tells
// it was not there. ErrNoAttachMenu for a bot without one. The entry row is
// read under a shared lock, so a DeleteAttachMenu at the same moment either
// sees the addition and takes it away or comes first and leaves nothing to
// add to.
func (s *Store) InstallAttachMenu(ctx context.Context, userId, botId int64, now int32) (added bool, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var bots []int64
		if err := tx.QueryRowsPartial(&bots, "SELECT bot_id FROM bot_attach_menus WHERE bot_id = ? LOCK IN SHARE MODE", botId); err != nil {
			return err
		}
		if len(bots) == 0 {
			return ErrNoAttachMenu
		}
		r, err := tx.Exec("INSERT IGNORE INTO bot_attach_menu_installs (user_id, bot_id, installed_at) VALUES (?, ?, ?)", userId, botId, now)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		added = n == 1
		return err
	})
	return added, err
}

// UninstallAttachMenu removes the bot's entry from the user's menu; removed
// tells it was there.
func (s *Store) UninstallAttachMenu(ctx context.Context, userId, botId int64) (removed bool, err error) {
	r, err := s.db.Exec(ctx, "DELETE FROM bot_attach_menu_installs WHERE user_id = ? AND bot_id = ?", userId, botId)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// AttachMenuInstalled tells whether the user added the bot's entry
// (attachMenuBot.inactive is the opposite).
func (s *Store) AttachMenuInstalled(ctx context.Context, userId, botId int64) (bool, error) {
	var rows []int64
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT bot_id FROM bot_attach_menu_installs WHERE user_id = ? AND bot_id = ?", userId, botId); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// InstalledAttachMenus returns the entries the user added, in the order
// they were added, leaving out the bots deleted since.
func (s *Store) InstalledAttachMenus(ctx context.Context, userId int64) ([]AttachMenu, error) {
	var rows []attachMenuRow
	if err := s.db.QueryRowsPartial(ctx, &rows, attachMenuSelect+`
		JOIN bot_attach_menu_installs i ON i.bot_id = m.bot_id JOIN users u ON u.id = m.bot_id
		WHERE i.user_id = ? AND u.deleted = 0 ORDER BY i.installed_at, m.bot_id`, userId); err != nil {
		return nil, err
	}
	out := make([]AttachMenu, 0, len(rows))
	for i := range rows {
		m, err := rows[i].menu()
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// AttachMenusHash is the hash of a list of entries for
// messages.getAttachMenuBots: the algorithm of core.telegram.org/api/offsets
// over each bot's id and platform version, so a change of any entry
// changes it. 0 for an empty list.
func AttachMenusHash(menus []AttachMenu) int64 {
	var h uint64
	step := func(v int64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(v)
	}
	for _, m := range menus {
		step(m.BotId)
		step(int64(m.PlatformVersion))
	}
	return int64(h)
}
