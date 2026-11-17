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

package business

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// What the connected bot is told (task 105, part 7; ADR 0067, p. 6): its
// connections (updateBotBusinessConnect) and the messages of the chats it
// serves, each with the qts of the authorization of the bot (pkg/qtsqueue).

// HomeDcID is dc_id of a connection: the DC the bot sends the requests of
// the connection to ("dc_id: ID of the datacenter where to send queries
// wrapped in a invokeWithBusinessConnection", core.telegram.org/constructor/
// botBusinessConnection). Pluma has one DC (help.getConfig this_dc 1).
const HomeDcID = 1

// ConnectUpdate is updateBotBusinessConnect of c for its bot, its qts left
// for the queue to give. With disabled it tells the bot that c is gone
// (core.telegram.org/constructor/botBusinessConnection: "Whether the
// business bot was disconnected"). date is when the bot was connected.
// can_reply is set for the older forms of botBusinessConnection, which have
// no rights.
func (c *Connection) ConnectUpdate(disabled bool) *mtproto.Update {
	return mtproto.MakeTLUpdateBotBusinessConnect(&mtproto.Update{
		Connection: mtproto.MakeTLBotBusinessConnection(&mtproto.BotBusinessConnection{
			Disabled:     disabled,
			ConnectionId: c.ConnectionID,
			UserId:       c.UserID,
			DcId:         HomeDcID,
			Date:         c.CreatedAt,
			Rights:       c.Rights.ToTL(),
			CanReply:     c.Rights.Has(RightReply),
		}).To_BotBusinessConnection(),
	}).To_Update()
}

// ConnectionByID returns the connection with connectionID, nil for none.
func (s *Store) ConnectionByID(ctx context.Context, connectionID string) (*Connection, error) {
	var rows []connectionRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+connectionColumns+" FROM business_bot_connections WHERE connection_id = ?", connectionID); err != nil {
		return nil, err
	}
	return firstConnection(rows)
}

// ServesTx tells, in tx, a box transaction of c.UserID that read c there,
// whether c gets the events of the private chat with peerID: its
// recipients take the chat in (new when it began after the bot was
// connected, a contact when contact) and the bot is not paused there. The
// caller has checked that the chat is one a bot serves (ServedUser).
func ServesTx(tx *sqlx.Tx, c *Connection, peerID int64, contact bool) (bool, error) {
	isNew, err := ChatIsNewTx(tx, c.UserID, peerID, c.CreatedAt)
	if err != nil {
		return false, err
	}
	if !c.Recipients.Match(Peer{UserID: peerID, New: isNew, Contact: contact}) {
		return false, nil
	}
	var rows []struct {
		N int `db:"n"`
	}
	if err = tx.QueryRowsPartial(&rows, "SELECT 1 AS n FROM business_bot_paused_peers WHERE user_id = ? AND peer_id = ?", c.UserID, peerID); err != nil {
		return false, err
	}
	return len(rows) == 0, nil
}
