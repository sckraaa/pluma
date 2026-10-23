/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"context"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
	"github.com/teamgram/teamgram-server/app/messenger/sync/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/status/status"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type SyncType int

const (
	syncTypeUser      SyncType = 1 // 该用户所有设备
	syncTypeUserNotMe SyncType = 2 // 该用户除了某个设备
	syncTypeUserMe    SyncType = 3 // 该用户指定某个设备
)

type SyncCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *SyncCore {
	return &SyncCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

func (c *SyncCore) processUpdates(syncType SyncType, userId int64, isBot bool, ups *mtproto.Updates) (needPush bool, err error) {
	mtproto.VisitUpdates(userId, ups, map[string]mtproto.UpdateVisitedFunc{
		mtproto.Predicate_updateNewMessage: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateDeleteMessages: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateReadHistoryInbox: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateReadHistoryOutbox: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateWebPage: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateReadMessagesContents: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updateEditMessage: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
		mtproto.Predicate_updatePhoneCall: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			if update.GetPhoneCall().GetPredicateName() == mtproto.Predicate_phoneCallRequested {
				// log.Debugf("recv phoneCallRequested")
				needPush = true
			}
		},
		mtproto.Predicate_updatePeerSettings: func(
			userId int64,
			update *mtproto.Update,
			users []*mtproto.User,
			chats []*mtproto.Chat,
			date int32,
		) {
			needPush = true
		},
	})

	return needPush, nil
}

func (c *SyncCore) pushUpdatesToSession(syncType SyncType, userId, permAuthKeyId int64, hasServerId *wrapperspb.StringValue, authKeyId, sessionId *wrapperspb.Int64Value, pushData *mtproto.Updates, notification bool) {
	if syncType == syncTypeUserMe && hasServerId != nil {
		if sessionId != nil {
			c.svcCtx.Dao.PushSessionUpdates(
				c.ctx,
				hasServerId.GetValue(),
				&session.TLSessionPushSessionUpdatesData{
					PermAuthKeyId: permAuthKeyId,
					AuthKeyId:     authKeyId.GetValue(),
					SessionId:     sessionId.GetValue(),
					Updates:       pushData,
				})
		} else {
			c.svcCtx.Dao.PushUpdates(
				c.ctx,
				hasServerId.GetValue(),
				&session.TLSessionPushUpdatesData{
					PermAuthKeyId: permAuthKeyId,
					Notification:  notification,
					Updates:       pushData,
				})
		}
		return
	}

	sessions, err := c.onlineSessions(userId)
	if err != nil {
		// The devices catch up with getDifference on their next update or
		// reconnect.
		c.Logger.Errorf("sync: online sessions of user %d: %v; push dropped", userId, err)
		return
	}
	for _, sess := range sessions {
		if syncType == syncTypeUserNotMe && sess.AuthKeyId == permAuthKeyId {
			continue
		}
		c.svcCtx.Dao.PushUpdates(
			c.ctx,
			sess.Gateway,
			&session.TLSessionPushUpdatesData{
				PermAuthKeyId: sess.AuthKeyId,
				Notification:  notification,
				Updates:       pushData,
			})
	}
}

// Online sessions are looked up with a short deadline and retried for a
// while (ADR 0012, p. 7): pushes of one partition wait for each other, so
// a slow status service must not hold them for long.
const (
	onlineSessionsCallTimeout = 2 * time.Second
	onlineSessionsRetryFor    = 10 * time.Second
	onlineSessionsRetryDelay  = 200 * time.Millisecond
)

func (c *SyncCore) onlineSessions(userId int64) ([]*status.SessionEntry, error) {
	deadline := time.Now().Add(onlineSessionsRetryFor)
	for {
		ctx, cancel := context.WithTimeout(c.ctx, onlineSessionsCallTimeout)
		list, err := c.svcCtx.Dao.StatusClient.StatusGetUserOnlineSessions(ctx, &status.TLStatusGetUserOnlineSessions{
			UserId: userId,
		})
		cancel()
		if err == nil {
			return list.GetUserSessions(), nil
		}
		if time.Now().Add(onlineSessionsRetryDelay).After(deadline) {
			return nil, err
		}
		time.Sleep(onlineSessionsRetryDelay)
	}
}
