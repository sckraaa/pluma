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

// Paid messages (RPCPaidMessage). Of them the server serves the switch of a
// channel's direct messages, which costs nothing (task 084, ADR 0055);
// messages are not paid for here, so the methods of the payments answer
// ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not retry.

var _ mtproto.RPCPaidMessageServer = (*Service)(nil)

// ChannelsUpdatePaidMessagesPrice
// channels.updatePaidMessagesPrice#4b12327b flags:# broadcast_messages_allowed:flags.0?true channel:InputChannel send_paid_messages_stars:long = Updates;
func (s *Service) ChannelsUpdatePaidMessagesPrice(ctx context.Context, request *mtproto.TLChannelsUpdatePaidMessagesPrice) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.updatePaidMessagesPrice - metadata: {%s}", c.MD)
	return c.ChannelsUpdatePaidMessagesPrice(request)
}

// AccountGetPaidMessagesRevenue is not served: account.getPaidMessagesRevenue#19ba4a67.
func (s *Service) AccountGetPaidMessagesRevenue(context.Context, *mtproto.TLAccountGetPaidMessagesRevenue) (*mtproto.Account_PaidMessagesRevenue, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountToggleNoPaidMessagesException is not served: account.toggleNoPaidMessagesException#fe2eda76.
func (s *Service) AccountToggleNoPaidMessagesException(context.Context, *mtproto.TLAccountToggleNoPaidMessagesException) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountAddNoPaidMessagesException is not served: account.addNoPaidMessagesException,
// of the layers before 228.
func (s *Service) AccountAddNoPaidMessagesException(context.Context, *mtproto.TLAccountAddNoPaidMessagesException) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
