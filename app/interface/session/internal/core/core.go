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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/sess"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/svc"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"

	"github.com/zeromicro/go-zero/core/logx"
)

type SessionCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *SessionCore {
	return &SessionCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// getOrFetchClientMainAuthWrapper returns the wrapper that serves traffic of
// the auth key authKeyId, routed as session.RouteKey describes. A temporary
// key that is not bound yet gets its own restricted wrapper; it is never
// attributed to a permanent key here: permAuthKeyId comes from authsession
// through the gateway and is set only after a verified auth.bindTempAuthKey.
func (c *SessionCore) getOrFetchClientMainAuthWrapper(authKeyId int64, keyType int32, permAuthKeyId int64) (*sess.MainAuthWrapper, error) {
	routeKey, unboundTemp := session.RouteKey(authKeyId, keyType, permAuthKeyId)
	if routeKey == 0 {
		return nil, mtproto.ErrInputRequestInvalid
	}

	if unboundTemp {
		return c.svcCtx.MainAuthMgr.AllocMainAuthWrapper(
			routeKey,
			func(authKeyId int64) *sess.MainAuthWrapper {
				return sess.NewUnboundTempAuthWrapper(authKeyId, c.svcCtx.MainAuthMgr)
			}), nil
	}

	return c.getOrFetchMainAuthWrapper(routeKey)
}

func (c *SessionCore) getOrFetchMainAuthWrapper(mainAuthId int64) (*sess.MainAuthWrapper, error) {
	if mainAuthId == 0 {
		return nil, mtproto.ErrInputRequestInvalid
	}

	mainAuth := c.svcCtx.MainAuthMgr.GetMainAuthWrapper(mainAuthId)
	if mainAuth != nil {
		return mainAuth, nil
	}

	kData, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionGetAuthStateData(c.ctx, &authsession.TLAuthsessionGetAuthStateData{
		AuthKeyId: mainAuthId,
	})
	if err != nil {
		c.Logger.Errorf("getOrFetchMainAuthWrapper - error: %v", err)
		return nil, err
	}

	mainAuth = c.svcCtx.MainAuthMgr.AllocMainAuthWrapper(
		mainAuthId,
		func(authKeyId int64) *sess.MainAuthWrapper {
			return sess.NewMainAuthWrapper(
				mainAuthId,
				kData.UserId,
				int(kData.KeyState),
				kData.Client,
				kData.GetAndroidPushSessionId().GetValue(),
				c.svcCtx.MainAuthMgr)
		})

	return mainAuth, nil
}
