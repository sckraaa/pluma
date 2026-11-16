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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"
)

// Service is RPCStatistics. The methods not served answer
// ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not retry, never
// gRPC Unimplemented (500), which tgnet retries.
type Service struct {
	svcCtx *svc.ServiceContext
}

func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

func (s *Service) StatsGetBroadcastStats(ctx context.Context, request *mtproto.TLStatsGetBroadcastStats) (*mtproto.Stats_BroadcastStats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getBroadcastStats - metadata: {%s}", c.MD)
	return c.StatsGetBroadcastStats(request)
}

func (s *Service) StatsLoadAsyncGraph(ctx context.Context, request *mtproto.TLStatsLoadAsyncGraph) (*mtproto.StatsGraph, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.loadAsyncGraph - metadata: {%s}", c.MD)
	return c.StatsLoadAsyncGraph(request)
}

func (s *Service) StatsGetMessageStats(ctx context.Context, request *mtproto.TLStatsGetMessageStats) (*mtproto.Stats_MessageStats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getMessageStats - metadata: {%s}", c.MD)
	return c.StatsGetMessageStats(request)
}

func (s *Service) StatsGetMegagroupStats(ctx context.Context, request *mtproto.TLStatsGetMegagroupStats) (*mtproto.Stats_MegagroupStats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getMegagroupStats - metadata: {%s}", c.MD)
	return c.StatsGetMegagroupStats(request)
}

func (s *Service) StatsGetMessagePublicForwards5F150144(ctx context.Context, request *mtproto.TLStatsGetMessagePublicForwards5F150144) (*mtproto.Stats_PublicForwards, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getMessagePublicForwards - metadata: {%s}", c.MD)
	return c.StatsGetMessagePublicForwards5F150144(request)
}

func (s *Service) StatsGetStoryStats(ctx context.Context, request *mtproto.TLStatsGetStoryStats) (*mtproto.Stats_StoryStats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getStoryStats - metadata: {%s}", c.MD)
	return c.StatsGetStoryStats(request)
}

func (s *Service) StatsGetStoryPublicForwards(ctx context.Context, request *mtproto.TLStatsGetStoryPublicForwards) (*mtproto.Stats_PublicForwards, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getStoryPublicForwards - metadata: {%s}", c.MD)
	return c.StatsGetStoryPublicForwards(request)
}

func (s *Service) StatsGetPollStats(ctx context.Context, request *mtproto.TLStatsGetPollStats) (*mtproto.Stats_PollStats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stats.getPollStats - metadata: {%s}", c.MD)
	return c.StatsGetPollStats(request)
}

// StatsGetMessagePublicForwards5630281B is the method of the layers before
// stats.publicForwards; the clients of layer 233 call #5f150144 (ADR 0066,
// "Что не делается").
func (s *Service) StatsGetMessagePublicForwards5630281B(context.Context, *mtproto.TLStatsGetMessagePublicForwards5630281B) (*mtproto.Messages_Messages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
