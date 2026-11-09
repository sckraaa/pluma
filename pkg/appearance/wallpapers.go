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
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ErrNotOverridden is the answer to a revert when the other side did not
// choose the wallpaper of the chat for this user.
var ErrNotOverridden = errors.New("appearance: the chat wallpaper is not overridden")

// AddWallpaper records an uploaded wallpaper. forChat: it was uploaded for
// one chat and is not offered in the gallery of its creator.
func (s *Store) AddWallpaper(ctx context.Context, creatorID int64, forChat bool, w *mtproto.WallPaper) error {
	data, err := marshal(w)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO wallpapers (id, access_hash, slug, creator_id, for_chat, data, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		w.GetId(), w.GetAccessHash(), w.GetSlug(), creatorID, forChat, data, time.Now().Unix())
	return err
}

type wallpaperRow struct {
	CreatorID int64  `db:"creator_id"`
	Data      []byte `db:"data"`
}

// WallpaperByID returns an uploaded wallpaper and its creator, nil if there
// is none.
func (s *Store) WallpaperByID(ctx context.Context, id int64) (*mtproto.WallPaper, int64, error) {
	return s.wallpaper(ctx, "id = ?", id)
}

// WallpaperBySlug returns an uploaded wallpaper by its slug and its creator,
// nil if there is none.
func (s *Store) WallpaperBySlug(ctx context.Context, slug string) (*mtproto.WallPaper, int64, error) {
	return s.wallpaper(ctx, "slug = ?", slug)
}

func (s *Store) wallpaper(ctx context.Context, where string, arg any) (*mtproto.WallPaper, int64, error) {
	var rows []wallpaperRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT creator_id, data FROM wallpapers WHERE "+where, arg); err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, nil
	}
	w, err := unmarshalWallPaper(rows[0].Data)
	return w, rows[0].CreatorID, err
}

// SavedWallpaper is an entry of an account's wallpaper list.
type SavedWallpaper struct {
	WallpaperID int64 `db:"wallpaper_id"`
	// Removed: the account took a preinstalled wallpaper out of its list
	// (account.saveWallPaper with unsave).
	Removed bool `db:"removed"`
	// Data is the wallpaper with the account's settings; none when Removed.
	Data []byte `db:"data"`
}

// Wallpaper decodes the entry's wallpaper.
func (e SavedWallpaper) Wallpaper() (*mtproto.WallPaper, error) {
	return unmarshalWallPaper(e.Data)
}

// SavedWallpapers returns the entries of the account, the last saved first.
func (s *Store) SavedWallpapers(ctx context.Context, userID int64) ([]SavedWallpaper, error) {
	var rows []SavedWallpaper
	err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT wallpaper_id, removed, data FROM user_wallpapers WHERE user_id = ? ORDER BY order2 DESC, wallpaper_id DESC", userID)
	return rows, err
}

// SaveWallpaper puts w, carrying the account's settings, on top of the list.
// Saving it again moves it there and takes the new settings.
func (s *Store) SaveWallpaper(ctx context.Context, userID int64, w *mtproto.WallPaper) error {
	data, err := marshal(w)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO user_wallpapers (user_id, wallpaper_id, removed, data, order2) VALUES (?, ?, 0, ?, ?)"+
			" AS new ON DUPLICATE KEY UPDATE removed = 0, data = new.data, order2 = new.order2",
		userID, w.GetId(), data, time.Now().UnixMilli())
	return err
}

// UnsaveWallpaper removes the wallpaper from the list: a preinstalled one
// stays removed until the list is reset (core.telegram.org/api/wallpapers),
// so inCatalog leaves a row saying so; any other just loses its row.
func (s *Store) UnsaveWallpaper(ctx context.Context, userID, wallpaperID int64, inCatalog bool) error {
	var err error
	if inCatalog {
		_, err = s.db.Exec(ctx,
			"INSERT INTO user_wallpapers (user_id, wallpaper_id, removed, data, order2) VALUES (?, ?, 1, NULL, 0)"+
				" AS new ON DUPLICATE KEY UPDATE removed = 1, data = NULL",
			userID, wallpaperID)
	} else {
		_, err = s.db.Exec(ctx, "DELETE FROM user_wallpapers WHERE user_id = ? AND wallpaper_id = ?", userID, wallpaperID)
	}
	return err
}

// ResetWallpapers gives the account the default list back: what it saved
// is gone and what it removed comes back.
func (s *Store) ResetWallpapers(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "DELETE FROM user_wallpapers WHERE user_id = ?", userID)
	return err
}

type chatWallpaperRow struct {
	Data       []byte `db:"data"`
	Overridden bool   `db:"overridden"`
	PrevData   []byte `db:"prev_data"`
}

// GetChatWallpaper is the wallpaper of userID's chat with peerID and whether
// the other side chose it for the user (userFull.wallpaper_overridden); nil
// if the chat has none.
func (s *Store) GetChatWallpaper(ctx context.Context, userID, peerID int64) (*mtproto.WallPaper, bool, error) {
	var rows []chatWallpaperRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT data, overridden, prev_data FROM chat_wallpapers WHERE user_id = ? AND peer_id = ?", userID, peerID); err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	w, err := unmarshalWallPaper(rows[0].Data)
	return w, rows[0].Overridden, err
}

// PeerWallpaperUpdate is updatePeerWallpaper of the chat with peer; nil w
// says the chat has no wallpaper any more (Android processUpdate, tdesktop
// api_updates.cpp mtpc_updatePeerWallpaper).
func PeerWallpaperUpdate(peer *mtproto.Peer, w *mtproto.WallPaper, overridden bool) *mtproto.Update {
	return mtproto.MakeTLUpdatePeerWallpaper(&mtproto.Update{
		Peer_PEER:           peer,
		Wallpaper:           w,
		WallpaperOverridden: overridden,
	}).To_Update()
}

// run is one change of the chat wallpaper in the account's box, the pushes
// it emits go out after.
func (s *Store) run(ctx context.Context, userID int64, fn func(b *userbox.Box) error) error {
	res, err := userbox.Run(ctx, s.boxDB, userID, fn)
	if err != nil {
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return nil
}

// SetChatWallpaper gives userID's chat with peerID the wallpaper w, nil for
// none, as the user's own choice: what the other side chose for it is
// forgotten. All the devices of the account are told, the one that asked
// too: Android ignores the answer to a request that clears
// (ChatThemeController.clearWallpaper).
func (s *Store) SetChatWallpaper(ctx context.Context, userID, peerID int64, w *mtproto.WallPaper) error {
	data, err := marshal(nonNil(w))
	if err != nil {
		return err
	}
	if w == nil {
		data = nil
	}
	return s.run(ctx, userID, func(b *userbox.Box) error {
		if _, err := b.Tx().Exec(
			"INSERT INTO chat_wallpapers (user_id, peer_id, data, overridden, prev_data, updated_at) VALUES (?, ?, ?, 0, NULL, ?)"+
				" AS new ON DUPLICATE KEY UPDATE data = new.data, overridden = 0, prev_data = NULL, updated_at = new.updated_at",
			userID, peerID, data, b.Date()); err != nil {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, PeerWallpaperUpdate(mtproto.MakePeerUser(peerID), w, false), nil, nil)
	})
}

// OverrideChatWallpaper is the other side's for_both: userID's chat with
// peerID takes w and remembers what the user had, for a revert. A second
// override keeps the first memory.
func (s *Store) OverrideChatWallpaper(ctx context.Context, userID, peerID int64, w *mtproto.WallPaper) error {
	data, err := marshal(w)
	if err != nil {
		return err
	}
	return s.run(ctx, userID, func(b *userbox.Box) error {
		var rows []chatWallpaperRow
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT data, overridden, prev_data FROM chat_wallpapers WHERE user_id = ? AND peer_id = ?", userID, peerID); err != nil {
			return err
		}
		var prev []byte
		if len(rows) > 0 {
			if rows[0].Overridden {
				prev = rows[0].PrevData
			} else {
				prev = rows[0].Data
			}
		}
		if _, err := b.Tx().Exec(
			"INSERT INTO chat_wallpapers (user_id, peer_id, data, overridden, prev_data, updated_at) VALUES (?, ?, ?, 1, ?, ?)"+
				" AS new ON DUPLICATE KEY UPDATE data = new.data, overridden = 1, prev_data = new.prev_data, updated_at = new.updated_at",
			userID, peerID, data, prev, b.Date()); err != nil {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, PeerWallpaperUpdate(mtproto.MakePeerUser(peerID), w, true), nil, nil)
	})
}

// RevertChatWallpaper brings back what userID had in the chat before the
// other side overrode it (revert of messages.setChatWallPaper) and returns
// it, nil if the chat had no wallpaper. ErrNotOverridden if the wallpaper was
// not chosen by the other side.
func (s *Store) RevertChatWallpaper(ctx context.Context, userID, peerID int64) (*mtproto.WallPaper, error) {
	var w *mtproto.WallPaper
	err := s.run(ctx, userID, func(b *userbox.Box) error {
		var rows []chatWallpaperRow
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT data, overridden, prev_data FROM chat_wallpapers WHERE user_id = ? AND peer_id = ?", userID, peerID); err != nil {
			return err
		}
		if len(rows) == 0 || !rows[0].Overridden {
			return ErrNotOverridden
		}
		var err error
		if w, err = unmarshalWallPaper(rows[0].PrevData); err != nil {
			return err
		}
		if _, err := b.Tx().Exec(
			"UPDATE chat_wallpapers SET data = ?, overridden = 0, prev_data = NULL, updated_at = ? WHERE user_id = ? AND peer_id = ?",
			rows[0].PrevData, b.Date(), userID, peerID); err != nil {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, PeerWallpaperUpdate(mtproto.MakePeerUser(peerID), w, false), nil, nil)
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// nonNil gives marshal a value for a nil wallpaper; the caller stores NULL.
func nonNil(w *mtproto.WallPaper) *mtproto.WallPaper {
	if w == nil {
		return &mtproto.WallPaper{}
	}
	return w
}

// ChannelLook is what getFullChannel shows of the channel's look: its
// wallpaper and the emoticon of its chat theme.
func (s *Store) ChannelLook(ctx context.Context, channelID int64) (*mtproto.WallPaper, string, error) {
	var rows []struct {
		Wallpaper     []byte `db:"wallpaper"`
		ThemeEmoticon string `db:"theme_emoticon"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT wallpaper, theme_emoticon FROM channels WHERE id = ?", channelID); err != nil {
		return nil, "", fmt.Errorf("appearance: the look of channel %d: %w", channelID, err)
	}
	if len(rows) == 0 {
		return nil, "", nil
	}
	w, err := unmarshalWallPaper(rows[0].Wallpaper)
	return w, rows[0].ThemeEmoticon, err
}
