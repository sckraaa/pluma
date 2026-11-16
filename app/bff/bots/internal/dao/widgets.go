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
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The requests of widgets to their bots (messages.getBotWidgetQueryAnswer,
// task 106) live in Redis as the presses of buttons do: the owner of a
// request while the user waits, then the bot's answer. Telegram's numbers
// (Android jni/twidget/widget-sandbox/docs/tgw-format.md, "The widget's
// state and requests"): the server waits for the bot 15 s, a user sends a
// bot at most 5 requests per 5 s, and a bot's object takes one edit a
// second.
const (
	// WidgetAnswerWait is how long a widget's request waits for the bot:
	// under the 30 s of the session for a pending request, and the clients
	// give up only at twice the server's wait (tdesktop tgw/tgw_bridge.cpp
	// kRequestTimeout, Android utils/twidget/WidgetMedia.java
	// REQUEST_LIMIT_MS).
	WidgetAnswerWait = 15 * time.Second
	// WidgetQueriesPer and widgetQueryWindow bound the requests of a user
	// to a bot.
	WidgetQueriesPer  = 5
	widgetQueryWindow = 5
	// widgetEditWindow is the time between two edits of an object.
	widgetEditWindow = 1
)

// WidgetAnswer is the answer of a bot to a widget's request: its result,
// if any ("none is an empty answer").
type WidgetAnswer struct {
	Result *string `json:"result,omitempty"`
}

// WidgetRef is what a widget result keeps for its send: whether the
// widget may ask its bot (allow_queries), becomes an object (shared_widget)
// or refers to one.
type WidgetRef struct {
	AllowQueries bool  `json:"allow_queries,omitempty"`
	Shared       bool  `json:"shared,omitempty"`
	WidgetId     int64 `json:"widget_id,omitempty"`
}

func widgetOwnerKey(q int64) string  { return "bot_widget_query:" + strconv.FormatInt(q, 10) }
func widgetAnswerKey(q int64) string { return "bot_widget_answer:" + strconv.FormatInt(q, 10) }

// BeginWidgetQuery records a request of a widget of botId.
func (d *Dao) BeginWidgetQuery(ctx context.Context, queryId, botId int64) error {
	return d.KV.SetexCtx(ctx, widgetOwnerKey(queryId), strconv.FormatInt(botId, 10), queryTTL)
}

// SetWidgetAnswer stores the bot's answer: false when the request is not
// one of this bot's, or expired.
func (d *Dao) SetWidgetAnswer(ctx context.Context, queryId, botId int64, a *WidgetAnswer) (bool, error) {
	s, ok, err := ownerOf(d.KV.GetCtx(ctx, widgetOwnerKey(queryId)))
	if err != nil || !ok || s != strconv.FormatInt(botId, 10) {
		return false, err
	}
	data, err := jsonx.Marshal(a)
	if err != nil {
		return false, err
	}
	return true, d.KV.SetexCtx(ctx, widgetAnswerKey(queryId), string(data), queryTTL)
}

// WaitWidgetAnswer waits for the answer, up to WidgetAnswerWait; nil when
// the bot did not answer in time.
func (d *Dao) WaitWidgetAnswer(ctx context.Context, queryId int64) (*WidgetAnswer, error) {
	return waitFor(ctx, WidgetAnswerWait, func(ctx context.Context) (*WidgetAnswer, error) {
		return decode[WidgetAnswer](d.KV.GetCtx(ctx, widgetAnswerKey(queryId)))
	})
}

// AllowWidgetQuery counts a request of userId to botId: 0 while the user
// has sent fewer than WidgetQueriesPer in the window, else the seconds to
// wait (FLOOD_WAIT).
func (d *Dao) AllowWidgetQuery(ctx context.Context, userId, botId int64) (int, error) {
	key := "bot_widget_rate:" + strconv.FormatInt(userId, 10) + ":" + strconv.FormatInt(botId, 10)
	n, err := d.KV.IncrCtx(ctx, key)
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err = d.KV.ExpireCtx(ctx, key, widgetQueryWindow); err != nil {
			return 0, err
		}
	}
	if n <= WidgetQueriesPer {
		return 0, nil
	}
	ttl, err := d.KV.TtlCtx(ctx, key)
	if err != nil {
		return 0, err
	}
	if ttl < 0 {
		// The window lost its expiry (a crash between the two calls): it
		// starts again.
		if err = d.KV.ExpireCtx(ctx, key, widgetQueryWindow); err != nil {
			return 0, err
		}
		ttl = widgetQueryWindow
	}
	return max(ttl, 1), nil
}

// AllowWidgetEdit takes the one edit of an object in its second: false
// when the object was edited in it.
func (d *Dao) AllowWidgetEdit(ctx context.Context, widgetId int64) (bool, error) {
	return d.KV.SetnxExCtx(ctx, "bot_widget_edit:"+strconv.FormatInt(widgetId, 10), "1", widgetEditWindow)
}
