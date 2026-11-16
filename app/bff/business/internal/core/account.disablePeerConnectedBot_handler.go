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
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// AccountDisablePeerConnectedBot
// account.disablePeerConnectedBot#5e437ed9 peer:InputPeer = Bool;
//
// Takes one chat away from the connected bot for good: the chat leaves its
// recipients ("adds the peer to exclude_users", or to the chats left out
// when all but some are chosen; core.telegram.org/api/bots/connected-business-bots,
// business.Recipients.WithoutPeer) and is no longer paused. The bar over
// the chat goes: tdesktop clears it itself and reads the connected bot
// again (data/business/data_business_chatbots.cpp:155-163), the other
// devices get updatePeerSettings without it. The bot is told of its
// changed connection (updateBotBusinessConnect, with the same
// connection_id).
//
// A list of excluded users already at business.RecipientsUsersMax is
// USERS_TOO_MUCH, the limit of account.updateConnectedBot.
func (c *BusinessCore) AccountDisablePeerConnectedBot(in *mtproto.TLAccountDisablePeerConnectedBot) (*mtproto.Bool, error) {
	const method = "account.disablePeerConnectedBot"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	peerID, err := c.chatPeer(method, in.GetPeer())
	if err != nil {
		return nil, err
	}
	if err = c.changeChat(method, peerID, func(b *userbox.Box, conn *business.Connection, peer business.Peer, now int32) (*business.PeerBar, error) {
		r := conn.Recipients.WithoutPeer(peer)
		if len(r.Users) > business.RecipientsUsersMax || len(r.ExcludeUsers) > business.RecipientsUsersMax {
			return nil, mtproto.ErrUsersTooMuch
		}
		if err := business.SetRecipientsTx(b.Tx(), conn, r, now); err != nil {
			return nil, err
		}
		if err := business.SetPausedTx(b.Tx(), c.MD.UserId, peerID, false, now); err != nil {
			return nil, err
		}
		return nil, c.tellBot(b, conn, false)
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
