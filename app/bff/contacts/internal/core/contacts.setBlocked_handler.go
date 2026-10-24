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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
)

// maxBlocked bounds the blocklist read to replace it.
const maxBlocked = 10000

// errStoriesUnavailable answers the story blocklist: Pluma has no stories
// (users are stories_unavailable), so there is no list to edit.
var errStoriesUnavailable = status.Error(mtproto.ErrBadRequest, "STORIES_UNAVAILABLE")

// ContactsSetBlocked
// contacts.setBlocked#94c65c76 flags:# my_stories_from:flags.0?true id:Vector<InputPeer> limit:int = Bool;
//
// Replaces the whole main blocklist with the first limit peers of id
// (core.telegram.org/api/block: limit is the length of id): the peers not
// in it are unblocked, the new ones blocked, and the user's other devices
// get updatePeerBlocked for each, as from contacts.block and
// contacts.unblock. Only users can be blocked, as with contacts.block.
func (c *ContactsCore) ContactsSetBlocked(in *mtproto.TLContactsSetBlocked) (*mtproto.Bool, error) {
	if in.GetMyStoriesFrom() {
		c.Logger.Errorf("contacts.setBlocked - error: %v", errStoriesUnavailable)
		return nil, errStoriesUnavailable
	}
	if in.GetLimit() < 0 {
		err := mtproto.ErrLimitInvalid
		c.Logger.Errorf("contacts.setBlocked - error: %v", err)
		return nil, err
	}
	peers := in.GetId()
	if int(in.GetLimit()) < len(peers) {
		peers = peers[:in.GetLimit()]
	}

	var (
		want  []int64
		wants = make(map[int64]bool, len(peers))
	)
	for _, p := range peers {
		peer := mtproto.FromInputPeer2(c.MD.UserId, p)
		if !peer.IsUser() || peer.PeerId == c.MD.UserId {
			err := mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("contacts.setBlocked - error: %v", err)
			return nil, err
		}
		if !wants[peer.PeerId] {
			wants[peer.PeerId] = true
			want = append(want, peer.PeerId)
		}
	}

	current, err := c.svcCtx.Dao.UserClient.UserGetBlockedList(c.ctx, &userpb.TLUserGetBlockedList{
		UserId: c.MD.UserId,
		Limit:  maxBlocked,
	})
	if err != nil {
		c.Logger.Errorf("contacts.setBlocked - error: %v", err)
		return nil, err
	}
	blocked := make(map[int64]bool, len(current.GetDatas()))
	var unblock []int64
	for _, b := range current.GetDatas() {
		id := b.GetPeerId().GetUserId()
		if b.GetPeerId().GetPredicateName() != mtproto.Predicate_peerUser {
			continue
		}
		blocked[id] = true
		if !wants[id] {
			unblock = append(unblock, id)
		}
	}
	var block []int64
	for _, id := range want {
		if !blocked[id] {
			block = append(block, id)
		}
	}
	if len(block) == 0 && len(unblock) == 0 {
		return mtproto.BoolTrue, nil
	}

	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: append(append([]int64{c.MD.UserId}, block...), unblock...),
		To: []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("contacts.setBlocked - error: %v", err)
		return nil, err
	}
	for _, id := range block {
		u, ok := users.GetImmutableUser(id)
		switch {
		case !ok:
			err = mtproto.ErrPeerIdInvalid
		case u.Deleted():
			err = mtproto.ErrInputUserDeactivated
		}
		if err != nil {
			c.Logger.Errorf("contacts.setBlocked - error: %v", err)
			return nil, err
		}
	}

	updates := mtproto.MakeUpdatesByUpdatesUsers(nil)
	change := func(id int64, block bool) error {
		var (
			ok  *mtproto.Bool
			err error
		)
		if block {
			ok, err = c.svcCtx.Dao.UserClient.UserBlockPeer(c.ctx, &userpb.TLUserBlockPeer{UserId: c.MD.UserId, PeerType: mtproto.PEER_USER, PeerId: id})
		} else {
			ok, err = c.svcCtx.Dao.UserClient.UserUnBlockPeer(c.ctx, &userpb.TLUserUnBlockPeer{UserId: c.MD.UserId, PeerType: mtproto.PEER_USER, PeerId: id})
		}
		if err == nil && !mtproto.FromBool(ok) {
			err = mtproto.ErrInternalServerError
		}
		if err != nil {
			return err
		}
		updates.PushBackUpdate(mtproto.MakeTLUpdatePeerBlocked(&mtproto.Update{
			Blocked_BOOL:        mtproto.ToBool(block),
			Blocked_FLAGBOOLEAN: block,
			PeerId:              mtproto.MakePeerUser(id),
		}).To_Update())
		if u, err := users.GetUnsafeUser(c.MD.UserId, id); err == nil {
			updates.PushUser(u)
		}
		return nil
	}
	for _, id := range unblock {
		if err = change(id, false); err != nil {
			break
		}
	}
	for _, id := range block {
		if err != nil {
			break
		}
		err = change(id, true)
	}
	if len(updates.GetUpdates()) > 0 {
		if _, err2 := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
			UserId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			Updates:       updates,
		}); err2 != nil {
			c.Logger.Errorf("contacts.setBlocked - push to the other devices of user %d: %v", c.MD.UserId, err2)
		}
	}
	if err != nil {
		// What changed is pushed; repeating the request finishes the rest.
		c.Logger.Errorf("contacts.setBlocked - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
