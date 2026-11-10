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

package layercompat

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The constructors of layers 232 and 233 must carry the fields the handlers
// fill. Without available_reactions in channelFull Android 13.2.1 shows no
// reaction panel in a channel (ReactionsContainerLayout.setMessage leaves the
// list empty when it is neither chatReactionsAll nor chatReactionsSome).

func decodeAt233(t *testing.T, o mtproto.TLObject, into interface {
	Decode(*mtproto.DecodeBuf) error
}) []byte {
	t.Helper()
	b, err := EncodeFor(o, 233)
	if err != nil {
		t.Fatal(err)
	}
	if err := into.Decode(mtproto.NewDecodeBuf(b)); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestChannelFullReactionsAt233(t *testing.T) {
	full := mtproto.MakeTLChannelFull(&mtproto.ChatFull{
		Id:                                   9,
		ChatPhoto:                            mtproto.MakeTLPhotoEmpty(nil).To_Photo(),
		NotifySettings:                       mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(),
		BotInfo:                              []*mtproto.BotInfo{},
		AvailableReactions_FLAGCHATREACTIONS: mtproto.MakeTLChatReactionsAll(&mtproto.ChatReactions{AllowCustom: true}).To_ChatReactions(),
	}).To_ChatFull()
	got := &mtproto.ChatFull{}
	b := decodeAt233(t, full, got)
	if id := uint32(int32FromBytes(b)); id != 0xc4c4dd40 {
		t.Fatalf("constructor %x", id)
	}
	// channelFull#c4c4dd40: available_reactions:flags.30?ChatReactions.
	if flags := int32FromBytes(b[4:]); flags&(1<<30) == 0 {
		t.Fatalf("flags %b: no available_reactions", flags)
	}
	r := got.GetAvailableReactions_FLAGCHATREACTIONS()
	if r.GetPredicateName() != mtproto.Predicate_chatReactionsAll || !r.GetAllowCustom() {
		t.Fatalf("available_reactions %v", r)
	}
}

func TestChannelColorAndStoriesAt233(t *testing.T) {
	ch := mtproto.MakeTLChannel(&mtproto.Chat{
		Id:                           9,
		Title:                        "c",
		Photo:                        mtproto.MakeTLChatPhotoEmpty(nil).To_ChatPhoto(),
		Color_FLAGPEERCOLOR:          mtproto.MakeTLPeerColor(&mtproto.PeerColor{Color: wrapperspb.Int32(5)}).To_PeerColor(),
		StoriesMaxId_FLAGRECENTSTORY: mtproto.MakeTLRecentStory(&mtproto.RecentStory{MaxId: wrapperspb.Int32(7)}).To_RecentStory(),
	}).To_Chat()
	got := &mtproto.Chat{}
	decodeAt233(t, ch, got)
	if got.GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 5 {
		t.Fatalf("color %v", got.GetColor_FLAGPEERCOLOR())
	}
	if got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 7 {
		t.Fatalf("stories_max_id %v", got.GetStoriesMaxId_FLAGRECENTSTORY())
	}
}

func TestUserFullStoriesAt233(t *testing.T) {
	full := mtproto.MakeTLUserFull(&mtproto.UserFull{
		Id:             1,
		Settings:       mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
		NotifySettings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(),
		Stories_FLAGPEERSTORIES: mtproto.MakeTLPeerStories(&mtproto.PeerStories{
			Peer:    mtproto.MakePeerUser(1),
			Stories: []*mtproto.StoryItem{},
		}).To_PeerStories(),
	}).To_UserFull()
	got := &mtproto.UserFull{}
	decodeAt233(t, full, got)
	if got.GetStories_FLAGPEERSTORIES().GetPeer().GetUserId() != 1 {
		t.Fatalf("stories %v", got.GetStories_FLAGPEERSTORIES())
	}
}
