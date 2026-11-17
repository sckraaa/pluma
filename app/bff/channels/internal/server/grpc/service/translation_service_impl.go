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

// channels.toggleAutotranslation is a setting of the channel (task 082,
// ADR 0050), served here with the other settings of channels. Its gRPC
// service, RPCTranslation, is registered by the translation area (task 101,
// ADR 0064), which calls this method through its AutotranslationToggler.

// ChannelsToggleAutotranslation
// channels.toggleAutotranslation#167fc0a1 channel:InputChannel enabled:Bool = Updates;
func (s *Service) ChannelsToggleAutotranslation(ctx context.Context, request *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleAutotranslation - metadata: {%s}", c.MD)
	return c.ChannelsToggleAutotranslation(request)
}
