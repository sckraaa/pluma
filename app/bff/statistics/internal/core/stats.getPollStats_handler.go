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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"google.golang.org/grpc/status"
)

// pollHourlyAge is how long a poll may run for its graph to be in hours
// rather than days (brief of task 103, "stats.pollStats"); up to hourlyAge
// it is in fives of minutes.
const pollHourlyAge = 60 * secondsPerDay

// StatsGetPollStats
// stats.getPollStats#c27dfa68 flags:# dark:flags.0?true peer:InputPeer msg_id:int = stats.PollStats;
//
// The votes of a poll over time, a series for each option named by its
// text, in stacked bars (Android draws them so, PollStatisticsBottomSheet.
// java:56): the standing votes by when they were cast, from the sending of
// the poll to now or its closing date; in fives of minutes while that is 48
// hours or less, in hours up to 60 days, in days after. A poll without votes
// is a statsGraphError, on which Android says the statistics come later
// (ChatActivity.java:34959-34963). The statistics are the poll's creator's,
// whose results carry can_view_stats (core.telegram.org/api/stats), and in
// the channel it was sent to also of whoever sees the statistics of that
// channel (ADR 0066, p. 1). Every refusal is PEER_ID_INVALID, the one error
// of the method's page: a peer or a message the viewer may not read, a
// message without a poll, a poll that is not the viewer's to see.
func (c *StatisticsCore) StatsGetPollStats(in *mtproto.TLStatsGetPollStats) (_ *mtproto.Stats_PollStats, err error) {
	defer c.observe("stats.getPollStats", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	rec, err := c.pollOf(in.GetPeer(), in.GetMsgId())
	if err != nil {
		return nil, err
	}
	v := view{lang: c.MD.GetLangCode(), dark: in.GetDark()}
	return build(c, c.flightKey("poll", rec.PeerId, rec.Id, v), func(ctx context.Context) (*mtproto.Stats_PollStats, error) {
		g, err := pollGraph(ctx, c.svcCtx.Dao, rec, c.now, langpack.StatsText(langpack.StatsNoVotes, v.lang))
		if err != nil {
			return nil, err
		}
		votes, err := g.Build(langpack.StatsText, v.lang, v.dark)
		if err != nil {
			return nil, err
		}
		return mtproto.MakeTLStatsPollStats(&mtproto.Stats_PollStats{VotesGraph: votes}).To_Stats_PollStats(), nil
	})
}

// refused tells an error that refuses the viewer (400 or 403), as against
// one of the services.
func refused(err error) bool {
	switch status.Code(err) {
	case mtproto.ErrBadRequest, mtproto.ErrForbidden:
		return true
	}
	return false
}

// pollOf is the poll of the message msgId of peer as the viewer has it, if
// the viewer may see its statistics; PEER_ID_INVALID otherwise.
func (c *StatisticsCore) pollOf(peer *mtproto.InputPeer, msgId int32) (*polls.Record, error) {
	if peer == nil || msgId <= 0 {
		return nil, mtproto.ErrPeerIdInvalid
	}
	me := c.MD.GetUserId()
	p := mtproto.FromInputPeer2(me, peer)
	var (
		pollId int64
		err    error
		// admin: the viewer sees the statistics of the channel of peer.
		admin bool
	)
	switch p.PeerType {
	case mtproto.PEER_CHANNEL:
		var mc *channelpb.MutableChannel
		if mc, err = c.statsChannelOf(peer); err != nil {
			return nil, err
		}
		if err = c.readable(mc); err != nil {
			return nil, mtproto.ErrPeerIdInvalid
		}
		switch err = c.mayView(mc, anyChannel); {
		case err == nil:
			admin = true
		case !refused(err):
			return nil, err
		}
		if pollId, err = c.svcCtx.Dao.ChannelPollId(c.ctx, p.PeerId, msgId); err != nil {
			return nil, err
		}
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
		peerType, peerId := p.PeerType, p.PeerId
		if peerType == mtproto.PEER_SELF {
			peerType, peerId = mtproto.PEER_USER, me
		}
		did := mtproto.MakeDialogId(me, peerType, peerId)
		if pollId, err = c.svcCtx.Dao.BoxPollId(c.ctx, me, msgId, did.A, did.B); err != nil {
			return nil, err
		}
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	rec, err := c.svcCtx.Dao.Poll(c.ctx, pollId)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	switch {
	case rec.Creator == me:
	// An admin sees the statistics of the polls sent to its channel, not
	// of copies forwarded there from elsewhere.
	case admin && rec.PeerType == mtproto.PEER_CHANNEL && rec.PeerId == p.PeerId && rec.MessageId == msgId:
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	return rec, nil
}

// statsChannelOf is the channel of an inputPeerChannel with its access hash
// checked, PEER_ID_INVALID for a wrong one or a channel that is not there.
func (c *StatisticsCore) statsChannelOf(peer *mtproto.InputPeer) (*channelpb.MutableChannel, error) {
	if peer.GetPredicateName() != mtproto.Predicate_inputPeerChannel || peer.GetChannelId() == 0 {
		return nil, mtproto.ErrPeerIdInvalid
	}
	mc, err := c.mutableChannel(peer.GetChannelId())
	if err != nil {
		if refused(err) {
			return nil, mtproto.ErrPeerIdInvalid
		}
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != peer.GetAccessHash() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	return mc, nil
}

// pollScale is the scale of the graph of a poll that ran from start to end.
func pollScale(start, end int64) statsgraph.Scale {
	switch d := end - start; {
	case d <= hourlyAge:
		return statsgraph.FiveMin
	case d <= pollHourlyAge:
		return statsgraph.Hour
	}
	return statsgraph.Day
}

// pollBuckets are the buckets of the graph of rec at now: from its sending
// to now or its closing date, two at least so that a young poll is drawn
// (the one before its sending has no votes).
func pollBuckets(rec *polls.Record, now int64) statsgraph.Buckets {
	start, end := rec.Date, now
	if rec.CloseDate > 0 && rec.CloseDate < end {
		end = rec.CloseDate
	}
	end = max(start, end)
	b := statsgraph.Span(pollScale(start, end), start, end)
	if b.Len() < statsgraph.MinPoints {
		b.First = b.Last - 1
	}
	return b
}

// pollGraph is the graph of the votes of rec at now; noVotes is the text
// of its statsGraphError when it has none.
func pollGraph(ctx context.Context, d *dao.Dao, rec *polls.Record, now int64, noVotes string) (*statsgraph.Graph, error) {
	b := pollBuckets(rec, now)
	rows, err := d.VoteCounts(ctx, rec.Id, rangeOf(b))
	if err != nil {
		return nil, err
	}
	return votesGraph(b, rec.Poll.GetAnswers(), rows, noVotes), nil
}

// votesGraph spreads the votes of rows over b, a series for each of
// answers in their order; the votes of an option no longer among them are
// not drawn. With no votes it has no series: the statsGraphError of text
// noVotes.
func votesGraph(b statsgraph.Buckets, answers []*mtproto.PollAnswer, rows []dao.KeyCount, noVotes string) *statsgraph.Graph {
	g := &statsgraph.Graph{Type: statsgraph.Bar, Stacked: true, Scale: b.Scale, X: b.X()}
	index := make(map[string]int, len(answers))
	values := make([][]int64, len(answers))
	for i, a := range answers {
		index[string(a.GetOption())] = i
		values[i] = make([]int64, b.Len())
	}
	votes := int64(0)
	for _, r := range rows {
		i, ok := index[r.K]
		if !ok {
			continue
		}
		if j, ok := b.Index(r.B); ok {
			values[i][j] += r.N
			votes += r.N
		}
	}
	if votes == 0 {
		g.Empty = noVotes
		return g
	}
	for i, a := range answers {
		g.Series = append(g.Series, statsgraph.Series{Name: a.GetTextString(), Values: values[i]})
	}
	return g
}
