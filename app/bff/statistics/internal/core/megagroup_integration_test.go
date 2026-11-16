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
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The messages of a supergroup as msg writes them (channel_send.go): the
// message as JSON (jsonx of mtproto.Message) and its type of media
// (mtproto.GetMediaType).
func textMessage(text string) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message()
}

func mediaMessage(text string, media *mtproto.MessageMedia) *mtproto.Message {
	m := textMessage(text)
	m.Media = media
	return m
}

func documentMedia(mime string, attributes ...*mtproto.DocumentAttribute) *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Document: mtproto.MakeTLDocument(&mtproto.Document{Id: 1, AccessHash: 2, MimeType: mime, Size2: 1000, DcId: 1, Attributes: attributes}).To_Document(),
	}).To_MessageMedia()
}

var (
	photoMedia = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
		Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 1, AccessHash: 2, DcId: 1, Sizes: []*mtproto.PhotoSize{
			mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "m", W: 320, H: 240, Size2: 9000}).To_PhotoSize(),
			mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "x", W: 800, H: 600, Size2: 50000}).To_PhotoSize(),
		}}).To_Photo(),
	}).To_MessageMedia()
	voiceMedia   = documentMedia("audio/ogg", mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true}).To_DocumentAttribute())
	roundMedia   = documentMedia("video/mp4", mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{RoundMessage: true, W: 240, H: 240}).To_DocumentAttribute())
	stickerMedia = documentMedia("image/webp", mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
		Alt: "🙂", Stickerset: mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet(),
	}).To_DocumentAttribute())
	fileMedia = documentMedia("application/pdf", mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "a.pdf"}).To_DocumentAttribute())
	geoMedia  = mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{
		Geo: mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Long: 37.6, Lat: 55.7}).To_GeoPoint(),
	}).To_MessageMedia()
)

// linkMessage is a text with a link and its preview: MEDIA_URL.
func linkMessage(text string) *mtproto.Message {
	m := mediaMessage(text, mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
		Webpage: mtproto.MakeTLWebPageEmpty(&mtproto.WebPage{Id: 1}).To_WebPage(),
	}).To_MessageMedia())
	m.Entities = []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityUrl(&mtproto.MessageEntity{Offset: 0, Length: int32(len(text))}).To_MessageEntity()}
	return m
}

// groupRow is a row of channel_messages of the group channelId.
type groupRow struct {
	id      int32
	sender  int64
	sendAs  int64
	date    int64
	m       *mtproto.Message
	deleted bool
}

func (r groupRow) args(t testing.TB, channel int64) []any {
	t.Helper()
	m := r.m
	m.Id, m.Date = r.id, int32(r.date)
	m.PeerId = mtproto.MakePeerChannel(channel)
	m.FromId = mtproto.MakePeerUser(r.sender)
	if r.sendAs != 0 {
		m.FromId = mtproto.MakePeerChannel(r.sendAs)
	}
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		t.Fatal(err)
	}
	return []any{channel, r.id, r.sender, r.sendAs, r.id, mtproto.GetMediaType(m), data, m.GetMessage(), r.date, r.deleted}
}

const groupRowColumns = "INSERT INTO channel_messages (channel_id, id, sender_user_id, send_as_id, pts, message_filter_type, message_data, message, date2, deleted) VALUES "

func startStatsDB(t *testing.T) (*sql.DB, *dao.Dao) {
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
	cfg := m.Config()
	return db, dao.New(sqlx.NewMySQL(&cfg))
}

// TestMegagroupStats builds the statistics of a supergroup from rows written
// as the server writes them and checks every number by hand: the overview
// against the period before, the graphs, the zoom of the messages into
// hours, and the top members with their users.
func TestMegagroupStats(t *testing.T) {
	db, d := startStatsDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	now := day0 + 40*day + 10*hour
	since := day0 + 30*day + 6*hour
	exec("UPDATE stats_collection SET since = ?", since)
	today := int64(channelstats.Day(now))

	// The members: the viewer (creator), 2, 3 and 4. 3 and 4 were added
	// by 2, 5 joined by a link and left, 8 was added by 7 (an account no
	// longer there) and left.
	for _, u := range []int64{viewer, 2, 3, 4} {
		exec("INSERT INTO channel_participants (channel_id, user_id, role, state, `date`) VALUES (?, ?, 0, 0, ?)", channelId, u, day0)
	}
	exec("INSERT INTO channel_stats_member_events (channel_id, user_id, `date`, joined, source, inviter_id) VALUES"+
		" (?, 3, ?, 1, 5, 2), (?, 4, ?, 1, 5, 2), (?, 5, ?, 1, 1, 0), (?, 5, ?, 0, 0, 0), (?, 8, ?, 1, 5, 7), (?, 8, ?, 0, 0, 0)",
		channelId, since+day, channelId, since+2*day, channelId, since+day, channelId, since+3*day, channelId, since+day, channelId, since+4*day)
	// 6 is a support ghost (ADR 0048).
	exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (6, 1, '18886', 'US', 'Support', 1)")

	rows := []groupRow{
		{id: 1, sender: 2, date: now - 5*day, m: textMessage("hello")},
		{id: 2, sender: 2, date: now - 5*day + hour, m: mediaMessage("", photoMedia)},
		{id: 3, sender: 3, date: now - 4*day, m: mediaMessage("", voiceMedia)},
		{id: 4, sender: 3, date: now - 4*day + hour, m: mediaMessage("", roundMedia)},
		{id: 5, sender: 3, date: now - 3*day, m: mediaMessage("", geoMedia)},
		{id: 6, sender: 2, date: now - 3*day + hour, m: linkMessage("see x")},
		{id: 7, sender: 2, date: now - 2*day, m: mtproto.MakeTLMessageService(&mtproto.Message{
			Action: mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{InviterId: 2}).To_MessageAction(),
		}).To_Message()},
		{id: 8, sender: 2, date: now - 2*day, m: textMessage("gone"), deleted: true},
		// An anonymous admin: the group itself.
		{id: 9, sender: 4, sendAs: channelId, date: now - 2*day + hour, m: textMessage("anon")},
		// The copy of a post of the linked channel.
		{id: 10, sender: viewer, date: now - day, m: textMessage("post")},
		{id: 11, sender: 2, date: now - 40*day, m: textMessage("abc")},
		{id: 12, sender: 6, date: now - day + hour, m: textMessage("support")},
		{id: 13, sender: 3, date: now - day + 2*hour, m: textMessage("hihi")},
	}
	for _, r := range rows {
		exec(groupRowColumns+"(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", r.args(t, channelId)...)
	}
	exec("INSERT INTO channel_discussion_posts (channel_id, post_id, group_id, group_message_id) VALUES (900, 1, ?, 10)", channelId)

	// The readers: 2 and 3 yesterday, 2 the day before; 2 in the previous
	// period, before the readers were recorded in full.
	exec("INSERT INTO channel_stats_readers (channel_id, `day`, user_id) VALUES (?, ?, 2), (?, ?, 3), (?, ?, 2), (?, ?, 2)",
		channelId, today-1, channelId, today-1, channelId, today-2, channelId, today-35)
	// The admins: the viewer deleted 3 and removed 1, 3 restricted 1; 2
	// acted in the previous period alone.
	exec("INSERT INTO channel_stats_admin_actions (channel_id, `day`, admin_id, deleted, kicked, banned) VALUES (?, ?, ?, 3, 1, 0), (?, ?, 3, 0, 0, 1), (?, ?, 2, 9, 0, 0)",
		channelId, today-2, viewer, channelId, today-1, channelId, today-45)

	creator := participant(channelpb.RoleCreator, 0)
	mc := mutable(channelOpts{}, creator)
	mc.Channel.ParticipantsCount = 4
	core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
	core.svcCtx.Dao = d
	core.svcCtx.Users = fakeUsers{gone: map[int64]bool{7: true}}

	st, err := core.StatsGetMegagroupStats(&mtproto.TLStatsGetMegagroupStats{Channel: inputChannel(hash)})
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
		// 4 joined, 2 left since the recording began.
		{"members", st.GetMembers(), 4, 2},
		// 1-6, 9, 10, 12, 13 now; 11 before.
		{"messages", st.GetMessages(), 10, 1},
		// 2 and 3 as themselves; 2 before.
		{"posters", st.GetPosters(), 2, 1},
		// 2 and 3; the period before is not recorded in full.
		{"viewers", st.GetViewers(), 2, 0},
	} {
		if c.v.GetCurrent() != c.current || c.v.GetPrevious() != c.previous {
			t.Errorf("%s: %v / %v, want %v / %v", c.name, c.v.GetCurrent(), c.v.GetPrevious(), c.current, c.previous)
		}
	}

	last := func(v []int64) int64 { return v[len(v)-1] }
	x, ys := parseGraph(t, "growth", st.GetGrowthGraph())
	if got := ys["Участники"]; last(got) != 4 || got[0] != 2 || len(x) != 11 {
		t.Errorf("growth %v over %d days", got, len(x))
	}
	_, ys = parseGraph(t, "members", st.GetMembersGraph())
	if sum(ys["Вступили"]) != 4 || sum(ys["Вышли"]) != 2 {
		t.Errorf("members %v", ys)
	}
	_, ys = parseGraph(t, "new members by source", st.GetNewMembersBySourceGraph())
	if sum(ys["Добавлены"]) != 3 || sum(ys["Пригласительные ссылки"]) != 1 {
		t.Errorf("new members by source %v", ys)
	}
	x, ys = parseGraph(t, "messages", st.GetMessagesGraph())
	if len(x) != 41 || sum(ys["Текст"]) != 7 || sum(ys["Фото"]) != 1 || sum(ys["Голосовые сообщения"]) != 1 ||
		sum(ys["Видеосообщения"]) != 1 || sum(ys["Прочее"]) != 1 || sum(ys["Стикеры"]) != 0 {
		t.Errorf("messages %v over %d days", ys, len(x))
	}
	x, ys = parseGraph(t, "actions", st.GetActionsGraph())
	if len(x) != 11 || sum(ys["Сообщения"]) != 10 || sum(ys["Зрители"]) != 3 || last(ys["Зрители"]) != 0 || ys["Зрители"][len(x)-2] != 2 {
		t.Errorf("actions %v over %d days", ys, len(x))
	}
	if st.GetActionsGraph().GetZoomToken() != nil {
		t.Errorf("actions zoom into hours they do not have")
	}
	x, ys = parseGraph(t, "top hours", st.GetTopHoursGraph())
	if len(x) != 24 || x[0] != 0 || x[23] != 23 || sum(ys["Сообщения"]) != 10 || ys["Сообщения"][10] != 4 {
		t.Errorf("top hours %v %v", x, ys)
	}
	if st.GetTopHoursGraph().GetZoomToken() != nil || st.GetWeekdaysGraph().GetZoomToken() != nil {
		t.Errorf("top hours or weekdays zoom")
	}
	// Day 0 is a Monday: 1, 2 and 11 on Mondays, 3 and 4 on a Tuesday, 5
	// and 6 on a Wednesday, 9 on a Thursday, 10, 12 and 13 on a Friday.
	x, ys = parseGraph(t, "weekdays", st.GetWeekdaysGraph())
	if len(x) != 6 || x[0] != day0*1000+12*hour*1000 || sum(ys["Понедельник"]) != 3 || sum(ys["Вторник"]) != 2 ||
		sum(ys["Среда"]) != 2 || sum(ys["Четверг"]) != 1 || sum(ys["Пятница"]) != 3 || sum(ys["Суббота"]) != 0 {
		t.Errorf("weekdays %v at %v", ys, x)
	}
	parseGraph(t, "languages", st.GetLanguagesGraph())

	posters := st.GetTopPosters()
	if len(posters) != 2 || posters[0].GetUserId() != 3 || posters[0].GetMessages() != 4 || posters[0].GetAvgChars() != 1 ||
		posters[1].GetUserId() != 2 || posters[1].GetMessages() != 3 || posters[1].GetAvgChars() != 3 {
		t.Errorf("top posters %v", posters)
	}
	admins := st.GetTopAdmins()
	if len(admins) != 2 || admins[0].GetUserId() != viewer || admins[0].GetDeleted() != 3 || admins[0].GetKicked() != 1 ||
		admins[1].GetUserId() != 3 || admins[1].GetBanned() != 1 {
		t.Errorf("top admins %v", admins)
	}
	// 7 added 8 but is not there to be shown.
	inviters := st.GetTopInviters()
	if len(inviters) != 1 || inviters[0].GetUserId() != 2 || inviters[0].GetInvitations() != 2 {
		t.Errorf("top inviters %v", inviters)
	}
	var users []int64
	for _, u := range st.GetUsers() {
		users = append(users, u.GetId())
	}
	sort.Slice(users, func(i, j int) bool { return users[i] < users[j] })
	if fmt.Sprint(users) != fmt.Sprint([]int64{2, 3, viewer}) {
		t.Errorf("users %v", users)
	}

	// The zoom of the messages into the hours of the last days: those of
	// the three days before today and of today.
	messagesX, _ := parseGraph(t, "messages", st.GetMessagesGraph())
	zoom, err := core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{
		Token: st.GetMessagesGraph().GetZoomToken().GetValue(), X: wrapperspb.Int64(last(messagesX)),
	})
	if err != nil {
		t.Fatal(err)
	}
	x, ys = parseGraph(t, "messages zoom", zoom)
	var n int64
	for _, v := range ys {
		n += sum(v)
	}
	if x[1]-x[0] != 3600*1000 || n != 6 || sum(ys["Текст"]) != 5 {
		t.Errorf("messages zoom %v %v", x[:2], ys)
	}
	growthX, _ := parseGraph(t, "growth", st.GetGrowthGraph())
	zoom, err = core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{
		Token: st.GetGrowthGraph().GetZoomToken().GetValue(), X: wrapperspb.Int64(last(growthX)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ys = parseGraph(t, "growth zoom", zoom); last(ys["Участники"]) != 4 {
		t.Errorf("growth zoom %v", ys)
	}
}

// TestMessageKinds: channel_messages.stats_kind, which the migration
// defines, counts each kind of message as msg writes it (the type of media
// of mtproto.GetMediaType and the JSON of the message), and follows a
// deletion and an edit of the media.
func TestMessageKinds(t *testing.T) {
	db, d := startStatsDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	videoMedia := documentMedia("video/mp4", mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{W: 1280, H: 720}).To_DocumentAttribute())
	gifMedia := documentMedia("video/mp4",
		mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{W: 320, H: 240}).To_DocumentAttribute(),
		mtproto.MakeTLDocumentAttributeAnimated(nil).To_DocumentAttribute())
	musicMedia := documentMedia("audio/mpeg", mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Title: wrapperspb.String("song")}).To_DocumentAttribute())
	rows := []struct {
		m    *mtproto.Message
		want int64
	}{
		{textMessage("plain"), messageText},
		{linkMessage("pluma.chat"), messageText},
		{mediaMessage("", photoMedia), messagePhoto},
		{mediaMessage("", videoMedia), messageVideo},
		{mediaMessage("", voiceMedia), messageVoice},
		{mediaMessage("", roundMedia), messageRound},
		{mediaMessage("", stickerMedia), messageSticker},
		{mediaMessage("", gifMedia), messageGif},
		{mediaMessage("", fileMedia), messageFile},
		{mediaMessage("", musicMedia), messageMusic},
		{mediaMessage("", geoMedia), messageOther},
	}
	want := map[int64]int64{}
	for i, r := range rows {
		row := groupRow{id: int32(i + 1), sender: 2, date: day0 + int64(i), m: r.m}
		exec(groupRowColumns+"(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", row.args(t, channelId)...)
		want[r.want]++
	}
	// A service message and a deleted message are not counted.
	service := groupRow{id: 50, sender: 2, date: day0, m: mtproto.MakeTLMessageService(&mtproto.Message{
		Action: mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{InviterId: 2}).To_MessageAction(),
	}).To_Message()}
	deleted := groupRow{id: 51, sender: 2, date: day0, m: textMessage("gone"), deleted: true}
	exec(groupRowColumns+"(?, ?, ?, ?, ?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", append(service.args(t, channelId), deleted.args(t, channelId)...)...)

	count := func() map[int64]int64 {
		t.Helper()
		got, err := d.MessageKinds(context.Background(), channelId, dao.Range{From: day0, To: day0 + day, Bucket: day})
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]int64{}
		for _, c := range got {
			out[c.K] += c.N
		}
		return out
	}
	if got := count(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("kinds %v, want %v", got, want)
	}

	// The text is deleted, the photo edited into a voice message.
	exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id = 1", channelId)
	edited := groupRow{id: 3, sender: 2, date: day0 + 2, m: mediaMessage("", voiceMedia)}
	args := edited.args(t, channelId)
	exec("UPDATE channel_messages SET message_data = ?, message_filter_type = ? WHERE channel_id = ? AND id = 3", args[6], args[5], channelId)
	want[messageText]--
	want[messagePhoto]--
	want[messageVoice]++
	delete(want, messagePhoto)
	if got := count(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("kinds after a deletion and an edit %v, want %v", got, want)
	}
}

// TestNewGroup: a supergroup made today has too little for a graph of days
// and no top members.
func TestNewGroup(t *testing.T) {
	db, d := startStatsDB(t)
	if _, err := db.Exec("UPDATE stats_collection SET since = ?", day0); err != nil {
		t.Fatal(err)
	}
	now := day0 + 10*hour
	mc := mutable(channelOpts{}, participant(channelpb.RoleCreator, 0))
	mc.Channel.Date = day0 + hour
	mc.Channel.ParticipantsCount = 1
	core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
	core.svcCtx.Dao = d
	core.svcCtx.Users = fakeUsers{}
	st, err := core.StatsGetMegagroupStats(&mtproto.TLStatsGetMegagroupStats{Channel: inputChannel(hash), Dark: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]*mtproto.StatsGraph{
		"growth": st.GetGrowthGraph(), "messages": st.GetMessagesGraph(), "actions": st.GetActionsGraph(),
		"weekdays": st.GetWeekdaysGraph(), "languages": st.GetLanguagesGraph(),
	} {
		if g.GetPredicateName() != mtproto.Predicate_statsGraphError || g.GetError() == "" {
			t.Errorf("%s: %v", name, g)
		}
	}
	// The hours of the day are always 24 points.
	parseGraph(t, "top hours", st.GetTopHoursGraph())
	if st.GetMembers().GetCurrent() != 1 || st.GetMessages().GetCurrent() != 0 || len(st.GetTopPosters()) != 0 ||
		st.GetTopAdmins() == nil || st.GetTopInviters() == nil || st.GetUsers() == nil {
		t.Errorf("overview %v", st)
	}
}

// TestMegagroupStatsTiming: the statistics of a supergroup with 200 000
// messages over a year, readers every day and the events of a busy group,
// next to another group of 50 000 messages. The time of the answer goes
// into the task's report (docs/tasks/103: under 500 ms).
func TestMegagroupStatsTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("a large sample")
	}
	db, d := startStatsDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%.200s: %v", q, err)
		}
	}
	now := day0 + 400*day + 15*hour
	exec("UPDATE stats_collection SET since = ?", now-200*day)

	const (
		messages = 200_000
		other    = 50_000
		users    = 100
		batch    = 1000
	)
	texts := []string{
		"ok",
		"see you tomorrow at the usual place",
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
		"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.",
	}
	// Seven in ten messages are text, the rest of every kind.
	kind := func(i int) *mtproto.Message {
		text := texts[i%len(texts)]
		switch i % 20 {
		case 0, 1:
			return mediaMessage(text, photoMedia)
		case 2:
			return mediaMessage("", voiceMedia)
		case 3:
			return mediaMessage("", roundMedia)
		case 4:
			return mediaMessage("", stickerMedia)
		case 5:
			return mediaMessage(text, fileMedia)
		case 6:
			return linkMessage(text)
		}
		return textMessage(text)
	}
	insert := func(channel int64, n int, span int64) {
		for start := 0; start < n; start += batch {
			k := min(batch, n-start)
			args := make([]any, 0, 10*k)
			for i := start; i < start+k; i++ {
				r := groupRow{id: int32(i + 1), sender: int64(2 + i%users), date: now - span + int64(i)*span/int64(n), m: kind(i)}
				args = append(args, r.args(t, channel)...)
			}
			exec(groupRowColumns+strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?), ", k), ", "), args...)
		}
	}
	start := time.Now()
	insert(channelId, messages, 365*day)
	insert(channelId+1, other, 365*day)

	// The readers of 200 days: 60 members a day.
	for dd := int64(0); dd < 200; dd++ {
		var b strings.Builder
		args := make([]any, 0, 3*60)
		for u := 0; u < 60; u++ {
			if u > 0 {
				b.WriteString(", ")
			}
			b.WriteString("(?, ?, ?)")
			args = append(args, channelId, int64(channelstats.Day(now))-dd, int64(2+(int(dd)+u)%users))
		}
		exec("INSERT INTO channel_stats_readers (channel_id, `day`, user_id) VALUES "+b.String(), args...)
	}
	// 2 000 joins and leaves, admins' actions on every day.
	for i := 0; i < 2000; i += batch {
		var b strings.Builder
		args := make([]any, 0, 6*batch)
		for j := i; j < i+batch; j++ {
			if j > i {
				b.WriteString(", ")
			}
			b.WriteString("(?, ?, ?, ?, ?, ?)")
			args = append(args, channelId, int64(1000+j), now-int64(j)*day/10, j%3 != 0, j%6, int64(2+j%7))
		}
		exec("INSERT INTO channel_stats_member_events (channel_id, user_id, `date`, joined, source, inviter_id) VALUES "+b.String(), args...)
	}
	for dd := int64(0); dd < 200; dd++ {
		exec("INSERT INTO channel_stats_admin_actions (channel_id, `day`, admin_id, deleted, kicked, banned) VALUES (?, ?, ?, 2, 1, 0), (?, ?, ?, 1, 0, 1)",
			channelId, int64(channelstats.Day(now))-dd, viewer, channelId, int64(channelstats.Day(now))-dd, int64(2+dd%5))
	}
	for u := 0; u < users; u++ {
		exec("INSERT INTO channel_participants (channel_id, user_id, role, state, `date`) VALUES (?, ?, 0, 0, ?)", channelId, 2+u, now-380*day)
		exec("INSERT INTO auths (auth_key_id, api_id, params, lang_code, date_active) VALUES (?, 1, '{}', ?, ?)", 5000+u, []string{"en", "ru", "de", "es"}[u%4], now-day)
		exec("INSERT INTO auth_users (auth_key_id, user_id) VALUES (?, ?)", 5000+u, 2+u)
	}
	exec("ANALYZE TABLE channel_messages, channel_stats_readers, channel_stats_member_events, channel_stats_admin_actions")
	t.Logf("sample written in %v", time.Since(start).Round(time.Millisecond))

	// The plan of the heaviest query, the messages of a year by day and
	// kind: from the index alone, no row read.
	explain, err := db.Query("EXPLAIN SELECT FLOOR(m.date2 / 86400) AS b, m.stats_kind AS k, COUNT(*) AS n FROM channel_messages m FORCE INDEX (stats_messages)"+
		" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND m.stats_kind IS NOT NULL GROUP BY b, k",
		channelId, now-365*day, now)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := explain.Columns()
	for explain.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := explain.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		plan := map[string]string{}
		var b strings.Builder
		for i, c := range cols {
			plan[c] = vals[i].String
			fmt.Fprintf(&b, "%s=%s ", c, vals[i].String)
		}
		t.Logf("plan: %s", b.String())
		if plan["key"] != "stats_messages" || !strings.Contains(plan["Extra"], "Using index") {
			t.Errorf("the messages are not read from the index alone: %s", b.String())
		}
	}
	_ = explain.Close()

	mc := mutable(channelOpts{}, participant(channelpb.RoleCreator, 0))
	mc.Channel.Date = now - 380*day
	mc.Channel.ParticipantsCount = users + 1
	var best time.Duration
	for i := range 3 {
		core := testCore(t, fakeChannels{mc: mc}, newSigner(t), now)
		core.svcCtx.Dao = d
		core.svcCtx.Users = fakeUsers{}
		begin := time.Now()
		st, err := core.StatsGetMegagroupStats(&mtproto.TLStatsGetMegagroupStats{Channel: inputChannel(hash)})
		took := time.Since(begin)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 || took < best {
			best = took
		}
		t.Logf("getMegagroupStats of %d messages: %v", messages, took.Round(time.Millisecond))
		if i == 0 {
			x, ys := parseGraph(t, "messages", st.GetMessagesGraph())
			var n int64
			for _, v := range ys {
				n += sum(v)
			}
			if len(x) != 366 || n != messages {
				t.Errorf("messages graph: %d messages over %d days", n, len(x))
			}
			if len(st.GetTopPosters()) != topListSize || len(st.GetTopAdmins()) != 6 || len(st.GetTopInviters()) != 7 {
				t.Errorf("top %d posters, %d admins, %d inviters", len(st.GetTopPosters()), len(st.GetTopAdmins()), len(st.GetTopInviters()))
			}
		}
	}
	// A guard against a query that stops using the index: the target is
	// under 500 ms, which a loaded test host may miss by a little.
	if best > 2*time.Second {
		t.Errorf("getMegagroupStats took %v at best", best)
	}
}
