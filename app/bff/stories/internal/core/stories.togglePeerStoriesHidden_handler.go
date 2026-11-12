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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// StoriesTogglePeerStoriesHidden
// stories.togglePeerStoriesHidden#bd0415c4 peer:InputPeer hidden:Bool = Bool;
//
// Moves the stories of a peer to the archive row of the caller, or back
// (Android StoriesController.java:355-372 sets stories_hidden of the user
// or the channel and sends this). Of a user it is a flag of the caller's
// contact, which the service of users keeps and shows as
// user.stories_hidden; the stories of a user who is not a contact are in
// no row, so there is nothing to keep for one. Of a channel it is the
// caller's own mark, shown as channel.stories_hidden.
func (c *StoriesCore) StoriesTogglePeerStoriesHidden(in *mtproto.TLStoriesTogglePeerStoriesHidden) (*mtproto.Bool, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.togglePeerStoriesHidden - error: %v", err)
		return nil, err
	}
	if err = c.setHidden(v, mtproto.FromBool(in.Hidden)); err != nil {
		c.Logger.Errorf("stories.togglePeerStoriesHidden - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

func (c *StoriesCore) setHidden(v *peerView, hidden bool) error {
	if v.own {
		return mtproto.ErrPeerIdInvalid
	}
	if !v.peer.IsUser() {
		return c.svcCtx.Dao.Store.SetHidden(c.ctx, c.MD.UserId, v.peer, hidden)
	}
	_, err := c.svcCtx.Dao.UserSetStoriesHidden(c.ctx, &userpb.TLUserSetStoriesHidden{
		UserId: c.MD.UserId,
		Id:     v.peer.Id,
		Hidden: mtproto.ToBool(hidden),
	})
	return err
}
