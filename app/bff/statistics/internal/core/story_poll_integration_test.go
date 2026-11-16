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

//go:build integration

package core

import (
	"context"
	"database/sql"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// fakeMessages answers channel.getMessages and channel.getChannelListByIdList
// as biz channel does for a viewer of public channels: the messages asked
// for, and CHANNEL_PRIVATE for the channels in private (one that bans the
// viewer).
type fakeMessages struct {
	private map[int64]bool
}

func (f fakeMessages) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	if f.private[in.ChannelId] {
		return nil, mtproto.ErrChannelPrivate
	}
	out := &channelpb.Vector_Message{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, mtproto.MakeTLMessage(&mtproto.Message{
			Id:     id,
			PeerId: mtproto.MakePeerChannel(in.ChannelId),
			Post:   true,
		}).To_Message())
	}
	return out, nil
}

func (f fakeMessages) ChannelGetChannelListByIdList(_ context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	out := &channelpb.Vector_MutableChannel{}
	for _, id := range in.IdList {
		out.Datas = append(out.Datas, channelpb.MakeTLMutableChannel(&channelpb.MutableChannel{
			Channel: channelpb.MakeTLImmutableChannel(&channelpb.ImmutableChannel{
				Id: id, AccessHash: id * 3, Broadcast: true, Title: "c", Username: "c", Date: day0,
			}).To_ImmutableChannel(),
		}).To_MutableChannel())
	}
	return out, nil
}

// statsDB starts MySQL with the schema and returns a statement runner and
// a core of the viewer, the creator of the broadcast channel channelId, at
// now.
func statsDB(t *testing.T, now int64) (func(q string, args ...any), *StatisticsCore) {
	t.Helper()
	ctx := context.Background()
	m, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Terminate(ctx) })
	db, err := sql.Open("mysql", m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mc := mutable(channelOpts{broadcast: true}, participant(channelpb.RoleCreator, 0))
	mc.Channel.ParticipantsCount = 4
	core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
	cfg := m.Config()
	core.svcCtx.Dao = dao.New(sqlx.NewMySQL(&cfg))
	core.svcCtx.Messages = fakeMessages{private: map[int64]bool{504: true}}
	core.svcCtx.Users = fakeUsers{}
	return exec, core
}

// channels makes the channels of the copies: 501, 503, 504 and 505 public,
// 502 private, 505 blocked by the owner of the server.
func channels(exec func(q string, args ...any)) {
	for _, c := range []struct {
		id      int64
		public  bool
		blocked bool
	}{{501, true, false}, {502, false, false}, {503, true, false}, {504, true, false}, {505, true, true}} {
		exec("INSERT INTO channels (id, access_hash, creator_user_id, title, broadcast, blocked, `date`) VALUES (?, ?, 9, 'c', 1, ?, ?)", c.id, c.id*3, c.blocked, day0)
		if c.public {
			exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", "pub"+string(rune('a'+c.id-501)), mtproto.PEER_CHANNEL, c.id)
		}
	}
}

func message(t *testing.T, m *mtproto.Message) string {
	t.Helper()
	s, err := jsonx.MarshalToString(m)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestStoryStats: the statistics of an older story by the day, zooming
// into its hours, and of a young one by the hour; its views, shares (the
// messages that shared it and its reposts) and reactions; the errors of a
// story that is not there and of a channel that never posted one.
func TestStoryStats(t *testing.T) {
	now := day0 + 40*day + 10*hour
	exec, core := statsDB(t, now)
	channels(exec)
	story := func(peer int64, id int32, date, expire int64, pinned bool, fwdPeer int64, fwdId int32, forwards int) {
		fwdType := 0
		if fwdPeer != 0 {
			fwdType = mtproto.PEER_CHANNEL
		}
		exec("INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date, pinned, privacy, caption, entities, media, media_areas, fwd_from, music, mentions, fwd_peer_type, fwd_peer_id, fwd_story_id, forwards_count)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', '', '', '', '', '', '', ?, ?, ?, ?)",
			mtproto.PEER_CHANNEL, peer, id, int64(id)*100+peer, viewer, date, expire, pinned, fwdType, fwdPeer, fwdId, forwards)
	}
	story(channelId, 1, now-6*day, now-5*day, false, 0, 0, 4)
	story(channelId, 2, now-10*hour, now+14*hour, false, 0, 0, 0)
	exec("INSERT INTO story_counters (peer_type, peer_id, last_id) VALUES (?, ?, 2)", mtproto.PEER_CHANNEL, channelId)
	// Reposts of story 1 in a public channel, a private one, and a public
	// one where it left the feeds and is not in the profile.
	story(501, 1, now-4*day, now+hour, false, channelId, 1, 0)
	story(502, 1, now-3*day, now+hour, false, channelId, 1, 0)
	story(503, 1, now-3*day, now-2*day, false, channelId, 1, 0)
	exec("INSERT INTO story_views (peer_type, peer_id, story_id, viewer_id, `date`, reaction, reaction_date) VALUES (?, ?, 1, 2, ?, 'e🔥', ?), (?, ?, 1, 3, ?, '', 0), (?, ?, 2, 2, ?, 'e👎', ?)",
		mtproto.PEER_CHANNEL, channelId, now-6*day+hour, now-6*day+2*hour, mtproto.PEER_CHANNEL, channelId, now-5*day,
		mtproto.PEER_CHANNEL, channelId, now-9*hour, now-8*hour)
	exec("INSERT INTO story_shares (peer_type, peer_id, story_id, user_id, random_id, `date`) VALUES (?, ?, 1, 3, 7, ?)", mtproto.PEER_CHANNEL, channelId, now-5*day+hour)

	st, err := core.StatsGetStoryStats(&mtproto.TLStatsGetStoryStats{Peer: inputPeerChannel(hash), Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	x, ys := parseGraph(t, "story 1", st.GetViewsGraph())
	// The share and the three reposts, whatever became of them.
	if x[1]-x[0] != 86400*1000 || len(x) != 7 || sum(ys["Просмотры"]) != 2 || sum(ys["Пересылки"]) != 4 {
		t.Errorf("story 1 %v %v", x, ys)
	}
	_, ys = parseGraph(t, "story 1 reactions", st.GetReactionsByEmotionGraph())
	if sum(ys["Положительные"]) != 1 || sum(ys["Отрицательные"]) != 0 {
		t.Errorf("story 1 reactions %v", ys)
	}
	zoom, err := core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{Token: st.GetViewsGraph().GetZoomToken().GetValue(), X: wrapperspb.Int64(x[0])})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "story 1 zoom", zoom)
	if x[1]-x[0] != 3600*1000 || sum(ys["Просмотры"]) != 2 || x[0] != (now-6*day)/3600*3600*1000 {
		t.Errorf("story 1 zoom %v %v", x[:2], ys)
	}

	st, err = core.StatsGetStoryStats(&mtproto.TLStatsGetStoryStats{Peer: inputPeerChannel(hash), Id: 2})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "story 2", st.GetViewsGraph())
	if x[1]-x[0] != 3600*1000 || sum(ys["Просмотры"]) != 1 || st.GetViewsGraph().GetZoomToken() != nil {
		t.Errorf("story 2 %v %v", x[:2], ys)
	}
	_, ys = parseGraph(t, "story 2 reactions", st.GetReactionsByEmotionGraph())
	if sum(ys["Отрицательные"]) != 1 {
		t.Errorf("story 2 reactions %v", ys)
	}

	if _, err = core.StatsGetStoryStats(&mtproto.TLStatsGetStoryStats{Peer: inputPeerChannel(hash), Id: 99}); !sameError(err, errStoryIdInvalid) {
		t.Errorf("story 99: %v", err)
	}
	exec("DELETE FROM story_counters")
	if _, err = core.StatsGetStoryStats(&mtproto.TLStatsGetStoryStats{Peer: inputPeerChannel(hash), Id: 99}); !sameError(err, mtproto.ErrStoriesNeverCreated) {
		t.Errorf("no stories: %v", err)
	}
}

// TestPublicForwards: the copies of a post and of a story in public
// channels, the latest first, page by page, with the count of them all;
// the copies in private, blocked or deleted channels, deleted messages and
// stories gone from view are not listed.
func TestPublicForwards(t *testing.T) {
	now := day0 + 40*day + 10*hour
	exec, core := statsDB(t, now)
	channels(exec)
	post := func(channel int64, id int32, date int64, deleted bool) {
		exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
			channel, id, viewer, id, `{"predicate_name":"message"}`, date, deleted)
	}
	post(channelId, 10, now-5*day, false)
	forward := func(kind int, src int32, dst int64, msg int32, date int64) {
		exec("INSERT INTO channel_stats_public_forwards (src_kind, src_peer_type, src_peer_id, src_id, dst_channel_id, dst_message_id, `date`) VALUES (?, ?, ?, ?, ?, ?, ?)",
			kind, mtproto.PEER_CHANNEL, channelId, src, dst, msg, date)
	}
	for _, f := range []struct {
		dst     int64
		msg     int32
		date    int64
		deleted bool
	}{
		{501, 1, now - 3*hour, false},
		{502, 1, now - 2*hour, false}, // a private channel
		{503, 5, now - hour, true},    // a deleted copy
		{501, 2, now - hour, false},
		{504, 1, now - hour, false},   // a channel that bans the viewer
		{505, 1, now - hour/2, false}, // a blocked channel
		{501, 7, now - 2*day, false},  // a share of the story
		{502, 3, now - day, false},    // a share of the story, private
		{503, 9, now - 30*day, false}, // a copy of another post
	} {
		post(f.dst, f.msg, f.date, f.deleted)
	}
	for _, f := range []struct {
		dst  int64
		msg  int32
		date int64
	}{{501, 1, now - 3*hour}, {502, 1, now - 2*hour}, {503, 5, now - hour}, {501, 2, now - hour}, {504, 1, now - hour}, {505, 1, now - hour/2}} {
		forward(1, 10, f.dst, f.msg, f.date)
	}
	forward(1, 11, 503, 9, now-30*day)
	forward(2, 1, 501, 7, now-2*day)
	forward(2, 1, 502, 3, now-day)

	page, err := core.StatsGetMessagePublicForwards5F150144(&mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: inputChannel(hash), MsgId: 10, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	// 501/2 and 504/1 an hour ago (504 is not for the viewer), then 501/1.
	if page.GetCount() != 3 || len(page.GetForwards()) != 1 || page.GetNextOffset() == nil {
		t.Fatalf("first page %v", page)
	}
	if m := page.GetForwards()[0].GetMessage(); page.GetForwards()[0].GetPredicateName() != mtproto.Predicate_publicForwardMessage || m.GetId() != 2 || m.GetPeerId().GetChannelId() != 501 {
		t.Errorf("first forward %v", page.GetForwards()[0])
	}
	chats := map[int64]bool{}
	for _, c := range page.GetChats() {
		chats[c.GetId()] = true
	}
	if !chats[channelId] || !chats[501] || chats[504] || len(chats) != 2 {
		t.Errorf("chats %v", chats)
	}
	page, err = core.StatsGetMessagePublicForwards5F150144(&mtproto.TLStatsGetMessagePublicForwards5F150144{
		Channel: inputChannel(hash), MsgId: 10, Limit: 2, Offset: page.GetNextOffset().GetValue(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.GetCount() != 3 || len(page.GetForwards()) != 1 || page.GetNextOffset() != nil || page.GetForwards()[0].GetMessage().GetId() != 1 {
		t.Errorf("last page %v", page)
	}

	// The story: its share in 501 and its repost there; the share in 502
	// and the reposts in 502 (private) and 503 (gone from view) are not
	// listed.
	exec("INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date, privacy, caption, entities, media, media_areas, fwd_from, music, mentions)"+
		" VALUES (?, ?, 1, 1, ?, ?, ?, '', '', '', '', '', '', '', '')", mtproto.PEER_CHANNEL, channelId, viewer, now-6*day, now-5*day)
	exec("INSERT INTO story_counters (peer_type, peer_id, last_id) VALUES (?, ?, 1)", mtproto.PEER_CHANNEL, channelId)
	for _, r := range []struct {
		peer         int64
		date, expire int64
	}{{501, now - 4*day, now + hour}, {502, now - 3*day, now + hour}, {503, now - 3*day, now - 2*day}} {
		exec("INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date, privacy, caption, entities, media, media_areas, fwd_from, music, mentions, fwd_peer_type, fwd_peer_id, fwd_story_id)"+
			" VALUES (?, ?, 1, ?, 9, ?, ?, '', '', '', '', '', '', '', '', ?, ?, 1)",
			mtproto.PEER_CHANNEL, r.peer, r.peer, r.date, r.expire, mtproto.PEER_CHANNEL, channelId)
	}
	var got []string
	offset := ""
	for range 4 {
		page, err = core.StatsGetStoryPublicForwards(&mtproto.TLStatsGetStoryPublicForwards{Peer: inputPeerChannel(hash), Id: 1, Limit: 1, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if page.GetCount() != 2 {
			t.Errorf("story count %d", page.GetCount())
		}
		for _, f := range page.GetForwards() {
			switch f.GetPredicateName() {
			case mtproto.Predicate_publicForwardMessage:
				got = append(got, "message")
				if f.GetMessage().GetId() != 7 {
					t.Errorf("story share %v", f)
				}
			case mtproto.Predicate_publicForwardStory:
				got = append(got, "story")
				if f.GetPeer().GetChannelId() != 501 || f.GetStory().GetId() != 1 || !f.GetStory().GetPublic() {
					t.Errorf("story repost %v", f)
				}
			}
		}
		if page.GetNextOffset() == nil {
			break
		}
		offset = page.GetNextOffset().GetValue()
	}
	if len(got) != 2 || got[0] != "message" || got[1] != "story" {
		t.Errorf("story forwards %v", got)
	}
}

// TestPollStats: the votes of a poll by option and time, for its creator
// and for the admins of the channel it was sent to; a poll without votes;
// the refusals.
func TestPollStats(t *testing.T) {
	now := day0 + 40*day + 10*hour
	exec, core := statsDB(t, now)
	pollMessage := func(id int32, pollId int64) string {
		return message(t, mtproto.MakeTLMessage(&mtproto.Message{
			Id: id,
			Media: mtproto.MakeTLMessageMediaPoll(&mtproto.MessageMedia{
				Poll: mtproto.MakeTLPoll(&mtproto.Poll{Id: pollId}).To_Poll(),
			}).To_MessageMedia(),
		}).To_Message())
	}
	poll := func(id, creator int64, peerType int32, peerId int64, msgId int32, date int64) {
		rec := &polls.Record{
			Id: id, Creator: creator, PeerType: peerType, PeerId: peerId, MessageId: msgId, Date: date,
			Poll: mtproto.MakeTLPoll(&mtproto.Poll{Id: id, Answers: []*mtproto.PollAnswer{answer("Да", "0"), answer("Нет", "1")}}).To_Poll(),
		}
		args, err := rec.Args()
		if err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO polls ("+polls.Columns+") VALUES ("+polls.InList(14)+")", args...)
	}
	// A poll of user 2 sent to the channel as post 20, and one forwarded
	// there as post 21 from channel 999.
	for _, p := range []struct {
		msg  int32
		poll int64
	}{{20, 9001}, {21, 9002}} {
		exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, 2, ?, ?, ?)",
			channelId, p.msg, p.msg, pollMessage(p.msg, p.poll), now-2*hour)
	}
	poll(9001, 2, mtproto.PEER_CHANNEL, channelId, 20, now-2*hour)
	poll(9002, 2, mtproto.PEER_CHANNEL, 999, 5, now-2*hour)
	exec("INSERT INTO poll_votes (poll_id, user_id, opt, `date`) VALUES (9001, 2, '0', ?), (9001, 3, '1', ?), (9001, 4, '0', ?), (9001, 5, '9', ?)",
		now-2*hour+10*60, now-hour, now-30*60, now-20*60)

	st, err := core.StatsGetPollStats(&mtproto.TLStatsGetPollStats{Peer: inputPeerChannel(hash), MsgId: 20})
	if err != nil {
		t.Fatal(err)
	}
	x, ys := parseGraph(t, "poll", st.GetVotesGraph())
	if x[1]-x[0] != 300*1000 || len(x) != 25 || sum(ys["Да"]) != 2 || sum(ys["Нет"]) != 1 || len(ys) != 2 {
		t.Errorf("poll %d points %v", len(x), ys)
	}
	if _, err = core.StatsGetPollStats(&mtproto.TLStatsGetPollStats{Peer: inputPeerChannel(hash), MsgId: 21}); !sameError(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("a forwarded poll: %v", err)
	}

	// The viewer's own poll in its chat with user 2, sent 70 days ago with
	// no votes, and a poll of user 2 there.
	did := mtproto.MakeDialogId(viewer, mtproto.PEER_USER, 2)
	for _, p := range []struct {
		box  int32
		poll int64
	}{{30, 9003}, {31, 9004}} {
		exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_data, date2) VALUES (?, ?, ?, ?, ?, ?, ?, 2, ?, ?)",
			viewer, p.box, did.A, did.B, p.poll, viewer, mtproto.PEER_USER, pollMessage(p.box, p.poll), now-70*day)
	}
	poll(9003, viewer, mtproto.PEER_USER, 2, 30, now-70*day)
	poll(9004, 2, mtproto.PEER_USER, viewer, 7, now-70*day)
	user2 := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 2, AccessHash: 20}).To_InputPeer()
	st, err = core.StatsGetPollStats(&mtproto.TLStatsGetPollStats{Peer: user2, MsgId: 30})
	if err != nil {
		t.Fatal(err)
	}
	if g := st.GetVotesGraph(); g.GetPredicateName() != mtproto.Predicate_statsGraphError || g.GetError() != langpack.StatsText(langpack.StatsNoVotes, "ru") {
		t.Errorf("no votes %v", g)
	}
	user3 := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 3, AccessHash: 30}).To_InputPeer()
	for name, in := range map[string]*mtproto.TLStatsGetPollStats{
		"another's poll":      {Peer: user2, MsgId: 31},
		"another dialog":      {Peer: user3, MsgId: 30},
		"no such message":     {Peer: user2, MsgId: 99},
		"a post with no poll": {Peer: inputPeerChannel(hash), MsgId: 99},
	} {
		if _, err = core.StatsGetPollStats(in); !sameError(err, mtproto.ErrPeerIdInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
