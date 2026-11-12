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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/layercompat"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// What the stories answer and push is encoded whole for both layers the
// server speaks, 228 and 233: a story with its areas, counters and rules,
// the lists, the updates, the message of a shared story. Each is decoded
// back and keeps what the clients read.
func TestAnswersEncodeForBothLayers(t *testing.T) {
	st := &stories.Story{
		Peer: stories.UserPeer(7), Id: 3, SenderId: 7, Date: 100, ExpireDate: 200, Pinned: true, Edited: true,
		Privacy: []*mtproto.PrivacyRule{
			mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule(),
			mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: []int64{9}}).To_PrivacyRule(),
		},
		Caption:  "hi there",
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 0, Length: 2, UserId_INT64: 9}).To_MessageEntity()},
		Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{
			Id: 5, AccessHash: 6, FileReference: []byte{}, Date: 100, Sizes: []*mtproto.PhotoSize{}, DcId: 1,
		}).To_Photo()}).To_MessageMedia(),
		MediaAreas: []*mtproto.MediaArea{
			mtproto.MakeTLMediaAreaSuggestedReaction(&mtproto.MediaArea{
				Coordinates: mtproto.MakeTLMediaAreaCoordinates(&mtproto.MediaAreaCoordinates{X: 10, Y: 20, W: 30, H: 40, Rotation: 5}).To_MediaAreaCoordinates(),
				Reaction:    mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: "❤"}).To_Reaction(),
			}).To_MediaArea(),
			mtproto.MakeTLMediaAreaChannelPost(&mtproto.MediaArea{
				Coordinates: mtproto.MakeTLMediaAreaCoordinates(&mtproto.MediaAreaCoordinates{X: 1, Y: 2, W: 3, H: 4}).To_MediaAreaCoordinates(),
				ChannelId:   50, MsgId: 2,
			}).To_MediaArea(),
		},
		FwdFrom: mtproto.MakeTLStoryFwdHeader(&mtproto.StoryFwdHeader{From: mtproto.MakePeerUser(8), StoryId: mtproto.MakeFlagsInt32(1)}).To_StoryFwdHeader(),
	}
	stats := &stories.Stats{Views: 2, Reactions: 1, ByKind: []stories.ReactionCount{{Reaction: "e❤", Count: 1}}, Recent: []int64{9}}
	own := stories.Item(st, stories.Seen{Out: true, Owner: true, Stats: stats})
	seen := stories.Item(st, stories.Seen{SentReaction: "e❤"})
	ps := stories.PeerStories(st.Peer, []*stories.Story{st}, 2, func(*stories.Story) stories.Seen { return stories.Seen{} })

	for _, layer := range []int32{228, 233} {
		roundTrip := func(name string, object mtproto.TLObject, into interface {
			Decode(*mtproto.DecodeBuf) error
		}) {
			t.Helper()
			body, err := layercompat.EncodeFor(object, layer)
			if err != nil {
				t.Fatalf("layer %d, %s: %v", layer, name, err)
			}
			d := mtproto.NewDecodeBuf(body)
			if err = into.Decode(d); err != nil || d.GetError() != nil {
				t.Fatalf("layer %d, %s: decode: %v, %v", layer, name, err, d.GetError())
			}
		}
		gotOwn := &mtproto.StoryItem{}
		roundTrip("the owner's story", own, gotOwn)
		if gotOwn.GetId() != 3 || !gotOwn.GetOut() || !gotOwn.GetContacts() || !gotOwn.GetPinned() || !gotOwn.GetEdited() || gotOwn.GetCaption().GetValue() != "hi there" ||
			len(gotOwn.GetPrivacy()) != 2 || len(gotOwn.GetMediaAreas()) != 2 || gotOwn.GetMediaAreas()[1].GetChannelId() != 50 ||
			gotOwn.GetViews().GetViewsCount() != 2 || !gotOwn.GetViews().GetHasViewers() || gotOwn.GetFwdFrom().GetFrom().GetUserId() != 8 ||
			gotOwn.GetMedia().GetPhoto_FLAGPHOTO().GetId() != 5 || gotOwn.GetExpireDate() != 200 {
			t.Errorf("layer %d, the owner's story: %v", layer, gotOwn)
		}
		gotSeen := &mtproto.StoryItem{}
		roundTrip("a viewer's story", seen, gotSeen)
		if gotSeen.GetOut() || gotSeen.GetPrivacy() != nil || gotSeen.GetViews() != nil || gotSeen.GetSentReaction().GetEmoticon() != "❤" {
			t.Errorf("layer %d, a viewer's story: %v", layer, gotSeen)
		}

		all := mtproto.MakeTLStoriesAllStories(&mtproto.Stories_AllStories{
			Count: 1, State: "s", PeerStories: []*mtproto.PeerStories{ps}, Chats: []*mtproto.Chat{}, Users: []*mtproto.User{},
			StealthMode: stealthMode(stories.Stealth{ActiveUntil: 500, CooldownUntil: 900}, 100),
		}).To_Stories_AllStories()
		gotAll := &mtproto.Stories_AllStories{}
		roundTrip("stories.allStories", all, gotAll)
		if gotAll.GetCount() != 1 || gotAll.GetState() != "s" || len(gotAll.GetPeerStories()) != 1 || gotAll.GetPeerStories()[0].GetMaxReadId().GetValue() != 2 ||
			gotAll.GetPeerStories()[0].GetPeer().GetUserId() != 7 || gotAll.GetStealthMode().GetCooldownUntilDate().GetValue() != 900 {
			t.Errorf("layer %d, stories.allStories: %v", layer, gotAll)
		}
		same := mtproto.MakeTLStoriesAllStoriesNotModified(&mtproto.Stories_AllStories{State: "s", StealthMode: stealthMode(stories.Stealth{}, 100)}).To_Stories_AllStories()
		roundTrip("stories.allStoriesNotModified", same, &mtproto.Stories_AllStories{})

		list := mtproto.MakeTLStoriesStories(&mtproto.Stories_Stories{
			Count: 1, Stories: []*mtproto.StoryItem{own}, PinnedToTop: []int32{3}, Chats: []*mtproto.Chat{}, Users: []*mtproto.User{},
		}).To_Stories_Stories()
		gotList := &mtproto.Stories_Stories{}
		roundTrip("stories.stories", list, gotList)
		if gotList.GetCount() != 1 || len(gotList.GetStories()) != 1 || len(gotList.GetPinnedToTop()) != 1 {
			t.Errorf("layer %d, stories.stories: %v", layer, gotList)
		}

		viewers := mtproto.MakeTLStoriesStoryViewsList(&mtproto.Stories_StoryViewsList{
			Count: 1, ViewsCount: 1, ReactionsCount: 1,
			Views: []*mtproto.StoryView{mtproto.MakeTLStoryView(&mtproto.StoryView{
				UserId: 9, Date: 150, Reaction: mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: "❤"}).To_Reaction(),
			}).To_StoryView()},
			Chats: []*mtproto.Chat{}, Users: []*mtproto.User{}, NextOffset: mtproto.MakeFlagsString("20"),
		}).To_Stories_StoryViewsList()
		gotViewers := &mtproto.Stories_StoryViewsList{}
		roundTrip("stories.storyViewsList", viewers, gotViewers)
		if gotViewers.GetCount() != 1 || gotViewers.GetViews()[0].GetUserId() != 9 || gotViewers.GetViews()[0].GetReaction().GetEmoticon() != "❤" || gotViewers.GetNextOffset().GetValue() != "20" {
			t.Errorf("layer %d, stories.storyViewsList: %v", layer, gotViewers)
		}

		updates := updatesOf([]*mtproto.Update{
			mtproto.MakeTLUpdateStoryID(&mtproto.Update{Id_INT32: 3, RandomId: 77}).To_Update(),
			updateStory(st.Peer, own),
			updateStory(st.Peer, stories.Deleted(3)),
			updateReadStories(stories.ChannelPeer(50), 3),
			mtproto.MakeTLUpdateStoriesStealthMode(&mtproto.Update{StealthMode: stealthMode(stories.Stealth{ActiveUntil: 500}, 100)}).To_Update(),
			mtproto.MakeTLUpdateSentStoryReaction(&mtproto.Update{Peer_PEER: st.Peer.ToPeer(), UserId: 7, StoryId: 3, Reaction: mtproto.MakeTLReactionEmpty(nil).To_Reaction()}).To_Update(),
			mtproto.MakeTLUpdateNewStoryReaction(&mtproto.Update{StoryId: 3, Peer_PEER: mtproto.MakePeerUser(9), Reaction: mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: "❤"}).To_Reaction()}).To_Update(),
		}, nil, nil)
		gotUpdates := &mtproto.Updates{}
		roundTrip("the updates", updates, gotUpdates)
		if len(gotUpdates.GetUpdates()) != 7 {
			t.Errorf("layer %d: %d updates of 7 came through: %v", layer, len(gotUpdates.GetUpdates()), gotUpdates.GetUpdates())
		}
		for i, want := range []string{
			mtproto.Predicate_updateStoryID, mtproto.Predicate_updateStory, mtproto.Predicate_updateStory, mtproto.Predicate_updateReadStories,
			mtproto.Predicate_updateStoriesStealthMode, mtproto.Predicate_updateSentStoryReaction, mtproto.Predicate_updateNewStoryReaction,
		} {
			if i < len(gotUpdates.GetUpdates()) && gotUpdates.GetUpdates()[i].GetPredicateName() != want {
				t.Errorf("layer %d, update %d: %s, want %s", layer, i, gotUpdates.GetUpdates()[i].GetPredicateName(), want)
			}
		}

		media := mtproto.MakeTLMessageMediaStory(&mtproto.MessageMedia{ViaMention: true, Peer: st.Peer.ToPeer(), UserId: 7, Id: 3}).To_MessageMedia()
		gotMedia := &mtproto.MessageMedia{}
		roundTrip("messageMediaStory", media, gotMedia)
		if gotMedia.GetPredicateName() != mtproto.Predicate_messageMediaStory || !gotMedia.GetViaMention() || gotMedia.GetPeer().GetUserId() != 7 || gotMedia.GetId() != 3 || gotMedia.GetStory() != nil {
			t.Errorf("layer %d, messageMediaStory: %v", layer, gotMedia)
		}
	}
}
