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

var _ mtproto.RPCTakeoutServer = (*Service)(nil)

// ChannelsGetLeftChannels
// channels.getLeftChannels#8341ecc0
func (s *Service) ChannelsGetLeftChannels(ctx context.Context, request *mtproto.TLChannelsGetLeftChannels) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getLeftChannels - metadata: {%s}", c.MD)
	return c.ChannelsGetLeftChannels(request)
}

// AccountInitTakeoutSession
// account.initTakeoutSession#8ef3eab0
func (s *Service) AccountInitTakeoutSession(ctx context.Context, request *mtproto.TLAccountInitTakeoutSession) (*mtproto.Account_Takeout, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.initTakeoutSession - metadata: {%s}", c.MD)
	return c.AccountInitTakeoutSession(request)
}

// AccountFinishTakeoutSession
// account.finishTakeoutSession#1d2652ee
func (s *Service) AccountFinishTakeoutSession(ctx context.Context, request *mtproto.TLAccountFinishTakeoutSession) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.finishTakeoutSession - metadata: {%s}", c.MD)
	return c.AccountFinishTakeoutSession(request)
}

// MessagesGetSplitRanges
// messages.getSplitRanges#1cff7e08
func (s *Service) MessagesGetSplitRanges(ctx context.Context, request *mtproto.TLMessagesGetSplitRanges) (*mtproto.Vector_MessageRange, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getSplitRanges - metadata: {%s}", c.MD)
	return c.MessagesGetSplitRanges(request)
}
