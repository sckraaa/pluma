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

// StoriesCanSendStoryC7DFDFDD
// stories.canSendStory#c7dfdfdd peer:InputPeer = Bool;
//
// The form of the layers before stories.canSendStoryCount: true when a
// story may be posted.
func (c *StoriesCore) StoriesCanSendStoryC7DFDFDD(in *mtproto.TLStoriesCanSendStoryC7DFDFDD) (*mtproto.Bool, error) {
	if _, err := c.canSend(in.Peer); err != nil {
		c.Logger.Errorf("stories.canSendStory#c7dfdfdd - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
