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

	"google.golang.org/grpc/status"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type DfsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *DfsCore {
	return &DfsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// partsError is the answer to a send whose upload cannot be opened: what
// the parts check found (FILE_PART_X_MISSING, FILE_PART_SIZE_CHANGED,
// FILE_PARTS_INVALID), as Telegram answers it, else MEDIA_INVALID.
func partsError(err error) error {
	if s, ok := status.FromError(err); ok && s.Code() == mtproto.ErrBadRequest {
		return err
	}
	return mtproto.ErrMediaInvalid
}
