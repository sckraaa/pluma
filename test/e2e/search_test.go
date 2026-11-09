//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func messagesOf(t *testing.T, o mtproto.TLObject) *mtproto.Messages_Messages {
	t.Helper()
	return o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages()
}

func emptyFilter(c *mtclient.Client) *mtproto.MessagesFilter {
	return &mtproto.MessagesFilter{Constructor: c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)}
}

func searchIn(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, q string, from *mtproto.InputPeer) *mtproto.Messages_Messages {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSearch{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_search),
		Peer:        peer,
		Q:           q,
		FromId:      from,
		Filter:      emptyFilter(u.c),
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.search %q: %v", q, err)
	}
	return messagesOf(t, o)
}

func ids(r *mtproto.Messages_Messages) string {
	out := []int32{}
	for _, m := range r.GetMessages() {
		out = append(out, m.GetId())
	}
	return fmt.Sprint(out)
}

// In a private chat a Russian word finds its other forms and not the inside
// of another word, the words of a query go in any order, from_id narrows to
// a sender, a hashtag is found whole; the media counters answer for filters;
// a global search merges the private chat with the channels, and the
// hashtag search of public channels pages with next_rate.
func TestSearch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	peer := inputPeerOf(ctx, t, alice, bob)

	sendMessageID(ctx, t, alice, peer, "Кот спит на диване", randomID())
	sendMessageID(ctx, t, bob, inputPeerOf(ctx, t, bob, alice), "наши коты дома", randomID())
	sendMessageID(ctx, t, alice, peer, "большое стадо, скот пасётся", randomID())
	tag := sendMessageID(ctx, t, alice, peer, "едем в #отпуск завтра", randomID())
	sendMessageID(ctx, t, alice, peer, "опять #отпуска кончились", randomID())

	// A message of another user reaches the box after the send has answered.
	for deadline := time.Now().Add(15 * time.Second); len(searchIn(ctx, t, alice, peer, "дома", nil).GetMessages()) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the message of the other user did not reach the box")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if r := searchIn(ctx, t, alice, peer, "кота", nil); len(r.GetMessages()) != 2 {
		t.Errorf("the forms of a word: %s", ids(r))
	}
	if r := searchIn(ctx, t, alice, peer, "диване спит", nil); len(r.GetMessages()) != 1 {
		t.Errorf("words in any order: %s", ids(r))
	}
	if r := searchIn(ctx, t, alice, peer, "#отпуск", nil); ids(r) != fmt.Sprint([]int32{tag}) {
		t.Errorf("a whole hashtag: %s want %d", ids(r), tag)
	}
	if r := searchIn(ctx, t, alice, peer, "кот", peer); len(r.GetMessages()) != 1 || r.GetMessages()[0].GetFromId().GetUserId() == alice.self.GetId() {
		t.Errorf("from_id in a private chat: %s", ids(r))
	}

	_, ch := createChannel(ctx, t, alice, "search channel", true)
	chPeer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	inGroup := sendMessageID(ctx, t, alice, chPeer, "кот в группе #отпуск", randomID())

	if r := searchIn(ctx, t, alice, chPeer, "#ОТПУСК", nil); ids(r) != fmt.Sprint([]int32{inGroup}) {
		t.Errorf("a hashtag in a supergroup: %s", ids(r))
	}

	raw, err := alice.c.InvokeRaw(ctx, &mtproto.TLMessagesGetSearchCounters{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getSearchCounters),
		Peer:        chPeer,
		Filters:     []*mtproto.MessagesFilter{emptyFilter(alice.c)},
	})
	if err != nil {
		t.Fatalf("messages.getSearchCounters: %v", err)
	}
	counters, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(counters) != 1 || counters[0].(interface {
		To_Messages_SearchCounter() *mtproto.Messages_SearchCounter
	}).To_Messages_SearchCounter().GetCount() < 1 {
		t.Errorf("counters in a channel: %v", counters)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSearchGlobal{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_searchGlobal),
		FolderId:    mtproto.MakeFlagsInt32(0),
		Q:           "#отпуск",
		Filter:      emptyFilter(alice.c),
		OffsetPeer:  &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("messages.searchGlobal: %v", err)
	}
	r := messagesOf(t, o)
	if len(r.GetMessages()) != 2 || len(r.GetChats()) != 1 {
		t.Errorf("searchGlobal: %s chats %d", ids(r), len(r.GetChats()))
	}
	if r.GetMessages()[0].GetDate() < r.GetMessages()[1].GetDate() {
		t.Errorf("not newest first: %v", r.GetMessages())
	}
}

// The hashtag search of channels.searchPosts covers public broadcast
// channels, also for a reader that is not in the channel; a text query is
// counted, the same query is free next time.
func TestSearchPosts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "posts", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     input,
		Username:    fmt.Sprintf("pluma_e2e_search_%d", ch.GetId()),
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	chPeer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	sendMessageID(ctx, t, alice, chPeer, "пост про #новости дня", randomID())

	empty := &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)}
	search := func(req *mtproto.TLChannelsSearchPosts) *mtproto.Messages_Messages {
		t.Helper()
		req.Constructor = bob.c.Constructor(mtproto.Predicate_channels_searchPosts)
		req.OffsetPeer = empty
		req.Limit = 10
		o, err := bob.c.Invoke(ctx, req)
		if err != nil {
			t.Fatalf("channels.searchPosts: %v", err)
		}
		return messagesOf(t, o)
	}

	r := search(&mtproto.TLChannelsSearchPosts{Hashtag_FLAGSTRING: mtproto.MakeFlagsString("новости")})
	if len(r.GetMessages()) != 1 || r.GetChats()[0].GetId() != ch.GetId() {
		t.Fatalf("a hashtag of a public channel: %s %v", ids(r), r.GetChats())
	}
	if r.GetSearchFlood() != nil {
		t.Errorf("a hashtag has no quota: %v", r.GetSearchFlood())
	}
	for _, tag := range []string{"#новости", "#НОВОСТИ"} {
		if r = search(&mtproto.TLChannelsSearchPosts{Hashtag_FLAGSTRING: mtproto.MakeFlagsString(tag)}); len(r.GetMessages()) != 1 {
			t.Errorf("a hashtag sent with its sign %q: %s", tag, ids(r))
		}
	}
	// The list the clients open with: no query, no slot of the quota.
	if r = search(&mtproto.TLChannelsSearchPosts{Query: &wrapperspb.StringValue{}}); len(r.GetMessages()) == 0 || r.GetSearchFlood() != nil {
		t.Errorf("the empty query: %s flood %v", ids(r), r.GetSearchFlood())
	}
	r = search(&mtproto.TLChannelsSearchPosts{Query: mtproto.MakeFlagsString("новости дня")})
	if len(r.GetMessages()) != 1 || r.GetSearchFlood().GetTotalDaily() == 0 {
		t.Fatalf("a text query: %s flood %v", ids(r), r.GetSearchFlood())
	}
	remains := r.GetSearchFlood().GetRemains()
	r = search(&mtproto.TLChannelsSearchPosts{Query: mtproto.MakeFlagsString("новости дня")})
	if r.GetSearchFlood().GetRemains() != remains || !r.GetSearchFlood().GetQueryIsFree() {
		t.Errorf("the same query again: %v", r.GetSearchFlood())
	}

	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsCheckSearchPostsFlood{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_checkSearchPostsFlood),
		Query:       mtproto.MakeFlagsString("новости дня"),
	})
	if err != nil {
		t.Fatalf("channels.checkSearchPostsFlood: %v", err)
	}
	if !o.(interface {
		To_SearchPostsFlood() *mtproto.SearchPostsFlood
	}).To_SearchPostsFlood().GetQueryIsFree() {
		t.Errorf("check: %v", o)
	}
}
