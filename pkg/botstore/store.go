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

// Package botstore keeps what the server knows of bot accounts beyond their
// row of users (task 070, ADR 0041): the tokens, the command lists by scope
// and language, the texts by language and the menu buttons. The flags that
// travel with the user are biz user's (user.updateBotData), which owns the
// cache of users.
package botstore

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"google.golang.org/grpc/status"
)

// Limits of the Bot API (core.telegram.org/bots/api: BotCommand,
// setMyCommands, setMyName, setMyDescription, setMyShortDescription), which
// sits on these methods.
const (
	MaxCommands       = 100
	maxCommandDesc    = 256
	MaxName           = 64
	MaxAbout          = 120
	MaxDescription    = 512
	maxLangCode       = 16
	userTypeBot       = 3
	accountDaysTTL    = 548
	menuKindCommands  = 1
	menuKindWebApp    = 2
	defaultMenuUserId = 0
)

// The command of a BotCommand: core.telegram.org/bots/api#botcommand.
var commandRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// A username a bot may take: 5 to 32 characters ending in "bot".
var usernameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{1,28}[bB][oO][tT]$`)

// errButtonInvalid: core.telegram.org/method/bots.setBotMenuButton.
var errButtonInvalid = status.Error(mtproto.ErrBadRequest, "BUTTON_INVALID")

// ErrUsernameTaken is Create for a username in use.
var ErrUsernameTaken = errors.New("botstore: username taken")

// ErrUsernameInvalid is Create for a username a bot cannot have.
var ErrUsernameInvalid = errors.New("botstore: invalid bot username")

// Store reads and writes the tables of bots.
type Store struct {
	db *sqlx.DB
}

// New returns a store on db.
func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Bot is a bot as the admin tool lists it.
type Bot struct {
	Id       int64  `db:"bot_id"`
	Username string `db:"username"`
	Name     string `db:"first_name"`
	Creator  int64  `db:"creator_user_id"`
}

// Create makes a bot account, with its username and first token, in one
// transaction; creator is the user who may edit it, 0 for none. The phone of
// the row is a placeholder no client sees (mtproto.ImmutableUser hides a
// bot's phone): users.phone is unique and not null.
func (s *Store) Create(ctx context.Context, username, name string, creator int64) (botId int64, token string, err error) {
	if !usernameRe.MatchString(username) {
		return 0, "", ErrUsernameInvalid
	}
	placeholder := make([]byte, 8)
	if _, err = rand.Read(placeholder); err != nil {
		return 0, "", err
	}
	accessHash := make([]byte, 8)
	if _, err = rand.Read(accessHash); err != nil {
		return 0, "", err
	}
	res := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		r, err := tx.Exec(
			"INSERT INTO users (user_type, access_hash, first_name, username, phone, country_code, is_bot, account_days_ttl) VALUES (?, ?, ?, ?, ?, '', 1, ?)",
			userTypeBot, int64(binary.BigEndian.Uint64(accessHash)>>1), name, username, "bot"+hex.EncodeToString(placeholder), accountDaysTTL)
		if err != nil {
			result.Err = err
			return
		}
		if botId, err = r.LastInsertId(); err != nil {
			result.Err = err
			return
		}
		if _, err = tx.Exec("INSERT INTO username (username, peer_type, peer_id) VALUES (?, ?, ?)", username, mtproto.PEER_USER, botId); err != nil {
			result.Err = err
			return
		}
		var hash string
		if token, hash, err = newToken(botId); err != nil {
			result.Err = err
			return
		}
		_, result.Err = tx.Exec("INSERT INTO bots (bot_id, creator_user_id, token, bot_nochats) VALUES (?, ?, ?, 0)", botId, creator, hash)
	})
	if res.Err != nil {
		if sqlx.IsDuplicate(res.Err) {
			return 0, "", ErrUsernameTaken
		}
		return 0, "", res.Err
	}
	return botId, token, nil
}

// List returns all the bots, by id.
func (s *Store) List(ctx context.Context) ([]Bot, error) {
	var rows []Bot
	err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT b.bot_id, u.username, u.first_name, b.creator_user_id FROM bots b JOIN users u ON u.id = b.bot_id ORDER BY b.bot_id")
	return rows, err
}

// ByUsername returns the id of the bot, 0 for none.
func (s *Store) ByUsername(ctx context.Context, username string) (int64, error) {
	var ids []int64
	err := s.db.QueryRowsPartial(ctx, &ids,
		"SELECT b.bot_id FROM bots b JOIN users u ON u.id = b.bot_id WHERE u.username = ? AND u.deleted = 0", username)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// Admined returns the ids of the bots creator may edit.
func (s *Store) Admined(ctx context.Context, creator int64) ([]int64, error) {
	var ids []int64
	err := s.db.QueryRowsPartial(ctx, &ids, "SELECT bot_id FROM bots WHERE creator_user_id = ? ORDER BY bot_id", creator)
	return ids, err
}

// Creator returns the creator of the bot, 0 for none; ok is false for an
// account that is not a bot.
func (s *Store) Creator(ctx context.Context, botId int64) (creator int64, ok bool, err error) {
	var rows []int64
	if err = s.db.QueryRowsPartial(ctx, &rows, "SELECT creator_user_id FROM bots WHERE bot_id = ?", botId); err != nil || len(rows) == 0 {
		return 0, false, err
	}
	return rows[0], true, nil
}

// NewToken replaces the token of the bot, which stops working at once, and
// returns the new one.
func (s *Store) NewToken(ctx context.Context, botId int64) (string, error) {
	token, hash, err := newToken(botId)
	if err != nil {
		return "", err
	}
	r, err := s.db.Exec(ctx, "UPDATE bots SET token = ? WHERE bot_id = ?", hash, botId)
	if err != nil {
		return "", err
	}
	if n, err := r.RowsAffected(); err != nil {
		return "", err
	} else if n != 1 {
		return "", fmt.Errorf("botstore: no bot %d", botId)
	}
	return token, nil
}

// BotIdOfToken returns the bot the token logs in: 0 for a token no bot has
// (a token replaced is such a token, see NewToken).
func (s *Store) BotIdOfToken(ctx context.Context, token string) (int64, error) {
	id, hash, ok := ParseToken(token)
	if !ok {
		return 0, nil
	}
	var rows []int64
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT bot_id FROM bots WHERE bot_id = ? AND token = ?", id, hash); err != nil || len(rows) == 0 {
		return 0, err
	}
	return rows[0], nil
}

// ValidCommands checks a list of bots.setBotCommands.
func ValidCommands(commands []*mtproto.BotCommand) error {
	if len(commands) > MaxCommands {
		return mtproto.ErrBotCommandInvalid
	}
	for _, c := range commands {
		if !commandRe.MatchString(c.GetCommand()) {
			return mtproto.ErrBotCommandInvalid
		}
		n := utf8.RuneCountInString(c.GetDescription())
		if n == 0 || n > maxCommandDesc {
			return mtproto.ErrBotCommandDescriptionInvalid
		}
	}
	return nil
}

// ValidLangCode checks the lang_code of a method: "" for the default one, or
// a code the clients would send (LANG_CODE_INVALID,
// core.telegram.org/method/bots.setBotCommands).
func ValidLangCode(lang string) bool {
	if len(lang) > maxLangCode {
		return false
	}
	for _, r := range lang {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

type storedCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Ephemeral   bool   `json:"ephemeral,omitempty"`
}

// SetCommands stores the list of the scope and language; an empty list
// removes it, so a broader scope applies again.
func (s *Store) SetCommands(ctx context.Context, botId int64, scope, lang string, commands []*mtproto.BotCommand) error {
	if len(commands) == 0 {
		return s.ResetCommands(ctx, botId, scope, lang)
	}
	stored := make([]storedCommand, 0, len(commands))
	for _, c := range commands {
		stored = append(stored, storedCommand{Command: c.GetCommand(), Description: c.GetDescription(), Ephemeral: c.GetEphemeral()})
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO bot_command_lists (bot_id, scope, lang_code, commands) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE commands = VALUES(commands)",
		botId, scope, lang, string(data))
	return err
}

// ResetCommands removes the list of the scope and language.
func (s *Store) ResetCommands(ctx context.Context, botId int64, scope, lang string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM bot_command_lists WHERE bot_id = ? AND scope = ? AND lang_code = ?", botId, scope, lang)
	return err
}

// Commands returns the list stored for exactly the scope and language,
// empty when there is none.
func (s *Store) Commands(ctx context.Context, botId int64, scope, lang string) ([]*mtproto.BotCommand, error) {
	var rows []string
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT commands FROM bot_command_lists WHERE bot_id = ? AND scope = ? AND lang_code = ?", botId, scope, lang); err != nil || len(rows) == 0 {
		return []*mtproto.BotCommand{}, err
	}
	return decodeCommands(rows[0])
}

func decodeCommands(data string) ([]*mtproto.BotCommand, error) {
	var stored []storedCommand
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return nil, fmt.Errorf("botstore: command list: %w", err)
	}
	out := make([]*mtproto.BotCommand, 0, len(stored))
	for _, c := range stored {
		out = append(out, mtproto.MakeTLBotCommand(&mtproto.BotCommand{Command: c.Command, Description: c.Description, Ephemeral: c.Ephemeral}).To_BotCommand())
	}
	return out, nil
}

// Viewer is who looks at a bot's info and where.
type Viewer struct {
	UserId   int64
	PeerType int32  // mtproto.PEER_USER for the private chat with the bot
	PeerId   int64  // the chat or channel; the viewer in a private chat
	Admin    bool   // the viewer administers the chat
	Lang     string // the viewer's language
}

// resolveCommands returns the first list set along the viewer's scopes, each
// with its language first and the default language next: the order of
// core.telegram.org/bots/api#determining-list-of-commands.
func (s *Store) resolveCommands(ctx context.Context, botId int64, v Viewer) ([]*mtproto.BotCommand, error) {
	var rows []struct {
		Scope    string `db:"scope"`
		LangCode string `db:"lang_code"`
		Commands string `db:"commands"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT scope, lang_code, commands FROM bot_command_lists WHERE bot_id = ?", botId); err != nil {
		return nil, err
	}
	lists := make(map[string]string, len(rows))
	for _, r := range rows {
		lists[r.Scope+"\x00"+r.LangCode] = r.Commands
	}
	for _, scope := range scopeChain(v) {
		for _, lang := range []string{v.Lang, ""} {
			if data, ok := lists[scope+"\x00"+lang]; ok {
				return decodeCommands(data)
			}
			if v.Lang == "" {
				break
			}
		}
	}
	return []*mtproto.BotCommand{}, nil
}

// Texts are the name, about and description of a bot in a language; nil
// fields are unset.
type Texts struct {
	Name, About, Description *string
}

// SetTexts stores the given texts of a language. The default language's
// texts are bots.description here and users.first_name and users.about by
// biz user, so the caller writes those; a nil field stays as it is.
func (s *Store) SetTexts(ctx context.Context, botId int64, lang string, t Texts) error {
	if lang == "" {
		if t.Description == nil {
			return nil
		}
		_, err := s.db.Exec(ctx, "UPDATE bots SET description = ? WHERE bot_id = ?", *t.Description, botId)
		return err
	}
	_, err := s.db.Exec(ctx, `INSERT INTO bot_info_texts (bot_id, lang_code, name, about, description) VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE name = COALESCE(VALUES(name), name), about = COALESCE(VALUES(about), about), description = COALESCE(VALUES(description), description)`,
		botId, lang, t.Name, t.About, t.Description)
	return err
}

// LangTexts returns the texts stored for a language other than the default
// one.
func (s *Store) LangTexts(ctx context.Context, botId int64, lang string) (Texts, error) {
	if lang == "" {
		return Texts{}, nil
	}
	var rows []struct {
		Name        *string `db:"name"`
		About       *string `db:"about"`
		Description *string `db:"description"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT name, about, description FROM bot_info_texts WHERE bot_id = ? AND lang_code = ?", botId, lang); err != nil || len(rows) == 0 {
		return Texts{}, err
	}
	return Texts{Name: rows[0].Name, About: rows[0].About, Description: rows[0].Description}, nil
}

// Description returns the description of the default language.
func (s *Store) Description(ctx context.Context, botId int64) (string, error) {
	var rows []string
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT description FROM bots WHERE bot_id = ?", botId); err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0], nil
}

// SetMenuButton sets the button of one user, or the default of all users
// with userId 0; botMenuButtonDefault removes the row, so a broader one
// applies again.
func (s *Store) SetMenuButton(ctx context.Context, botId, userId int64, button *mtproto.BotMenuButton) error {
	switch button.GetPredicateName() {
	case mtproto.Predicate_botMenuButtonDefault:
		_, err := s.db.Exec(ctx, "DELETE FROM bot_menu_buttons WHERE bot_id = ? AND user_id = ?", botId, userId)
		return err
	case mtproto.Predicate_botMenuButtonCommands:
		_, err := s.db.Exec(ctx,
			"INSERT INTO bot_menu_buttons (bot_id, user_id, kind, text, url) VALUES (?, ?, ?, '', '') ON DUPLICATE KEY UPDATE kind = VALUES(kind), text = '', url = ''",
			botId, userId, menuKindCommands)
		return err
	case mtproto.Predicate_botMenuButton:
		if strings.TrimSpace(button.GetText()) == "" || utf8.RuneCountInString(button.GetText()) > 64 {
			return mtproto.ErrButtonTextInvalid
		}
		if !httpsURL(button.GetUrl()) {
			return mtproto.ErrButtonUrlInvalid
		}
		_, err := s.db.Exec(ctx,
			"INSERT INTO bot_menu_buttons (bot_id, user_id, kind, text, url) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE kind = VALUES(kind), text = VALUES(text), url = VALUES(url)",
			botId, userId, menuKindWebApp, button.GetText(), button.GetUrl())
		return err
	}
	return errButtonInvalid
}

// MenuButton returns the button userId sees: its own, the default of all
// users, then botMenuButtonDefault.
func (s *Store) MenuButton(ctx context.Context, botId, userId int64) (*mtproto.BotMenuButton, error) {
	var rows []struct {
		UserId int64  `db:"user_id"`
		Kind   int32  `db:"kind"`
		Text   string `db:"text"`
		Url    string `db:"url"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, kind, text, url FROM bot_menu_buttons WHERE bot_id = ? AND user_id IN (?, ?) ORDER BY user_id DESC", botId, userId, defaultMenuUserId); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		r := rows[0]
		switch r.Kind {
		case menuKindCommands:
			return mtproto.MakeTLBotMenuButtonCommands(nil).To_BotMenuButton(), nil
		case menuKindWebApp:
			return mtproto.MakeTLBotMenuButton(&mtproto.BotMenuButton{Text: r.Text, Url: r.Url}).To_BotMenuButton(), nil
		}
	}
	return mtproto.MakeTLBotMenuButtonDefault(nil).To_BotMenuButton(), nil
}

// httpsURL tells a link a web app may open.
func httpsURL(u string) bool {
	return strings.HasPrefix(u, "https://") && len(u) > len("https://")
}

// BotInfo is the info of the bot as viewer sees it, for userFull.bot_info and
// the bot_info of a full chat (core.telegram.org/api/bots/commands).
func (s *Store) BotInfo(ctx context.Context, botId int64, v Viewer) (*mtproto.BotInfo, error) {
	description, err := s.Description(ctx, botId)
	if err != nil {
		return nil, err
	}
	texts, err := s.LangTexts(ctx, botId, v.Lang)
	if err != nil {
		return nil, err
	}
	if texts.Description != nil {
		description = *texts.Description
	}
	commands, err := s.resolveCommands(ctx, botId, v)
	if err != nil {
		return nil, err
	}
	menu, err := s.MenuButton(ctx, botId, v.UserId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLBotInfo(&mtproto.BotInfo{
		UserId_INT64:           botId,
		UserId_FLAGINT64:       mtproto.MakeFlagsInt64(botId),
		Description_STRING:     description,
		Description_FLAGSTRING: mtproto.MakeFlagsString(description),
		Commands:               commands,
		MenuButton:             menu,
	}).To_BotInfo(), nil
}
