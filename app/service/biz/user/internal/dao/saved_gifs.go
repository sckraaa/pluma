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

package dao

import (
	"context"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The most saved GIFs an account keeps: help.getAppConfig gives the same
// numbers (saved_gifs_limit_default, saved_gifs_limit_premium), and the
// clients trim their lists to them. A GIF saved over the limit drops the
// oldest.
const (
	SavedGifsLimit        = 200
	SavedGifsLimitPremium = 200
)

// SavedGifIdList returns the ids of userId's saved GIFs, the newest first.
func (d *Dao) SavedGifIdList(ctx context.Context, userId int64) ([]int64, error) {
	var ids []int64
	err := d.DB.QueryRowsPartial(ctx, &ids, "SELECT document_id FROM user_saved_gifs WHERE user_id = ? ORDER BY order2 DESC", userId)
	return ids, err
}

type savedGifRow struct {
	Id    int64 `db:"document_id"`
	Order int64 `db:"order2"`
}

// SaveGif puts the GIF id at the top of userId's saved GIFs, or takes it off
// with unsave, in one box transaction of the account; every device of the
// account gets updateSavedGifs with seq (ADR 0020) and reloads the list. A
// GIF that is already at the top, or not saved when unsaved, changes
// nothing and nothing is pushed. It returns whether the list changed.
func (d *Dao) SaveGif(ctx context.Context, userId, id int64, unsave bool) (bool, error) {
	var changed bool
	_, err := d.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
		changed = false
		var rows []savedGifRow
		if err := b.Tx().QueryRowsPartial(&rows, "SELECT document_id, order2 FROM user_saved_gifs WHERE user_id = ? ORDER BY order2 DESC", userId); err != nil {
			return err
		}
		at := slices.IndexFunc(rows, func(r savedGifRow) bool { return r.Id == id })
		var drop []int64
		switch {
		case unsave && at < 0, !unsave && at == 0:
			return nil
		case unsave:
			drop = []int64{id}
		default:
			limit, err := savedGifsLimit(b)
			if err != nil {
				return err
			}
			order := int64(1)
			if len(rows) > 0 {
				order = rows[0].Order + 1
			}
			if _, err = b.Tx().Exec("INSERT INTO user_saved_gifs (user_id, document_id, order2) VALUES (?, ?, ?) AS new ON DUPLICATE KEY UPDATE order2 = new.order2",
				userId, id, order); err != nil {
				return err
			}
			// The list is now id, then the other rows by order: those past
			// the limit go.
			if at >= 0 {
				rows = slices.Delete(rows, at, at+1)
			}
			for i := limit - 1; i < len(rows); i++ {
				drop = append(drop, rows[i].Id)
			}
		}
		for _, gif := range drop {
			if _, err := b.Tx().Exec("DELETE FROM user_saved_gifs WHERE user_id = ? AND document_id = ?", userId, gif); err != nil {
				return err
			}
		}
		changed = true
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateSavedGifs(nil).To_Update(), nil, nil)
	})
	return changed, err
}

// savedGifsLimit is the saved GIFs limit of b's account, by its Premium.
func savedGifsLimit(b *userbox.Box) (int, error) {
	var rows []struct {
		Premium bool  `db:"premium"`
		Until   int64 `db:"premium_expire_date"`
	}
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT premium, premium_expire_date FROM users WHERE id = ?", b.UserId()); err != nil {
		return 0, err
	}
	if len(rows) == 1 && (PremiumState{Premium: rows[0].Premium, Until: rows[0].Until}).Active(b.Date()) {
		return SavedGifsLimitPremium, nil
	}
	return SavedGifsLimit, nil
}
