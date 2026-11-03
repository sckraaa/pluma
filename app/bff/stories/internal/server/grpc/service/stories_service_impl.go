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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/core"
)

// StoriesGetAllStories
// stories.getAllStories#eeb0d625 flags:# next:flags.1?true hidden:flags.2?true state:flags.0?string = stories.AllStories;
func (s *Service) StoriesGetAllStories(ctx context.Context, request *mtproto.TLStoriesGetAllStories) (*mtproto.Stories_AllStories, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getAllStories - metadata: {%s}", c.MD)
	return c.StoriesGetAllStories(request)
}
