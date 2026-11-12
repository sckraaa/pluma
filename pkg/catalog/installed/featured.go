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

package installed

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// FeaturedRead returns the trending sets the account has seen
// (user_featured_sticker_reads, task 084).
func (s *Store) FeaturedRead(ctx context.Context, userID int64) (map[int64]bool, error) {
	var rows []struct {
		SetID int64 `db:"set_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT set_id FROM user_featured_sticker_reads WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(rows))
	for _, r := range rows {
		out[r.SetID] = true
	}
	return out, nil
}

// ReadFeatured marks trending sets as seen by the account: stickers are
// sticker sets, emojis custom emoji sets. The account's other devices get
// updateReadFeaturedStickers or updateReadFeaturedEmojiStickers when a set
// of the kind was not seen before; the device that read them (the
// authorization on exceptAuthKeyID) has them read already. Marking again
// changes and pushes nothing.
func (s *Store) ReadFeatured(ctx context.Context, userID, exceptAuthKeyID int64, stickers, emojis []int64) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		for _, part := range []struct {
			ids    []int64
			update *mtproto.Update
		}{
			{stickers, mtproto.MakeTLUpdateReadFeaturedStickers(nil).To_Update()},
			{emojis, mtproto.MakeTLUpdateReadFeaturedEmojiStickers(nil).To_Update()},
		} {
			marked := int64(0)
			for _, id := range part.ids {
				res, err := b.Tx().Exec("INSERT IGNORE INTO user_featured_sticker_reads (user_id, set_id) VALUES (?, ?)", userID, id)
				if err != nil {
					return err
				}
				if n, err := res.RowsAffected(); err == nil {
					marked += n
				}
			}
			if marked == 0 {
				continue
			}
			if err := sync_client.EmitSeqUpdateExcept(b, s.topic, part.update, nil, nil, exceptAuthKeyID); err != nil {
				return err
			}
		}
		return nil
	})
}
