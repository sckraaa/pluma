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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// Rights are the rights an account gives its connected bot, the bits of
// the flags of businessBotRights (api.tl:2070) as the client sent them:
// tdesktop and iOS set them one by one (iOS
// TelegramEngine/Messages/QuickReplyMessages.swift:1222-1264). The rights
// of gifts, stars and stories are kept and answered back, but nothing of
// Pluma acts on them (ADR 0067).
type Rights int32

const (
	RightReply                   Rights = 1 << 0
	RightReadMessages            Rights = 1 << 1
	RightDeleteSentMessages      Rights = 1 << 2
	RightDeleteReceivedMessages  Rights = 1 << 3
	RightEditName                Rights = 1 << 4
	RightEditBio                 Rights = 1 << 5
	RightEditProfilePhoto        Rights = 1 << 6
	RightEditUsername            Rights = 1 << 7
	RightViewGifts               Rights = 1 << 8
	RightSellGifts               Rights = 1 << 9
	RightChangeGiftSettings      Rights = 1 << 10
	RightTransferAndUpgradeGifts Rights = 1 << 11
	RightTransferStars           Rights = 1 << 12
	RightManageStories           Rights = 1 << 13

	rightsAll Rights = 1<<14 - 1
)

// rightBits pairs every right with its field of businessBotRights.
var rightBits = []struct {
	right Rights
	get   func(*mtproto.BusinessBotRights) bool
	set   func(*mtproto.BusinessBotRights)
}{
	{RightReply, (*mtproto.BusinessBotRights).GetReply, func(r *mtproto.BusinessBotRights) { r.Reply = true }},
	{RightReadMessages, (*mtproto.BusinessBotRights).GetReadMessages, func(r *mtproto.BusinessBotRights) { r.ReadMessages = true }},
	{RightDeleteSentMessages, (*mtproto.BusinessBotRights).GetDeleteSentMessages, func(r *mtproto.BusinessBotRights) { r.DeleteSentMessages = true }},
	{RightDeleteReceivedMessages, (*mtproto.BusinessBotRights).GetDeleteReceivedMessages, func(r *mtproto.BusinessBotRights) { r.DeleteReceivedMessages = true }},
	{RightEditName, (*mtproto.BusinessBotRights).GetEditName, func(r *mtproto.BusinessBotRights) { r.EditName = true }},
	{RightEditBio, (*mtproto.BusinessBotRights).GetEditBio, func(r *mtproto.BusinessBotRights) { r.EditBio = true }},
	{RightEditProfilePhoto, (*mtproto.BusinessBotRights).GetEditProfilePhoto, func(r *mtproto.BusinessBotRights) { r.EditProfilePhoto = true }},
	{RightEditUsername, (*mtproto.BusinessBotRights).GetEditUsername, func(r *mtproto.BusinessBotRights) { r.EditUsername = true }},
	{RightViewGifts, (*mtproto.BusinessBotRights).GetViewGifts, func(r *mtproto.BusinessBotRights) { r.ViewGifts = true }},
	{RightSellGifts, (*mtproto.BusinessBotRights).GetSellGifts, func(r *mtproto.BusinessBotRights) { r.SellGifts = true }},
	{RightChangeGiftSettings, (*mtproto.BusinessBotRights).GetChangeGiftSettings, func(r *mtproto.BusinessBotRights) { r.ChangeGiftSettings = true }},
	{RightTransferAndUpgradeGifts, (*mtproto.BusinessBotRights).GetTransferAndUpgradeGifts, func(r *mtproto.BusinessBotRights) { r.TransferAndUpgradeGifts = true }},
	{RightTransferStars, (*mtproto.BusinessBotRights).GetTransferStars, func(r *mtproto.BusinessBotRights) { r.TransferStars = true }},
	{RightManageStories, (*mtproto.BusinessBotRights).GetManageStories, func(r *mtproto.BusinessBotRights) { r.ManageStories = true }},
}

// RightsFromTL are the rights of r; nil (the flag of
// account.updateConnectedBot left out) is none.
func RightsFromTL(r *mtproto.BusinessBotRights) Rights {
	var out Rights
	if r == nil {
		return out
	}
	for _, b := range rightBits {
		if b.get(r) {
			out |= b.right
		}
	}
	return out
}

// ToTL is r as businessBotRights.
func (r Rights) ToTL() *mtproto.BusinessBotRights {
	out := &mtproto.BusinessBotRights{}
	for _, b := range rightBits {
		if r&b.right != 0 {
			b.set(out)
		}
	}
	return mtproto.MakeTLBusinessBotRights(out).To_BusinessBotRights()
}

// Has tells whether r holds every right of x.
func (r Rights) Has(x Rights) bool { return r&x == x }

// Connection is the connected bot of an account: one bot per account
// (core.telegram.org/api/bots/connected-business-bots). ConnectionID is
// the connection_id the bot knows it by; it stays while the same bot stays
// connected and is new for every bot connected anew.
type Connection struct {
	UserID       int64
	BotID        int64
	ConnectionID string
	Rights       Rights
	Recipients   Recipients
	// CreatedAt is unix time: when this bot was connected, the line between
	// the existing chats and the new ones of its recipients.
	CreatedAt int32
	UpdatedAt int32
}

// ToTL is c as connectedBot. device, date and location are of a
// connection that waits for its confirmation, which Pluma does not make.
func (c *Connection) ToTL() *mtproto.ConnectedBot {
	return mtproto.MakeTLConnectedBot(&mtproto.ConnectedBot{
		BotId:                            c.BotID,
		Recipients_BUSINESSBOTRECIPIENTS: c.Recipients.ToBotTL(),
		Rights:                           c.Rights.ToTL(),
		// can_reply of the older forms of connectedBot (#bd068601),
		// which have no rights.
		CanReply: c.Rights.Has(RightReply),
	}).To_ConnectedBot()
}

// Same tells whether c and o are the same connection with the same
// settings: what was read of it still holds.
func (c *Connection) Same(o *Connection) bool {
	if c == nil || o == nil {
		return c == o
	}
	a, b := c.Recipients, o.Recipients
	return c.UserID == o.UserID && c.BotID == o.BotID && c.ConnectionID == o.ConnectionID && c.Rights == o.Rights &&
		a.ExistingChats == b.ExistingChats && a.NewChats == b.NewChats && a.Contacts == b.Contacts &&
		a.NonContacts == b.NonContacts && a.ExcludeSelected == b.ExcludeSelected &&
		slices.Equal(a.Users, b.Users) && slices.Equal(a.ExcludeUsers, b.ExcludeUsers)
}

// connectionIDBytes is the length of a connection_id before base64: 192
// random bits, which nobody guesses.
const connectionIDBytes = 24

// NewConnectionID is a new connection_id: 24 random bytes in base64url,
// 32 characters.
func NewConnectionID() (string, error) {
	b := make([]byte, connectionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("business: a connection id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type connectionRow struct {
	UserID       int64  `db:"user_id"`
	BotID        int64  `db:"bot_id"`
	ConnectionID string `db:"connection_id"`
	Rights       int32  `db:"rights"`
	Recipients   string `db:"recipients"`
	CreatedAt    int32  `db:"created_at"`
	UpdatedAt    int32  `db:"updated_at"`
}

const connectionColumns = "user_id, bot_id, connection_id, rights, recipients, created_at, updated_at"

func (r connectionRow) connection() (*Connection, error) {
	recipients, err := UnmarshalRecipients(r.Recipients)
	if err != nil {
		return nil, fmt.Errorf("business: the recipients of the bot of %d: %w", r.UserID, err)
	}
	return &Connection{
		UserID:       r.UserID,
		BotID:        r.BotID,
		ConnectionID: r.ConnectionID,
		Rights:       Rights(r.Rights),
		Recipients:   recipients,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}, nil
}

func firstConnection(rows []connectionRow) (*Connection, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].connection()
}

// Connection returns the connected bot of userID, nil for none.
func (s *Store) Connection(ctx context.Context, userID int64) (*Connection, error) {
	var rows []connectionRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+connectionColumns+" FROM business_bot_connections WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	return firstConnection(rows)
}

// ConnectionTx returns the connected bot of userID in tx, locked to the end
// of tx; nil for none.
func ConnectionTx(tx *sqlx.Tx, userID int64) (*Connection, error) {
	var rows []connectionRow
	if err := tx.QueryRowsPartial(&rows, "SELECT "+connectionColumns+" FROM business_bot_connections WHERE user_id = ? FOR UPDATE", userID); err != nil {
		return nil, err
	}
	return firstConnection(rows)
}

// AccountsOfBot are the accounts botID is the connected bot of.
func (s *Store) AccountsOfBot(ctx context.Context, botID int64) ([]int64, error) {
	var rows []struct {
		UserID int64 `db:"user_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT user_id FROM business_bot_connections WHERE bot_id = ? ORDER BY user_id", botID); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UserID)
	}
	return ids, nil
}

// ConnectTx makes botID the connected bot of userID in tx with rights and
// recipients, at now (unix time), and returns the connection as kept. The
// same bot keeps its connection_id, its creation time and the chats it is
// paused in; another bot replaces the connected one (one bot per account):
// it gets a new connection_id, and replaced is the connection it took the
// place of, nil when there was none or it was the same bot. The bot is
// told of both (updateBotBusinessConnect, part 7 of task 105).
func ConnectTx(tx *sqlx.Tx, userID, botID int64, rights Rights, recipients Recipients, now int32) (c, replaced *Connection, err error) {
	prev, err := ConnectionTx(tx, userID)
	if err != nil {
		return nil, nil, err
	}
	data, err := MarshalRecipients(recipients)
	if err != nil {
		return nil, nil, fmt.Errorf("business: the recipients of the bot of %d: %w", userID, err)
	}
	c = &Connection{UserID: userID, BotID: botID, Rights: rights & rightsAll, Recipients: recipients.Normalize(), CreatedAt: now, UpdatedAt: now}
	if prev != nil && prev.BotID == botID {
		c.ConnectionID, c.CreatedAt = prev.ConnectionID, prev.CreatedAt
		if _, err = tx.Exec("UPDATE business_bot_connections SET rights = ?, recipients = ?, updated_at = ? WHERE user_id = ?",
			int32(c.Rights), data, now, userID); err != nil {
			return nil, nil, err
		}
		return c, nil, nil
	}
	if c.ConnectionID, err = NewConnectionID(); err != nil {
		return nil, nil, err
	}
	if prev != nil {
		if _, err = tx.Exec("DELETE FROM business_bot_paused_peers WHERE user_id = ?", userID); err != nil {
			return nil, nil, err
		}
	}
	if _, err = tx.Exec("INSERT INTO business_bot_connections ("+connectionColumns+") VALUES (?, ?, ?, ?, ?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE bot_id = new.bot_id, connection_id = new.connection_id, rights = new.rights,"+
		" recipients = new.recipients, created_at = new.created_at, updated_at = new.updated_at",
		userID, botID, c.ConnectionID, int32(c.Rights), data, now, now); err != nil {
		return nil, nil, err
	}
	return c, prev, nil
}

// DisconnectTx removes the connected bot of userID in tx, with the chats
// it is paused in, and returns the connection removed, nil when there was
// none. With botID other than 0 only that bot is removed: the connection of
// another bot stays and nil is returned.
func DisconnectTx(tx *sqlx.Tx, userID, botID int64) (*Connection, error) {
	prev, err := ConnectionTx(tx, userID)
	if err != nil || prev == nil {
		return nil, err
	}
	if botID != 0 && prev.BotID != botID {
		return nil, nil
	}
	if _, err = tx.Exec("DELETE FROM business_bot_connections WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec("DELETE FROM business_bot_paused_peers WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	return prev, nil
}

// SetRecipientsTx keeps r as the recipients of c, the connection of
// c.UserID read in tx, at now (unix time), and sets them in c.
func SetRecipientsTx(tx *sqlx.Tx, c *Connection, r Recipients, now int32) error {
	data, err := MarshalRecipients(r)
	if err != nil {
		return fmt.Errorf("business: the recipients of the bot of %d: %w", c.UserID, err)
	}
	if _, err = tx.Exec("UPDATE business_bot_connections SET recipients = ?, updated_at = ? WHERE user_id = ? AND connection_id = ?",
		data, now, c.UserID, c.ConnectionID); err != nil {
		return err
	}
	c.Recipients, c.UpdatedAt = r.Normalize(), now
	return nil
}

// SetPausedTx pauses the connected bot of userID in the chat with peerID
// in tx, at now (unix time), or resumes it there.
func SetPausedTx(tx *sqlx.Tx, userID, peerID int64, paused bool, now int32) error {
	var err error
	if paused {
		_, err = tx.Exec("INSERT INTO business_bot_paused_peers (user_id, peer_id, paused_at) VALUES (?, ?, ?) AS new"+
			" ON DUPLICATE KEY UPDATE paused_at = business_bot_paused_peers.paused_at", userID, peerID, now)
	} else {
		_, err = tx.Exec("DELETE FROM business_bot_paused_peers WHERE user_id = ? AND peer_id = ?", userID, peerID)
	}
	return err
}

// Paused tells whether the connected bot of userID is paused in the chat
// with peerID.
func (s *Store) Paused(ctx context.Context, userID, peerID int64) (bool, error) {
	var rows []struct {
		N int `db:"n"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT 1 AS n FROM business_bot_paused_peers WHERE user_id = ? AND peer_id = ?", userID, peerID); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// ChatIsNew tells whether the private chat of userID with peerID began at
// since (unix time) or later: its first message in userID's box is that
// recent, or the box holds none of it yet. A chat that began earlier is an
// existing one (existing_chats); the docs do not say where the line is
// (core.telegram.org/constructor/inputBusinessRecipients: "all existing
// private chats", "all new private chats"), so it is the moment the setting
// began (task 105, decisions to confirm, 2). The first message is the one
// of the lowest box id of the dialog, deleted or not: a chat whose history
// was cleared still began when it began. Each half of the union reads one
// row of the index (user_id, dialog_id1, dialog_id2, deleted,
// user_message_box_id) instead of every message of the dialog.
func (s *Store) ChatIsNew(ctx context.Context, userID, peerID int64, since int32) (bool, error) {
	did := mtproto.MakeDialogId(userID, mtproto.PEER_USER, peerID)
	var rows []struct {
		Date2 int64 `db:"date2"`
		BoxID int32 `db:"user_message_box_id"`
	}
	const first = "(SELECT date2, user_message_box_id FROM messages" +
		" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = ? ORDER BY user_message_box_id LIMIT 1)"
	if err := s.db.QueryRowsPartial(ctx, &rows, first+" UNION ALL "+first,
		userID, did.A, did.B, 0, userID, did.A, did.B, 1); err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return true, nil
	}
	oldest := rows[0]
	for _, r := range rows[1:] {
		if r.BoxID < oldest.BoxID {
			oldest = r
		}
	}
	return oldest.Date2 >= int64(since), nil
}

// WithoutPeer is r with the chat of p left out, as
// account.disablePeerConnectedBot does: "adds the peer to exclude_users,
// or removes it from users if exclude_selected is set"
// (core.telegram.org/api/bots/connected-business-bots). Choosing only the
// selected chats, p leaves users and joins exclude_users, which tdesktop
// sends only in this mode (ForBotsToMTP,
// data/business/data_business_common.cpp:90-102), whether or not a category
// takes it in now, as iOS keeps it (_internal_removeChatManagingBot,
// TelegramEngine/Messages/BotWebView.swift:739-747): a category that takes
// it in later (it becomes a contact) does not bring the bot back. Choosing
// all but the selected ones, p joins users, the chats left out.
func (r Recipients) WithoutPeer(p Peer) Recipients {
	r.Users = slices.Clone(r.Users)
	r.ExcludeUsers = slices.Clone(r.ExcludeUsers)
	if r.ExcludeSelected {
		r.Users = append(r.Users, p.UserID)
		r.ExcludeUsers = slices.DeleteFunc(r.ExcludeUsers, func(id int64) bool { return id == p.UserID })
		return r.Normalize()
	}
	r.Users = slices.DeleteFunc(r.Users, func(id int64) bool { return id == p.UserID })
	r.ExcludeUsers = append(r.ExcludeUsers, p.UserID)
	return r.Normalize()
}
