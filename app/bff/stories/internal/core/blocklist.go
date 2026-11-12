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
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/stories"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The blocklist of stories of the caller (contacts.block, contacts.unblock,
// contacts.getBlocked and contacts.setBlocked with my_stories_from): the
// users it hides its stories from, who still write and call it. The
// clients offer it in the list of viewers of a story (Android
// SelfStoryViewsPage.java:348-349 and StoriesController.java:4483-4513,
// iOS BlockedPeers.swift:42-50) and in the privacy of a new story
// (StoryPrivacyBottomSheet.java:944-951, StoriesController.java:4418-4440),
// and read it back with contacts.getBlocked (StoriesController.java:
// 4370-4401, iOS BlockedPeersContext.swift:83-86).

// SetStoriesBlocked puts the user in on the blocklist of stories of the
// caller (blocked) or takes it off. Like contacts.block it takes users
// only, never the caller; a deleted account cannot be put on it.
func (c *StoriesCore) SetStoriesBlocked(in *mtproto.InputPeer, blocked bool) error {
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	if !p.IsUser() || p.IsSelf() || p.PeerId == c.MD.UserId {
		return mtproto.ErrPeerIdInvalid
	}
	users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: []int64{c.MD.UserId, p.PeerId},
		To: []int64{c.MD.UserId},
	})
	if err != nil {
		return err
	}
	u, ok := users.GetImmutableUser(p.PeerId)
	if !ok {
		return mtproto.ErrContactIdInvalid
	}
	if blocked && u.Deleted() {
		return mtproto.ErrInputUserDeactivated
	}
	return c.changeStoriesBlocked(users, func(*sqlx.Tx) (map[int64]bool, error) {
		return map[int64]bool{p.PeerId: blocked}, nil
	})
}

// SetStoriesBlocklist replaces the blocklist of stories of the caller with
// the first limit users of list (core.telegram.org/method/contacts.setBlocked:
// limit is the length of the list).
func (c *StoriesCore) SetStoriesBlocklist(list []*mtproto.InputPeer, limit int32) error {
	if limit < 0 {
		return mtproto.ErrLimitInvalid
	}
	if int(limit) < len(list) {
		list = list[:limit]
	}
	want := make(map[int64]bool, len(list))
	ids := []int64{c.MD.UserId}
	for _, in := range list {
		p := mtproto.FromInputPeer2(c.MD.UserId, in)
		if !p.IsUser() || p.IsSelf() || p.PeerId == c.MD.UserId {
			return mtproto.ErrPeerIdInvalid
		}
		if !want[p.PeerId] {
			want[p.PeerId] = true
			ids = append(ids, p.PeerId)
		}
	}
	if len(want) > stories.MaxBlocked {
		return mtproto.ErrLimitInvalid
	}
	// The users taken off the list are read too, for their updates.
	current, _, err := c.svcCtx.Dao.Store.BlockedPage(c.ctx, c.MD.UserId, 0, stories.MaxBlocked)
	if err != nil {
		return err
	}
	for _, b := range current {
		if !want[b.PeerId] {
			ids = append(ids, b.PeerId)
		}
	}
	users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{c.MD.UserId}})
	if err != nil {
		return err
	}
	for id := range want {
		u, ok := users.GetImmutableUser(id)
		if !ok {
			return mtproto.ErrPeerIdInvalid
		}
		if u.Deleted() {
			return mtproto.ErrInputUserDeactivated
		}
	}
	// The list is compared with the one the transaction holds, so a change
	// made meanwhile from another device is not undone by half.
	return c.changeStoriesBlocked(users, func(tx *sqlx.Tx) (map[int64]bool, error) {
		have, err := stories.BlockedListTx(tx, c.MD.UserId)
		if err != nil {
			return nil, err
		}
		change := make(map[int64]bool)
		held := make(map[int64]bool, len(have))
		for _, id := range have {
			held[id] = true
			if !want[id] {
				change[id] = false
			}
		}
		for id := range want {
			if !held[id] {
				change[id] = true
			}
		}
		return change, nil
	})
}

// changeStoriesBlocked applies the change plan returns in the transaction
// (user: blocked or not) to the blocklist of stories of the caller in one
// box transaction of the caller, with updatePeerBlocked with seq to every device of the account
// for each user whose state changed (ADR 0020). The update carries the
// main blocklist too: the clients read it as the whole state of the peer.
// The stories of the caller then keep the blocked out (stories.Audiences),
// which the service of users learns as for an edit of their rules.
func (c *StoriesCore) changeStoriesBlocked(users *userpb.Vector_ImmutableUser, plan func(tx *sqlx.Tx) (map[int64]bool, error)) error {
	var (
		changed bool
		at      int32
	)
	res, err := userbox.Run(c.ctx, c.svcCtx.Dao.BoxDB, c.MD.UserId, func(b *userbox.Box) error {
		changed, at = false, int32(b.Date())
		change, err := plan(b.Tx())
		if err != nil {
			return err
		}
		for id, blocked := range change {
			done, err := stories.SetBlockedTx(b.Tx(), c.MD.UserId, id, blocked, at)
			if err != nil {
				return err
			}
			if !done {
				continue
			}
			changed = true
			main, err := stories.MainBlockedTx(b.Tx(), c.MD.UserId, id)
			if err != nil {
				return err
			}
			var shown []*mtproto.User
			if u, err := users.GetUnsafeUser(c.MD.UserId, id); err == nil {
				shown = []*mtproto.User{u}
			}
			if err := sync_client.EmitSeqUpdate(b, c.svcCtx.Dao.SyncTopic, mtproto.MakeTLUpdatePeerBlocked(&mtproto.Update{
				Blocked_BOOL:         mtproto.ToBool(main),
				Blocked_FLAGBOOLEAN:  main,
				BlockedMyStoriesFrom: blocked,
				PeerId:               mtproto.MakePeerUser(id),
			}).To_Update(), shown, nil); err != nil {
				return err
			}
		}
		if changed {
			return stories.MarkDirtyTx(b.Tx(), stories.UserPeer(c.MD.UserId), at)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	if changed {
		c.changed(stories.UserPeer(c.MD.UserId), at)
	}
	return nil
}

// StoriesBlocklist is contacts.getBlocked with my_stories_from: limit users
// of the blocklist of stories of the caller from offset, newest first, as
// contacts.blocked when that is the whole list and contacts.blockedSlice
// with the count otherwise.
func (c *StoriesCore) StoriesBlocklist(offset, limit int32) (*mtproto.Contacts_Blocked, error) {
	if offset < 0 {
		offset = 0
	}
	// iOS asks 200 at first (BlockedPeersContext.swift:84), Android 100
	// (StoriesController.java:4372-4377).
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	list, total, err := c.svcCtx.Dao.Store.BlockedPage(c.ctx, c.MD.UserId, offset, limit)
	if err != nil {
		return nil, err
	}
	out := &mtproto.Contacts_Blocked{
		Blocked: make([]*mtproto.PeerBlocked, 0, len(list)),
		Chats:   []*mtproto.Chat{},
		Users:   []*mtproto.User{},
		Count:   total,
	}
	if len(list) > 0 {
		ids := make([]int64, 0, len(list))
		for _, b := range list {
			ids = append(ids, b.PeerId)
			out.Blocked = append(out.Blocked, mtproto.MakeTLPeerBlocked(&mtproto.PeerBlocked{
				PeerId: mtproto.MakePeerUser(b.PeerId),
				Date:   b.Date,
			}).To_PeerBlocked())
		}
		users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: append([]int64{c.MD.UserId}, ids...),
			To: []int64{c.MD.UserId},
		})
		if err != nil {
			return nil, err
		}
		out.Users = users.GetUserListByIdList(c.MD.UserId, ids...)
	}
	if offset == 0 && int32(len(list)) == total {
		return mtproto.MakeTLContactsBlocked(out).To_Contacts_Blocked(), nil
	}
	return mtproto.MakeTLContactsBlockedSlice(out).To_Contacts_Blocked(), nil
}

// HidesStoriesFrom is userFull.blocked_my_stories_from: the user userId
// hides its stories from peerId.
func (c *StoriesCore) HidesStoriesFrom(userId, peerId int64) (bool, error) {
	blocked, err := stories.BlockedOf(c.ctx, c.svcCtx.Dao.DB, userId, []int64{peerId})
	if err != nil {
		return false, err
	}
	return blocked[peerId], nil
}
