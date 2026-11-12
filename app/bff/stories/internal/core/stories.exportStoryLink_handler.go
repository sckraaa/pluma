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
	"strconv"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// StoriesExportStoryLink
// stories.exportStoryLink#7b8def20 peer:InputPeer id:int = ExportedStoryLink;
//
// The link of a story: <host>/<username>/s/<id>, the form the clients
// build themselves (Android PeerStoriesView.java:7119, 7128), on the host
// of the server's links (ADR 0036). A peer without a public username has
// none.
func (c *StoriesCore) StoriesExportStoryLink(in *mtproto.TLStoriesExportStoryLink) (*mtproto.ExportedStoryLink, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.exportStoryLink - error: %v", err)
		return nil, err
	}
	st, err := c.one(v, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.exportStoryLink - error: %v", err)
		return nil, err
	}
	var username string
	switch {
	case v.channel != nil:
		username = v.channel.GetChannel().GetUsername()
	case v.owner != nil:
		username = v.owner.Username()
	default:
		self, err := c.svcCtx.Dao.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
		if err != nil {
			c.Logger.Errorf("stories.exportStoryLink - error: %v", err)
			return nil, err
		}
		username = self.Username()
	}
	if username == "" {
		return nil, errUserPublicMissing
	}
	return mtproto.MakeTLExportedStoryLink(&mtproto.ExportedStoryLink{
		Link: "https://" + env2.TDotMe + "/" + username + "/s/" + strconv.Itoa(int(st.Id)),
	}).To_ExportedStoryLink(), nil
}
