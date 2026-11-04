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

package core

import (
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesClearRecentReactions
// messages.clearRecentReactions#9dfeefb4 = Bool;
//
// The account's recent reactions start again from now (task 053).
func (c *CatalogCore) MessagesClearRecentReactions(in *mtproto.TLMessagesClearRecentReactions) (*mtproto.Bool, error) {
	if err := reactions.ClearRecent(c.ctx, c.svcCtx.Reactions, c.MD.UserId, time.Now().Unix()); err != nil {
		c.Logger.Errorf("messages.clearRecentReactions - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
