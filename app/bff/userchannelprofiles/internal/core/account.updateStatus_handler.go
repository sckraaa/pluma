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
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/presence"
)

// AccountUpdateStatus
// account.updateStatus#6628562c offline:Bool = Bool;
//
// The status of every authorization is kept apart (pkg/presence): the user
// is online while any of them is. A change, or an online window running out,
// is pushed as updateUserStatus without seq (ADR 0020, p. 2) to the user's
// other authorizations and to the users who see the user and may see their
// last seen (ADR 0030).
func (c *UserChannelProfilesCore) AccountUpdateStatus(in *mtproto.TLAccountUpdateStatus) (*mtproto.Bool, error) {
	var (
		offline = mtproto.FromBool(in.GetOffline())
		now     = time.Now().Unix()
	)

	change, err := c.svcCtx.Presence.Update(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, !offline, now)
	if err != nil {
		c.Logger.Errorf("account.updateStatus - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}

	// Viewers learn of the change first; the stored value is what the users
	// who ask later read (users.getUsers, contacts.getStatuses).
	if change.Push {
		c.pushStatus(change, now)
	}

	if _, err = c.svcCtx.Dao.UserClient.UserUpdateLastSeen(c.ctx, &userpb.TLUserUpdateLastSeen{
		Id:         c.MD.UserId,
		LastSeenAt: now,
		Expires:    int32(change.SeenUntil - now),
	}); err != nil {
		c.Logger.Errorf("account.updateStatus - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}

	return mtproto.BoolTrue, nil
}

func (c *UserChannelProfilesCore) pushStatus(change presence.Change, now int64) {
	status := mtproto.MakeTLUserStatusOffline(&mtproto.UserStatus{
		WasOnline: int32(change.SeenUntil),
	}).To_UserStatus()
	if change.Online {
		status = mtproto.MakeTLUserStatusOnline(&mtproto.UserStatus{
			Expires: int32(change.SeenUntil),
		}).To_UserStatus()
	}
	updates := mtproto.MakeTLUpdateShort(&mtproto.Updates{
		Update: mtproto.MakeTLUpdateUserStatus(&mtproto.Update{
			UserId:            c.MD.UserId,
			Status_USERSTATUS: status,
		}).To_Update(),
		Date: int32(now),
	}).To_Updates()

	// The other authorizations of the user: Telegram-iOS holds its
	// notifications while another device is online
	// (AccountStateManagementUtils.swift, delayNotificatonsUntil), tdesktop
	// says online again when told it is offline while active
	// (api_updates.cpp, mtpc_updateUserStatus).
	if _, err := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &syncpb.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       updates,
	}); err != nil {
		c.Logger.Errorf("account.updateStatus - push to the other authorizations: %v", err)
	}

	recipients, err := c.statusRecipients()
	if err != nil {
		c.Logger.Errorf("account.updateStatus - recipients: %v", err)
		return
	}
	for _, id := range recipients {
		if _, err = c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &syncpb.TLSyncPushUpdates{
			UserId:  id,
			Updates: updates,
		}); err != nil {
			c.Logger.Errorf("account.updateStatus - push to user %d: %v", id, err)
		}
	}
}

// statusRecipients returns the users who see the caller (user.getStatusWatchers:
// contacts, private dialogs, basic groups) and may see the caller's exact
// last seen: the viewer may by the privacy rule of the status timestamp, as for
// users.getUsers (ImmutableUser.StatusFor, viewers have Premium, ADR 0041),
// and neither user has blocked the other. The others would only be told a
// rounded status ("recently"), which coming online or going offline does not
// change.
func (c *UserChannelProfilesCore) statusRecipients() ([]int64, error) {
	watchers, err := c.svcCtx.Dao.UserClient.UserGetStatusWatchers(c.ctx, &userpb.TLUserGetStatusWatchers{
		UserId: c.MD.UserId,
	})
	if err != nil {
		return nil, err
	}
	ids := watchers.GetDatas()
	if len(ids) == 0 {
		return nil, nil
	}

	all := append([]int64{c.MD.UserId}, ids...)
	mUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: all,
		To: all,
	})
	if err != nil {
		return nil, err
	}
	self, ok := mUsers.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrUserIdInvalid
	}

	blockedByMe, err := c.svcCtx.Dao.UserClient.UserCheckBlockUserList(c.ctx, &userpb.TLUserCheckBlockUserList{
		UserId: c.MD.UserId,
		Id:     ids,
	})
	if err != nil {
		return nil, err
	}
	skip := make(map[int64]bool, len(blockedByMe.GetDatas()))
	for _, id := range blockedByMe.GetDatas() {
		skip[id] = true
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = make([]int64, 0, len(ids))
	)
	for _, id := range ids {
		viewer, ok := mUsers.GetImmutableUser(id)
		// Only who sees the exact time (ImmutableUser.StatusFor): the others'
		// rounded status does not change when the user comes or goes.
		if !ok || skip[id] || viewer.Deleted() ||
			!self.CheckPrivacyFor(mtproto.STATUS_TIMESTAMP, viewer) {
			continue
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			blocked, err := c.svcCtx.Dao.UserClient.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{
				UserId:     id,
				PeerUserId: c.MD.UserId,
			})
			if err != nil {
				c.Logger.Errorf("account.updateStatus - block of user %d: %v", id, err)
				return
			}
			if !mtproto.FromBool(blocked) {
				mu.Lock()
				out = append(out, id)
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()

	return out, nil
}
