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

package dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The public link of a channel (task 044): one editable row of the
// username table, which users and channels share and which keeps a
// username unique without case (its unique key). A channel with a username
// is public.

// PublicChannelsLimit is the most public channels one user may own:
// channels_public_limit_default of the clients and Telegram (10;
// core.telegram.org/api/config; Android MessagesController.java:1608, iOS
// UserLimitsConfiguration.swift:42), the limit the owner kept (task 035).
const PublicChannelsLimit = 10

// usernameEditor checks that b's owner created the channel: only the
// creator gives a channel its public link, as Android offers it only to the
// creator, when channelFull.can_set_username is set
// (ChatEditActivity.java:891 at aac1efcb; getFullChannel sets it for the
// creator). Others are CHAT_ADMIN_REQUIRED
// (core.telegram.org/method/channels.updateUsername). As the creator's box
// transaction locks the creator, the limit of its public channels is
// counted under that lock.
func usernameEditor(b *userbox.Box, channelId int64) error {
	if _, err := openChannel(b, channelId); err != nil {
		return err
	}
	var creators []int64
	if err := b.Tx().QueryRowsPartial(&creators, "SELECT creator_user_id FROM channels WHERE id = ? AND deleted = 0", channelId); err != nil {
		return err
	}
	switch {
	case len(creators) == 0:
		return mtproto.ErrChannelInvalid
	case creators[0] != b.UserId():
		return mtproto.ErrChatAdminRequired
	}
	return nil
}

// ownedPublic counts the public channels userId created, but channelId.
func ownedPublic(tx *sqlx.Tx, userId, channelId int64) (int, error) {
	var n []int
	err := tx.QueryRowsPartial(&n,
		"SELECT COUNT(*) FROM channels c JOIN username u ON u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1"+
			" WHERE c.creator_user_id = ? AND c.deleted = 0 AND c.id <> ?",
		mtproto.PEER_CHANNEL, userId, channelId)
	if len(n) == 0 {
		return 0, err
	}
	return n[0], err
}

// AdminedPublicChannels are the public channels userId created, newest
// first (channels.getAdminedPublicChannels: the clients offer to free the
// link of one when the limit is reached).
func (d *Dao) AdminedPublicChannels(ctx context.Context, userId int64) ([]int64, error) {
	var ids []int64
	err := d.Mysql.QueryRowsPartial(ctx, &ids,
		"SELECT c.id FROM channels c JOIN username u ON u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1"+
			" WHERE c.creator_user_id = ? AND c.deleted = 0 ORDER BY c.id DESC",
		mtproto.PEER_CHANNEL, userId)
	return ids, err
}

// CheckUsername tells whether b's owner may give the channel (0: one not
// made yet) the username: false when another peer has it, true when it is
// free or the channel's own. CHANNELS_ADMIN_PUBLIC_TOO_MUCH when the owner
// has PublicChannelsLimit public channels and this one is not one of them;
// Android probes it with a channel not made yet (ChannelCreateActivity.java:
// 170-173 at aac1efcb). For a channel, CHAT_ADMIN_REQUIRED unless the owner
// created it (usernameEditor). The format is the caller's to check.
func (d *Dao) CheckUsername(b *userbox.Box, channelId int64, username string) (bool, error) {
	if channelId != 0 {
		if err := usernameEditor(b, channelId); err != nil {
			return false, err
		}
	}
	current, err := channelUsername(b.Tx(), channelId)
	if err != nil {
		return false, err
	}
	if current == "" {
		n, err := ownedPublic(b.Tx(), b.UserId(), channelId)
		if err != nil {
			return false, err
		}
		if n >= PublicChannelsLimit {
			return false, mtproto.ErrChannelsAdminPublicTooMuch
		}
	}
	if username == "" {
		return true, nil
	}
	free, err := usernameFree(b.Tx(), channelId, username)
	return free, err
}

// channelUsername is the editable username of the channel, "" for none.
func channelUsername(tx *sqlx.Tx, channelId int64) (string, error) {
	if channelId == 0 {
		return "", nil
	}
	var names []string
	err := tx.QueryRowsPartial(&names, "SELECT username FROM username WHERE peer_type = ? AND peer_id = ? AND editable = 1",
		mtproto.PEER_CHANNEL, channelId)
	if len(names) == 0 {
		return "", err
	}
	return names[0], err
}

// usernameFree tells whether nobody but the channel has the username; the
// row is locked, so that the transaction may take it.
func usernameFree(tx *sqlx.Tx, channelId int64, username string) (bool, error) {
	var owners []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
	}
	if err := tx.QueryRowsPartial(&owners, "SELECT peer_type, peer_id FROM username WHERE username = ? FOR UPDATE", username); err != nil {
		return false, err
	}
	return len(owners) == 0 || owners[0].PeerType == mtproto.PEER_CHANNEL && owners[0].PeerId == channelId, nil
}

// UpdateUsername gives the channel the username, "" makes it private, by b's
// owner, who must be its creator (usernameEditor, CHAT_ADMIN_REQUIRED). The
// same username is USERNAME_NOT_MODIFIED, the case of the letters being a
// change; one another peer has is USERNAME_OCCUPIED; making a channel public
// past PublicChannelsLimit is CHANNELS_ADMIN_PUBLIC_TOO_MUCH, as Telegram
// answers (core.telegram.org/method/channels.updateUsername). The members
// load the channel again.
func (d *Dao) UpdateUsername(b *userbox.Box, channelId int64, username string) (*ChannelChange, error) {
	if err := usernameEditor(b, channelId); err != nil {
		return nil, err
	}
	current, err := channelUsername(b.Tx(), channelId)
	if err != nil {
		return nil, err
	}
	switch {
	case current == username:
		return nil, mtproto.ErrUsernameNotModified
	case current == "" && username != "":
		n, err := ownedPublic(b.Tx(), b.UserId(), channelId)
		if err != nil {
			return nil, err
		}
		if n >= PublicChannelsLimit {
			return nil, mtproto.ErrChannelsAdminPublicTooMuch
		}
	}
	if username != "" {
		free, err := usernameFree(b.Tx(), channelId, username)
		if err != nil {
			return nil, err
		}
		if !free {
			return nil, mtproto.ErrUsernameOccupied
		}
	}
	if _, err = b.Tx().Exec("DELETE FROM username WHERE peer_type = ? AND peer_id = ? AND editable = 1", mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	if username != "" {
		// Taken since the check (a racing transaction inserted it: under
		// READ COMMITTED the check locks no gap) is no row added. Not a
		// duplicate key error: a failed statement fails the whole box
		// transaction (userbox, runAttempt), whatever fn makes of it. Rows
		// affected without clientFoundRows in the DSN: 1 added, 0 a no-op
		// on the existing row; username is the only unique key besides id.
		res, err := b.Tx().Exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, ?) "+
			"ON DUPLICATE KEY UPDATE username = username",
			username, mtproto.PEER_CHANNEL, channelId, b.Date()<<32)
		if err != nil {
			return nil, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if n == 0 {
			return nil, mtproto.ErrUsernameOccupied
		}
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}
