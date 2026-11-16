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

// The methods of Telegram Business not served yet (task 105, parts 2-8):
// each answers ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not
// retry, never an internal error (500), which Android's and Web K's
// transports retry without end. A method moves to its own handler file
// when its part serves it.

// AccountUpdateBusinessGreetingMessage is not served yet: account.updateBusinessGreetingMessage#66cdafc4.
func (c *BusinessCore) AccountUpdateBusinessGreetingMessage(in *mtproto.TLAccountUpdateBusinessGreetingMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountUpdateBusinessAwayMessage is not served yet: account.updateBusinessAwayMessage#a26a7fa5.
func (c *BusinessCore) AccountUpdateBusinessAwayMessage(in *mtproto.TLAccountUpdateBusinessAwayMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountCreateBusinessChatLink is not served yet: account.createBusinessChatLink#8851e68e.
func (c *BusinessCore) AccountCreateBusinessChatLink(in *mtproto.TLAccountCreateBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountEditBusinessChatLink is not served yet: account.editBusinessChatLink#8c3410af.
func (c *BusinessCore) AccountEditBusinessChatLink(in *mtproto.TLAccountEditBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountDeleteBusinessChatLink is not served yet: account.deleteBusinessChatLink#60073674.
func (c *BusinessCore) AccountDeleteBusinessChatLink(in *mtproto.TLAccountDeleteBusinessChatLink) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetBusinessChatLinks is not served yet: account.getBusinessChatLinks#6f70dde1.
func (c *BusinessCore) AccountGetBusinessChatLinks(in *mtproto.TLAccountGetBusinessChatLinks) (*mtproto.Account_BusinessChatLinks, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountResolveBusinessChatLink is not served yet: account.resolveBusinessChatLink#5492e5ee.
func (c *BusinessCore) AccountResolveBusinessChatLink(in *mtproto.TLAccountResolveBusinessChatLink) (*mtproto.Account_ResolvedBusinessChatLinks, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetQuickReplies is not served yet: messages.getQuickReplies#d483f2a8.
func (c *BusinessCore) MessagesGetQuickReplies(in *mtproto.TLMessagesGetQuickReplies) (*mtproto.Messages_QuickReplies, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesReorderQuickReplies is not served yet: messages.reorderQuickReplies#60331907.
func (c *BusinessCore) MessagesReorderQuickReplies(in *mtproto.TLMessagesReorderQuickReplies) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesCheckQuickReplyShortcut is not served yet: messages.checkQuickReplyShortcut#f1d0fbd3.
func (c *BusinessCore) MessagesCheckQuickReplyShortcut(in *mtproto.TLMessagesCheckQuickReplyShortcut) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesEditQuickReplyShortcut is not served yet: messages.editQuickReplyShortcut#5c003cef.
func (c *BusinessCore) MessagesEditQuickReplyShortcut(in *mtproto.TLMessagesEditQuickReplyShortcut) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesDeleteQuickReplyShortcut is not served yet: messages.deleteQuickReplyShortcut#3cc04740.
func (c *BusinessCore) MessagesDeleteQuickReplyShortcut(in *mtproto.TLMessagesDeleteQuickReplyShortcut) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetQuickReplyMessages is not served yet: messages.getQuickReplyMessages#94a495c3.
func (c *BusinessCore) MessagesGetQuickReplyMessages(in *mtproto.TLMessagesGetQuickReplyMessages) (*mtproto.Messages_Messages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSendQuickReplyMessages is not served yet: messages.sendQuickReplyMessages#6c750de1.
func (c *BusinessCore) MessagesSendQuickReplyMessages(in *mtproto.TLMessagesSendQuickReplyMessages) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesDeleteQuickReplyMessages is not served yet: messages.deleteQuickReplyMessages#e105e910.
func (c *BusinessCore) MessagesDeleteQuickReplyMessages(in *mtproto.TLMessagesDeleteQuickReplyMessages) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountUpdateConnectedBot is not served yet: account.updateConnectedBot#66a08c7e.
func (c *BusinessCore) AccountUpdateConnectedBot(in *mtproto.TLAccountUpdateConnectedBot) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetConnectedBots is not served yet: account.getConnectedBots#4ea4c80f.
func (c *BusinessCore) AccountGetConnectedBots(in *mtproto.TLAccountGetConnectedBots) (*mtproto.Account_ConnectedBots, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetBotBusinessConnection is not served yet: account.getBotBusinessConnection#76a86270.
func (c *BusinessCore) AccountGetBotBusinessConnection(in *mtproto.TLAccountGetBotBusinessConnection) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountToggleConnectedBotPaused is not served yet: account.toggleConnectedBotPaused#646e1097.
func (c *BusinessCore) AccountToggleConnectedBotPaused(in *mtproto.TLAccountToggleConnectedBotPaused) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountDisablePeerConnectedBot is not served yet: account.disablePeerConnectedBot#5e437ed9.
func (c *BusinessCore) AccountDisablePeerConnectedBot(in *mtproto.TLAccountDisablePeerConnectedBot) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
