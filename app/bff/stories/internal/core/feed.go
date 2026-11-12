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
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/protobuf/proto"
)

// feedEntry is one peer of the caller's feeds with its stories the caller
// may open.
type feedEntry struct {
	view   *peerView
	list   []*stories.Story
	hidden bool
}

// feed collects the stories of the feeds of the caller at the time at: its
// own, those of its contacts and those of the channels it is in. The
// clients keep the stories of users who are not their contacts out of the
// rows anyway (Android StoriesController.java:219-230,
// fixDeletedAndNonContactsStories). hidden tells, for each, whether the
// caller moved the peer to the archive row.
func (c *StoriesCore) feed(at int32) ([]*feedEntry, error) {
	var out []*feedEntry
	me := c.MD.UserId

	posting, err := c.svcCtx.Dao.Store.ActivePeers(c.ctx, mtproto.PEER_USER, at)
	if err != nil {
		return nil, err
	}
	if len(posting) > 0 {
		contacts, err := c.svcCtx.Dao.UserGetContactIdList(c.ctx, &userpb.TLUserGetContactIdList{UserId: me})
		if err != nil {
			return nil, err
		}
		var owners []int64
		for _, id := range posting {
			if id == me || containsId(contacts.GetDatas(), id) {
				owners = append(owners, id)
			}
		}
		if len(owners) > 0 {
			ids := owners
			if !slices.Contains(ids, me) {
				ids = append(slices.Clone(ids), me)
			}
			users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{me}})
			if err != nil {
				return nil, err
			}
			active, err := c.svcCtx.Dao.Store.ActiveOf(c.ctx, mtproto.PEER_USER, owners, at)
			if err != nil {
				return nil, err
			}
			for _, id := range owners {
				v := &peerView{peer: stories.UserPeer(id), own: id == me}
				e := &feedEntry{view: v}
				if !v.own {
					owner, ok := users.GetImmutableUser(id)
					if !ok || owner.Deleted() {
						continue
					}
					if v.blocked, err = c.shutOutBy(id); err != nil {
						return nil, err
					}
					v.owner = owner
					// Which row the stories of a contact are in is the
					// flag of the contact, as the clients read it from
					// the user (StoriesController.java:496-511).
					if shown, err := users.GetUnsafeUser(me, id); err == nil {
						e.hidden = shown.GetStoriesHidden()
					}
				}
				if e.list, err = c.visible(v, active[id]); err != nil {
					return nil, err
				}
				if len(e.list) > 0 {
					out = append(out, e)
				}
			}
		}
	}

	channels, err := c.svcCtx.Dao.Store.ActivePeers(c.ctx, mtproto.PEER_CHANNEL, at)
	if err != nil {
		return nil, err
	}
	if len(channels) > 0 {
		list, err := c.svcCtx.Dao.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: me, IdList: channels})
		if err != nil {
			return nil, err
		}
		var joined []int64
		views := map[int64]*peerView{}
		for _, mc := range list.GetDatas() {
			p, _ := mc.GetParticipant(me)
			if !p.Member() || mc.ToChat(me, at).GetPredicateName() != mtproto.Predicate_channel {
				continue
			}
			id := mc.GetChannel().GetId()
			joined = append(joined, id)
			views[id] = &peerView{peer: stories.ChannelPeer(id), channel: mc, me: p}
		}
		active, err := c.svcCtx.Dao.Store.ActiveOf(c.ctx, mtproto.PEER_CHANNEL, joined, at)
		if err != nil {
			return nil, err
		}
		for _, id := range joined {
			if len(active[id]) > 0 {
				out = append(out, &feedEntry{view: views[id], list: active[id], hidden: views[id].me.GetStoriesHidden()})
			}
		}
	}
	return out, nil
}

// feedAnswer builds peerStories of the entries, the caller's own first,
// then the peers with stories it has not read, then the others, each group
// by its newest story; the clients sort the rows again by their own rule
// (StoriesController.java:216). It returns them with what they refer to
// and the state of the list: a digest of exactly what is answered, so the
// same state means the same stories, read marks and counters.
func (c *StoriesCore) feedAnswer(entries []*feedEntry) ([]*mtproto.PeerStories, *refs, string, error) {
	reads, err := c.svcCtx.Dao.Store.Reads(c.ctx, c.MD.UserId)
	if err != nil {
		return nil, nil, "", err
	}
	rank := func(e *feedEntry) int {
		newest := e.list[len(e.list)-1]
		switch {
		case e.view.own:
			return 0
		case newest.Id > reads[e.view.peer]:
			return 1
		}
		return 2
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		return a.list[len(a.list)-1].Date > b.list[len(b.list)-1].Date
	})
	var (
		out    = make([]*mtproto.PeerStories, 0, len(entries))
		r      = &refs{}
		digest = sha256.New()
	)
	for _, e := range entries {
		seen, err := c.svcCtx.Dao.Store.SeenOf(c.ctx, e.view.peer, e.list, c.MD.UserId, e.view.own)
		if err != nil {
			return nil, nil, "", err
		}
		ps := stories.PeerStories(e.view.peer, e.list, reads[e.view.peer], seen)
		out = append(out, ps)
		r.peer(e.view.peer)
		for _, st := range e.list {
			r.story(st, e.view.own)
		}
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(ps)
		if err != nil {
			return nil, nil, "", err
		}
		digest.Write(data)
	}
	return out, r, hex.EncodeToString(digest.Sum(nil)[:16]), nil
}
