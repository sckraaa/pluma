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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// AccountUpdateConnectedBot
// account.updateConnectedBot#66a08c7e flags:# deleted:flags.1?true rights:flags.0?BusinessBotRights bot:InputUser recipients:InputBusinessBotRecipients = Updates;
//
// Connects a bot to the account, changes its rights and recipients, or
// with deleted disconnects it. One bot serves an account
// (core.telegram.org/api/bots/connected-business-bots): connecting another
// replaces the connected one, which loses its connection_id and its pauses.
//
// With deleted the connected bot goes whatever bot is named: tdesktop names
// the bot it had (data/business/data_business_chatbots.cpp:86-90), iOS
// sends inputUserEmpty with empty recipients
// (TelegramEngine/Messages/QuickReplyMessages.swift:1212-1268).
//
// Otherwise the bot must have the business mode of its user
// (user.bot_business), else BOT_BUSINESS_MISSING, which tdesktop shows
// (settings/business/settings_chatbots.cpp:875-877) and which "the
// specified bot is not a business bot" names for any other user too; the
// recipients must choose a chat, else BUSINESS_RECIPIENTS_EMPTY
// (/method/account.updateConnectedBot). Rights left out are none, or the
// right to reply when can_reply of an older form of the method is set.
//
// The answer carries the bot in users; both clients apply it as updates
// (tdesktop :92, iOS :1272-1279). What Telegram puts in its updates is not
// published (not verified): nothing here, the clients keep what they sent
// and read account.getConnectedBots again.
func (c *BusinessCore) AccountUpdateConnectedBot(in *mtproto.TLAccountUpdateConnectedBot) (*mtproto.Updates, error) {
	const method = "account.updateConnectedBot"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	if in.GetDeleted() {
		var removed *business.Connection
		if err := c.runBox(func(b *userbox.Box) error {
			var err error
			if removed, err = business.DisconnectTx(b.Tx(), c.MD.UserId, 0); err != nil {
				return err
			}
			// The bot that is no longer connected is told so
			// (updateBotBusinessConnect with disabled).
			return c.tellBot(b, removed, true)
		}); err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return nil, err
		}
		var ids []int64
		if removed != nil {
			ids = []int64{removed.BotID}
		}
		return c.connectedBotUpdates(method, ids)
	}

	p := mtproto.FromInputUser(c.MD.UserId, in.GetBot())
	if p.PeerType != mtproto.PEER_USER {
		err := mtproto.ErrBotBusinessMissing
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	input := botRecipientsInput(in)
	users, err := c.recipientUsers(method, input.GetUsers())
	if err != nil {
		return nil, err
	}
	excluded, err := c.recipientUsers(method, input.GetExcludeUsers())
	if err != nil {
		return nil, err
	}
	recipients := business.RecipientsFromBotInput(input, users, excluded)
	if recipients.Empty() {
		err = mtproto.ErrBusinessRecipientsEmpty
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	bot, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: p.PeerId})
	if err != nil {
		c.Logger.Errorf("%s - bot %d: %v", method, p.PeerId, err)
		return nil, err
	}
	if !bot.IsBot() || !bot.BotBusiness() || bot.Deleted() {
		err = mtproto.ErrBotBusinessMissing
		c.Logger.Errorf("%s - user %d: %v", method, p.PeerId, err)
		return nil, err
	}
	rights := business.RightsFromTL(in.GetRights())
	if in.GetRights() == nil && in.GetCanReply() {
		// An older form of the method (account.updateConnectedBot#43d8521d,
		// older than the layers the clients use): can_reply in place of
		// the rights.
		rights = business.RightReply
	}
	if err = c.runBox(func(b *userbox.Box) error {
		// The connection and the one it replaced are told to their bots
		// (updateBotBusinessConnect): the bot replaced first, then the new
		// one, each in its own lane.
		conn, replaced, err := business.ConnectTx(b.Tx(), c.MD.UserId, bot.Id(), rights, recipients, int32(time.Now().Unix()))
		if err != nil {
			return err
		}
		if err = c.tellBot(b, replaced, true); err != nil {
			return err
		}
		return c.tellBot(b, conn, false)
	}); err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return c.connectedBotUpdates(method, []int64{bot.Id()})
}

// botRecipientsInput are the recipients of in: inputBusinessBotRecipients
// of the layers the clients use, or inputBusinessRecipients of the oldest
// form of the method (account.updateConnectedBot#9c2d527d), which has no
// exclude_users.
func botRecipientsInput(in *mtproto.TLAccountUpdateConnectedBot) *mtproto.InputBusinessBotRecipients {
	if r := in.GetRecipients_INPUTBUSINESSBOTRECIPIENTS(); r != nil {
		return r
	}
	r := in.GetRecipients_INPUTBUSINESSRECIPIENTS()
	if r == nil {
		return nil
	}
	return mtproto.MakeTLInputBusinessBotRecipients(&mtproto.InputBusinessBotRecipients{
		ExistingChats:   r.GetExistingChats(),
		NewChats:        r.GetNewChats(),
		Contacts:        r.GetContacts(),
		NonContacts:     r.GetNonContacts(),
		ExcludeSelected: r.GetExcludeSelected(),
		Users:           r.GetUsers(),
	}).To_InputBusinessBotRecipients()
}

// connectedBotUpdates is the answer of account.updateConnectedBot: no
// update, the users of bots as the caller sees them.
func (c *BusinessCore) connectedBotUpdates(method string, bots []int64) (*mtproto.Updates, error) {
	users, err := c.usersFor(method, bots)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{},
		Users:   users,
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}
