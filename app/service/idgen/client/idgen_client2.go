/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package idgen_client

import (
	"context"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/idgen/idgen"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type IDGenClient2 struct {
	cli IdgenClient
}

func NewIDGenClient2(cli zrpc.Client) IDGenClient2 {
	return IDGenClient2{
		cli: NewIdgenClient(cli),
	}
}

// NextId returns a new snowflake id (dialog_message_id, photo and document
// ids, grouped_id). An idgen failure is logged and returned as
// INTERNAL_SERVER_ERROR; the id is never 0.
func (m *IDGenClient2) NextId(ctx context.Context) (int64, error) {
	rVal, err := m.cli.IdgenNextId(ctx, &idgen.TLIdgenNextId{})
	if err != nil {
		logx.WithContext(ctx).Errorf("idgen.nextId - error: %v", err)
		return 0, mtproto.ErrInternalServerError
	}
	if rVal.GetV() == 0 {
		logx.WithContext(ctx).Errorf("idgen.nextId - returned 0")
		return 0, mtproto.ErrInternalServerError
	}
	return rVal.GetV(), nil
}

// NextIds returns num new snowflake ids, as NextId does.
func (m *IDGenClient2) NextIds(ctx context.Context, num int) ([]int64, error) {
	rVal, err := m.cli.IdgenNextIds(ctx, &idgen.TLIdgenNextIds{
		Num: int32(num),
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("idgen.nextIds - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	ids := rVal.GetDatas()
	if len(ids) != num || slices.Contains(ids, 0) {
		logx.WithContext(ctx).Errorf("idgen.nextIds - %d ids for %d, or a 0", len(ids), num)
		return nil, mtproto.ErrInternalServerError
	}
	return ids, nil
}
