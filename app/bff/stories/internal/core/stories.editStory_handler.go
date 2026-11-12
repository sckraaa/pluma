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

// StoriesEditStory
// stories.editStory#2c63a72b flags:# peer:InputPeer id:int media:flags.0?InputMedia media_areas:flags.3?Vector<MediaArea> caption:flags.1?string entities:flags.1?Vector<MessageEntity> privacy_rules:flags.2?Vector<InputPrivacyRule> music:flags.4?InputDocument = Updates;
//
// Changes what the request carries of a story: its media, its areas, its
// caption with the entities, who it is for, its track. The story becomes
// edited. Those who see it get it anew, and those an edit of the rules
// shuts out get it as deleted.
func (c *StoriesCore) StoriesEditStory(in *mtproto.TLStoriesEditStory) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.editStory - error: %v", err)
		return nil, err
	}
	// Who may edit depends on the sender of the story, which an edit does
	// not change.
	found, err := c.svcCtx.Dao.Store.One(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.editStory - error: %v", err)
		return nil, err
	}
	if found == nil {
		return nil, errStoryIdInvalid
	}
	if !v.canEdit(found, c.MD.UserId) {
		if v.peer.IsUser() {
			return nil, errStoryIdInvalid
		}
		return nil, mtproto.ErrChatAdminRequired
	}

	// The new values are made from the request alone and applied to the
	// story as it is stored under the lock of its peer (Store.Update), so
	// two edits at once each change only what they carry.
	var (
		caption  string
		entities []*mtproto.MessageEntity
		privacy  []*mtproto.PrivacyRule
		areas    []*mtproto.MediaArea
		music    *mtproto.Document
		media    *mtproto.MessageMedia
	)
	if in.Caption != nil {
		caption = in.GetCaption().GetValue()
		// The users a story mentions are told when it is posted; an edit
		// tells nobody again.
		if entities, _, err = c.storyCaption(caption, in.Entities); err != nil {
			c.Logger.Errorf("stories.editStory - error: %v", err)
			return nil, err
		}
	}
	setPrivacy := len(in.PrivacyRules) > 0 && v.peer.IsUser()
	if setPrivacy {
		if privacy, err = storyRules(c.MD.UserId, in.PrivacyRules); err != nil {
			c.Logger.Errorf("stories.editStory - error: %v", err)
			return nil, err
		}
	}
	// A vector that is sent empty reads as one that is not sent: the
	// clients set the flag of the areas only with areas in it (Android
	// StoriesController.java:2358-2360).
	if len(in.MediaAreas) > 0 {
		if areas, err = c.storyAreas(in.MediaAreas); err != nil {
			c.Logger.Errorf("stories.editStory - error: %v", err)
			return nil, err
		}
	}
	if in.Music != nil {
		if music, err = c.storyMusic(in.Music); err != nil {
			c.Logger.Errorf("stories.editStory - error: %v", err)
			return nil, err
		}
	}
	if in.Media != nil {
		if media, err = c.storyMedia(in.Media); err != nil {
			return nil, err
		}
	}

	at := now()
	before, after, err := c.svcCtx.Dao.Store.Update(c.ctx, v.peer, in.Id, at, func(st *stories.Story) {
		if in.Caption != nil {
			st.Caption, st.Entities = caption, entities
		}
		if setPrivacy {
			st.Privacy = privacy
		}
		if len(in.MediaAreas) > 0 {
			st.MediaAreas = areas
		}
		if in.Music != nil {
			st.Music = music
		}
		if in.Media != nil {
			st.Media = media
			// New media without new areas keeps none: the old ones were
			// laid over the old picture.
			if len(in.MediaAreas) == 0 {
				st.MediaAreas = nil
			}
		}
	})
	if err != nil {
		c.Logger.Errorf("stories.editStory - error: %v", err)
		return nil, err
	}
	if after == nil {
		return nil, errStoryIdInvalid
	}
	st := *after
	c.changed(st.Peer, at)
	c.announce(&st, before, false)

	items, err := c.items(v, []*stories.Story{&st})
	if err != nil {
		c.Logger.Errorf("stories.editStory - error: %v", err)
		return nil, err
	}
	updates := c.withRefs(v, &st, updateStory(st.Peer, items[0]))
	c.pushOthers(updates)
	return updates, nil
}
