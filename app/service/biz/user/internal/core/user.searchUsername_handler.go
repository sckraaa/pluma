// Copyright 2025 Teamgram Authors
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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserSearchUsername
// user.searchUsername q:string excluded_contacts:Vector<long> limit:int = Vector<UsernameData>;
func (c *UserCore) UserSearchUsername(in *user.TLUserSearchUsername) (*user.Vector_UsernameData, error) {
	var (
		rValList = &user.Vector_UsernameData{}
	)

	// Check query string and limit
	if len(in.Q) < 3 || in.Limit <= 0 {
		return rValList, nil
	}

	if in.Limit > 50 {
		in.Limit = 50
	}

	matches, err := c.svcCtx.Dao.SearchUsernames(c.ctx, in.Q, in.ExcludedContacts, in.Limit)
	if err != nil {
		c.Logger.Errorf("user.searchUsername - error: %v", err)
		return nil, err
	}
	for _, m := range matches {
		var peer *mtproto.Peer
		switch m.PeerType {
		case mtproto.PEER_USER:
			peer = mtproto.MakePeerUser(m.PeerId)
		case mtproto.PEER_CHANNEL:
			peer = mtproto.MakePeerChannel(m.PeerId)
		default:
			continue
		}
		rValList.Datas = append(rValList.Datas, user.MakeTLUsernameData(&user.UsernameData{
			Username: m.Username,
			Peer:     peer,
		}).To_UsernameData())
	}

	return rValList, nil
}
