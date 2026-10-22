/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2022 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package idgen_client

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/idgen/idgen"

	"github.com/zeromicro/go-zero/zrpc"
)

var _ *mtproto.Bool

type IdgenClient interface {
	IdgenNextId(ctx context.Context, in *idgen.TLIdgenNextId) (*mtproto.Int64, error)
	IdgenNextIds(ctx context.Context, in *idgen.TLIdgenNextIds) (*idgen.Vector_Long, error)
}

type defaultIdgenClient struct {
	cli zrpc.Client
}

func NewIdgenClient(cli zrpc.Client) IdgenClient {
	return &defaultIdgenClient{
		cli: cli,
	}
}

// IdgenNextId
// idgen.nextId = Int64;
func (m *defaultIdgenClient) IdgenNextId(ctx context.Context, in *idgen.TLIdgenNextId) (*mtproto.Int64, error) {
	client := idgen.NewRPCIdgenClient(m.cli.Conn())
	return client.IdgenNextId(ctx, in)
}

// IdgenNextIds
// idgen.nextIds num:int = Vector<long>;
func (m *defaultIdgenClient) IdgenNextIds(ctx context.Context, in *idgen.TLIdgenNextIds) (*idgen.Vector_Long, error) {
	client := idgen.NewRPCIdgenClient(m.cli.Conn())
	return client.IdgenNextIds(ctx, in)
}
