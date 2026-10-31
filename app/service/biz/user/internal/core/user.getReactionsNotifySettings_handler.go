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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserGetReactionsNotifySettings
// user.getReactionsNotifySettings user_id:long = ReactionsNotifySettings;
//
// The reaction notification settings of the user, the defaults while the
// user has set none (task 020).
func (c *UserCore) UserGetReactionsNotifySettings(in *user.TLUserGetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	settings, err := c.svcCtx.Dao.GetReactionsNotifySettings(c.ctx, in.GetUserId())
	if err != nil {
		c.Logger.Errorf("user.getReactionsNotifySettings - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return settings, nil
}
