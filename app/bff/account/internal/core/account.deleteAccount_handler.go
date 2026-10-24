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
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

// AccountDeleteAccount
// account.deleteAccount#a2c0cf74 flags:# reason:string password:flags.0?InputCheckPasswordSRP = Bool;
//
// Deletes the account of the logged-in user or, on a key whose login waits
// for the 2FA password, of the user logging in: Telegram's reset of an
// account whose password is forgotten. An account with a password is
// deleted at once only with the password; without it the deletion waits a
// week (2FA_CONFIRM_WAIT_X, pkg/twofa Service.CheckDeletion), the user's
// devices are told, and the password given meanwhile cancels it. After the
// reset the key signs up again with the code it signed in with.
func (c *AccountCore) AccountDeleteAccount(in *mtproto.TLAccountDeleteAccount) (*mtproto.Bool, error) {
	var (
		userId = c.MD.UserId
		login  *twofa.Login
		err    error
	)
	if userId == 0 {
		if login, err = c.svcCtx.Twofa.WaitingLogin(c.ctx, c.MD.PermAuthKeyId); err != nil {
			return nil, err
		}
		if login == nil {
			return nil, mtproto.ErrAuthKeyUnregistered
		}
		userId = login.UserId
	}

	started, err := c.svcCtx.Twofa.CheckDeletion(c.ctx, userId, c.MD.PermAuthKeyId, in.GetPassword())
	if started {
		c.notifyDeletionWait(userId)
	}
	if err != nil {
		c.Logger.Errorf("account.deleteAccount - user %d: %v", userId, err)
		return nil, err
	}

	me, err := c.svcCtx.Dao.UserClient.UserGetUserDataById(c.ctx, &user.TLUserGetUserDataById{
		UserId: userId,
	})
	if err != nil {
		c.Logger.Errorf("account.deleteAccount - error: %v", err)
		return nil, err
	}

	if me.Username != "" {
		_, err = c.svcCtx.Dao.UserClient.UserDeleteUsername(c.ctx, &user.TLUserDeleteUsername{
			Username: me.Username,
		})
		if err != nil {
			c.Logger.Errorf("account.deleteAccount - error: %v", err)
			return nil, err
		}
	}

	// TODO(@benqi): 1. Clear account data 2. Kickoff other client
	_, err = c.svcCtx.UserClient.UserDeleteUser(c.ctx, &user.TLUserDeleteUser{
		UserId: userId,
		Reason: in.Reason,
		Phone:  me.Phone,
	})
	if err != nil {
		c.Logger.Errorf("account.deleteAccount - error: %v", err)
		return nil, err
	}
	c.Logger.Infof("account.deleteAccount - user %d deleted", userId)
	if err = c.svcCtx.Twofa.Forget(c.ctx, userId); err != nil {
		return nil, err
	}

	// s.AuthSessionRpcClient
	tKeyIdList, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionResetAuthorization(c.ctx, &authsession.TLAuthsessionResetAuthorization{
		UserId:    userId,
		AuthKeyId: 0,
		Hash:      0,
	})
	if err != nil {
		c.Logger.Errorf("account.resetAuthorization#df77f3bc - error: %v", err)
		return nil, err
	}

	for _, id := range tKeyIdList.Datas {
		// notify kill session
		upds := mtproto.MakeTLUpdateAccountResetAuthorization(&mtproto.Updates{
			UserId:    userId,
			AuthKeyId: id,
		}).To_Updates()
		_, _ = c.svcCtx.Dao.SyncClient.SyncUpdatesMe(
			c.ctx,
			&sync.TLSyncUpdatesMe{
				UserId:        userId,
				PermAuthKeyId: id,
				ServerId:      nil,
				AuthKeyId:     nil,
				SessionId:     nil,
				Updates:       upds,
			})
	}

	_, _ = c.svcCtx.Dao.AuthsessionClient.AuthsessionUnbindAuthKeyUser(c.ctx, &authsession.TLAuthsessionUnbindAuthKeyUser{
		AuthKeyId: 0,
		UserId:    userId,
	})

	if login != nil {
		c.svcCtx.Twofa.EndLogin(c.ctx, c.MD.PermAuthKeyId)
		if login.Phone != "" {
			if err = c.svcCtx.AuthLogic.AllowSignUp(c.ctx, c.MD.PermAuthKeyId, login.Phone); err != nil {
				c.Logger.Errorf("account.deleteAccount - sign-up after the reset: %v", err)
			}
		}
	}

	return mtproto.BoolTrue, nil
}

// notifyDeletionWait tells userId's devices that a deletion of the account
// without the password waits (2FA_CONFIRM_WAIT_X), as Telegram does, and how
// to cancel it.
func (c *AccountCore) notifyDeletionWait(userId int64) {
	now := time.Now()
	until := now.Add(twofa.DeleteWait).UTC().Format("2006-01-02 15:04")
	n := mtproto.MakeTLUpdateServiceNotification(&mtproto.Update{
		Popup:     false,
		InboxDate: mtproto.MakeFlagsInt32(int32(now.Unix())),
		Type:      fmt.Sprintf("delete%d_%d", c.MD.PermAuthKeyId, now.Unix()),
		Media:     mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia(),
	}).To_Update()
	n.Message_STRING, n.Entities = mtproto.MakeTextAndMessageEntities([]mtproto.MessageBuildEntry{
		{
			Param:      "Account deletion requested.",
			EntityType: mtproto.Predicate_messageEntityBold,
		},
		{
			Text: fmt.Sprintf("\n\nSomeone who logged in with your phone number asked to delete your account without the two-step verification password. "+
				"The account will be deleted after %s UTC.\n\nIf this wasn't you, enter your password in ", until),
			Param:      "Settings > Privacy and Security > Two-Step Verification",
			EntityType: mtproto.Predicate_messageEntityBold,
		},
		{
			Text: ": that cancels the deletion.",
		},
	})
	if _, err := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        userId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(n),
	}); err != nil {
		c.Logger.Errorf("account.deleteAccount - tell user %d: %v", userId, err)
	}
}
