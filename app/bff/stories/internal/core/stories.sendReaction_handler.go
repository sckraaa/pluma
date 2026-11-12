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
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// StoriesSendReaction
// stories.sendReaction#7fd736b2 flags:# add_to_recent:flags.0?true peer:InputPeer story_id:int reaction:Reaction = Updates;
//
// Leaves the caller's reaction on a story it may open, or with
// reactionEmpty takes it back (Android StoriesController.java:1872-1901).
// A reaction shows the viewer to the owner whatever its stealth mode: it
// chose to be seen. The other devices of the caller get
// updateSentStoryReaction, and the owner of a user's story
// updateNewStoryReaction.
func (c *StoriesCore) StoriesSendReaction(in *mtproto.TLStoriesSendReaction) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.sendReaction - error: %v", err)
		return nil, err
	}
	st, err := c.one(v, in.StoryId)
	if err != nil {
		c.Logger.Errorf("stories.sendReaction - error: %v", err)
		return nil, err
	}
	var key string
	reaction := in.Reaction
	if reaction != nil && reaction.GetPredicateName() != mtproto.Predicate_reactionEmpty {
		if key, err = reactions.Key(reaction); err != nil {
			return nil, err
		}
	} else {
		reaction = mtproto.MakeTLReactionEmpty(nil).To_Reaction()
	}
	if err = c.svcCtx.Dao.Store.SetReaction(c.ctx, st.Peer, st.Id, c.MD.UserId, key, now()); err != nil {
		c.Logger.Errorf("stories.sendReaction - error: %v", err)
		return nil, err
	}

	sent := &mtproto.Update{Peer_PEER: st.Peer.ToPeer(), StoryId: st.Id, Reaction: reaction}
	if st.Peer.IsUser() {
		sent.UserId = st.Peer.Id
	}
	updates := updatesOf([]*mtproto.Update{mtproto.MakeTLUpdateSentStoryReaction(sent).To_Update()}, nil, nil)
	c.pushOthers(updates)
	if key != "" && st.Peer.IsUser() && !v.own {
		r := &refs{}
		r.user(c.MD.UserId)
		// The owner is shown the one who reacted as the owner sees it.
		owner := NewFor(c.ctx, c.svcCtx, st.Peer.Id)
		users, _, err := owner.resolve(r)
		if err != nil {
			c.Logger.Errorf("stories.sendReaction - the user of the reaction: %v", err)
		}
		c.pushTo(st.Peer.Id, updatesOf([]*mtproto.Update{mtproto.MakeTLUpdateNewStoryReaction(&mtproto.Update{
			StoryId:   st.Id,
			Peer_PEER: mtproto.MakePeerUser(c.MD.UserId),
			Reaction:  reaction,
		}).To_Update()}, users, nil))
	}
	return updates, nil
}
