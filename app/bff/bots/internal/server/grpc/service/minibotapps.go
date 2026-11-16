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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/core"
)

// RPCMiniBotApps and RPCMainMiniBotApps (task 106, ADR 0068 p. 4): the mini
// apps of bots. The methods the clients of this layer do not send (the
// older constructors of requestSimpleWebView and requestAppWebView), the
// ones the task leaves out (ADR 0068, "Что не делается") and those later
// parts of the task serve answer ERR_ENTERPRISE_IS_BLOCKED, a 400 the
// clients show as an error and do not repeat.

// MessagesRequestWebView
// messages.requestWebView#269dc2c1 flags:# from_bot_menu:flags.4?true silent:flags.5?true compact:flags.7?true fullscreen:flags.8?true peer:InputPeer bot:InputUser url:flags.1?string start_param:flags.3?string theme_params:flags.2?DataJSON platform:string reply_to:flags.0?InputReplyTo send_as:flags.13?InputPeer = WebViewResult;
func (s *Service) MessagesRequestWebView(ctx context.Context, request *mtproto.TLMessagesRequestWebView) (*mtproto.WebViewResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestWebView - metadata: {%s}", c.MD)
	return c.MessagesRequestWebView(request)
}

// MessagesProlongWebView
// messages.prolongWebView#b0d81a83 flags:# silent:flags.5?true peer:InputPeer bot:InputUser query_id:long reply_to:flags.0?InputReplyTo send_as:flags.13?InputPeer = Bool;
func (s *Service) MessagesProlongWebView(ctx context.Context, request *mtproto.TLMessagesProlongWebView) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.prolongWebView - metadata: {%s}", c.MD)
	return c.MessagesProlongWebView(request)
}

// MessagesRequestSimpleWebView413A3E73
// messages.requestSimpleWebView#413a3e73 flags:# from_switch_webview:flags.1?true from_side_menu:flags.2?true compact:flags.7?true fullscreen:flags.8?true bot:InputUser url:flags.3?string start_param:flags.4?string theme_params:flags.0?DataJSON platform:string = WebViewResult;
func (s *Service) MessagesRequestSimpleWebView413A3E73(ctx context.Context, request *mtproto.TLMessagesRequestSimpleWebView413A3E73) (*mtproto.WebViewResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestSimpleWebView - metadata: {%s}", c.MD)
	return c.MessagesRequestSimpleWebView413A3E73(request)
}

// MessagesGetBotApp
// messages.getBotApp#34fdc5c3 app:InputBotApp hash:long = messages.BotApp;
func (s *Service) MessagesGetBotApp(ctx context.Context, request *mtproto.TLMessagesGetBotApp) (*mtproto.Messages_BotApp, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getBotApp - metadata: {%s}", c.MD)
	return c.MessagesGetBotApp(request)
}

// MessagesRequestAppWebView53618BCE
// messages.requestAppWebView#53618bce flags:# write_allowed:flags.0?true compact:flags.7?true fullscreen:flags.8?true peer:InputPeer app:InputBotApp start_param:flags.1?string theme_params:flags.2?DataJSON platform:string = WebViewResult;
func (s *Service) MessagesRequestAppWebView53618BCE(ctx context.Context, request *mtproto.TLMessagesRequestAppWebView53618BCE) (*mtproto.WebViewResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestAppWebView - metadata: {%s}", c.MD)
	return c.MessagesRequestAppWebView53618BCE(request)
}

// BotsCanSendMessage
// bots.canSendMessage#1359f4e6 bot:InputUser = Bool;
func (s *Service) BotsCanSendMessage(ctx context.Context, request *mtproto.TLBotsCanSendMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.canSendMessage - metadata: {%s}", c.MD)
	return c.BotsCanSendMessage(request)
}

// BotsAllowSendMessage
// bots.allowSendMessage#f132e3ef bot:InputUser = Updates;
func (s *Service) BotsAllowSendMessage(ctx context.Context, request *mtproto.TLBotsAllowSendMessage) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.allowSendMessage - metadata: {%s}", c.MD)
	return c.BotsAllowSendMessage(request)
}

// MessagesRequestMainWebView
// messages.requestMainWebView#c9e01e7b flags:# compact:flags.7?true fullscreen:flags.8?true peer:InputPeer bot:InputUser start_param:flags.1?string theme_params:flags.0?DataJSON platform:string = WebViewResult;
func (s *Service) MessagesRequestMainWebView(ctx context.Context, request *mtproto.TLMessagesRequestMainWebView) (*mtproto.WebViewResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestMainWebView - metadata: {%s}", c.MD)
	return c.MessagesRequestMainWebView(request)
}

// MessagesSendWebViewResultMessage is the bot's answer of a web view; it
// comes with the common send of inline results (task 106, part D2).
func (s *Service) MessagesSendWebViewResultMessage(ctx context.Context, request *mtproto.TLMessagesSendWebViewResultMessage) (*mtproto.WebViewMessageSent, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSendWebViewData is sendData of a mini app; it comes with the
// incoming action of msg (task 106, part D2).
func (s *Service) MessagesSendWebViewData(ctx context.Context, request *mtproto.TLMessagesSendWebViewData) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsRequestWebViewButton prepares a button for a mini app (task 106,
// part F).
func (s *Service) BotsRequestWebViewButton(ctx context.Context, request *mtproto.TLBotsRequestWebViewButton) (*mtproto.Bots_RequestedButton, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsGetRequestedWebViewButton returns a prepared button (task 106,
// part F).
func (s *Service) BotsGetRequestedWebViewButton(ctx context.Context, request *mtproto.TLBotsGetRequestedWebViewButton) (*mtproto.KeyboardButton, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesRequestChatJoinWebView belongs to the bots that screen join
// requests, a task of its own (ADR 0068, "Что не делается").
func (s *Service) MessagesRequestChatJoinWebView(ctx context.Context, request *mtproto.TLMessagesRequestChatJoinWebView) (*mtproto.WebViewResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsInvokeWebViewCustomMethod is not served (ADR 0068, "Что не
// делается").
func (s *Service) BotsInvokeWebViewCustomMethod(ctx context.Context, request *mtproto.TLBotsInvokeWebViewCustomMethod) (*mtproto.DataJSON, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsCheckDownloadFileParams is not served (ADR 0068, "Что не
// делается").
func (s *Service) BotsCheckDownloadFileParams(ctx context.Context, request *mtproto.TLBotsCheckDownloadFileParams) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesRequestSimpleWebView1A46500A is a constructor of an older layer.
func (s *Service) MessagesRequestSimpleWebView1A46500A(ctx context.Context, request *mtproto.TLMessagesRequestSimpleWebView1A46500A) (*mtproto.SimpleWebViewResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesRequestAppWebView8C5A3B3C is a constructor of an older layer.
func (s *Service) MessagesRequestAppWebView8C5A3B3C(ctx context.Context, request *mtproto.TLMessagesRequestAppWebView8C5A3B3C) (*mtproto.AppWebViewResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesRequestSimpleWebView299BEC8E is a constructor of an older layer.
func (s *Service) MessagesRequestSimpleWebView299BEC8E(ctx context.Context, request *mtproto.TLMessagesRequestSimpleWebView299BEC8E) (*mtproto.SimpleWebViewResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesRequestSimpleWebView6ABB2F73 is a constructor of an older layer.
func (s *Service) MessagesRequestSimpleWebView6ABB2F73(ctx context.Context, request *mtproto.TLMessagesRequestSimpleWebView6ABB2F73) (*mtproto.SimpleWebViewResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsGetPopularAppBots is not served (ADR 0068, "Что не делается").
func (s *Service) BotsGetPopularAppBots(ctx context.Context, request *mtproto.TLBotsGetPopularAppBots) (*mtproto.Bots_PopularAppBots, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsAddPreviewMedia is not served (ADR 0068, "Что не делается").
func (s *Service) BotsAddPreviewMedia(ctx context.Context, request *mtproto.TLBotsAddPreviewMedia) (*mtproto.BotPreviewMedia, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsEditPreviewMedia is not served (ADR 0068, "Что не делается").
func (s *Service) BotsEditPreviewMedia(ctx context.Context, request *mtproto.TLBotsEditPreviewMedia) (*mtproto.BotPreviewMedia, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsDeletePreviewMedia is not served (ADR 0068, "Что не делается").
func (s *Service) BotsDeletePreviewMedia(ctx context.Context, request *mtproto.TLBotsDeletePreviewMedia) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsReorderPreviewMedias is not served (ADR 0068, "Что не делается").
func (s *Service) BotsReorderPreviewMedias(ctx context.Context, request *mtproto.TLBotsReorderPreviewMedias) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsGetPreviewInfo is not served (ADR 0068, "Что не делается").
func (s *Service) BotsGetPreviewInfo(ctx context.Context, request *mtproto.TLBotsGetPreviewInfo) (*mtproto.Bots_PreviewInfo, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsGetPreviewMedias is not served (ADR 0068, "Что не делается").
func (s *Service) BotsGetPreviewMedias(ctx context.Context, request *mtproto.TLBotsGetPreviewMedias) (*mtproto.Vector_BotPreviewMedia, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
