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
)

// UsersGetStoriesMaxIDs
// users.getStoriesMaxIDs#ca1cb9ab id:Vector<InputUser> = Vector<int>;
//
// The form of stories.getPeerMaxIDs of the layers that had stories of
// users only.
func (c *StoriesCore) UsersGetStoriesMaxIDs(in *mtproto.TLUsersGetStoriesMaxIDs) (*mtproto.Vector_Int, error) {
	peers := make([]*mtproto.InputPeer, 0, len(in.Id))
	for _, u := range in.Id {
		if u == nil {
			return nil, mtproto.ErrUserIdInvalid
		}
		peers = append(peers, mtproto.FromInputUser(c.MD.UserId, u).ToInputPeer())
	}
	ids, err := c.peerMaxIds(peers)
	if err != nil {
		c.Logger.Errorf("users.getStoriesMaxIDs - error: %v", err)
		return nil, err
	}
	return &mtproto.Vector_Int{Datas: ids}, nil
}
