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

package core

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"strconv"

	"github.com/teamgram/proto/mtproto"
)

// MessagesSendWebViewResultMessage
// messages.sendWebViewResultMessage#a4314f5 bot_query_id:string result:InputBotInlineResult = WebViewMessageSent;
//
// The bot's answer of a web view (Bot API answerWebAppQuery): the result,
// an inline result, is sent from the user who opened the web view to the
// chat it was opened in, via the bot, with the reply, silent and send_as
// the client last gave (messages.requestWebView, messages.prolongWebView).
// Then every session of the user gets updateWebViewResultSent(query_id),
// with seq, on which the clients close the web view of that query (Android
// messenger/MessagesController.java:20716-20718, tdesktop
// api/api_updates.cpp:2326, iOS State/Sync/UpdateInterpreter.swift:380-381).
// bot_query_id is the query_id of the initData, a string in the Bot API.
// Only the bot of the web view answers it, once: another bot's, an answered
// or an expired query is QUERY_ID_INVALID.
//
// The query is taken last. The message has a random_id of its own for the
// query (webViewResultRandomId): a request that fails after the send is
// repeated by the bot and msg answers it with the message sent
// (dao.SentBefore), so the answer goes out once whatever fails on the way,
// and a failed send leaves the query for another try. msg_id of the answer
// names the message for edits of inline messages, which come with
// messages.editInlineBotMessage (task 106, part G2); until then it is left
// out, as the schema allows.
func (c *BotsCore) MessagesSendWebViewResultMessage(in *mtproto.TLMessagesSendWebViewResultMessage) (*mtproto.WebViewMessageSent, error) {
	me, err := c.meBot()
	if err != nil {
		return nil, err
	}
	queryId, err := strconv.ParseInt(in.GetBotQueryId(), 10, 64)
	if err != nil || queryId <= 0 {
		return nil, mtproto.ErrQueryIdInvalid
	}
	// The result is checked before the query is looked at: a bad one leaves
	// the query to answer.
	result, err := c.inlineResult(in.GetResult())
	if err != nil {
		return nil, err
	}
	w, err := c.svcCtx.GetWebView(c.ctx, queryId)
	if err != nil {
		c.Logger.Errorf("messages.sendWebViewResultMessage - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if w == nil || w.Bot != me.Id() {
		return nil, mtproto.ErrQueryIdInvalid
	}

	_, _, err = c.sendInlineResult(&inlineSend{
		userId: w.User,
		// The user's every session gets the message: none of them asked.
		authKeyId: 0,
		peer:      mtproto.MakePeerUtil(w.PeerType, w.PeerId),
		botId:     me.Id(),
		result:    result,
		silent:    w.Silent,
		randomId:  webViewResultRandomId(queryId),
		replyTo:   w.ReplyTo,
		sendAs:    w.SendAs,
	})
	if errors.Is(err, mtproto.ErrRandomIdDuplicate) {
		// Sent by an earlier try and deleted by the user since.
		err = nil
	}
	if err != nil {
		c.Logger.Errorf("messages.sendWebViewResultMessage - bot %d, user %d: %v", me.Id(), w.User, err)
		return nil, err
	}

	sent := mtproto.MakeTLUpdateWebViewResultSent(&mtproto.Update{QueryId_INT64: queryId}).To_Update()
	if err = c.svcCtx.UserSeq.PushSeq(c.ctx, w.User, sent, []*mtproto.User{}, []*mtproto.Chat{}); err != nil {
		c.Logger.Errorf("messages.sendWebViewResultMessage - update of user %d: %v", w.User, err)
		return nil, mtproto.ErrInternalServerError
	}
	// A concurrent answer may have taken the query meanwhile: it sent the
	// same message, so this one stands as well.
	if _, err = c.svcCtx.TakeWebView(c.ctx, queryId); err != nil {
		c.Logger.Errorf("messages.sendWebViewResultMessage - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	return mtproto.MakeTLWebViewMessageSent(&mtproto.WebViewMessageSent{}).To_WebViewMessageSent(), nil
}

// webViewResultRandomId is the random_id of the answer of a web view: one
// message per query, however often the bot repeats the answer.
func webViewResultRandomId(queryId int64) int64 {
	h := fnv.New64a()
	var b [len("webview_result") + 8]byte
	n := copy(b[:], "webview_result")
	binary.BigEndian.PutUint64(b[n:], uint64(queryId))
	_, _ = h.Write(b[:])
	// Never 0, which msg reads as "no random_id" (randomIds).
	return int64(h.Sum64()>>1) | 1
}
