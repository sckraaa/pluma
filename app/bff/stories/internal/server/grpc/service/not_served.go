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
)

// The methods of mtproto.RPCStories the server does not serve: the albums
// of stories, live stories, the search of stories, hiding every story at
// once, and two forms of layers below the oldest the server speaks. They
// answer ERR_ENTERPRISE_IS_BLOCKED (400): a client gives up on a 400, while
// it retries an internal error.

func (s *Service) StoriesToggleAllStoriesHidden(ctx context.Context, request *mtproto.TLStoriesToggleAllStoriesHidden) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesSearchPosts(ctx context.Context, request *mtproto.TLStoriesSearchPosts) (*mtproto.Stories_FoundStories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesCreateAlbum(ctx context.Context, request *mtproto.TLStoriesCreateAlbum) (*mtproto.StoryAlbum, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesUpdateAlbum(ctx context.Context, request *mtproto.TLStoriesUpdateAlbum) (*mtproto.StoryAlbum, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesReorderAlbums(ctx context.Context, request *mtproto.TLStoriesReorderAlbums) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesDeleteAlbum(ctx context.Context, request *mtproto.TLStoriesDeleteAlbum) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetAlbums(ctx context.Context, request *mtproto.TLStoriesGetAlbums) (*mtproto.Stories_Albums, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetAlbumStories(ctx context.Context, request *mtproto.TLStoriesGetAlbumStories) (*mtproto.Stories_Stories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesStartLive(ctx context.Context, request *mtproto.TLStoriesStartLive) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetUserStories(ctx context.Context, request *mtproto.TLStoriesGetUserStories) (*mtproto.Stories_UserStories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetAllReadUserStories(ctx context.Context, request *mtproto.TLStoriesGetAllReadUserStories) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
