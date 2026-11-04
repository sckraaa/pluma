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
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/core"
)

// The boosts of channels (RPCBoosts): the module of channels serves them, as
// they are a channel's state.

var _ mtproto.RPCBoostsServer = (*Service)(nil)

// ChannelsSetBoostsToUnblockRestrictions
// channels.setBoostsToUnblockRestrictions
func (s *Service) ChannelsSetBoostsToUnblockRestrictions(ctx context.Context, request *mtproto.TLChannelsSetBoostsToUnblockRestrictions) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.setBoostsToUnblockRestrictions - metadata: {%s}", c.MD)
	return c.ChannelsSetBoostsToUnblockRestrictions(request)
}

// PremiumGetBoostsList
// premium.getBoostsList
func (s *Service) PremiumGetBoostsList(ctx context.Context, request *mtproto.TLPremiumGetBoostsList) (*mtproto.Premium_BoostsList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("premium.getBoostsList - metadata: {%s}", c.MD)
	return c.PremiumGetBoostsList(request)
}

// PremiumGetMyBoosts
// premium.getMyBoosts
func (s *Service) PremiumGetMyBoosts(ctx context.Context, request *mtproto.TLPremiumGetMyBoosts) (*mtproto.Premium_MyBoosts, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("premium.getMyBoosts - metadata: {%s}", c.MD)
	return c.PremiumGetMyBoosts(request)
}

// PremiumApplyBoost
// premium.applyBoost
func (s *Service) PremiumApplyBoost(ctx context.Context, request *mtproto.TLPremiumApplyBoost) (*mtproto.Premium_MyBoosts, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("premium.applyBoost - metadata: {%s}", c.MD)
	return c.PremiumApplyBoost(request)
}

// PremiumGetBoostsStatus
// premium.getBoostsStatus
func (s *Service) PremiumGetBoostsStatus(ctx context.Context, request *mtproto.TLPremiumGetBoostsStatus) (*mtproto.Premium_BoostsStatus, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("premium.getBoostsStatus - metadata: {%s}", c.MD)
	return c.PremiumGetBoostsStatus(request)
}

// PremiumGetUserBoosts
// premium.getUserBoosts
func (s *Service) PremiumGetUserBoosts(ctx context.Context, request *mtproto.TLPremiumGetUserBoosts) (*mtproto.Premium_BoostsList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("premium.getUserBoosts - metadata: {%s}", c.MD)
	return c.PremiumGetUserBoosts(request)
}

// StoriesGetBoostsStatus
// stories.getBoostsStatus
func (s *Service) StoriesGetBoostsStatus(ctx context.Context, request *mtproto.TLStoriesGetBoostsStatus) (*mtproto.Stories_BoostsStatus, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getBoostsStatus - metadata: {%s}", c.MD)
	return c.StoriesGetBoostsStatus(request)
}

// StoriesGetBoostersList
// stories.getBoostersList
func (s *Service) StoriesGetBoostersList(ctx context.Context, request *mtproto.TLStoriesGetBoostersList) (*mtproto.Stories_BoostersList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getBoostersList - metadata: {%s}", c.MD)
	return c.StoriesGetBoostersList(request)
}

// StoriesCanApplyBoost
// stories.canApplyBoost
func (s *Service) StoriesCanApplyBoost(ctx context.Context, request *mtproto.TLStoriesCanApplyBoost) (*mtproto.Stories_CanApplyBoostResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.canApplyBoost - metadata: {%s}", c.MD)
	return c.StoriesCanApplyBoost(request)
}

// StoriesApplyBoost
// stories.applyBoost
func (s *Service) StoriesApplyBoost(ctx context.Context, request *mtproto.TLStoriesApplyBoost) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.applyBoost - metadata: {%s}", c.MD)
	return c.StoriesApplyBoost(request)
}
