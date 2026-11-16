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
	"github.com/teamgram/proto/mtproto"
)

// MessagesProlongWebView
// messages.prolongWebView#b0d81a83 flags:# silent:flags.5?true peer:InputPeer bot:InputUser query_id:long reply_to:flags.0?InputReplyTo send_as:flags.13?InputPeer = Bool;
//
// Keeps a web view with a query_id open: the clients send it every 60
// seconds while the view is open and close the view on any error (Android
// ui/bots/BotWebViewAttachedSheet.java:164, 366-393;
// core.telegram.org/api/bots/webapps: "QUERY_ID_INVALID ... the webview
// must be closed"). The request repeats where the bot's answer goes, which
// the user may have changed meanwhile (the reply, silent, send_as); the
// last of them is what the answer uses. A web view of another user, bot or
// chat, or one the bot answered or that expired, is QUERY_ID_INVALID.
func (c *BotsCore) MessagesProlongWebView(in *mtproto.TLMessagesProlongWebView) (*mtproto.Bool, error) {
	w, err := c.svcCtx.GetWebView(c.ctx, in.GetQueryId())
	if err != nil {
		c.Logger.Errorf("messages.prolongWebView - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if w == nil || w.User != c.MD.UserId {
		return nil, mtproto.ErrQueryIdInvalid
	}
	bot := mtproto.FromInputUser(c.MD.UserId, in.GetBot())
	peer := c.chatPeer(in.GetPeer())
	if !bot.IsUser() || bot.PeerId != w.Bot || peer.PeerType != w.PeerType || peer.PeerId != w.PeerId {
		return nil, mtproto.ErrQueryIdInvalid
	}
	if _, err = c.svcCtx.SendAs.From(c.ctx, c.MD.UserId, peer, in.GetSendAs()); err != nil {
		return nil, err
	}
	w.ReplyTo, w.Silent, w.SendAs = in.GetReplyTo(), in.GetSilent(), in.GetSendAs()
	ok, err := c.svcCtx.ProlongWebView(c.ctx, in.GetQueryId(), w)
	if err != nil {
		c.Logger.Errorf("messages.prolongWebView - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		// Answered or expired since it was read.
		return nil, mtproto.ErrQueryIdInvalid
	}
	return mtproto.BoolTrue, nil
}
