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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// Shared widgets (bot_widgets, ADR 0068 p. 8): a widget a bot sends with
// inputMediaWidget(shared_widget) once and then by its id
// (inputMediaWidgetId); its public data is one for every message showing
// it (messages.getWidgetPublicData, messages.editWidgetData). Telegram does
// not document widgets: the limits are the server's (task 106).

// Limits of a widget.
const (
	// MaxWidgetHTML is the HTML of a widget before compression.
	MaxWidgetHTML = 64 << 10
	// MaxWidgetPayload bounds the stored zlib form; zlib grows data that
	// does not compress by a few bytes per 16 KiB block only.
	MaxWidgetPayload = MaxWidgetHTML + 1024
	// MaxWidgetData is the public data and the chat data.
	MaxWidgetData = 4 << 10
	// MaxWidgetSide bounds the width and the height, the size anchors of
	// the widget in CSS px; 0 is "auto" (Android
	// jni/twidget/widget-sandbox/docs/tgw-format.md, "Size anchors").
	MaxWidgetSide = 4096
	// The period clients wait between reads of the public data
	// (widgetPublicData.period): 30 s unless the bot sets it, as Android
	// assumes, with the same bounds (utils/twidget/WidgetMedia.java:1020).
	DefaultWidgetPeriod = 30
	MinWidgetPeriod     = 5
	MaxWidgetPeriod     = 3600
)

// ErrNoWidget is a widget id that is not the bot's.
var ErrNoWidget = errors.New("botstore: no such widget")

// Widget is a shared widget.
type Widget struct {
	Id           int64  `db:"widget_id"`
	BotId        int64  `db:"bot_id"`
	Payload      []byte `db:"payload"`
	Width        int32  `db:"width"`
	Height       int32  `db:"height"`
	AllowQueries bool   `db:"allow_queries"`
	PublicData   string `db:"public_data"`
	PublicHash   int64  `db:"public_hash"`
	Period       int32  `db:"period"`
	UpdatedAt    int32  `db:"updated_at"`
}

const widgetColumns = "widget_id, bot_id, payload, width, height, allow_queries, public_data, public_hash, period, updated_at"

// ClampWidgetPeriod is the period of a cache_time a bot gives: the default
// for none (0 or less), as Android reads a period it was not given
// (utils/twidget/WidgetMedia.java:1088), else cacheTime within the bounds.
func ClampWidgetPeriod(cacheTime int32) int32 {
	if cacheTime <= 0 {
		return DefaultWidgetPeriod
	}
	return min(max(cacheTime, MinWidgetPeriod), MaxWidgetPeriod)
}

// WidgetDataHash is the hash of public data, the widgetPublicData.hash a
// client sends back to learn whether the data changed: the first 8 bytes of
// its SHA-256, never 0, which a client with no data may send.
func WidgetDataHash(data string) int64 {
	sum := sha256.Sum256([]byte(data))
	h := int64(binary.BigEndian.Uint64(sum[:8]))
	if h == 0 {
		h = 1
	}
	return h
}

func validWidgetData(data string) error {
	if len(data) > MaxWidgetData {
		return mtproto.ErrDataTooLong
	}
	return nil
}

func validWidgetPeriod(period int32) error {
	if period < MinWidgetPeriod || period > MaxWidgetPeriod {
		return fmt.Errorf("botstore: widget period %d out of %d..%d", period, MinWidgetPeriod, MaxWidgetPeriod)
	}
	return nil
}

// CreateWidget stores a new shared widget of w.BotId with a random id and
// returns it as stored. A payload that is empty or over MaxWidgetPayload
// and a side out of 0..MaxWidgetSide are MEDIA_INVALID, public data over
// MaxWidgetData DATA_TOO_LONG.
func (s *Store) CreateWidget(ctx context.Context, w Widget, now int32) (Widget, error) {
	if len(w.Payload) == 0 || len(w.Payload) > MaxWidgetPayload ||
		w.Width < 0 || w.Width > MaxWidgetSide || w.Height < 0 || w.Height > MaxWidgetSide {
		return Widget{}, mtproto.ErrMediaInvalid
	}
	if err := validWidgetData(w.PublicData); err != nil {
		return Widget{}, err
	}
	if w.Period == 0 {
		w.Period = DefaultWidgetPeriod
	}
	if err := validWidgetPeriod(w.Period); err != nil {
		return Widget{}, err
	}
	w.PublicHash = WidgetDataHash(w.PublicData)
	w.UpdatedAt = now
	for range randomIdAttempts {
		id, err := randomId63()
		if err != nil {
			return Widget{}, err
		}
		w.Id = id
		_, err = s.db.Exec(ctx, "INSERT INTO bot_widgets ("+widgetColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			w.Id, w.BotId, w.Payload, w.Width, w.Height, w.AllowQueries, w.PublicData, w.PublicHash, w.Period, w.UpdatedAt)
		if sqlx.IsDuplicate(err) {
			continue
		}
		if err != nil {
			return Widget{}, err
		}
		return w, nil
	}
	return Widget{}, fmt.Errorf("botstore: no free widget id after %d attempts", randomIdAttempts)
}

// WidgetOf returns the widget of the id, nil for none.
func (s *Store) WidgetOf(ctx context.Context, id int64) (*Widget, error) {
	var rows []Widget
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+widgetColumns+" FROM bot_widgets WHERE widget_id = ?", id); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// BotWidget returns the bot's widget of the id (inputMediaWidgetId sends
// only one's own), ErrNoWidget for none or another bot's.
func (s *Store) BotWidget(ctx context.Context, botId, id int64) (*Widget, error) {
	w, err := s.WidgetOf(ctx, id)
	if err != nil {
		return nil, err
	}
	if w == nil || w.BotId != botId {
		return nil, ErrNoWidget
	}
	return w, nil
}

// SetWidgetPublicData replaces the public data and the period of the bot's
// widget and returns the widget as stored; ErrNoWidget for a widget that
// is not the bot's, DATA_TOO_LONG for data over MaxWidgetData.
func (s *Store) SetWidgetPublicData(ctx context.Context, botId, id int64, data string, period int32, now int32) (Widget, error) {
	if err := validWidgetData(data); err != nil {
		return Widget{}, err
	}
	if err := validWidgetPeriod(period); err != nil {
		return Widget{}, err
	}
	var out Widget
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.Exec("UPDATE bot_widgets SET public_data = ?, public_hash = ?, period = ?, updated_at = ? WHERE widget_id = ? AND bot_id = ?",
			data, WidgetDataHash(data), period, now, id, botId)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			// 0 is also an update that changed nothing: the same data and
			// period in the same second.
			var rows []int64
			if err := tx.QueryRowsPartial(&rows, "SELECT widget_id FROM bot_widgets WHERE widget_id = ? AND bot_id = ?", id, botId); err != nil {
				return err
			}
			if len(rows) == 0 {
				return ErrNoWidget
			}
		}
		return tx.QueryRowPartial(&out, "SELECT "+widgetColumns+" FROM bot_widgets WHERE widget_id = ?", id)
	})
	return out, err
}
