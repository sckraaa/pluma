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

// StoriesCanSendStory30EB63F0
// stories.canSendStory#30eb63f0 peer:InputPeer = stories.CanSendStoryCount;
func (s *Service) StoriesCanSendStory30EB63F0(ctx context.Context, request *mtproto.TLStoriesCanSendStory30EB63F0) (*mtproto.Stories_CanSendStoryCount, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.canSendStory - metadata: {%s}", c.MD)
	return c.StoriesCanSendStory30EB63F0(request)
}

// StoriesCanSendStoryC7DFDFDD
// stories.canSendStory#c7dfdfdd peer:InputPeer = Bool;
func (s *Service) StoriesCanSendStoryC7DFDFDD(ctx context.Context, request *mtproto.TLStoriesCanSendStoryC7DFDFDD) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.canSendStory#c7dfdfdd - metadata: {%s}", c.MD)
	return c.StoriesCanSendStoryC7DFDFDD(request)
}

// StoriesCanSendStoryB100D45D
// stories.canSendStory#b100d45d = Bool;
func (s *Service) StoriesCanSendStoryB100D45D(ctx context.Context, request *mtproto.TLStoriesCanSendStoryB100D45D) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.canSendStory#b100d45d - metadata: {%s}", c.MD)
	return c.StoriesCanSendStoryB100D45D(request)
}

// StoriesSendStory
// stories.sendStory#8f9e6898 flags:# pinned:flags.2?true noforwards:flags.4?true fwd_modified:flags.7?true peer:InputPeer media:InputMedia media_areas:flags.5?Vector<MediaArea> caption:flags.0?string entities:flags.1?Vector<MessageEntity> privacy_rules:Vector<InputPrivacyRule> random_id:long period:flags.3?int fwd_from_id:flags.6?InputPeer fwd_from_story:flags.6?int albums:flags.8?Vector<int> music:flags.9?InputDocument = Updates;
func (s *Service) StoriesSendStory(ctx context.Context, request *mtproto.TLStoriesSendStory) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.sendStory - metadata: {%s}", c.MD)
	return c.StoriesSendStory(request)
}

// StoriesEditStory
// stories.editStory#2c63a72b flags:# peer:InputPeer id:int media:flags.0?InputMedia media_areas:flags.3?Vector<MediaArea> caption:flags.1?string entities:flags.1?Vector<MessageEntity> privacy_rules:flags.2?Vector<InputPrivacyRule> music:flags.4?InputDocument = Updates;
func (s *Service) StoriesEditStory(ctx context.Context, request *mtproto.TLStoriesEditStory) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.editStory - metadata: {%s}", c.MD)
	return c.StoriesEditStory(request)
}

// StoriesDeleteStories
// stories.deleteStories#ae59db5f peer:InputPeer id:Vector<int> = Vector<int>;
func (s *Service) StoriesDeleteStories(ctx context.Context, request *mtproto.TLStoriesDeleteStories) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.deleteStories - metadata: {%s}", c.MD)
	return c.StoriesDeleteStories(request)
}

// StoriesTogglePinned
// stories.togglePinned#9a75a1ef peer:InputPeer id:Vector<int> pinned:Bool = Vector<int>;
func (s *Service) StoriesTogglePinned(ctx context.Context, request *mtproto.TLStoriesTogglePinned) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.togglePinned - metadata: {%s}", c.MD)
	return c.StoriesTogglePinned(request)
}

// StoriesTogglePinnedToTop
// stories.togglePinnedToTop#b297e9b peer:InputPeer id:Vector<int> = Bool;
func (s *Service) StoriesTogglePinnedToTop(ctx context.Context, request *mtproto.TLStoriesTogglePinnedToTop) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.togglePinnedToTop - metadata: {%s}", c.MD)
	return c.StoriesTogglePinnedToTop(request)
}

// StoriesGetPinnedStories
// stories.getPinnedStories#5821a5dc peer:InputPeer offset_id:int limit:int = stories.Stories;
func (s *Service) StoriesGetPinnedStories(ctx context.Context, request *mtproto.TLStoriesGetPinnedStories) (*mtproto.Stories_Stories, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getPinnedStories - metadata: {%s}", c.MD)
	return c.StoriesGetPinnedStories(request)
}

// StoriesGetStoriesArchive
// stories.getStoriesArchive#b4352016 peer:InputPeer offset_id:int limit:int = stories.Stories;
func (s *Service) StoriesGetStoriesArchive(ctx context.Context, request *mtproto.TLStoriesGetStoriesArchive) (*mtproto.Stories_Stories, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getStoriesArchive - metadata: {%s}", c.MD)
	return c.StoriesGetStoriesArchive(request)
}

// StoriesGetStoriesByID
// stories.getStoriesByID#5774ca74 peer:InputPeer id:Vector<int> = stories.Stories;
func (s *Service) StoriesGetStoriesByID(ctx context.Context, request *mtproto.TLStoriesGetStoriesByID) (*mtproto.Stories_Stories, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getStoriesByID - metadata: {%s}", c.MD)
	return c.StoriesGetStoriesByID(request)
}

// StoriesGetPeerStories
// stories.getPeerStories#2c4ada50 peer:InputPeer = stories.PeerStories;
func (s *Service) StoriesGetPeerStories(ctx context.Context, request *mtproto.TLStoriesGetPeerStories) (*mtproto.Stories_PeerStories, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getPeerStories - metadata: {%s}", c.MD)
	return c.StoriesGetPeerStories(request)
}

// StoriesGetAllReadPeerStories
// stories.getAllReadPeerStories#9b5ae7f9 = Updates;
func (s *Service) StoriesGetAllReadPeerStories(ctx context.Context, request *mtproto.TLStoriesGetAllReadPeerStories) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getAllReadPeerStories - metadata: {%s}", c.MD)
	return c.StoriesGetAllReadPeerStories(request)
}

// StoriesGetPeerMaxIDs78499170
// stories.getPeerMaxIDs#78499170 id:Vector<InputPeer> = Vector<RecentStory>;
func (s *Service) StoriesGetPeerMaxIDs78499170(ctx context.Context, request *mtproto.TLStoriesGetPeerMaxIDs78499170) (*mtproto.Vector_RecentStory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getPeerMaxIDs - metadata: {%s}", c.MD)
	return c.StoriesGetPeerMaxIDs78499170(request)
}

// StoriesGetPeerMaxIDs535983C3
// stories.getPeerMaxIDs#535983c3 id:Vector<InputPeer> = Vector<int>;
func (s *Service) StoriesGetPeerMaxIDs535983C3(ctx context.Context, request *mtproto.TLStoriesGetPeerMaxIDs535983C3) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getPeerMaxIDs#535983c3 - metadata: {%s}", c.MD)
	return c.StoriesGetPeerMaxIDs535983C3(request)
}

// UsersGetStoriesMaxIDs
// users.getStoriesMaxIDs#ca1cb9ab id:Vector<InputUser> = Vector<int>;
func (s *Service) UsersGetStoriesMaxIDs(ctx context.Context, request *mtproto.TLUsersGetStoriesMaxIDs) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("users.getStoriesMaxIDs - metadata: {%s}", c.MD)
	return c.UsersGetStoriesMaxIDs(request)
}

// StoriesReadStories
// stories.readStories#a556dac8 peer:InputPeer max_id:int = Vector<int>;
func (s *Service) StoriesReadStories(ctx context.Context, request *mtproto.TLStoriesReadStories) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.readStories - metadata: {%s}", c.MD)
	return c.StoriesReadStories(request)
}

// StoriesIncrementStoryViews
// stories.incrementStoryViews#b2028afb peer:InputPeer id:Vector<int> = Bool;
func (s *Service) StoriesIncrementStoryViews(ctx context.Context, request *mtproto.TLStoriesIncrementStoryViews) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.incrementStoryViews - metadata: {%s}", c.MD)
	return c.StoriesIncrementStoryViews(request)
}

// StoriesGetStoriesViews
// stories.getStoriesViews#28e16cc8 peer:InputPeer id:Vector<int> = stories.StoryViews;
func (s *Service) StoriesGetStoriesViews(ctx context.Context, request *mtproto.TLStoriesGetStoriesViews) (*mtproto.Stories_StoryViews, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getStoriesViews - metadata: {%s}", c.MD)
	return c.StoriesGetStoriesViews(request)
}

// StoriesGetStoryViewsList
// stories.getStoryViewsList#7ed23c57 flags:# just_contacts:flags.0?true reactions_first:flags.2?true forwards_first:flags.3?true peer:InputPeer q:flags.1?string id:int offset:string limit:int = stories.StoryViewsList;
func (s *Service) StoriesGetStoryViewsList(ctx context.Context, request *mtproto.TLStoriesGetStoryViewsList) (*mtproto.Stories_StoryViewsList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getStoryViewsList - metadata: {%s}", c.MD)
	return c.StoriesGetStoryViewsList(request)
}

// StoriesSendReaction
// stories.sendReaction#7fd736b2 flags:# add_to_recent:flags.0?true peer:InputPeer story_id:int reaction:Reaction = Updates;
func (s *Service) StoriesSendReaction(ctx context.Context, request *mtproto.TLStoriesSendReaction) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.sendReaction - metadata: {%s}", c.MD)
	return c.StoriesSendReaction(request)
}

// StoriesGetStoryReactionsList
// stories.getStoryReactionsList#b9b2881f flags:# forwards_first:flags.2?true peer:InputPeer id:int reaction:flags.0?Reaction offset:flags.1?string limit:int = stories.StoryReactionsList;
func (s *Service) StoriesGetStoryReactionsList(ctx context.Context, request *mtproto.TLStoriesGetStoryReactionsList) (*mtproto.Stories_StoryReactionsList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getStoryReactionsList - metadata: {%s}", c.MD)
	return c.StoriesGetStoryReactionsList(request)
}

// StoriesTogglePeerStoriesHidden
// stories.togglePeerStoriesHidden#bd0415c4 peer:InputPeer hidden:Bool = Bool;
func (s *Service) StoriesTogglePeerStoriesHidden(ctx context.Context, request *mtproto.TLStoriesTogglePeerStoriesHidden) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.togglePeerStoriesHidden - metadata: {%s}", c.MD)
	return c.StoriesTogglePeerStoriesHidden(request)
}

// ContactsToggleStoriesHidden
// contacts.toggleStoriesHidden#753fb865 id:InputUser hidden:Bool = Bool;
func (s *Service) ContactsToggleStoriesHidden(ctx context.Context, request *mtproto.TLContactsToggleStoriesHidden) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("contacts.toggleStoriesHidden - metadata: {%s}", c.MD)
	return c.ContactsToggleStoriesHidden(request)
}

// StoriesExportStoryLink
// stories.exportStoryLink#7b8def20 peer:InputPeer id:int = ExportedStoryLink;
func (s *Service) StoriesExportStoryLink(ctx context.Context, request *mtproto.TLStoriesExportStoryLink) (*mtproto.ExportedStoryLink, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.exportStoryLink - metadata: {%s}", c.MD)
	return c.StoriesExportStoryLink(request)
}

// StoriesActivateStealthMode
// stories.activateStealthMode#57bbd166 flags:# past:flags.0?true future:flags.1?true = Updates;
func (s *Service) StoriesActivateStealthMode(ctx context.Context, request *mtproto.TLStoriesActivateStealthMode) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.activateStealthMode - metadata: {%s}", c.MD)
	return c.StoriesActivateStealthMode(request)
}

// StoriesGetChatsToSend
// stories.getChatsToSend#a56a8b60 = messages.Chats;
func (s *Service) StoriesGetChatsToSend(ctx context.Context, request *mtproto.TLStoriesGetChatsToSend) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.getChatsToSend - metadata: {%s}", c.MD)
	return c.StoriesGetChatsToSend(request)
}

// StoriesReport19D8EB45
// stories.report#19d8eb45 peer:InputPeer id:Vector<int> option:bytes message:string = ReportResult;
func (s *Service) StoriesReport19D8EB45(ctx context.Context, request *mtproto.TLStoriesReport19D8EB45) (*mtproto.ReportResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.report - metadata: {%s}", c.MD)
	return c.StoriesReport19D8EB45(request)
}

// StoriesReport1923FA8C
// stories.report#1923fa8c peer:InputPeer id:Vector<int> reason:ReportReason message:string = Bool;
func (s *Service) StoriesReport1923FA8C(ctx context.Context, request *mtproto.TLStoriesReport1923FA8C) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.report#1923fa8c - metadata: {%s}", c.MD)
	return c.StoriesReport1923FA8C(request)
}

// StoriesReportC95BE06A
// stories.report#c95be06a user_id:InputUser id:Vector<int> reason:ReportReason message:string = Bool;
func (s *Service) StoriesReportC95BE06A(ctx context.Context, request *mtproto.TLStoriesReportC95BE06A) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("stories.report#c95be06a - metadata: {%s}", c.MD)
	return c.StoriesReportC95BE06A(request)
}
