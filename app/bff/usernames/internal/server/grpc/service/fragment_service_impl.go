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
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/core"
)

// mtproto.RPCFragment: the usernames beside the editable one (task 078).
// Those of channels are served; those of accounts and bots not yet, and
// answer ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not retry.

// AccountReorderUsernames is not served: account.reorderUsernames#ef500eab order:Vector<string> = Bool;
func (s *Service) AccountReorderUsernames(ctx context.Context, request *mtproto.TLAccountReorderUsernames) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountToggleUsername is not served: account.toggleUsername#58d6b376 username:string active:Bool = Bool;
func (s *Service) AccountToggleUsername(ctx context.Context, request *mtproto.TLAccountToggleUsername) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsReorderUsernames
// channels.reorderUsernames#b45ced1d channel:InputChannel order:Vector<string> = Bool;
func (s *Service) ChannelsReorderUsernames(ctx context.Context, request *mtproto.TLChannelsReorderUsernames) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.reorderUsernames - metadata: {%s}", c.MD)
	return c.ChannelsReorderUsernames(request)
}

// ChannelsToggleUsername
// channels.toggleUsername#50f24105 channel:InputChannel username:string active:Bool = Bool;
func (s *Service) ChannelsToggleUsername(ctx context.Context, request *mtproto.TLChannelsToggleUsername) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleUsername - metadata: {%s}", c.MD)
	return c.ChannelsToggleUsername(request)
}

// ChannelsDeactivateAllUsernames
// channels.deactivateAllUsernames#a245dd3 channel:InputChannel = Bool;
func (s *Service) ChannelsDeactivateAllUsernames(ctx context.Context, request *mtproto.TLChannelsDeactivateAllUsernames) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deactivateAllUsernames - metadata: {%s}", c.MD)
	return c.ChannelsDeactivateAllUsernames(request)
}

// BotsReorderUsernames is not served: bots.reorderUsernames#9709b1c2 bot:InputUser order:Vector<string> = Bool;
func (s *Service) BotsReorderUsernames(ctx context.Context, request *mtproto.TLBotsReorderUsernames) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsToggleUsername is not served: bots.toggleUsername#53ca973 bot:InputUser username:string active:Bool = Bool;
func (s *Service) BotsToggleUsername(ctx context.Context, request *mtproto.TLBotsToggleUsername) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
