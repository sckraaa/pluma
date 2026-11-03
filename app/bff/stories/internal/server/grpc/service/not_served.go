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

// The methods of mtproto.RPCStories the server does not serve: Pluma has no
// stories yet. They answer ERR_ENTERPRISE_IS_BLOCKED (400): a client gives
// up on a 400, while it retries an internal error.

func (s *Service) StoriesCanSendStory30EB63F0(ctx context.Context, request *mtproto.TLStoriesCanSendStory30EB63F0) (*mtproto.Stories_CanSendStoryCount, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesSendStory(ctx context.Context, request *mtproto.TLStoriesSendStory) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesEditStory(ctx context.Context, request *mtproto.TLStoriesEditStory) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesDeleteStories(ctx context.Context, request *mtproto.TLStoriesDeleteStories) (*mtproto.Vector_Int, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesTogglePinned(ctx context.Context, request *mtproto.TLStoriesTogglePinned) (*mtproto.Vector_Int, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetPinnedStories(ctx context.Context, request *mtproto.TLStoriesGetPinnedStories) (*mtproto.Stories_Stories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetStoriesArchive(ctx context.Context, request *mtproto.TLStoriesGetStoriesArchive) (*mtproto.Stories_Stories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetStoriesByID(ctx context.Context, request *mtproto.TLStoriesGetStoriesByID) (*mtproto.Stories_Stories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesToggleAllStoriesHidden(ctx context.Context, request *mtproto.TLStoriesToggleAllStoriesHidden) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesReadStories(ctx context.Context, request *mtproto.TLStoriesReadStories) (*mtproto.Vector_Int, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesIncrementStoryViews(ctx context.Context, request *mtproto.TLStoriesIncrementStoryViews) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetStoryViewsList(ctx context.Context, request *mtproto.TLStoriesGetStoryViewsList) (*mtproto.Stories_StoryViewsList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetStoriesViews(ctx context.Context, request *mtproto.TLStoriesGetStoriesViews) (*mtproto.Stories_StoryViews, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesExportStoryLink(ctx context.Context, request *mtproto.TLStoriesExportStoryLink) (*mtproto.ExportedStoryLink, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesReport19D8EB45(ctx context.Context, request *mtproto.TLStoriesReport19D8EB45) (*mtproto.ReportResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesActivateStealthMode(ctx context.Context, request *mtproto.TLStoriesActivateStealthMode) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesSendReaction(ctx context.Context, request *mtproto.TLStoriesSendReaction) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetPeerStories(ctx context.Context, request *mtproto.TLStoriesGetPeerStories) (*mtproto.Stories_PeerStories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetAllReadPeerStories(ctx context.Context, request *mtproto.TLStoriesGetAllReadPeerStories) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetPeerMaxIDs78499170(ctx context.Context, request *mtproto.TLStoriesGetPeerMaxIDs78499170) (*mtproto.Vector_RecentStory, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetChatsToSend(ctx context.Context, request *mtproto.TLStoriesGetChatsToSend) (*mtproto.Messages_Chats, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesTogglePeerStoriesHidden(ctx context.Context, request *mtproto.TLStoriesTogglePeerStoriesHidden) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetStoryReactionsList(ctx context.Context, request *mtproto.TLStoriesGetStoryReactionsList) (*mtproto.Stories_StoryReactionsList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesTogglePinnedToTop(ctx context.Context, request *mtproto.TLStoriesTogglePinnedToTop) (*mtproto.Bool, error) {
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

func (s *Service) StoriesGetPeerMaxIDs535983C3(ctx context.Context, request *mtproto.TLStoriesGetPeerMaxIDs535983C3) (*mtproto.Vector_Int, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesCanSendStoryC7DFDFDD(ctx context.Context, request *mtproto.TLStoriesCanSendStoryC7DFDFDD) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesReport1923FA8C(ctx context.Context, request *mtproto.TLStoriesReport1923FA8C) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) UsersGetStoriesMaxIDs(ctx context.Context, request *mtproto.TLUsersGetStoriesMaxIDs) (*mtproto.Vector_Int, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ContactsToggleStoriesHidden(ctx context.Context, request *mtproto.TLContactsToggleStoriesHidden) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesCanSendStoryB100D45D(ctx context.Context, request *mtproto.TLStoriesCanSendStoryB100D45D) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetUserStories(ctx context.Context, request *mtproto.TLStoriesGetUserStories) (*mtproto.Stories_UserStories, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesGetAllReadUserStories(ctx context.Context, request *mtproto.TLStoriesGetAllReadUserStories) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) StoriesReportC95BE06A(ctx context.Context, request *mtproto.TLStoriesReportC95BE06A) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
