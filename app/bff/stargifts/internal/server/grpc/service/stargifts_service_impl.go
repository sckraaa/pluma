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
	"github.com/teamgram/teamgram-server/app/bff/stargifts/internal/core"
)

// PaymentsGetSavedStarGifts
// payments.getSavedStarGifts#a319e569 flags:# exclude_unsaved:flags.0?true exclude_saved:flags.1?true exclude_unlimited:flags.2?true exclude_unique:flags.4?true sort_by_value:flags.5?true exclude_upgradable:flags.7?true exclude_unupgradable:flags.8?true peer_color_available:flags.9?true exclude_hosted:flags.10?true peer:InputPeer collection_id:flags.6?int offset:string limit:int = payments.SavedStarGifts;
func (s *Service) PaymentsGetSavedStarGifts(ctx context.Context, request *mtproto.TLPaymentsGetSavedStarGifts) (*mtproto.Payments_SavedStarGifts, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("payments.getSavedStarGifts - metadata: {%s}", c.MD)
	return c.PaymentsGetSavedStarGifts(request)
}
