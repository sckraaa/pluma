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
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// graphJSON is what the clients read of a graph.
type graphJSON struct {
	Columns  [][]any           `json:"columns"`
	Types    map[string]string `json:"types"`
	Names    map[string]string `json:"names"`
	Subchart struct {
		Show bool `json:"show"`
	} `json:"subchart"`
}

// parseGraph checks g as the parsers of the clients do (pkg/statsgraph
// holds the rules) and returns its x and its y columns by name.
func parseGraph(t *testing.T, name string, g *mtproto.StatsGraph) (x []int64, ys map[string][]int64) {
	t.Helper()
	if g.GetPredicateName() != mtproto.Predicate_statsGraph {
		t.Fatalf("%s: %s %q", name, g.GetPredicateName(), g.GetError())
	}
	data := g.GetJson().GetData()
	if !strings.HasPrefix(data, `{"columns":[["x",`) {
		t.Fatalf("%s: %.40s", name, data)
	}
	var j graphJSON
	if err := json.Unmarshal([]byte(data), &j); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	ys = map[string][]int64{}
	for i, col := range j.Columns {
		id := col[0].(string)
		values := make([]int64, 0, len(col)-1)
		for _, v := range col[1:] {
			values = append(values, int64(v.(float64)))
		}
		if i == 0 {
			x = values
			continue
		}
		if len(values) != len(x) {
			t.Fatalf("%s: %s of %d points, x of %d", name, id, len(values), len(x))
		}
		ys[j.Names[id]] = values
	}
	return x, ys
}

func sum(v []int64) (n int64) {
	for _, x := range v {
		n += x
	}
	return n
}

// TestBroadcastStats builds the statistics of a channel from rows written
// as the server writes them and checks every number by hand: the overview
// against the period before, each graph of days, its zoom into hours, the
// list of the latest posts and stories, and the statistics of a fresh and
// of an older post.
func TestBroadcastStats(t *testing.T) {
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

	now := day0 + 40*day + 10*hour
	since := day0 + 30*day + 6*hour
	exec("UPDATE stats_collection SET since = ?", since)

	// The members: the viewer (creator), 2, 3 and 4 in, 5 left.
	for _, p := range []struct {
		user        int64
		role, state int32
		date        int64
	}{{viewer, 2, 0, day0}, {2, 0, 0, day0 + day}, {3, 0, 0, since + day}, {4, 0, 0, since + 2*day}, {5, 0, 1, since + day}} {
		exec("INSERT INTO channel_participants (channel_id, user_id, role, state, `date`) VALUES (?, ?, ?, ?, ?)", channelId, p.user, p.role, p.state, p.date)
	}
	exec("INSERT INTO channel_stats_member_events (channel_id, user_id, `date`, joined, source, inviter_id) VALUES"+
		" (?, 3, ?, 1, 1, 0), (?, 4, ?, 1, 5, ?), (?, 5, ?, 1, 4, 0), (?, 5, ?, 0, 0, 0)",
		channelId, since+day, channelId, since+2*day, viewer, channelId, since+day, channelId, since+3*day)

	// The posts: 1 in the period, 2 in the one before, 3 a service
	// message, 4 deleted, 5 ten hours old.
	post := func(id int32, date int64, predicate string, deleted bool, views, forwards int) {
		exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2, deleted, views, forwards) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			channelId, id, viewer, id, `{"predicate_name":"`+predicate+`"}`, date, deleted, views, forwards)
	}
	post(1, now-5*day, "message", false, 10, 2)
	post(2, now-40*day, "message", false, 4, 0)
	post(3, now-3*day, "messageService", false, 1, 0)
	post(4, now-2*day, "message", true, 100, 0)
	post(5, now-10*hour, "message", false, 3, 0)
	exec("INSERT INTO channel_message_views (channel_id, message_id, user_id, `date`, source) VALUES"+
		" (?, 1, 2, ?, 1), (?, 1, 3, ?, 4), (?, 5, 2, ?, 1), (?, 5, 3, ?, 0), (?, 4, 2, ?, 1)",
		channelId, now-5*day+hour, channelId, now-4*day, channelId, now-9*hour, channelId, now-8*hour, channelId, now-2*day)
	exec("INSERT INTO channel_message_forwards (channel_id, message_id, user_id, random_id, `date`) VALUES (?, 1, 2, 1, ?)", channelId, now-4*day+2*hour)
	exec("INSERT INTO channel_message_reactions (channel_id, message_id, user_id, reaction, chosen_order, `date`) VALUES"+
		" (?, 1, 2, 'e👍', 0, ?), (?, 1, 3, 'e👎', 0, ?), (?, 5, 4, 'c123', 0, ?)",
		channelId, now-5*day+3*hour, channelId, now-4*day, channelId, now-9*hour)

	// The mutings: 2 for good, 3 for a day that ran out.
	exec("INSERT INTO channel_stats_mute_events (channel_id, user_id, `date`, mute_until) VALUES (?, 2, ?, ?), (?, 3, ?, ?)",
		channelId, since+4*day, math.MaxInt32, channelId, since+5*day, since+6*day)
	exec("INSERT INTO user_notify_settings (id, user_id, peer_type, peer_id, mute_until) VALUES (1, 2, ?, ?, ?), (2, 3, ?, ?, ?), (3, 4, ?, 0, ?)",
		mtproto.PEER_CHANNEL, channelId, math.MaxInt32, mtproto.PEER_CHANNEL, channelId, since+6*day, mtproto.PEER_BROADCASTS, math.MaxInt32)

	// A story six days old, viewed by 2 (with a reaction) and 3, shared by 3.
	exec("INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date, privacy, caption, entities, media, media_areas, fwd_from, music, mentions, forwards_count)"+
		" VALUES (?, ?, 1, 1, ?, ?, ?, '', '', '', '', '', '', '', '', 1)", mtproto.PEER_CHANNEL, channelId, viewer, now-6*day, now-5*day)
	exec("INSERT INTO story_views (peer_type, peer_id, story_id, viewer_id, `date`, reaction, reaction_date) VALUES (?, ?, 1, 2, ?, 'e🔥', ?), (?, ?, 1, 3, ?, '', 0)",
		mtproto.PEER_CHANNEL, channelId, now-6*day+hour, now-6*day+2*hour, mtproto.PEER_CHANNEL, channelId, now-5*day)
	exec("INSERT INTO story_shares (peer_type, peer_id, story_id, user_id, random_id, `date`) VALUES (?, ?, 1, 3, 7, ?)", mtproto.PEER_CHANNEL, channelId, now-5*day+hour)

	// The languages: 2 used English, then Russian; 3 German; the viewer
	// Russian; 4 has no session.
	for i, s := range []struct {
		user   int64
		lang   string
		active int64
	}{{2, "en", now - 9*day}, {2, "ru", now - day}, {3, "de", now - day}, {viewer, "ru-RU", now - hour}} {
		key := int64(500 + i)
		exec("INSERT INTO auths (auth_key_id, api_id, params, lang_code, date_active) VALUES (?, 1, '{}', ?, ?)", key, s.lang, s.active)
		exec("INSERT INTO auth_users (auth_key_id, user_id) VALUES (?, ?)", key, s.user)
	}

	creator := participant(channelpb.RoleCreator, 0)
	mc := mutable(channelOpts{broadcast: true}, creator)
	mc.Channel.ParticipantsCount = 4
	core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
	cfg := m.Config()
	core.svcCtx.Dao = dao.New(sqlx.NewMySQL(&cfg))

	st, err := core.StatsGetBroadcastStats(&mtproto.TLStatsGetBroadcastStats{Channel: inputChannel(hash)})
	if err != nil {
		t.Fatal(err)
	}
	if p := st.GetPeriod(); int64(p.GetMinDate()) != now-30*day || int64(p.GetMaxDate()) != now {
		t.Errorf("period %v", p)
	}
	for _, c := range []struct {
		name              string
		v                 *mtproto.StatsAbsValueAndPrev
		current, previous float64
	}{
		// 3 joined, 1 left since the recording began (after the start
		// of the period).
		{"followers", st.GetFollowers(), 4, 2},
		// Posts 1 and 5 now (13 views, 2 forwards, 3 reactions); post 2
		// before.
		{"views per post", st.GetViewsPerPost(), 6.5, 4},
		{"shares per post", st.GetSharesPerPost(), 1, 0},
		{"reactions per post", st.GetReactionsPerPost(), 1.5, 0},
		{"views per story", st.GetViewsPerStory(), 2, 0},
		{"shares per story", st.GetSharesPerStory(), 1, 0},
		{"reactions per story", st.GetReactionsPerStory(), 1, 0},
	} {
		if c.v.GetCurrent() != c.current || c.v.GetPrevious() != c.previous {
			t.Errorf("%s: %v / %v, want %v / %v", c.name, c.v.GetCurrent(), c.v.GetPrevious(), c.current, c.previous)
		}
	}
	// 2 muted for good, 4 mutes all channels; 3's muting ran out.
	if n := st.GetEnabledNotifications(); n.GetPart() != 2 || n.GetTotal() != 4 {
		t.Errorf("enabled notifications %v", n)
	}

	x, ys := parseGraph(t, "growth", st.GetGrowthGraph())
	if got := ys["Подписчики"]; got[len(got)-1] != 4 || got[0] != 2 || len(x) != 11 {
		t.Errorf("growth %v over %d days", got, len(x))
	}
	_, ys = parseGraph(t, "followers", st.GetFollowersGraph())
	if sum(ys["Вступили"]) != 3 || sum(ys["Вышли"]) != 1 {
		t.Errorf("followers %v", ys)
	}
	_, ys = parseGraph(t, "mute", st.GetMuteGraph())
	if sum(ys["Отключили звук"]) != 2 || sum(ys["Включили звук"]) != 1 {
		t.Errorf("mute %v", ys)
	}
	x, ys = parseGraph(t, "interactions", st.GetInteractionsGraph())
	if sum(ys["Просмотры"]) != 4 || sum(ys["Пересылки"]) != 1 || len(x) != 41 {
		t.Errorf("interactions %v over %d days", ys, len(x))
	}
	_, ys = parseGraph(t, "views by source", st.GetViewsBySourceGraph())
	if sum(ys["Подписчики"]) != 2 || sum(ys["Личные чаты"]) != 1 || sum(ys["Прочее"]) != 1 || sum(ys["Каналы"]) != 0 {
		t.Errorf("views by source %v", ys)
	}
	_, ys = parseGraph(t, "new followers by source", st.GetNewFollowersBySourceGraph())
	if sum(ys["Пригласительные ссылки"]) != 1 || sum(ys["Добавлены"]) != 1 || sum(ys["Публичная ссылка"]) != 1 {
		t.Errorf("new followers by source %v", ys)
	}
	_, ys = parseGraph(t, "reactions", st.GetReactionsByEmotionGraph())
	if sum(ys["Положительные"]) != 1 || sum(ys["Отрицательные"]) != 1 || sum(ys["Прочие"]) != 1 {
		t.Errorf("reactions %v", ys)
	}
	_, ys = parseGraph(t, "story interactions", st.GetStoryInteractionsGraph())
	if sum(ys["Просмотры"]) != 2 || sum(ys["Пересылки"]) != 1 {
		t.Errorf("story interactions %v", ys)
	}
	_, ys = parseGraph(t, "story reactions", st.GetStoryReactionsByEmotionGraph())
	if sum(ys["Положительные"]) != 1 {
		t.Errorf("story reactions %v", ys)
	}
	x, ys = parseGraph(t, "top hours", st.GetTopHoursGraph())
	if len(x) != 24 || x[0] != 0 || x[23] != 23 || sum(ys["Этот период"]) != 4 {
		t.Errorf("top hours %v %v", x, ys)
	}
	if g := st.GetIvInteractionsGraph(); g.GetPredicateName() != mtproto.Predicate_statsGraphError {
		t.Errorf("iv interactions %v", g)
	}
	_, ys = parseGraph(t, "languages", st.GetLanguagesGraph())
	last := func(v []int64) int64 { return v[len(v)-1] }
	if last(ys["Русский"]) != 2 || last(ys["Немецкий"]) != 1 || last(ys["Прочие"]) != 1 {
		t.Errorf("languages %v", ys)
	}

	recent := st.GetRecentPostsInteractions()
	if len(recent) != 4 || recent[0].GetMsgId() != 5 || recent[1].GetMsgId() != 1 || recent[2].GetStoryId() != 1 || recent[3].GetMsgId() != 2 {
		t.Fatalf("recent %v", recent)
	}
	if r := recent[1]; r.GetViews() != 10 || r.GetForwards() != 2 || r.GetReactions() != 2 {
		t.Errorf("post 1 %v", r)
	}
	if r := recent[2]; r.GetPredicateName() != mtproto.Predicate_postInteractionCountersStory || r.GetViews() != 2 || r.GetForwards() != 1 || r.GetReactions() != 1 {
		t.Errorf("story 1 %v", r)
	}

	// The zoom of the growth into the hours around today.
	growthX, _ := parseGraph(t, "growth", st.GetGrowthGraph())
	zoom, err := core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{
		Token: st.GetGrowthGraph().GetZoomToken().GetValue(), X: wrapperspb.Int64(growthX[len(growthX)-1]),
	})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "growth zoom", zoom)
	if x[1]-x[0] != 3600*1000 || last(ys["Подписчики"]) != 4 {
		t.Errorf("growth zoom %v %v", x[:2], ys)
	}

	// A post ten hours old: by the hour.
	ms, err := core.StatsGetMessageStats(&mtproto.TLStatsGetMessageStats{Channel: inputChannel(hash), MsgId: 5})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "post 5", ms.GetViewsGraph())
	if x[1]-x[0] != 3600*1000 || sum(ys["Просмотры"]) != 2 || ms.GetViewsGraph().GetZoomToken() != nil {
		t.Errorf("post 5 %v %v", x[:2], ys)
	}
	_, ys = parseGraph(t, "post 5 reactions", ms.GetReactionsByEmotionGraph())
	if sum(ys["Прочие"]) != 1 {
		t.Errorf("post 5 reactions %v", ys)
	}

	// A post five days old: by the day, zooming into its hours.
	ms, err = core.StatsGetMessageStats(&mtproto.TLStatsGetMessageStats{Channel: inputChannel(hash), MsgId: 1})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "post 1", ms.GetViewsGraph())
	if x[1]-x[0] != 86400*1000 || sum(ys["Просмотры"]) != 2 || sum(ys["Пересылки"]) != 1 {
		t.Errorf("post 1 %v %v", x[:2], ys)
	}
	zoom, err = core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{Token: ms.GetViewsGraph().GetZoomToken().GetValue(), X: wrapperspb.Int64(x[0])})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "post 1 zoom", zoom)
	if x[1]-x[0] != 3600*1000 || sum(ys["Просмотры"]) != 2 {
		t.Errorf("post 1 zoom %v %v", x[:2], ys)
	}

	for _, id := range []int32{3, 4, 99} {
		if _, err := core.StatsGetMessageStats(&mtproto.TLStatsGetMessageStats{Channel: inputChannel(hash), MsgId: id}); !sameError(err, mtproto.ErrMessageIdInvalid) {
			t.Errorf("post %d: %v", id, err)
		}
	}
}

// TestNewChannel: a channel made today has too little for a graph of days:
// each is a statsGraphError the clients hide, and the rest is zero.
func TestNewChannel(t *testing.T) {
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
	if _, err = db.Exec("UPDATE stats_collection SET since = ?", day0); err != nil {
		t.Fatal(err)
	}
	now := day0 + 10*hour
	creator := participant(channelpb.RoleCreator, 0)
	mc := mutable(channelOpts{broadcast: true}, creator)
	mc.Channel.Date = day0 + hour
	mc.Channel.ParticipantsCount = 1
	core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
	cfg := m.Config()
	core.svcCtx.Dao = dao.New(sqlx.NewMySQL(&cfg))
	st, err := core.StatsGetBroadcastStats(&mtproto.TLStatsGetBroadcastStats{Channel: inputChannel(hash), Dark: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]*mtproto.StatsGraph{
		"growth": st.GetGrowthGraph(), "interactions": st.GetInteractionsGraph(), "languages": st.GetLanguagesGraph(),
	} {
		if g.GetPredicateName() != mtproto.Predicate_statsGraphError || g.GetError() == "" {
			t.Errorf("%s: %v", name, g)
		}
	}
	if st.GetFollowers().GetCurrent() != 1 || st.GetViewsPerPost().GetCurrent() != 0 || len(st.GetRecentPostsInteractions()) != 0 {
		t.Errorf("overview %v", st)
	}
}
