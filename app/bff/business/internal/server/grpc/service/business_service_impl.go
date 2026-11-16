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
	"github.com/teamgram/teamgram-server/app/bff/business/internal/core"
)

// RPCBusinessOpeningHours

// AccountUpdateBusinessWorkHours
// account.updateBusinessWorkHours#4b00e066 flags:# business_work_hours:flags.0?BusinessWorkHours = Bool;
func (s *Service) AccountUpdateBusinessWorkHours(ctx context.Context, request *mtproto.TLAccountUpdateBusinessWorkHours) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBusinessWorkHours - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBusinessWorkHours(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessLocation

// AccountUpdateBusinessLocation
// account.updateBusinessLocation#9e6b131a flags:# geo_point:flags.1?InputGeoPoint address:flags.0?string = Bool;
func (s *Service) AccountUpdateBusinessLocation(ctx context.Context, request *mtproto.TLAccountUpdateBusinessLocation) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBusinessLocation - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBusinessLocation(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessGreeting

// AccountUpdateBusinessGreetingMessage
// account.updateBusinessGreetingMessage#66cdafc4 flags:# message:flags.0?InputBusinessGreetingMessage = Bool;
func (s *Service) AccountUpdateBusinessGreetingMessage(ctx context.Context, request *mtproto.TLAccountUpdateBusinessGreetingMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBusinessGreetingMessage - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBusinessGreetingMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountUpdateBusinessAwayMessage
// account.updateBusinessAwayMessage#a26a7fa5 flags:# message:flags.0?InputBusinessAwayMessage = Bool;
func (s *Service) AccountUpdateBusinessAwayMessage(ctx context.Context, request *mtproto.TLAccountUpdateBusinessAwayMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBusinessAwayMessage - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBusinessAwayMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessIntro

// AccountUpdateBusinessIntro
// account.updateBusinessIntro#a614d034 flags:# intro:flags.0?InputBusinessIntro = Bool;
func (s *Service) AccountUpdateBusinessIntro(ctx context.Context, request *mtproto.TLAccountUpdateBusinessIntro) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBusinessIntro - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBusinessIntro(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessChatLinks

// AccountCreateBusinessChatLink
// account.createBusinessChatLink#8851e68e link:InputBusinessChatLink = BusinessChatLink;
func (s *Service) AccountCreateBusinessChatLink(ctx context.Context, request *mtproto.TLAccountCreateBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.createBusinessChatLink - metadata: {%s}", c.MD)

	r, err := c.AccountCreateBusinessChatLink(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountEditBusinessChatLink
// account.editBusinessChatLink#8c3410af slug:string link:InputBusinessChatLink = BusinessChatLink;
func (s *Service) AccountEditBusinessChatLink(ctx context.Context, request *mtproto.TLAccountEditBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.editBusinessChatLink - metadata: {%s}", c.MD)

	r, err := c.AccountEditBusinessChatLink(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountDeleteBusinessChatLink
// account.deleteBusinessChatLink#60073674 slug:string = Bool;
func (s *Service) AccountDeleteBusinessChatLink(ctx context.Context, request *mtproto.TLAccountDeleteBusinessChatLink) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.deleteBusinessChatLink - metadata: {%s}", c.MD)

	r, err := c.AccountDeleteBusinessChatLink(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountGetBusinessChatLinks
// account.getBusinessChatLinks#6f70dde1 = account.BusinessChatLinks;
func (s *Service) AccountGetBusinessChatLinks(ctx context.Context, request *mtproto.TLAccountGetBusinessChatLinks) (*mtproto.Account_BusinessChatLinks, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getBusinessChatLinks - metadata: {%s}", c.MD)

	r, err := c.AccountGetBusinessChatLinks(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountResolveBusinessChatLink
// account.resolveBusinessChatLink#5492e5ee slug:string = account.ResolvedBusinessChatLinks;
func (s *Service) AccountResolveBusinessChatLink(ctx context.Context, request *mtproto.TLAccountResolveBusinessChatLink) (*mtproto.Account_ResolvedBusinessChatLinks, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.resolveBusinessChatLink - metadata: {%s}", c.MD)

	r, err := c.AccountResolveBusinessChatLink(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessQuickReply

// MessagesGetQuickReplies
// messages.getQuickReplies#d483f2a8 hash:long = messages.QuickReplies;
func (s *Service) MessagesGetQuickReplies(ctx context.Context, request *mtproto.TLMessagesGetQuickReplies) (*mtproto.Messages_QuickReplies, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getQuickReplies - metadata: {%s}", c.MD)

	r, err := c.MessagesGetQuickReplies(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesReorderQuickReplies
// messages.reorderQuickReplies#60331907 order:Vector<int> = Bool;
func (s *Service) MessagesReorderQuickReplies(ctx context.Context, request *mtproto.TLMessagesReorderQuickReplies) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.reorderQuickReplies - metadata: {%s}", c.MD)

	r, err := c.MessagesReorderQuickReplies(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesCheckQuickReplyShortcut
// messages.checkQuickReplyShortcut#f1d0fbd3 shortcut:string = Bool;
func (s *Service) MessagesCheckQuickReplyShortcut(ctx context.Context, request *mtproto.TLMessagesCheckQuickReplyShortcut) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.checkQuickReplyShortcut - metadata: {%s}", c.MD)

	r, err := c.MessagesCheckQuickReplyShortcut(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesEditQuickReplyShortcut
// messages.editQuickReplyShortcut#5c003cef shortcut_id:int shortcut:string = Bool;
func (s *Service) MessagesEditQuickReplyShortcut(ctx context.Context, request *mtproto.TLMessagesEditQuickReplyShortcut) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.editQuickReplyShortcut - metadata: {%s}", c.MD)

	r, err := c.MessagesEditQuickReplyShortcut(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesDeleteQuickReplyShortcut
// messages.deleteQuickReplyShortcut#3cc04740 shortcut_id:int = Bool;
func (s *Service) MessagesDeleteQuickReplyShortcut(ctx context.Context, request *mtproto.TLMessagesDeleteQuickReplyShortcut) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.deleteQuickReplyShortcut - metadata: {%s}", c.MD)

	r, err := c.MessagesDeleteQuickReplyShortcut(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesGetQuickReplyMessages
// messages.getQuickReplyMessages#94a495c3 flags:# shortcut_id:int id:flags.0?Vector<int> hash:long = messages.Messages;
func (s *Service) MessagesGetQuickReplyMessages(ctx context.Context, request *mtproto.TLMessagesGetQuickReplyMessages) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getQuickReplyMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesGetQuickReplyMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesSendQuickReplyMessages
// messages.sendQuickReplyMessages#6c750de1 peer:InputPeer shortcut_id:int id:Vector<int> random_id:Vector<long> = Updates;
func (s *Service) MessagesSendQuickReplyMessages(ctx context.Context, request *mtproto.TLMessagesSendQuickReplyMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendQuickReplyMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesSendQuickReplyMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesDeleteQuickReplyMessages
// messages.deleteQuickReplyMessages#e105e910 shortcut_id:int id:Vector<int> = Updates;
func (s *Service) MessagesDeleteQuickReplyMessages(ctx context.Context, request *mtproto.TLMessagesDeleteQuickReplyMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.deleteQuickReplyMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesDeleteQuickReplyMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// RPCBusinessConnectedBots

// AccountUpdateConnectedBot
// account.updateConnectedBot#66a08c7e flags:# deleted:flags.1?true rights:flags.0?BusinessBotRights bot:InputUser recipients:InputBusinessBotRecipients = Updates;
func (s *Service) AccountUpdateConnectedBot(ctx context.Context, request *mtproto.TLAccountUpdateConnectedBot) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateConnectedBot - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateConnectedBot(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountGetConnectedBots
// account.getConnectedBots#4ea4c80f = account.ConnectedBots;
func (s *Service) AccountGetConnectedBots(ctx context.Context, request *mtproto.TLAccountGetConnectedBots) (*mtproto.Account_ConnectedBots, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getConnectedBots - metadata: {%s}", c.MD)

	r, err := c.AccountGetConnectedBots(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountGetBotBusinessConnection
// account.getBotBusinessConnection#76a86270 connection_id:string = Updates;
func (s *Service) AccountGetBotBusinessConnection(ctx context.Context, request *mtproto.TLAccountGetBotBusinessConnection) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getBotBusinessConnection - metadata: {%s}", c.MD)

	r, err := c.AccountGetBotBusinessConnection(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountToggleConnectedBotPaused
// account.toggleConnectedBotPaused#646e1097 peer:InputPeer paused:Bool = Bool;
func (s *Service) AccountToggleConnectedBotPaused(ctx context.Context, request *mtproto.TLAccountToggleConnectedBotPaused) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.toggleConnectedBotPaused - metadata: {%s}", c.MD)

	r, err := c.AccountToggleConnectedBotPaused(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountDisablePeerConnectedBot
// account.disablePeerConnectedBot#5e437ed9 peer:InputPeer = Bool;
func (s *Service) AccountDisablePeerConnectedBot(ctx context.Context, request *mtproto.TLAccountDisablePeerConnectedBot) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.disablePeerConnectedBot - metadata: {%s}", c.MD)

	r, err := c.AccountDisablePeerConnectedBot(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
