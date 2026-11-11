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

var _ mtproto.RPCChannelRecommendationsServer = (*Service)(nil)

// ChannelsGetChannelRecommendations
// channels.getChannelRecommendations#25a71742
func (s *Service) ChannelsGetChannelRecommendations(ctx context.Context, request *mtproto.TLChannelsGetChannelRecommendations) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getChannelRecommendations - metadata: {%s}", c.MD)
	return c.ChannelsGetChannelRecommendations(request)
}
