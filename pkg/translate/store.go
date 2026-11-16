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

package translate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Store keeps what translation needs to outlive a process, in the tables
// of migrate-20261119-translation.sql: the cache of translations
// (cache.go) and the chats whose translation bar their reader hid
// (peer_translation_settings). The queue of the engine is not here: it
// lives in memory (scheduler), and a restart loses only work that the
// clients ask for again (ADR 0064, section 6).
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns the store in db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

// sqlPlaceholders returns n comma-separated "?" for an IN list.
func sqlPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// TranslationPeer is a chat of peer_translation_settings: Type is
// mtproto.PEER_USER (saved messages are the user's own id), PEER_CHAT or
// PEER_CHANNEL.
type TranslationPeer struct {
	Type int32
	Id   int64
}

func (p TranslationPeer) check() error {
	switch p.Type {
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
	default:
		return fmt.Errorf("translate: peer type %d is not a user, a chat or a channel", p.Type)
	}
	if p.Id <= 0 {
		return fmt.Errorf("translate: peer id %d", p.Id)
	}
	return nil
}

// SetTranslationsDisabled records messages.togglePeerTranslations of
// userId for peer: disabled hides the translation bar of the chat for
// him. Only hidden bars have a row; showing the bar again deletes it, so
// "no row" is the one way to say "shown". Writing the same value again
// changes nothing but updated_at.
func (s *Store) SetTranslationsDisabled(ctx context.Context, userId int64, peer TranslationPeer, disabled bool) error {
	if err := peer.check(); err != nil {
		return err
	}
	var err error
	if disabled {
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO peer_translation_settings (user_id, peer_type, peer_id, disabled, updated_at) VALUES (?, ?, ?, 1, ?)"+
				" ON DUPLICATE KEY UPDATE disabled = 1, updated_at = VALUES(updated_at)",
			userId, peer.Type, peer.Id, s.now().UnixMilli())
	} else {
		_, err = s.db.ExecContext(ctx,
			"DELETE FROM peer_translation_settings WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
			userId, peer.Type, peer.Id)
	}
	if err != nil {
		return fmt.Errorf("translate: set translations of %d for %d/%d: %w", userId, peer.Type, peer.Id, err)
	}
	return nil
}

// TranslationsDisabled returns which of peers userId hid the translation
// bar of: translations_disabled of userFull, chatFull and channelFull. A
// peer that is not in the map is not hidden.
func (s *Store) TranslationsDisabled(ctx context.Context, userId int64, peers ...TranslationPeer) (map[TranslationPeer]bool, error) {
	disabled := make(map[TranslationPeer]bool)
	if len(peers) == 0 {
		return disabled, nil
	}
	conds := make([]string, 0, len(peers))
	args := make([]any, 0, 1+2*len(peers))
	args = append(args, userId)
	for _, p := range peers {
		if err := p.check(); err != nil {
			return nil, err
		}
		conds = append(conds, "(peer_type = ? AND peer_id = ?)")
		args = append(args, p.Type, p.Id)
	}
	query := "SELECT peer_type, peer_id FROM peer_translation_settings WHERE user_id = ? AND disabled = 1 AND (" + strings.Join(conds, " OR ") + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("translate: translations of %d: %w", userId, err)
	}
	defer rows.Close()
	for rows.Next() {
		var p TranslationPeer
		if err = rows.Scan(&p.Type, &p.Id); err != nil {
			return nil, fmt.Errorf("translate: translations of %d: %w", userId, err)
		}
		disabled[p] = true
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("translate: translations of %d: %w", userId, err)
	}
	return disabled, nil
}
