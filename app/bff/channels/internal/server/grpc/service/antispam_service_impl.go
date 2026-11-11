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

// The antispam of supergroups (RPCAntiSpam, task 082, ADR 0050).

var _ mtproto.RPCAntiSpamServer = (*Service)(nil)

// ChannelsToggleAntiSpam
// channels.toggleAntiSpam#68f3e4eb channel:InputChannel enabled:Bool = Updates;
func (s *Service) ChannelsToggleAntiSpam(ctx context.Context, request *mtproto.TLChannelsToggleAntiSpam) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleAntiSpam - metadata: {%s}", c.MD)
	return c.ChannelsToggleAntiSpam(request)
}

// ChannelsReportAntiSpamFalsePositive
// channels.reportAntiSpamFalsePositive#a850a693 channel:InputChannel msg_id:int = Bool;
func (s *Service) ChannelsReportAntiSpamFalsePositive(ctx context.Context, request *mtproto.TLChannelsReportAntiSpamFalsePositive) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.reportAntiSpamFalsePositive - metadata: {%s}", c.MD)
	return c.ChannelsReportAntiSpamFalsePositive(request)
}
