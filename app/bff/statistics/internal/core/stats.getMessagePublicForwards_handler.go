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
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// maxForwardsLimit bounds a page of public forwards (brief of task 103,
// "Пределы"): a limit out of 1..100 is taken into it.
const maxForwardsLimit = 100

// StatsGetMessagePublicForwards5F150144
// stats.getMessagePublicForwards#5f150144 channel:InputChannel msg_id:int offset:string limit:int = stats.PublicForwards;
//
// The copies of a post of a broadcast channel in public channels and
// supergroups, for whoever sees the statistics of the channel, the latest
// first, each as the viewer sees it, with the count of them all. offset is
// empty at first, then the next_offset of the page before; a page with no
// next_offset is the last (core.telegram.org/method/
// stats.getMessagePublicForwards, ADR 0066). Android asks for the post of a
// copy by its saved_from_msg_id in its channel (MessageStatisticActivity.
// java:661-668) and counts by count, else by the page
// (:679-683). A post that is not there, deleted, or a service message is
// MESSAGE_ID_INVALID; the posts of supergroups are not forwarded with
// channel_post: BROADCAST_REQUIRED.
func (c *StatisticsCore) StatsGetMessagePublicForwards5F150144(in *mtproto.TLStatsGetMessagePublicForwards5F150144) (_ *mtproto.Stats_PublicForwards, err error) {
	defer c.observe("stats.getMessagePublicForwards", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.statsChannel(in.GetChannel(), broadcastOnly)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t, err := c.postTarget(c.ctx, ch.GetId(), int64(ch.GetParticipantsCount()), in.GetMsgId())
	if err != nil {
		return nil, err
	}
	src := dao.ForwardSource{Kind: channelstats.ForwardPost, ChannelId: t.channelId, Id: t.object}
	return c.publicForwards(src, in.GetOffset(), in.GetLimit())
}

// forwardsLimit is limit taken into 1..maxForwardsLimit.
func forwardsLimit(limit int32) int {
	return int(max(1, min(limit, maxForwardsLimit)))
}

// publicForwards is a page of the public copies of src for the viewer
// after offset: OFFSET_INVALID for an offset this server did not give.
func (c *StatisticsCore) publicForwards(src dao.ForwardSource, offset string, limit int32) (*mtproto.Stats_PublicForwards, error) {
	var after *dao.ForwardCursor
	if offset != "" {
		cur, ok := dao.ParseForwardCursor(offset)
		if !ok {
			return nil, mtproto.ErrOffsetInvalid
		}
		after = &cur
	}
	n := forwardsLimit(limit)
	method := fmt.Sprintf("forwards:%d:%d:%d:%s", src.Kind, src.Id, n, offset)
	return build(c, c.flightKey(method, src.ChannelId, int64(src.Id), view{}), func(ctx context.Context) (*mtproto.Stats_PublicForwards, error) {
		return c.forwardsPage(ctx, src, after, n)
	})
}

func (c *StatisticsCore) forwardsPage(ctx context.Context, src dao.ForwardSource, after *dao.ForwardCursor, limit int) (*mtproto.Stats_PublicForwards, error) {
	count, err := c.svcCtx.Dao.PublicForwardsCount(ctx, src, c.now)
	if err != nil {
		return nil, err
	}
	rows, err := c.svcCtx.Dao.PublicForwards(ctx, src, c.now, after, limit)
	if err != nil {
		return nil, err
	}
	p := &forwardPeers{}
	p.channel(src.ChannelId)
	forwards, err := c.forwardItems(ctx, rows, p)
	if err != nil {
		return nil, err
	}
	out := &mtproto.Stats_PublicForwards{Count: count, Forwards: forwards}
	// A full page may have more after it; its last row is where the next
	// one starts, also when a copy of it was not for the viewer.
	if len(rows) == limit {
		out.NextOffset = wrapperspb.String(rows[len(rows)-1].After().String())
	}
	if out.Chats, out.Users, err = c.resolveForwardPeers(ctx, p); err != nil {
		return nil, err
	}
	return mtproto.MakeTLStatsPublicForwards(out).To_Stats_PublicForwards(), nil
}

// forwardPeers are the channels and the users a page of forwards refers to.
type forwardPeers struct {
	channels []int64
	users    []int64
}

func (p *forwardPeers) channel(ids ...int64) {
	for _, id := range ids {
		if id != 0 && !slices.Contains(p.channels, id) {
			p.channels = append(p.channels, id)
		}
	}
}

func (p *forwardPeers) user(ids ...int64) {
	for _, id := range ids {
		if id != 0 && !slices.Contains(p.users, id) {
			p.users = append(p.users, id)
		}
	}
}

func (p *forwardPeers) peer(peer *mtproto.Peer) {
	switch peer.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		p.user(peer.GetUserId())
	case mtproto.Predicate_peerChannel:
		p.channel(peer.GetChannelId())
	}
}

// forwardItems are the copies of rows as the viewer sees them, in the order
// of rows: publicForwardMessage of a message, publicForwardStory of a story.
// A copy the viewer may not see (a channel that bans it, a message hidden
// from it) is left out.
func (c *StatisticsCore) forwardItems(ctx context.Context, rows []dao.Forward, p *forwardPeers) ([]*mtproto.PublicForward, error) {
	messageIds := map[int64][]int32{}
	storyIds := map[int64][]int32{}
	var order []int64
	for _, r := range rows {
		if !slices.Contains(order, r.PeerId) {
			order = append(order, r.PeerId)
		}
		if r.Story {
			storyIds[r.PeerId] = append(storyIds[r.PeerId], r.Id)
		} else {
			messageIds[r.PeerId] = append(messageIds[r.PeerId], r.Id)
		}
	}
	type key struct {
		story  bool
		peerId int64
		id     int32
	}
	items := make(map[key]*mtproto.PublicForward, len(rows))
	me := c.MD.GetUserId()
	for _, channelId := range order {
		if ids := messageIds[channelId]; len(ids) > 0 {
			messages, err := c.svcCtx.Messages.ChannelGetMessages(ctx, &channelpb.TLChannelGetMessages{SelfId: me, ChannelId: channelId, Id: ids})
			if err != nil {
				if hidden(err) {
					continue
				}
				return nil, err
			}
			h := mtproto.NewIDListHelper(me)
			h.PickByMessages(messages.GetDatas()...)
			p.user(h.UserIdList...)
			p.channel(h.ChannelIdList...)
			p.channel(channelId)
			for _, m := range messages.GetDatas() {
				items[key{false, channelId, m.GetId()}] = mtproto.MakeTLPublicForwardMessage(&mtproto.PublicForward{Message: m}).To_PublicForward()
			}
		}
		if ids := storyIds[channelId]; len(ids) > 0 {
			peer := stories.ChannelPeer(channelId)
			list, err := c.svcCtx.Dao.Stories().Get(ctx, peer, ids)
			if err != nil {
				return nil, err
			}
			seen, err := c.svcCtx.Dao.Stories().SeenOf(ctx, peer, list, me, false)
			if err != nil {
				return nil, err
			}
			for _, st := range list {
				// A story that left the feeds and the profile since the
				// page was read is not for the viewer (bff stories shown).
				if !st.Active(int32(c.now)) && !st.Pinned {
					continue
				}
				p.channel(channelId)
				p.peer(st.FwdFrom.GetFrom())
				for _, a := range st.MediaAreas {
					if a.GetPredicateName() == mtproto.Predicate_mediaAreaChannelPost {
						p.channel(a.GetChannelId())
					}
				}
				items[key{true, channelId, st.Id}] = mtproto.MakeTLPublicForwardStory(&mtproto.PublicForward{
					Peer:  peer.ToPeer(),
					Story: stories.Item(st, seen(st)),
				}).To_PublicForward()
			}
		}
	}
	out := make([]*mtproto.PublicForward, 0, len(rows))
	for _, r := range rows {
		if item, ok := items[key{r.Story, r.PeerId, r.Id}]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// hidden tells an error of a channel the viewer may not read: the channel
// went private or bans the viewer since the page was read.
func hidden(err error) bool {
	got, want := status.Convert(err), status.Convert(mtproto.ErrChannelPrivate)
	return got.Code() == want.Code() && got.Message() == want.Message()
}

// resolveForwardPeers are the chats and the users of p as the viewer sees
// them.
func (c *StatisticsCore) resolveForwardPeers(ctx context.Context, p *forwardPeers) ([]*mtproto.Chat, []*mtproto.User, error) {
	me := c.MD.GetUserId()
	chats := []*mtproto.Chat{}
	users := []*mtproto.User{}
	if len(p.channels) > 0 {
		list, err := c.svcCtx.Messages.ChannelGetChannelListByIdList(ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: me, IdList: p.channels})
		if err != nil {
			return nil, nil, err
		}
		chats = list.GetChannelListByIdList(me)
	}
	if len(p.users) > 0 {
		list, err := c.svcCtx.Users.UserGetMutableUsersV2(ctx, &userpb.TLUserGetMutableUsersV2{
			Id:      append([]int64{me}, p.users...),
			Privacy: true,
			HasTo:   true,
			To:      []int64{me},
		})
		if err != nil {
			return nil, nil, err
		}
		users = list.GetUserListByIdList(me, p.users...)
	}
	return chats, users, nil
}
