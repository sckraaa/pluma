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

// StoriesCanSendStoryB100D45D
// stories.canSendStory#b100d45d = Bool;
//
// The oldest form, of the account's own stories only.
func (c *StoriesCore) StoriesCanSendStoryB100D45D(in *mtproto.TLStoriesCanSendStoryB100D45D) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	if _, err := c.svcCtx.Dao.Store.Remains(c.ctx, stories.UserPeer(c.MD.UserId), now()); err != nil {
		c.Logger.Errorf("stories.canSendStory#b100d45d - error: %v", err)
		return nil, limitError(err)
	}
	return mtproto.BoolTrue, nil
}
