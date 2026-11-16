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
	"errors"
	"strconv"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// The mini apps of bots (task 106, ADR 0068 p. 4). A web view the bot can
// answer into a chat (query_id of webViewResultUrl) lives in Redis while the
// client keeps it open: the client prolongs it every 60 seconds (Android
// ui/bots/BotWebViewAttachedSheet.java:164, 366-393;
// core.telegram.org/api/bots/webapps), and messages.sendWebViewResultMessage
// of the bot takes it once. A web view lost with Redis is QUERY_ID_INVALID,
// on which the clients close it.

// WebViewTTL is how long a web view lives after it was opened or last
// prolonged: three of the clients' 60-second prolongations.
const WebViewTTL = 180

// WebView is a web view the bot may answer: who opened it, of which bot, and
// where and how the answer goes (the chat, reply_to, silent, send_as of the
// request, the last of them as messages.prolongWebView repeats them).
type WebView struct {
	User     int64                 `json:"u"`
	Bot      int64                 `json:"b"`
	PeerType int32                 `json:"pt"`
	PeerId   int64                 `json:"p"`
	ReplyTo  *mtproto.InputReplyTo `json:"r,omitempty"`
	Silent   bool                  `json:"s,omitempty"`
	SendAs   *mtproto.InputPeer    `json:"a,omitempty"`
}

func webViewKey(queryId int64) string { return "bot_webview:" + strconv.FormatInt(queryId, 10) }

// PutWebView stores a new web view for WebViewTTL.
func (d *Dao) PutWebView(ctx context.Context, queryId int64, w *WebView) error {
	data, err := jsonx.Marshal(w)
	if err != nil {
		return err
	}
	return d.KV.SetexCtx(ctx, webViewKey(queryId), string(data), WebViewTTL)
}

// GetWebView returns the web view of the query, nil when it is gone.
func (d *Dao) GetWebView(ctx context.Context, queryId int64) (*WebView, error) {
	return decode[WebView](d.KV.GetCtx(ctx, webViewKey(queryId)))
}

// prolongScript replaces a web view and its lifetime only while it is
// there: a web view the bot answered (taken) in the meantime is not brought
// back.
const prolongScript = `return redis.call('SET', KEYS[1], ARGV[1], 'XX', 'EX', ARGV[2])`

// ProlongWebView stores w as the web view of the query for another
// WebViewTTL; false when it is gone.
func (d *Dao) ProlongWebView(ctx context.Context, queryId int64, w *WebView) (bool, error) {
	data, err := jsonx.Marshal(w)
	if err != nil {
		return false, err
	}
	_, err = d.KV.EvalCtx(ctx, prolongScript, webViewKey(queryId), string(data), WebViewTTL)
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return err == nil, err
}

// takeScript removes a web view and returns it, nil when it is gone: of
// two answers of the bot at once, one takes it.
const takeScript = `local v = redis.call('GET', KEYS[1]); if v then redis.call('DEL', KEYS[1]) end; return v`

// TakeWebView removes the web view of the query: false when it was gone
// (answered by another request or expired).
func (d *Dao) TakeWebView(ctx context.Context, queryId int64) (bool, error) {
	v, err := d.KV.EvalCtx(ctx, takeScript, webViewKey(queryId))
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return err == nil && v != nil, err
}

func switchWebviewKey(botId, userId int64) string {
	return "bot_switch_webview:" + strconv.FormatInt(botId, 10) + ":" + strconv.FormatInt(userId, 10)
}

// RememberSwitchWebview records the address of the switch_webview button of
// the inline results the user got from the bot (messages.botResults), for
// as long as the results may be shown (inlineKeepOf):
// messages.requestSimpleWebView with from_switch_webview opens only it.
func (d *Dao) RememberSwitchWebview(ctx context.Context, botId, userId int64, url string, cacheTime int32) error {
	return d.KV.SetexCtx(ctx, switchWebviewKey(botId, userId), url, inlineKeepOf(cacheTime))
}

// SwitchWebview returns the address RememberSwitchWebview recorded last, ""
// for none.
func (d *Dao) SwitchWebview(ctx context.Context, botId, userId int64) (string, error) {
	s, err := d.KV.GetCtx(ctx, switchWebviewKey(botId, userId))
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return s, err
}

// RecentBotMarkups returns the reply markups of the latest messages of the
// bot in its private chat with the user, newest first, up to limit
// messages: messages.sendWebViewData takes only the text of a button of one
// of them. Messages without markup are counted and left out.
func (d *Dao) RecentBotMarkups(ctx context.Context, userId, botId int64, limit int) ([]*mtproto.ReplyMarkup, error) {
	dialog := mtproto.MakeDialogId(userId, mtproto.PEER_USER, botId)
	var rows []string
	if err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT message_data FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND sender_user_id = ? AND deleted = 0 ORDER BY user_message_box_id DESC LIMIT ?",
		userId, dialog.A, dialog.B, botId, limit); err != nil {
		return nil, err
	}
	var out []*mtproto.ReplyMarkup
	for _, data := range rows {
		m := new(mtproto.Message)
		if err := jsonx.UnmarshalFromString(data, m); err != nil {
			return nil, err
		}
		if m.GetReplyMarkup() != nil {
			out = append(out, m.GetReplyMarkup())
		}
	}
	return out, nil
}

// WroteToBot tells whether the user has a message of its own in its private
// chat with the bot (bots.canSendMessage: a bot may write to a user who
// wrote to it, core.telegram.org/bots/faq). The user's box is read by its
// (user_id, dialog_id1, dialog_id2) key.
func (d *Dao) WroteToBot(ctx context.Context, userId, botId int64) (bool, error) {
	dialog := mtproto.MakeDialogId(userId, mtproto.PEER_USER, botId)
	var ids []int64
	if err := d.db.QueryRowsPartial(ctx, &ids,
		"SELECT id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND sender_user_id = ? AND deleted = 0 LIMIT 1",
		userId, dialog.A, dialog.B, userId); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}
