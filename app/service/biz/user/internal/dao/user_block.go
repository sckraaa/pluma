// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package dao

import (
	"context"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/stories"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

var (
	contactsBlockPeerPrefix = "user_block_peer"
)

type CachedPeerBlocked struct {
	PeerBlocked *mtproto.PeerBlocked `json:"peer_blocked"`
}

func (c *CachedPeerBlocked) IsEmpty() bool {
	if c == nil {
		return true
	}

	return c.PeerBlocked == nil
}

func genContactsBlockPeerCacheKey(id, blockedId int64) string {
	return fmt.Sprintf("%s_%d_%d", contactsBlockPeerPrefix, id, blockedId)
}

func (d *Dao) CheckBlocked(ctx context.Context, id, blockedId int64) bool {
	var (
		blocked = new(CachedPeerBlocked)
	)
	d.CachedConn.QueryRow(
		ctx,
		blocked,
		genContactsBlockPeerCacheKey(id, blockedId),
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			do, err := d.UserPeerBlocksDAO.Select(ctx, id, mtproto.PEER_USER, blockedId)
			if err != nil {
				return err
			}
			if do != nil {
				v.(*CachedPeerBlocked).PeerBlocked = mtproto.MakeTLPeerBlocked(&mtproto.PeerBlocked{
					PeerId: mtproto.MakePeerUser(do.PeerId),
					Date:   int32(do.Date),
				}).To_PeerBlocked()
			} else {
				return sqlc.ErrNotFound
			}

			return nil
		},
	)

	return !blocked.IsEmpty()
}

// BlockUser blocks the user blockId for id in one box transaction of id,
// with updatePeerBlocked for every device of the account (ADR 0020). users
// are blockId as id sees it, for the push. A user already blocked is a
// no-op: nothing is written or pushed. The cached user id is dropped: the
// stories it carries keep its blocked users out (stories.Audiences).
func (d *Dao) BlockUser(ctx context.Context, id, blockId int64, users []*mtproto.User) error {
	_, err := d.RunBox(ctx, id, []string{genContactsBlockPeerCacheKey(id, blockId), genCacheUserDataCacheKey(id)}, func(b *userbox.Box) error {
		blocked, err := isBlockedTx(b, id, blockId)
		if err != nil || blocked {
			return err
		}
		if _, _, err = d.UserPeerBlocksDAO.InsertOrUpdateTx(b.Tx(), &dataobject.UserPeerBlocksDO{
			UserId:   id,
			PeerType: mtproto.PEER_USER,
			PeerId:   blockId,
			Date:     b.Date(),
		}); err != nil {
			return err
		}
		u, err := peerBlockedUpdate(b, blockId, true)
		if err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, u, users, nil)
	})
	return err
}

// UnBlockUser unblocks the user unblockId for id in one box transaction of
// id, with updatePeerBlocked for every device of the account (ADR 0020).
// users are unblockId as id sees it, for the push. A user not blocked is a
// no-op: nothing is written or pushed. The cached user id is dropped, as
// with BlockUser.
func (d *Dao) UnBlockUser(ctx context.Context, id, unblockId int64, users []*mtproto.User) error {
	_, err := d.RunBox(ctx, id, []string{genContactsBlockPeerCacheKey(id, unblockId), genCacheUserDataCacheKey(id)}, func(b *userbox.Box) error {
		blocked, err := isBlockedTx(b, id, unblockId)
		if err != nil || !blocked {
			return err
		}
		if _, err = d.UserPeerBlocksDAO.DeleteTx(b.Tx(), id, mtproto.PEER_USER, unblockId); err != nil {
			return err
		}
		u, err := peerBlockedUpdate(b, unblockId, false)
		if err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, u, users, nil)
	})
	return err
}

// isBlockedTx tells whether id blocks the user peerId, under the lock of
// the row in b's transaction.
func isBlockedTx(b *userbox.Box, id, peerId int64) (bool, error) {
	var list []int64
	if err := b.Tx().QueryRowsPartial(&list,
		"SELECT peer_id FROM user_peer_blocks WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0 FOR UPDATE",
		id, mtproto.PEER_USER, peerId); err != nil {
		return false, err
	}
	return len(list) > 0, nil
}

// peerBlockedUpdate is updatePeerBlocked of the user peerId for the owner
// of b, read in its transaction: blocked is the main blocklist, and
// blocked_my_stories_from says whether the owner also hides its stories
// from peerId. The clients take the update as the whole state of the peer
// (stories.SetBlockedTx), so it carries both.
func peerBlockedUpdate(b *userbox.Box, peerId int64, blocked bool) (*mtproto.Update, error) {
	fromStories, err := stories.IsBlockedTx(b.Tx(), b.UserId(), peerId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdatePeerBlocked(&mtproto.Update{
		Blocked_BOOL:         mtproto.ToBool(blocked),
		Blocked_FLAGBOOLEAN:  blocked,
		BlockedMyStoriesFrom: fromStories,
		PeerId:               mtproto.MakePeerUser(peerId),
	}).To_Update(), nil
}
