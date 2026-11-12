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

package stories

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/reactions"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Seen is how one viewer sees a story: what of it is the viewer's own.
type Seen struct {
	// Out: the viewer posted it (storyItem.out).
	Out bool
	// Owner: the story is of the viewer's own account, which is shown who
	// it is for and who opened it.
	Owner bool
	// Stats are the counters the viewer is shown, nil for none: the owner
	// of a user's story sees them, and everyone those of a channel's.
	Stats *Stats
	// SentReaction is the reaction the viewer left, "" for none.
	SentReaction string
}

// Item is storyItem of st for a viewer.
func Item(st *Story, seen Seen) *mtproto.StoryItem {
	item := &mtproto.StoryItem{
		Id:         st.Id,
		Date:       st.Date,
		ExpireDate: st.ExpireDate,
		Pinned:     st.Pinned,
		Noforwards: st.Noforwards,
		Edited:     st.Edited,
		Out:        seen.Out,
		FwdFrom:    st.FwdFrom,
		Caption:    mtproto.MakeFlagsString(st.Caption),
		Entities:   st.Entities,
		Media:      st.Media,
		MediaAreas: st.MediaAreas,
		Music:      st.Music,
	}
	if st.Peer.IsUser() {
		// Who the story is for is drawn on it for every viewer from these
		// flags, not from the rules (Android PeerStoriesView.java:2282-2289).
		switch mtproto.StoryAudienceOf(st.Privacy) {
		case mtproto.StoryAudiencePublic:
			item.Public = true
		case mtproto.StoryAudienceCloseFriends:
			item.CloseFriends = true
		case mtproto.StoryAudienceContacts:
			item.Contacts = true
		default:
			item.SelectedContacts = true
		}
		if seen.Owner {
			item.Privacy = st.Privacy
		}
	} else {
		// Everyone who sees the channel sees its stories; the clients let a
		// public story be shared (Android PeerStoriesView.java:5097).
		item.Public = true
	}
	if seen.Stats != nil {
		item.Views = Views(seen.Stats, st.ForwardsCount, seen.Owner && st.Peer.IsUser())
	}
	if seen.SentReaction != "" {
		item.SentReaction = reactions.FromKey(seen.SentReaction)
	}
	return mtproto.MakeTLStoryItem(item).To_StoryItem()
}

// Views is storyViews of the counters stats. withViewers: the viewer may
// also read who opened the story (stories.getStoryViewsList), which is the
// owner of a user's story.
func Views(stats *Stats, forwards int32, withViewers bool) *mtproto.StoryViews {
	if stats == nil {
		stats = &Stats{}
	}
	v := &mtproto.StoryViews{
		ViewsCount:               stats.Views,
		ForwardsCount:            mtproto.MakeFlagsInt32(forwards),
		ReactionsCount_FLAGINT32: mtproto.MakeFlagsInt32(stats.Reactions),
		ReactionsCount_INT32:     stats.Reactions,
	}
	for _, r := range stats.ByKind {
		reaction := reactions.FromKey(r.Reaction)
		if reaction == nil {
			continue
		}
		v.Reactions = append(v.Reactions, mtproto.MakeTLReactionCount(&mtproto.ReactionCount{
			Reaction_REACTION: reaction,
			Count:             r.Count,
		}).To_ReactionCount())
	}
	if withViewers {
		v.HasViewers = stats.Views > 0
		v.RecentViewers = stats.Recent
	}
	return mtproto.MakeTLStoryViews(v).To_StoryViews()
}

// Deleted is storyItemDeleted of the story id.
func Deleted(id int32) *mtproto.StoryItem {
	return mtproto.MakeTLStoryItemDeleted(&mtproto.StoryItem{Id: id}).To_StoryItem()
}

// PeerStories is peerStories of the stories list of peer, each as seen
// gives it, with how far the viewer has read (0: nothing).
func PeerStories(peer Peer, list []*Story, maxRead int32, seen func(*Story) Seen) *mtproto.PeerStories {
	items := make([]*mtproto.StoryItem, 0, len(list))
	for _, st := range list {
		items = append(items, Item(st, seen(st)))
	}
	out := &mtproto.PeerStories{Peer: peer.ToPeer(), Stories: items}
	if maxRead > 0 {
		out.MaxReadId = wrapperspb.Int32(maxRead)
	}
	return mtproto.MakeTLPeerStories(out).To_PeerStories()
}

// SeenOf reads from the store how viewer sees the stories list of one peer:
// its reactions, and the counters when it is shown them (everyone for a
// channel, the owner for a user). own tells the stories of the viewer's own
// account from the others.
func (s *Store) SeenOf(ctx context.Context, peer Peer, list []*Story, viewer int64, own bool) (func(*Story) Seen, error) {
	ids := make([]int32, 0, len(list))
	for _, st := range list {
		ids = append(ids, st.Id)
	}
	sent, err := s.SentReactions(ctx, peer, ids, viewer)
	if err != nil {
		return nil, err
	}
	var stats map[int32]*Stats
	if own || !peer.IsUser() {
		if stats, err = s.StatsOf(ctx, peer, ids, 0); err != nil {
			return nil, err
		}
	}
	return func(st *Story) Seen {
		seen := Seen{
			Out:          own || st.SenderId == viewer,
			Owner:        own,
			SentReaction: sent[st.Id],
		}
		if stats != nil {
			seen.Stats = stats[st.Id]
			if seen.Stats == nil {
				seen.Stats = &Stats{}
			}
		}
		return seen
	}, nil
}

// ChannelStories is channelFull.stories of the channel for viewer at now:
// its stories in the feeds, nil when it has none, and whether its profile
// has stories (stories_pinned_available). Whoever sees the channel sees
// them all.
func (s *Store) ChannelStories(ctx context.Context, channelId, viewer int64, now int32) (*mtproto.PeerStories, bool, error) {
	peer := ChannelPeer(channelId)
	pinned, err := s.HasPinned(ctx, peer)
	if err != nil {
		return nil, false, err
	}
	list, err := s.Active(ctx, peer, now)
	if err != nil || len(list) == 0 {
		return nil, pinned, err
	}
	seen, err := s.SeenOf(ctx, peer, list, viewer, false)
	if err != nil {
		return nil, false, err
	}
	maxRead, err := s.MaxRead(ctx, viewer, peer)
	if err != nil {
		return nil, false, err
	}
	return PeerStories(peer, list, maxRead, seen), pinned, nil
}
