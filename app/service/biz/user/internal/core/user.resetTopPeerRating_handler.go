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

// UserResetTopPeerRating
// user.resetTopPeerRating user_id:long category:TopPeerCategory peer_type:int peer_id:long = Bool;
//
// contacts.resetTopPeerRating: only the messages sent to the peer after
// now count for it in the category.
func (c *UserCore) UserResetTopPeerRating(in *user.TLUserResetTopPeerRating) (*mtproto.Bool, error) {
	category, ok := topPeerCategory(in.Category)
	if !ok {
		err := mtproto.ErrInputConstructorInvalid
		c.Logger.Errorf("user.resetTopPeerRating - category %s: %v", in.Category.GetPredicateName(), err)
		return nil, err
	}
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		return dao.ResetTopPeer(b, category, in.PeerType, in.PeerId)
	})
	if err != nil {
		c.Logger.Errorf("user.resetTopPeerRating - user %d: %v", in.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
