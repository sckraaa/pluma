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

// StoriesSendStory
// stories.sendStory#8f9e6898 flags:# pinned:flags.2?true noforwards:flags.4?true fwd_modified:flags.7?true peer:InputPeer media:InputMedia media_areas:flags.5?Vector<MediaArea> caption:flags.0?string entities:flags.1?Vector<MessageEntity> privacy_rules:Vector<InputPrivacyRule> random_id:long period:flags.3?int fwd_from_id:flags.6?InputPeer fwd_from_story:flags.6?int albums:flags.8?Vector<int> music:flags.9?InputDocument = Updates;
//
// Posts a story of the account or, by an admin with post_stories, of a
// channel. The answer carries updateStoryID with the id of the request's
// random_id and updateStory with the story: Android takes the story from
// updateStory and falls back to the id (StoriesController.java:2434-2462).
// A repeated request answers the story stored the first time. Albums are
// not served: the story is posted and joins none.
func (c *StoriesCore) StoriesSendStory(in *mtproto.TLStoriesSendStory) (*mtproto.Updates, error) {
	v, err := c.poster(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	}
	if in.RandomId == 0 {
		return nil, mtproto.ErrRandomIdEmpty
	}
	if stored, err := c.svcCtx.Dao.Store.ByRandom(c.ctx, v.peer, c.MD.UserId, in.RandomId); err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	} else if stored != nil {
		return c.sentAnswer(v, stored, in.RandomId)
	}

	at := now()
	st := &stories.Story{
		Peer:       v.peer,
		RandomId:   in.RandomId,
		SenderId:   c.MD.UserId,
		Date:       at,
		ExpireDate: at + stories.DefaultPeriod,
		Pinned:     in.Pinned,
		Noforwards: in.Noforwards,
		Caption:    in.GetCaption().GetValue(),
	}
	if in.GetPeriod() != nil {
		if !stories.ValidPeriod(in.GetPeriod().GetValue()) {
			return nil, mtproto.ErrStoryPeriodInvalid
		}
		st.ExpireDate = at + in.GetPeriod().GetValue()
	}
	if v.peer.IsUser() {
		if st.Privacy, err = storyRules(c.MD.UserId, in.PrivacyRules); err != nil {
			c.Logger.Errorf("stories.sendStory - error: %v", err)
			return nil, err
		}
	}
	if st.Entities, st.Mentions, err = c.storyCaption(st.Caption, in.Entities); err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	}
	if !v.peer.IsUser() {
		// The message of a mention comes from a user: a channel's story
		// tells nobody.
		st.Mentions = nil
	}
	if st.MediaAreas, err = c.storyAreas(in.MediaAreas); err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	}
	if st.Music, err = c.storyMusic(in.Music); err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	}
	if in.FwdFromId != nil {
		if err = c.repost(st, in); err != nil {
			c.Logger.Errorf("stories.sendStory - error: %v", err)
			return nil, err
		}
	}
	// The limits first: a story that will not be posted uploads nothing.
	if _, err = c.svcCtx.Dao.Store.Remains(c.ctx, v.peer, at); err != nil {
		return nil, limitError(err)
	}
	if st.Media, err = c.storyMedia(in.Media); err != nil {
		return nil, err
	}

	stored, created, err := c.svcCtx.Dao.Store.Create(c.ctx, st)
	if err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, limitError(err)
	}
	if created {
		// The story is stored; what follows is repeated by the worker if
		// it fails here.
		c.changed(stored.Peer, stored.Date)
		c.tellMentions(stored)
		c.announce(stored, nil, false)
		if item, err := c.items(v, []*stories.Story{stored}); err == nil {
			c.pushOthers(c.withRefs(v, stored, updateStory(stored.Peer, item[0])))
		}
	}
	return c.sentAnswer(v, stored, in.RandomId)
}

// repost fills what makes st a repost of the story the request names: the
// caller must be able to open that story, and it must allow forwarding.
func (c *StoriesCore) repost(st *stories.Story, in *mtproto.TLStoriesSendStory) error {
	from, err := c.view(in.FwdFromId)
	if err != nil {
		return err
	}
	src, err := c.one(from, in.GetFwdFromStory().GetValue())
	if err != nil {
		return err
	}
	if src.Noforwards && !from.own {
		return mtproto.ErrChatForwardsRestricted
	}
	st.FwdPeer, st.FwdStoryId = src.Peer, src.Id
	st.FwdFrom = mtproto.MakeTLStoryFwdHeader(&mtproto.StoryFwdHeader{
		Modified: in.FwdModified,
		From:     src.Peer.ToPeer(),
		StoryId:  mtproto.MakeFlagsInt32(src.Id),
	}).To_StoryFwdHeader()
	return nil
}

// withRefs is the updates container of one update of the story st with the
// users and channels the story refers to. A failure to read them leaves
// them out: the update still names its peer.
func (c *StoriesCore) withRefs(v *peerView, st *stories.Story, updates ...*mtproto.Update) *mtproto.Updates {
	r := &refs{}
	r.story(st, v.own)
	users, chats, err := c.resolve(r)
	if err != nil {
		c.Logger.Errorf("stories: users and chats of story %d: %v", st.Id, err)
	}
	return updatesOf(updates, users, chats)
}

// sentAnswer is the answer of stories.sendStory for the stored story.
func (c *StoriesCore) sentAnswer(v *peerView, st *stories.Story, randomId int64) (*mtproto.Updates, error) {
	items, err := c.items(v, []*stories.Story{st})
	if err != nil {
		c.Logger.Errorf("stories.sendStory - error: %v", err)
		return nil, err
	}
	return c.withRefs(v, st,
		mtproto.MakeTLUpdateStoryID(&mtproto.Update{Id_INT32: st.Id, RandomId: randomId}).To_Update(),
		updateStory(st.Peer, items[0])), nil
}
