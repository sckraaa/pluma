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

// StoriesGetPeerMaxIDs535983C3
// stories.getPeerMaxIDs#535983c3 id:Vector<InputPeer> = Vector<int>;
//
// The form of the layers before recentStory: the ids themselves, 0 for a
// peer without a story the caller may open.
func (c *StoriesCore) StoriesGetPeerMaxIDs535983C3(in *mtproto.TLStoriesGetPeerMaxIDs535983C3) (*mtproto.Vector_Int, error) {
	ids, err := c.peerMaxIds(in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getPeerMaxIDs#535983c3 - error: %v", err)
		return nil, err
	}
	return &mtproto.Vector_Int{Datas: ids}, nil
}
