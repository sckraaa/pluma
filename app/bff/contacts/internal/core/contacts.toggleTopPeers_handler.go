// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsToggleTopPeers
// contacts.toggleTopPeers#8514bdda enabled:Bool = Bool;
//
// "Suggest frequent contacts": off, contacts.getTopPeers answers
// contacts.topPeersDisabled and the data is dropped; back on, the ratings
// count the messages from then on (user.toggleTopPeers).
func (c *ContactsCore) ContactsToggleTopPeers(in *mtproto.TLContactsToggleTopPeers) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.UserClient.UserToggleTopPeers(c.ctx, &userpb.TLUserToggleTopPeers{
		UserId:  c.MD.UserId,
		Enabled: in.GetEnabled(),
	}); err != nil {
		c.Logger.Errorf("contacts.toggleTopPeers - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
