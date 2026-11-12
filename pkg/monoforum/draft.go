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

package monoforum

import (
	"context"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The draft of an admin in a sublist (messages.saveDraft with
// reply_to.monoforum_peer_id; monoForumDialog.draft): each admin has its
// own, as each account has its drafts. A subscriber's draft in the
// monoforum is the draft of its dialog, as in any chat.

// DraftOf is userId's draft in the sublist peerId of the monoforum, nil for
// none.
func DraftOf(q Querier, userId, monoforumId, peerId int64) (*mtproto.DraftMessage, error) {
	var rows []string
	if err := q.QueryRowsPartial(&rows,
		"SELECT draft_message_data FROM channel_monoforum_drafts WHERE user_id = ? AND channel_id = ? AND peer_id = ?",
		userId, monoforumId, peerId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	draft := new(mtproto.DraftMessage)
	if err := jsonx.UnmarshalFromString(rows[0], draft); err != nil {
		return nil, fmt.Errorf("monoforum: draft of %d in sublist %d of %d: %w", userId, peerId, monoforumId, err)
	}
	return draft, nil
}

// SublistDraft is a draft with the sublist it is in.
type SublistDraft struct {
	MonoforumId int64
	PeerId      int64
	Draft       *mtproto.DraftMessage
}

// Drafts are every draft userId has in sublists, the newest first.
func Drafts(q Querier, userId int64) ([]SublistDraft, error) {
	var rows []struct {
		ChannelId int64  `db:"channel_id"`
		PeerId    int64  `db:"peer_id"`
		Data      string `db:"draft_message_data"`
	}
	if err := q.QueryRowsPartial(&rows,
		"SELECT channel_id, peer_id, draft_message_data FROM channel_monoforum_drafts WHERE user_id = ? ORDER BY date2 DESC", userId); err != nil {
		return nil, err
	}
	out := make([]SublistDraft, 0, len(rows))
	for _, r := range rows {
		draft := new(mtproto.DraftMessage)
		if err := jsonx.UnmarshalFromString(r.Data, draft); err != nil {
			return nil, fmt.Errorf("monoforum: draft of %d in sublist %d of %d: %w", userId, r.PeerId, r.ChannelId, err)
		}
		out = append(out, SublistDraft{MonoforumId: r.ChannelId, PeerId: r.PeerId, Draft: draft})
	}
	return out, nil
}

// SaveDraft keeps userId's draft in the sublist peerId; nil clears it. It
// tells whether anything changed: clearing none is no change.
func SaveDraft(ctx context.Context, db *sqlx.DB, userId, monoforumId, peerId int64, draft *mtproto.DraftMessage, date int64) (bool, error) {
	if draft == nil {
		res, err := db.Exec(ctx, "DELETE FROM channel_monoforum_drafts WHERE user_id = ? AND channel_id = ? AND peer_id = ?", userId, monoforumId, peerId)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n > 0, err
	}
	data, err := jsonx.MarshalToString(draft)
	if err != nil {
		return false, err
	}
	_, err = db.Exec(ctx, "INSERT INTO channel_monoforum_drafts (user_id, channel_id, peer_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE draft_message_data = VALUES(draft_message_data), date2 = VALUES(date2)",
		userId, monoforumId, peerId, data, date)
	return err == nil, err
}

// ClearDrafts drops userId's drafts in every sublist and returns the
// sublists that had one.
func ClearDrafts(ctx context.Context, db *sqlx.DB, userId int64) ([]SublistDraft, error) {
	had, err := Drafts(OnDB(ctx, db), userId)
	if err != nil || len(had) == 0 {
		return nil, err
	}
	if _, err = db.Exec(ctx, "DELETE FROM channel_monoforum_drafts WHERE user_id = ?", userId); err != nil {
		return nil, err
	}
	return had, nil
}
