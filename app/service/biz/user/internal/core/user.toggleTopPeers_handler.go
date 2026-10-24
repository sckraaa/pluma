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
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UserToggleTopPeers
// user.toggleTopPeers user_id:long enabled:Bool = Bool;
//
// contacts.toggleTopPeers. Turning top peers off drops their data, as the
// clients warn ("delete all data about the people you message
// frequently"): turned back on, they count the messages from then on.
// Turning on what is on changes nothing.
func (c *UserCore) UserToggleTopPeers(in *user.TLUserToggleTopPeers) (*mtproto.Bool, error) {
	on := mtproto.FromBool(in.Enabled)
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		if on {
			already, err := dao.TopPeersOn(b)
			if err != nil || already {
				return err
			}
		}
		return dao.SetTopPeers(b, on)
	})
	if err != nil {
		c.Logger.Errorf("user.toggleTopPeers - user %d: %v", in.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
