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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
)

// Service is the gRPC server of RPCBots, RPCBotMenuButton, RPCInlineBot and
// RPCDeepLinks.
type Service struct {
	svcCtx *svc.ServiceContext
}

// New returns the service.
func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// BotsSetBotCommands
// bots.setBotCommands#517165a scope:BotCommandScope lang_code:string commands:Vector<BotCommand> = Bool;
func (s *Service) BotsSetBotCommands(ctx context.Context, request *mtproto.TLBotsSetBotCommands) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.setBotCommands - metadata: {%s}", c.MD)
	return c.BotsSetBotCommands(request)
}

// BotsResetBotCommands
// bots.resetBotCommands#3d8de0f9 scope:BotCommandScope lang_code:string = Bool;
func (s *Service) BotsResetBotCommands(ctx context.Context, request *mtproto.TLBotsResetBotCommands) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.resetBotCommands - metadata: {%s}", c.MD)
	return c.BotsResetBotCommands(request)
}

// BotsGetBotCommands
// bots.getBotCommands#e34c0dd6 scope:BotCommandScope lang_code:string = Vector<BotCommand>;
func (s *Service) BotsGetBotCommands(ctx context.Context, request *mtproto.TLBotsGetBotCommands) (*mtproto.Vector_BotCommand, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.getBotCommands - metadata: {%s}", c.MD)
	return c.BotsGetBotCommands(request)
}

// BotsSetBotInfo
// bots.setBotInfo#10cf3123 flags:# bot:flags.2?InputUser lang_code:string name:flags.3?string about:flags.0?string description:flags.1?string = Bool;
func (s *Service) BotsSetBotInfo(ctx context.Context, request *mtproto.TLBotsSetBotInfo) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.setBotInfo - metadata: {%s}", c.MD)
	return c.BotsSetBotInfo(request)
}

// BotsGetBotInfoDCD914FD
// bots.getBotInfo#dcd914fd flags:# bot:flags.0?InputUser lang_code:string = bots.BotInfo;
func (s *Service) BotsGetBotInfoDCD914FD(ctx context.Context, request *mtproto.TLBotsGetBotInfoDCD914FD) (*mtproto.Bots_BotInfo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.getBotInfo#dcd914fd - metadata: {%s}", c.MD)
	return c.BotsGetBotInfoDCD914FD(request)
}

// BotsGetBotInfo75EC12E6
// bots.getBotInfo#75ec12e6 lang_code:string = Vector<string>;
func (s *Service) BotsGetBotInfo75EC12E6(ctx context.Context, request *mtproto.TLBotsGetBotInfo75EC12E6) (*mtproto.Vector_String, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.getBotInfo#75ec12e6 - metadata: {%s}", c.MD)
	return c.BotsGetBotInfo75EC12E6(request)
}

// BotsGetAdminedBots
// bots.getAdminedBots#b0711d83 = Vector<User>;
func (s *Service) BotsGetAdminedBots(ctx context.Context, request *mtproto.TLBotsGetAdminedBots) (*mtproto.Vector_User, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.getAdminedBots - metadata: {%s}", c.MD)
	return c.BotsGetAdminedBots(request)
}

// BotsSetBotMenuButton
// bots.setBotMenuButton#4504d54f user_id:InputUser button:BotMenuButton = Bool;
func (s *Service) BotsSetBotMenuButton(ctx context.Context, request *mtproto.TLBotsSetBotMenuButton) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.setBotMenuButton - metadata: {%s}", c.MD)
	return c.BotsSetBotMenuButton(request)
}

// BotsGetBotMenuButton
// bots.getBotMenuButton#9c60eb28 user_id:InputUser = BotMenuButton;
func (s *Service) BotsGetBotMenuButton(ctx context.Context, request *mtproto.TLBotsGetBotMenuButton) (*mtproto.BotMenuButton, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("bots.getBotMenuButton - metadata: {%s}", c.MD)
	return c.BotsGetBotMenuButton(request)
}

// MessagesGetBotCallbackAnswer
// messages.getBotCallbackAnswer#9342ca07 flags:# game:flags.1?true peer:InputPeer msg_id:int data:flags.0?bytes password:flags.2?InputCheckPasswordSRP = messages.BotCallbackAnswer;
func (s *Service) MessagesGetBotCallbackAnswer(ctx context.Context, request *mtproto.TLMessagesGetBotCallbackAnswer) (*mtproto.Messages_BotCallbackAnswer, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getBotCallbackAnswer - metadata: {%s}", c.MD)
	return c.MessagesGetBotCallbackAnswer(request)
}

// MessagesSetBotCallbackAnswer
// messages.setBotCallbackAnswer#d58f130a flags:# alert:flags.1?true query_id:long message:flags.0?string url:flags.2?string cache_time:int = Bool;
func (s *Service) MessagesSetBotCallbackAnswer(ctx context.Context, request *mtproto.TLMessagesSetBotCallbackAnswer) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setBotCallbackAnswer - metadata: {%s}", c.MD)
	return c.MessagesSetBotCallbackAnswer(request)
}

// MessagesGetInlineBotResults
// messages.getInlineBotResults#514e999d flags:# bot:InputUser peer:InputPeer geo_point:flags.0?InputGeoPoint query:string offset:string = messages.BotResults;
func (s *Service) MessagesGetInlineBotResults(ctx context.Context, request *mtproto.TLMessagesGetInlineBotResults) (*mtproto.Messages_BotResults, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getInlineBotResults - metadata: {%s}", c.MD)
	return c.MessagesGetInlineBotResults(request)
}

// MessagesSetInlineBotResults
// messages.setInlineBotResults#bb12a419 flags:# gallery:flags.0?true private:flags.1?true query_id:long results:Vector<InputBotInlineResult> cache_time:int next_offset:flags.2?string switch_pm:flags.3?InlineBotSwitchPM switch_webview:flags.4?InlineBotWebView = Bool;
func (s *Service) MessagesSetInlineBotResults(ctx context.Context, request *mtproto.TLMessagesSetInlineBotResults) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setInlineBotResults - metadata: {%s}", c.MD)
	return c.MessagesSetInlineBotResults(request)
}

// MessagesSendInlineBotResult
// messages.sendInlineBotResult#c0cf7646 = Updates;
func (s *Service) MessagesSendInlineBotResult(ctx context.Context, request *mtproto.TLMessagesSendInlineBotResult) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendInlineBotResult - metadata: {%s}", c.MD)
	return c.MessagesSendInlineBotResult(request)
}

// MessagesStartBot
// messages.startBot#e6df7378 bot:InputUser peer:InputPeer random_id:long start_param:string = Updates;
func (s *Service) MessagesStartBot(ctx context.Context, request *mtproto.TLMessagesStartBot) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.startBot - metadata: {%s}", c.MD)
	return c.MessagesStartBot(request)
}

// The methods of these services outside the core of bots. They keep the
// 400 the session proxy answers without a route: tgnet retries a 500.

// BotsCheckUsername is served by no flow of ours.
func (s *Service) BotsCheckUsername(ctx context.Context, request *mtproto.TLBotsCheckUsername) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsCreateBot is the managed-bot flow, not served (ADR 0043).
func (s *Service) BotsCreateBot(ctx context.Context, request *mtproto.TLBotsCreateBot) (*mtproto.User, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsExportBotToken is the managed-bot flow, not served (ADR 0043).
func (s *Service) BotsExportBotToken(ctx context.Context, request *mtproto.TLBotsExportBotToken) (*mtproto.Bots_ExportedBotToken, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsGetAccessSettings is not served.
func (s *Service) BotsGetAccessSettings(ctx context.Context, request *mtproto.TLBotsGetAccessSettings) (*mtproto.Bots_AccessSettings, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsEditAccessSettings is not served.
func (s *Service) BotsEditAccessSettings(ctx context.Context, request *mtproto.TLBotsEditAccessSettings) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsSetJoinChatResults is not served.
func (s *Service) BotsSetJoinChatResults(ctx context.Context, request *mtproto.TLBotsSetJoinChatResults) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesEditInlineBotMessage is not served (the brief's follow-ups).
func (s *Service) MessagesEditInlineBotMessage(ctx context.Context, request *mtproto.TLMessagesEditInlineBotMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSendBotRequestedPeer is not served.
func (s *Service) MessagesSendBotRequestedPeer(ctx context.Context, request *mtproto.TLMessagesSendBotRequestedPeer) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSetBotGuestChatResult is not served.
func (s *Service) MessagesSetBotGuestChatResult(ctx context.Context, request *mtproto.TLMessagesSetBotGuestChatResult) (*mtproto.InputBotInlineMessageID, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// HelpGetRecentMeUrls is not served.
func (s *Service) HelpGetRecentMeUrls(ctx context.Context, request *mtproto.TLHelpGetRecentMeUrls) (*mtproto.Help_RecentMeUrls, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// HelpGetDeepLinkInfo is not served.
func (s *Service) HelpGetDeepLinkInfo(ctx context.Context, request *mtproto.TLHelpGetDeepLinkInfo) (*mtproto.Help_DeepLinkInfo, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
