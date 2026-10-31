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

// UserSetReactionsNotifySettings
// user.setReactionsNotifySettings user_id:long settings:ReactionsNotifySettings keep_poll_votes:Bool = ReactionsNotifySettings;
//
// The settings replace the user's reaction notification settings, an absent
// field meaning nobody, and come back as stored (task 020). With
// keep_poll_votes the stored poll votes setting stays. No update tells the
// other devices: the schema has none, and the clients ask again.
func (c *UserCore) UserSetReactionsNotifySettings(in *user.TLUserSetReactionsNotifySettings) (*mtproto.ReactionsNotifySettings, error) {
	if in.GetUserId() == 0 {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("user.setReactionsNotifySettings - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	settings, err := c.svcCtx.Dao.SetReactionsNotifySettings(c.ctx, in.GetUserId(), in.GetSettings(), mtproto.FromBool(in.GetKeepPollVotes()))
	if err != nil {
		c.Logger.Errorf("user.setReactionsNotifySettings - user %d: %v", in.GetUserId(), err)
		return nil, err
	}
	return settings, nil
}
