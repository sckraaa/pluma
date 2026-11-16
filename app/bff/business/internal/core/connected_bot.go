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
	"errors"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/env2"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The connected bot of Telegram Business (task 105, part 6; ADR 0067):
// one bot per account that reads and answers the private chats the account
// chooses, by the rights it gives
// (core.telegram.org/api/bots/connected-business-bots). Here the account
// sets it up and the bot is told of its connection
// (updateBotBusinessConnect, with qts); the messages of the chats it serves
// are told by the service msg, and what it does through the connection is
// part 8.

// tellBot adds to b, the box transaction of the account of conn that
// changed it, the event that tells the bot of conn of its connection:
// updateBotBusinessConnect "with the new settings and connection_id", or
// with disabled when it is gone (core.telegram.org/constructor/
// updateBotBusinessConnect). The bot gets it with qts in its queue.
func (c *BusinessCore) tellBot(b *userbox.Box, conn *business.Connection, disabled bool) error {
	if conn == nil {
		return nil
	}
	return inbox.EmitBotBusinessUpdate(b, c.svcCtx.Dao.InboxTopic, conn.BotID, conn.ConnectUpdate(disabled))
}

// runBox runs fn in a box transaction of the caller and wakes the relay
// of the process for what it emitted.
func (c *BusinessCore) runBox(fn func(b *userbox.Box) error) error {
	res, err := userbox.Run(c.ctx, c.svcCtx.Dao.BoxDB, c.MD.UserId, fn)
	if err != nil {
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return nil
}

// recipientUsers are the ids of the users of a list of recipients: people
// other than the caller, at most business.RecipientsUsersMax of them
// (USERS_TOO_MUCH; Telegram publishes no limit, not verified). The clients
// pick them from the caller's private chats (tdesktop
// settings/business/settings_recipients_helper.cpp), so a user that is not
// one (the caller, a bot, an id nobody has) is USER_ID_INVALID; a deleted
// account is left out, as nobody can write from it.
func (c *BusinessCore) recipientUsers(method string, in []*mtproto.InputUser) ([]int64, error) {
	if len(in) > business.RecipientsUsersMax {
		err := mtproto.ErrUsersTooMuch
		c.Logger.Errorf("%s - %d users: %v", method, len(in), err)
		return nil, err
	}
	ids := make([]int64, 0, len(in))
	for _, u := range in {
		p := mtproto.FromInputUser(c.MD.UserId, u)
		if p.PeerType != mtproto.PEER_USER || p.PeerId == c.MD.UserId || p.PeerId == 0 {
			err := mtproto.ErrUserIdInvalid
			c.Logger.Errorf("%s - error: %v", method, err)
			return nil, err
		}
		if !slices.Contains(ids, p.PeerId) {
			ids = append(ids, p.PeerId)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: ids})
	if err != nil {
		c.Logger.Errorf("%s - users: %v", method, err)
		return nil, err
	}
	out := ids[:0]
	for _, id := range ids {
		u, ok := users.GetImmutableUser(id)
		switch {
		case !ok || u.IsBot():
			err := mtproto.ErrUserIdInvalid
			c.Logger.Errorf("%s - user %d: %v", method, id, err)
			return nil, err
		case u.Deleted():
		default:
			out = append(out, id)
		}
	}
	return out, nil
}

// chatPeer is the user of the private chat peer names for the bar of the
// connected bot: a user other than the caller, else PEER_ID_INVALID.
func (c *BusinessCore) chatPeer(method string, peer *mtproto.InputPeer) (int64, error) {
	p := mtproto.FromInputPeer2(c.MD.UserId, peer)
	if p.PeerType != mtproto.PEER_USER || p.PeerId == c.MD.UserId || p.PeerId == 0 {
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return 0, err
	}
	return p.PeerId, nil
}

// errConnectionChanged: the connection read before the box transaction is
// not the one locked in it, changed by a call that came between.
var errConnectionChanged = errors.New("the connected bot changed meanwhile")

// connectionChangeAttempts bounds the rounds of changeChat: a round goes
// again only when another change of the connection came between its read
// and its transaction.
const connectionChangeAttempts = 3

// changeChat changes the connected bot of the caller in its chat with
// peerID: it reads the connection and asks whether the bot serves the chat
// (PEER_ID_INVALID when there is no bot or it does not; Telegram's answer
// is not published, not verified), runs change in a box transaction of the
// caller on the connection locked there, and sends updatePeerSettings with
// the new bar, the whole settings of the chat, to the caller's other
// devices with seq (ADR 0020), as the bar is shown on each of them. The
// caller's device keeps the bar it set: tdesktop changes the flags itself
// after the answer (data/business/data_business_chatbots.cpp:122-131,
// 156-160). The user service is asked before the transaction, so that no
// call to it waits under the lock.
func (c *BusinessCore) changeChat(method string, peerID int64,
	change func(b *userbox.Box, conn *business.Connection, peer business.Peer, now int32) (*business.PeerBar, error)) error {
	settings, err := c.svcCtx.Dao.UserClient.UserGetPeerSettings(c.ctx, &userpb.TLUserGetPeerSettings{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   peerID,
	})
	if err != nil {
		c.Logger.Errorf("%s - peer settings: %v", method, err)
		return err
	}
	for range connectionChangeAttempts {
		conn, err := c.svcCtx.Dao.Store.Connection(c.ctx, c.MD.UserId)
		if err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return err
		}
		if conn == nil {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("%s - no connected bot: %v", method, err)
			return err
		}
		peer, served, err := c.svcCtx.Dao.Store.ChatPeer(c.ctx, c.svcCtx.Dao.UserClient, conn, peerID)
		if err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return err
		}
		if !served {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("%s - the bot does not serve %d: %v", method, peerID, err)
			return err
		}
		bot, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: conn.BotID})
		if err != nil {
			c.Logger.Errorf("%s - bot %d: %v", method, conn.BotID, err)
			return err
		}
		users, err := c.usersFor(method, []int64{peerID, conn.BotID})
		if err != nil {
			return err
		}
		res, err := userbox.Run(c.ctx, c.svcCtx.Dao.BoxDB, c.MD.UserId, func(b *userbox.Box) error {
			locked, err := business.ConnectionTx(b.Tx(), c.MD.UserId)
			if err != nil {
				return err
			}
			if !locked.Same(conn) {
				return errConnectionChanged
			}
			bar, err := change(b, locked, peer, int32(time.Now().Unix()))
			if err != nil {
				return err
			}
			if bar != nil {
				bar.ManageURL = business.ManageURL(env2.TDotMe, bot.Username())
			}
			return sync_client.EmitSeqUpdateExcept(b, c.svcCtx.Dao.SyncTopic,
				mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
					Peer_PEER: mtproto.MakePeerUser(peerID),
					Settings:  bar.Apply(settings),
				}).To_Update(),
				users, nil, c.MD.PermAuthKeyId)
		})
		if errors.Is(err, errConnectionChanged) {
			continue
		}
		if err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return err
		}
		if res.Emitted > 0 {
			outbox.Notify()
		}
		return nil
	}
	c.Logger.Errorf("%s - error: %v", method, errConnectionChanged)
	return errConnectionChanged
}
