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

// AccountToggleConnectedBotPaused
// account.toggleConnectedBotPaused#646e1097 peer:InputPeer paused:Bool = Bool;
//
// Pauses the connected bot in one chat, or resumes it there: "temporarily"
// (core.telegram.org/api/bots/connected-business-bots), until resumed. The
// buttons Pause and Resume of the bar over the chat call it (tdesktop
// history/view/history_view_contact_status.cpp:1290-1298, Android
// ui/Business/BusinessBotButton.java:116, 133). A paused bot cannot reply
// there (business_bot_can_reply off) and gets nothing of the chat
// (business.ServesTx); its connection is not changed, and it is not told.
func (c *BusinessCore) AccountToggleConnectedBotPaused(in *mtproto.TLAccountToggleConnectedBotPaused) (*mtproto.Bool, error) {
	const method = "account.toggleConnectedBotPaused"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	peerID, err := c.chatPeer(method, in.GetPeer())
	if err != nil {
		return nil, err
	}
	paused := mtproto.FromBool(in.GetPaused())
	if err = c.changeChat(method, peerID, func(b *userbox.Box, conn *business.Connection, _ business.Peer, now int32) (*business.PeerBar, error) {
		if err := business.SetPausedTx(b.Tx(), c.MD.UserId, peerID, paused, now); err != nil {
			return nil, err
		}
		return &business.PeerBar{
			BotID:    conn.BotID,
			Paused:   paused,
			CanReply: !paused && conn.Rights.Has(business.RightReply),
		}, nil
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
