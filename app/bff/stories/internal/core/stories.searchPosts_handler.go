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
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// maxSearchPage bounds a page of stories.searchPosts; iOS asks for 100
// (StoryListContext.swift:2532), Android for the size of its grid.
const maxSearchPage = 100

// StoriesSearchPosts
// stories.searchPosts#d1810907 flags:# hashtag:flags.0?string area:flags.1?MediaArea peer:flags.2?InputPeer offset:string limit:int = stories.FoundStories;
//
// The public stories with a hashtag or a cashtag, or with a place (task
// 104, ADR 0069, p. 5), newest first, of peer when it is set. A user's
// story is found when its rules are "everyone" and let the caller in, and
// its owner did not block the caller or hide its stories from it; a
// channel's when the channel has a public username and the caller is not
// banned there. Either way the story is in the feeds or in the profile.
//
// count is every story the caller finds among the candidates the index
// gives (stories.MaxSearchScan at most), the same for each page; offset is
// the cursor of the last story of the page before, and next_offset is left
// out on the last page. Android stops at count or without next_offset
// (StoriesController.java:3198), iOS without next_offset.
func (c *StoriesCore) StoriesSearchPosts(in *mtproto.TLStoriesSearchPosts) (*mtproto.Stories_FoundStories, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	q, err := storySearchOf(in.GetHashtag(), in.GetArea())
	if err != nil {
		return nil, err
	}
	after, err := parseSearchOffset(in.GetOffset())
	if err != nil {
		return nil, err
	}
	f := &finder{c: c, views: map[stories.Peer]*peerView{}}
	var peer *stories.Peer
	if in.GetPeer() != nil {
		v, err := c.view(in.GetPeer())
		if err != nil {
			c.Logger.Errorf("stories.searchPosts - error: %v", err)
			return nil, err
		}
		f.views[v.peer] = findable(v)
		peer = &v.peer
	}

	at := now()
	var candidates []*stories.Story
	if q.place != nil {
		candidates, err = c.svcCtx.Dao.Store.SearchPlaces(c.ctx, *q.place, peer, stories.SearchCursor{}, stories.MaxSearchScan, at)
	} else {
		candidates, err = c.svcCtx.Dao.Store.SearchTags(c.ctx, q.kind, q.tag, peer, stories.SearchCursor{}, stories.MaxSearchScan, at)
	}
	if err != nil {
		c.Logger.Errorf("stories.searchPosts - error: %v", err)
		return nil, err
	}
	found, err := f.found(candidates)
	if err != nil {
		c.Logger.Errorf("stories.searchPosts - error: %v", err)
		return nil, err
	}
	page, next := searchPage(found, after, int(in.GetLimit()))
	r, err := c.foundStories(f, page)
	if err != nil {
		c.Logger.Errorf("stories.searchPosts - error: %v", err)
		return nil, err
	}
	r.Count = int32(len(found))
	if next != "" {
		r.NextOffset = wrapperspb.String(next)
	}
	return mtproto.MakeTLStoriesFoundStories(r).To_Stories_FoundStories(), nil
}

// storySearch is what stories.searchPosts looks for: a tag of kind, or a
// place.
type storySearch struct {
	kind  int8
	tag   string
	place *stories.Place
}

// storySearchOf reads the query of stories.searchPosts: exactly one of
// hashtag and area ("Either hashtag or area must always be set",
// core.telegram.org/method/stories.searchPosts), else SEARCH_QUERY_EMPTY,
// as channels.searchPosts answers a query it cannot take.
//
// A hashtag that starts with "$" is a cashtag, any other one a hashtag
// with or without its "#": Android sends the sign (HashtagActivity.java:
// 63-73, HashtagsSearchAdapter.java:96), iOS and Web K cut the "#" only
// (StoryListContext.swift:2516, appStoriesManager.ts:1864). What is left
// must be one whole tag of 1 to stories.MaxTag letters, digits and
// underscores, else HASHTAG_INVALID.
//
// An area is a mediaAreaVenue with a venue id or a mediaAreaGeoPoint with
// an address (the method's page: a point is searched by its address only);
// any other is MEDIA_INVALID.
func storySearchOf(hashtag *wrapperspb.StringValue, area *mtproto.MediaArea) (storySearch, error) {
	if (hashtag == nil) == (area == nil) {
		return storySearch{}, mtproto.ErrSearchQueryEmpty
	}
	if area != nil {
		place, ok := stories.PlaceOf(area)
		if !ok {
			return storySearch{}, mtproto.ErrMediaInvalid
		}
		return storySearch{place: &place}, nil
	}
	kind, body := stories.TagHashtag, hashtag.GetValue()
	if rest, ok := strings.CutPrefix(body, "$"); ok {
		kind, body = stories.TagCashtag, rest
	} else {
		body = strings.TrimPrefix(body, "#")
	}
	tag, ok := stories.NormalizeTag(kind, body)
	// NormalizeTag stops at the first character a tag does not have; the
	// query must be the whole tag (lower casing keeps the number of
	// characters).
	if !ok || utf8.RuneCountInString(tag) != utf8.RuneCountInString(body) {
		return storySearch{}, errHashtagInvalid
	}
	return storySearch{kind: kind, tag: tag}, nil
}

// parseSearchOffset reads the offset of stories.searchPosts: empty for the
// first page, else the next_offset of the page before (searchOffset).
func parseSearchOffset(offset string) (stories.SearchCursor, error) {
	if offset == "" {
		return stories.SearchCursor{}, nil
	}
	parts := strings.Split(offset, ".")
	if len(parts) != 4 {
		return stories.SearchCursor{}, mtproto.ErrOffsetInvalid
	}
	var n [4]int64
	for i, bits := range [4]int{32, 32, 64, 32} {
		v, err := strconv.ParseInt(parts[i], 10, bits)
		if err != nil {
			return stories.SearchCursor{}, mtproto.ErrOffsetInvalid
		}
		n[i] = v
	}
	cur := stories.SearchCursor{Date: int32(n[0]), Peer: stories.Peer{Type: int32(n[1]), Id: n[2]}, Id: int32(n[3])}
	if cur == (stories.SearchCursor{}) {
		return stories.SearchCursor{}, mtproto.ErrOffsetInvalid
	}
	return cur, nil
}

// searchOffset is the next_offset after st: its place in the order of the
// results, which the clients pass back as it is.
func searchOffset(st *stories.Story) string {
	cur := stories.CursorOf(st)
	return fmt.Sprintf("%d.%d.%d.%d", cur.Date, cur.Peer.Type, cur.Peer.Id, cur.Id)
}

// searchAfter tells whether st comes after the cursor in the order of the
// results (stories.SearchCursor: date, then the type and id of the peer and
// the id of the story, all descending). Everything comes after the zero
// cursor.
func searchAfter(st *stories.Story, cur stories.SearchCursor) bool {
	if cur == (stories.SearchCursor{}) {
		return true
	}
	switch {
	case st.Date != cur.Date:
		return st.Date < cur.Date
	case st.Peer.Type != cur.Peer.Type:
		return st.Peer.Type < cur.Peer.Type
	case st.Peer.Id != cur.Peer.Id:
		return st.Peer.Id < cur.Peer.Id
	}
	return st.Id < cur.Id
}

// searchPage is the page of found, in the order of the results, after the
// cursor by limit (taken as 1 to maxSearchPage), with the offset of the
// next page: empty when nothing follows.
func searchPage(found []*stories.Story, after stories.SearchCursor, limit int) ([]*stories.Story, string) {
	limit = min(max(limit, 1), maxSearchPage)
	from := len(found)
	for i, st := range found {
		if searchAfter(st, after) {
			from = i
			break
		}
	}
	to := min(from+limit, len(found))
	page := found[from:to]
	if to == len(found) || len(page) == 0 {
		return page, ""
	}
	return page, searchOffset(page[len(page)-1])
}

// finder decides which candidates of a search the caller finds, reading
// each owner once.
type finder struct {
	c *StoriesCore
	// views are the owners read so far: nil for one whose stories the
	// caller does not find.
	views map[stories.Peer]*peerView
}

// findable is v if the caller may find stories of its peer: a user who did
// not block the caller nor hides its stories from it (its rules are checked
// for each story), a channel with a public username; else nil. A channel
// the caller is banned in, or may not read, never gets here (viewOf).
func findable(v *peerView) *peerView {
	if v.peer.IsUser() {
		if v.blocked {
			return nil
		}
		return v
	}
	if !publicChannel(v.channel.GetChannel()) {
		return nil
	}
	return v
}

// publicChannel tells whether a channel shows a public username: the
// editable one, or another active one (channels.toggleUsername, task 078).
func publicChannel(ch *channelpb.ImmutableChannel) bool {
	if ch.Public() {
		return true
	}
	for _, u := range ch.GetUsernames() {
		if u.GetActive() {
			return true
		}
	}
	return false
}

// view is the owner peer as the search reads it: nil when the caller finds
// none of its stories. An owner that is gone or that the caller may not
// see (a deleted account, a private channel, a ban) is one of those.
func (f *finder) view(peer stories.Peer) (*peerView, error) {
	if v, ok := f.views[peer]; ok {
		return v, nil
	}
	v, err := f.c.viewOf(peer)
	switch {
	case err == nil:
		v = findable(v)
	case refused(err):
		v = nil
	default:
		return nil, err
	}
	f.views[peer] = v
	return v, nil
}

// found keeps the candidates the caller finds, in their order: those of
// owners it may find whose rules let it in (visible).
func (f *finder) found(candidates []*stories.Story) ([]*stories.Story, error) {
	out := make([]*stories.Story, 0, len(candidates))
	for _, st := range candidates {
		v, err := f.view(st.Peer)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		list, err := f.c.visible(v, []*stories.Story{st})
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

// foundStories builds stories.foundStories of page, stories the caller
// found, each with its peer and as the caller sees it, with what they
// refer to.
func (c *StoriesCore) foundStories(f *finder, page []*stories.Story) (*mtproto.Stories_FoundStories, error) {
	var owners []stories.Peer
	byOwner := map[stories.Peer][]*stories.Story{}
	for _, st := range page {
		if _, ok := byOwner[st.Peer]; !ok {
			owners = append(owners, st.Peer)
		}
		byOwner[st.Peer] = append(byOwner[st.Peer], st)
	}
	seen := make(map[stories.Peer]func(*stories.Story) stories.Seen, len(owners))
	r := &refs{}
	for _, p := range owners {
		v := f.views[p]
		s, err := c.svcCtx.Dao.Store.SeenOf(c.ctx, p, byOwner[p], c.MD.UserId, v.own)
		if err != nil {
			return nil, err
		}
		seen[p] = s
		for _, st := range byOwner[p] {
			r.story(st, v.own)
		}
	}
	out := make([]*mtproto.FoundStory, 0, len(page))
	for _, st := range page {
		out = append(out, mtproto.MakeTLFoundStory(&mtproto.FoundStory{
			Peer:  st.Peer.ToPeer(),
			Story: stories.Item(st, seen[st.Peer](st)),
		}).To_FoundStory())
	}
	users, chats, err := c.resolve(r)
	if err != nil {
		return nil, err
	}
	return &mtproto.Stories_FoundStories{
		Stories: out,
		Chats:   chats,
		Users:   users,
	}, nil
}
