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
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// ContactsToggleStoriesHidden
// contacts.toggleStoriesHidden#753fb865 id:InputUser hidden:Bool = Bool;
//
// The form of stories.togglePeerStoriesHidden of the layers that had
// stories of users only.
func (c *StoriesCore) ContactsToggleStoriesHidden(in *mtproto.TLContactsToggleStoriesHidden) (*mtproto.Bool, error) {
	if in.Id == nil {
		return nil, mtproto.ErrUserIdInvalid
	}
	p := mtproto.FromInputUser(c.MD.UserId, in.Id)
	if p.PeerType != mtproto.PEER_USER && p.PeerType != mtproto.PEER_SELF {
		return nil, mtproto.ErrUserIdInvalid
	}
	v, err := c.viewOf(stories.UserPeer(p.PeerId))
	if err != nil {
		c.Logger.Errorf("contacts.toggleStoriesHidden - error: %v", err)
		return nil, err
	}
	if err = c.setHidden(v, mtproto.FromBool(in.Hidden)); err != nil {
		c.Logger.Errorf("contacts.toggleStoriesHidden - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
