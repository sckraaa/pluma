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
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// A query in flight lives in Redis: its owner while the user waits, and its
// answer, which stays for the cache and for messages.sendInlineBotResult.
// Nothing here has to outlive a restart: a press or a query that is lost is
// BOT_RESPONSE_TIMEOUT.
const (
	// AnswerWait is how long a user's request waits for the bot. Telegram
	// does not say (core.telegram.org/api/bots/buttons: bots must reply
	// "as quickly as possible", clients handle BOT_RESPONSE_TIMEOUT); ten
	// seconds stays under the session's 30 s for a pending request
	// (app/interface/session, session_waiting_result_queue.go).
	AnswerWait = 10 * time.Second
	// answerPoll is how often the waiting request looks for the answer.
	answerPoll = 100 * time.Millisecond
	// queryTTL is how long an unanswered query can be answered.
	queryTTL = 60
	// inlineKeep is how long inline results stay for a send at least,
	// whatever their cache_time: the user may send a result long after the
	// list appeared.
	inlineKeep = 3600
	// inlineKeepMax bounds a large cache_time from a bot.
	inlineKeepMax = 86400
)

// CallbackAnswer is the answer of a bot to a button.
type CallbackAnswer struct {
	Alert     bool   `json:"alert,omitempty"`
	Message   string `json:"message,omitempty"`
	Url       string `json:"url,omitempty"`
	CacheTime int32  `json:"cache_time,omitempty"`
}

// InlineAnswer is the answer of a bot to an inline query.
type InlineAnswer struct {
	Results       []*mtproto.BotInlineResult `json:"results"`
	Gallery       bool                       `json:"gallery,omitempty"`
	NextOffset    string                     `json:"next_offset,omitempty"`
	SwitchPm      *mtproto.InlineBotSwitchPM `json:"switch_pm,omitempty"`
	SwitchWebview *mtproto.InlineBotWebView  `json:"switch_webview,omitempty"`
	CacheTime     int32                      `json:"cache_time,omitempty"`
	Private       bool                       `json:"private,omitempty"`
	Bot           int64                      `json:"bot"`
}

// inlineOwner is who asked an inline query and of which bot.
type inlineOwner struct {
	Bot  int64 `json:"b"`
	User int64 `json:"u"`
}

// NewQueryId returns the id of a new press or inline query.
func NewQueryId() (int64, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0, fmt.Errorf("bots: read random: %w", err)
	}
	return int64(binary.BigEndian.Uint64(b) >> 1), nil
}

func callbackOwnerKey(q int64) string  { return "bot_callback:" + strconv.FormatInt(q, 10) }
func callbackAnswerKey(q int64) string { return "bot_callback_answer:" + strconv.FormatInt(q, 10) }
func inlineOwnerKey(q int64) string    { return "bot_inline:" + strconv.FormatInt(q, 10) }
func inlineAnswerKey(q int64) string   { return "bot_inline_answer:" + strconv.FormatInt(q, 10) }

// ownerOf reads a query's owner; zero with no error when it is gone.
func ownerOf(s string, err error) (string, bool, error) {
	if errors.Is(err, redis.Nil) || err == nil && s == "" {
		return "", false, nil
	}
	return s, err == nil, err
}

// BeginCallback records a press of a button of botId.
func (d *Dao) BeginCallback(ctx context.Context, queryId, botId int64) error {
	return d.KV.SetexCtx(ctx, callbackOwnerKey(queryId), strconv.FormatInt(botId, 10), queryTTL)
}

// SetCallbackAnswer stores the bot's answer: false when the query is not one
// of this bot's, or expired.
func (d *Dao) SetCallbackAnswer(ctx context.Context, queryId, botId int64, a *CallbackAnswer) (bool, error) {
	s, ok, err := ownerOf(d.KV.GetCtx(ctx, callbackOwnerKey(queryId)))
	if err != nil || !ok || s != strconv.FormatInt(botId, 10) {
		return false, err
	}
	data, err := jsonx.Marshal(a)
	if err != nil {
		return false, err
	}
	return true, d.KV.SetexCtx(ctx, callbackAnswerKey(queryId), string(data), queryTTL)
}

// WaitCallbackAnswer waits for the answer, up to AnswerWait; nil when the
// bot did not answer in time.
func (d *Dao) WaitCallbackAnswer(ctx context.Context, queryId int64) (*CallbackAnswer, error) {
	return wait(ctx, func(ctx context.Context) (*CallbackAnswer, error) {
		return decode[CallbackAnswer](d.KV.GetCtx(ctx, callbackAnswerKey(queryId)))
	})
}

// CachedCallback returns the answer cached for a press, nil when there is
// none (core.telegram.org/constructor/messages.botCallbackAnswer cache_time).
func (d *Dao) CachedCallback(ctx context.Context, key string) (*CallbackAnswer, error) {
	return decode[CallbackAnswer](d.KV.GetCtx(ctx, key))
}

// CacheCallback keeps the answer for its cache_time, when it has one.
func (d *Dao) CacheCallback(ctx context.Context, key string, a *CallbackAnswer) error {
	if a.CacheTime <= 0 {
		return nil
	}
	data, err := jsonx.Marshal(a)
	if err != nil {
		return err
	}
	return d.KV.SetexCtx(ctx, key, string(data), int(a.CacheTime))
}

// CallbackCacheKey is the key of the cached answer to userId's press of the
// button with data on message msgId of the chat.
func CallbackCacheKey(userId int64, peerType int32, peerId int64, msgId int32, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("bot_callback_cache:%d:%d:%d:%d:%s", userId, peerType, peerId, msgId, hex.EncodeToString(sum[:16]))
}

// BeginInline records an inline query of userId to botId.
func (d *Dao) BeginInline(ctx context.Context, queryId, botId, userId int64) error {
	data, err := jsonx.Marshal(&inlineOwner{Bot: botId, User: userId})
	if err != nil {
		return err
	}
	return d.KV.SetexCtx(ctx, inlineOwnerKey(queryId), string(data), queryTTL)
}

// SetInlineAnswer stores the bot's answer: false when the query is not one
// of this bot's, or expired; it also stays for messages.sendInlineBotResult.
func (d *Dao) SetInlineAnswer(ctx context.Context, queryId, botId int64, a *InlineAnswer) (bool, error) {
	s, ok, err := ownerOf(d.KV.GetCtx(ctx, inlineOwnerKey(queryId)))
	if err != nil || !ok {
		return false, err
	}
	var owner inlineOwner
	if err = jsonx.UnmarshalFromString(s, &owner); err != nil {
		return false, err
	}
	if owner.Bot != botId {
		return false, nil
	}
	data, err := jsonx.Marshal(a)
	if err != nil {
		return false, err
	}
	return true, d.KV.SetexCtx(ctx, inlineAnswerKey(queryId), string(data), inlineKeepOf(a.CacheTime))
}

func inlineKeepOf(cacheTime int32) int {
	switch {
	case cacheTime < inlineKeep:
		return inlineKeep
	case cacheTime > inlineKeepMax:
		return inlineKeepMax
	}
	return int(cacheTime)
}

// WaitInlineAnswer waits for the answer, up to AnswerWait; nil when the bot
// did not answer in time.
func (d *Dao) WaitInlineAnswer(ctx context.Context, queryId int64) (*InlineAnswer, error) {
	return wait(ctx, func(ctx context.Context) (*InlineAnswer, error) { return d.InlineAnswer(ctx, queryId) })
}

// InlineAnswer returns the stored answer of a query, nil when it is gone.
func (d *Dao) InlineAnswer(ctx context.Context, queryId int64) (*InlineAnswer, error) {
	return decode[InlineAnswer](d.KV.GetCtx(ctx, inlineAnswerKey(queryId)))
}

// CachedInlineQuery returns the query whose answer is cached under key, 0
// for none.
func (d *Dao) CachedInlineQuery(ctx context.Context, key string) (int64, error) {
	s, err := d.KV.GetCtx(ctx, key)
	if errors.Is(err, redis.Nil) || err == nil && s == "" {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// CacheInline remembers that key is answered by queryId, for cacheTime.
func (d *Dao) CacheInline(ctx context.Context, key string, queryId int64, cacheTime int32) error {
	if cacheTime <= 0 {
		return nil
	}
	return d.KV.SetexCtx(ctx, key, strconv.FormatInt(queryId, 10), int(cacheTime))
}

// InlineCacheKey is the cache of the same query to a bot; userId is 0 for
// results any user may get (core.telegram.org/method/messages.setInlineBotResults,
// private).
func InlineCacheKey(botId, userId int64, peerKind string, geo *mtproto.InputGeoPoint, query, offset string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%v\x00%v", query, offset, peerKind, geo.GetLat(), geo.GetLong())
	return fmt.Sprintf("bot_inline_cache:%d:%d:%s", botId, userId, hex.EncodeToString(h.Sum(nil)[:16]))
}

func decode[T any](s string, err error) (*T, error) {
	if errors.Is(err, redis.Nil) || err == nil && s == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := new(T)
	if err = jsonx.UnmarshalFromString(s, v); err != nil {
		return nil, err
	}
	return v, nil
}

// wait polls get until it answers something, the request ends or AnswerWait
// passes: the store is Redis and the waits are short and few.
func wait[T any](ctx context.Context, get func(context.Context) (*T, error)) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, AnswerWait)
	defer cancel()
	tick := time.NewTicker(answerPoll)
	defer tick.Stop()
	for {
		v, err := get(ctx)
		if err != nil && ctx.Err() != nil {
			// The wait ended during the read: no answer, not a failure.
			return nil, nil
		}
		if err != nil || v != nil {
			return v, err
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-tick.C:
		}
	}
}
