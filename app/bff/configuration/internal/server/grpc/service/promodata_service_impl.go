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
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/core"
)

// The Service serves mtproto.RPCPromoData too: the suggestions of the
// account (task 020).

// HelpGetPromoData
// help.getPromoData#c0977421 = help.PromoData;
func (s *Service) HelpGetPromoData(ctx context.Context, request *mtproto.TLHelpGetPromoData) (*mtproto.Help_PromoData, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.getPromoData - metadata: {%s}", c.MD)
	return c.HelpGetPromoData(request)
}

// HelpHidePromoData
// help.hidePromoData#1e251c95 peer:InputPeer = Bool;
func (s *Service) HelpHidePromoData(ctx context.Context, request *mtproto.TLHelpHidePromoData) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.hidePromoData - metadata: {%s}", c.MD)
	return c.HelpHidePromoData(request)
}
