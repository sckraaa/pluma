/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2022 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/idgen/idgen"
	"github.com/teamgram/teamgram-server/app/service/idgen/internal/core"
)

// IdgenNextId
// idgen.nextId = Int64;
func (s *Service) IdgenNextId(ctx context.Context, request *idgen.TLIdgenNextId) (*mtproto.Int64, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("idgen.nextId - metadata: %s", c.MD)

	r, err := c.IdgenNextId(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// IdgenNextIds
// idgen.nextIds num:int = Vector<long>;
func (s *Service) IdgenNextIds(ctx context.Context, request *idgen.TLIdgenNextIds) (*idgen.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("idgen.nextIds - metadata: %s", c.MD)

	r, err := c.IdgenNextIds(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

func (s *Service) mustEmbedUnimplementedMyService() {}
