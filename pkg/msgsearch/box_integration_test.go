//go:build integration

package msgsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

var itSeq atomic.Int64

func itId() int64 { return 50_000_000 + itSeq.Add(1) }

type boxFixture struct {
	t    *testing.T
	db   *sqlx.DB
	user int64
	peer int64
	next int32
}

func newBoxFixture(t *testing.T) *boxFixture {
	conf := itDB.Config()
	return &boxFixture{t: t, db: sqlx.NewMySQL(&conf), user: itId(), peer: itId(), next: 1}
}

type rowOpts struct {
	text, media string
	kind        int32
	from        int64
	date        int32
	mentioned   bool
	peerType    int32
	peerId      int64
	saved       int64
}

func (f *boxFixture) add(o rowOpts) int32 {
	f.t.Helper()
	id := f.next
	f.next++
	if o.peerType == 0 {
		o.peerType, o.peerId = mtproto.PEER_USER, f.peer
	}
	if o.from == 0 {
		o.from = f.user
	}
	if o.kind == 0 && o.media == "" {
		o.kind = mtproto.MEDIA_EMPTY
	}
	did := mtproto.MakeDialogId(f.user, o.peerType, o.peerId)
	data := map[string]any{"predicate_name": mtproto.Predicate_message}
	if o.media != "" {
		var media any
		if err := json.Unmarshal([]byte(o.media), &media); err != nil {
			f.t.Fatal(err)
		}
		data["media"] = media
	}
	body, _ := json.Marshal(data)
	mentioned := 0
	if o.mentioned {
		mentioned = 1
	}
	if _, err := f.db.Exec(context.Background(),
		"INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_filter_type, message_data, message, mentioned, saved_peer_type, saved_peer_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?, ?, ?, ?)",
		f.user, id, did.A, did.B, int64(id), o.from, o.peerType, o.peerId, o.kind, string(body), o.text, mentioned, mtproto.PEER_USER, o.saved, o.date); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *boxFixture) query() BoxQuery {
	return BoxQuery{UserId: f.user, PeerType: mtproto.PEER_USER, PeerId: f.peer, Kind: KindEmpty}
}

func (f *boxFixture) ids(q BoxQuery) []int32 {
	f.t.Helper()
	ids, count, err := SearchBox(context.Background(), f.db, q, Page{Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	if int(count) != len(ids) {
		f.t.Fatalf("count %d, %d ids", count, len(ids))
	}
	return ids
}

// The text of a query on the real MySQL: the words of Russian text, any
// case and order, е and ё, a prefix but not the inside of a word.
func TestSearchRussianText(t *testing.T) {
	f := newBoxFixture(t)
	cat := f.add(rowOpts{text: "Кот спит на диване"})
	cats := f.add(rowOpts{text: "наши коты уже дома"})
	cattle := f.add(rowOpts{text: "большое стадо, скот пасётся"})
	f.add(rowOpts{text: "совсем другое"})
	bread := f.add(rowOpts{text: "купить хлеба и молока"})
	yo := f.add(rowOpts{text: "пётр приехал"})

	for _, c := range []struct {
		q    string
		want []int32
	}{
		{"кот", []int32{cats, cat}},
		{"КОТА", []int32{cats, cat}},
		{"кота!", []int32{cats, cat}},
		{"хлеб купить", []int32{bread}},
		{"скот", []int32{cattle}},
		{"петр", []int32{yo}},
		{"пётр", []int32{yo}},
		{"пасётся", []int32{cattle}},
		{"молока купить хлеб", []int32{bread}},
		{"несуществующее", nil},
		{"молоко кот", nil},
	} {
		q := f.query()
		q.Q = c.q
		if got := f.ids(q); !slices.Equal(got, c.want) {
			t.Errorf("q %q: %v, want %v", c.q, got, c.want)
		}
	}
}

// Hashtags and cashtags are found whole, in either case, with signs in
// queries taken literally.
func TestSearchTagsAndSigns(t *testing.T) {
	f := newBoxFixture(t)
	tag := f.add(rowOpts{text: "едем в #отпуск завтра"})
	f.add(rowOpts{text: "опять #отпуска кончились"})
	cashtag := f.add(rowOpts{text: "курс $BTC вырос"})
	percent := f.add(rowOpts{text: "скидка 50% сегодня"})
	f.add(rowOpts{text: "скидка 500 сегодня"})
	plus := f.add(rowOpts{text: "пишу на c++ теперь"})

	for q, want := range map[string][]int32{
		"#отпуск": {tag},
		"#Отпуск": {tag},
		"$btc":    {cashtag},
		"50%":     {percent},
		"c++":     {plus},
	} {
		query := f.query()
		query.Q = q
		if got := f.ids(query); !slices.Equal(got, want) {
			t.Errorf("q %q: %v, want %v", q, got, want)
		}
	}
}

func doc(fileName, attrs string) string {
	return fmt.Sprintf(`{"predicate_name":"messageMediaDocument","document":{"attributes":[{"file_name":%q}%s]}}`, fileName, attrs)
}

// The filters of messages.search over the box.
func TestSearchFilters(t *testing.T) {
	f := newBoxFixture(t)
	photo := f.add(rowOpts{kind: mtproto.MEDIA_PHOTOS_ONLY, media: `{"predicate_name":"messageMediaPhoto"}`})
	video := f.add(rowOpts{kind: mtproto.MEDIA_VIDEOS_ONLY, media: `{"predicate_name":"messageMediaDocument"}`})
	voice := f.add(rowOpts{kind: mtproto.MEDIA_AUDIO, media: doc("v.ogg", `,{"voice":true}`)})
	round := f.add(rowOpts{kind: mtproto.MEDIA_AUDIO, media: doc("r.mp4", `,{"round_message":true}`)})
	report := f.add(rowOpts{kind: mtproto.MEDIA_FILE, media: doc("Отчёт за год.pdf", "")})
	track := f.add(rowOpts{kind: mtproto.MEDIA_MUSIC, media: doc("a.mp3", `,{"title":{"value":"Звезда"},"performer":{"value":"Кино"}}`)})
	geo := f.add(rowOpts{media: `{"predicate_name":"messageMediaGeo"}`})
	venue := f.add(rowOpts{media: `{"predicate_name":"messageMediaVenue"}`})
	contact := f.add(rowOpts{media: `{"predicate_name":"messageMediaContact"}`})
	poll := f.add(rowOpts{media: `{"predicate_name":"messageMediaPoll"}`})
	mention := f.add(rowOpts{text: "эй", mentioned: true})

	for name, c := range map[string]struct {
		kind int32
		q    string
		want []int32
	}{
		"photo":      {mtproto.MEDIA_PHOTOS_ONLY, "", []int32{photo}},
		"photovideo": {mtproto.MEDIA_PHOTOVIDEO, "", []int32{video, photo}},
		"voice":      {mtproto.MEDIA_VOICE_FILE, "", []int32{voice}},
		"round":      {mtproto.MEDIA_ROUND_FILE, "", []int32{round}},
		"roundvoice": {mtproto.MEDIA_AUDIO, "", []int32{round, voice}},
		"file":       {mtproto.MEDIA_FILE, "", []int32{report}},
		"file name":  {mtproto.MEDIA_FILE, "отчёт", []int32{report}},
		"track":      {mtproto.MEDIA_MUSIC, "звезда", []int32{track}},
		"performer":  {mtproto.MEDIA_MUSIC, "кино", []int32{track}},
		"no track":   {mtproto.MEDIA_MUSIC, "отчёт", nil},
		"geo":        {KindGeo, "", []int32{venue, geo}},
		"contact":    {KindContact, "", []int32{contact}},
		"poll":       {KindPoll, "", []int32{poll}},
		"mentions":   {KindMyMentions, "", []int32{mention}},
	} {
		q := f.query()
		q.Kind, q.Q = c.kind, c.q
		if got := f.ids(q); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// From a user, the saved dialog, dates, paging, and the count alone.
func TestSearchScopesAndPaging(t *testing.T) {
	f := newBoxFixture(t)
	f.add(rowOpts{text: "слово", date: 100})
	b := f.add(rowOpts{text: "слово", date: 200, from: f.peer})
	c := f.add(rowOpts{text: "слово", date: 300, saved: 77})
	d := f.add(rowOpts{text: "слово", date: 400})

	q := f.query()
	q.Q = "слово"
	q.FromId = f.peer
	if got := f.ids(q); !slices.Equal(got, []int32{b}) {
		t.Errorf("from: %v", got)
	}
	q = f.query()
	q.Q, q.HasSaved, q.SavedPeerType, q.SavedPeerId = "слово", true, mtproto.PEER_USER, 77
	if got := f.ids(q); !slices.Equal(got, []int32{c}) {
		t.Errorf("saved: %v", got)
	}
	q = f.query()
	q.Q, q.MinDate, q.MaxDate = "слово", 200, 300
	if got := f.ids(q); !slices.Equal(got, []int32{c, b}) {
		t.Errorf("dates: %v", got)
	}

	q = f.query()
	q.Q = "слово"
	ids, count, err := SearchBox(context.Background(), f.db, q, Page{OffsetId: c, Limit: 1})
	if err != nil || !slices.Equal(ids, []int32{b}) || count != 4 {
		t.Errorf("a page below c: %v %d %v", ids, count, err)
	}
	cursor := q
	cursor.Cursor.And("user_message_box_id < ?", c)
	ids, count, err = SearchBox(context.Background(), f.db, cursor, Page{Limit: 10})
	if err != nil || len(ids) != 2 || ids[0] != b || count != 4 {
		t.Errorf("a cursor narrows the page, not the count: %v %d %v", ids, count, err)
	}
	ids, _, err = SearchBox(context.Background(), f.db, q, Page{OffsetId: c, AddOffset: -1, Limit: 2})
	if err != nil || !slices.Equal(ids, []int32{c, b}) {
		t.Errorf("a negative add_offset: %v %v", ids, err)
	}
	ids, count, err = SearchBox(context.Background(), f.db, q, Page{OffsetId: 0, Limit: 0})
	if err != nil || len(ids) != 0 || count != 4 {
		t.Errorf("the count alone: %v %d %v", ids, count, err)
	}
	if ids, _, _ := SearchBox(context.Background(), f.db, q, Page{Limit: 100}); ids[0] != d {
		t.Errorf("not newest first: %v", ids)
	}
}

// The calendar: whole UTC days, newest first, each with its ids and count,
// the count of all, and a next page below the oldest day.
func TestCalendar(t *testing.T) {
	f := newBoxFixture(t)
	const day = int32(86400)
	photo := rowOpts{kind: mtproto.MEDIA_PHOTOS_ONLY, media: `{"predicate_name":"messageMediaPhoto"}`}
	add := func(date int32) int32 { o := photo; o.date = date; return f.add(o) }
	d1a := add(10*day + 1)
	d1b := add(10*day + 2)
	add(11*day + 5)
	add(12*day + 7)
	q := f.query()
	q.Kind = mtproto.MEDIA_PHOTOS_ONLY
	src := q.Source()
	days, total, err := Calendar(context.Background(), f.db, src, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(days) != 3 {
		t.Fatalf("%v total %d", days, total)
	}
	if days[0].Date != 12*day || days[2].MinId != d1a || days[2].MaxId != d1b || days[2].Count != 2 || days[2].MinDate != 10*day+1 {
		t.Errorf("%+v", days)
	}
	next, _, err := Calendar(context.Background(), f.db, src, days[1].MinId, days[1].MinDate)
	if err != nil || len(next) != 1 || next[0].MinId != d1a {
		t.Errorf("the next page: %+v %v", next, err)
	}
}

// Sparse positions: 0-based places spread over the whole list.
func TestPositions(t *testing.T) {
	f := newBoxFixture(t)
	photo := rowOpts{kind: mtproto.MEDIA_PHOTOS_ONLY, media: `{"predicate_name":"messageMediaPhoto"}`}
	ids := make([]int32, 0, 10)
	for i := range 10 {
		o := photo
		o.date = int32(i + 1)
		ids = append(ids, f.add(o))
	}
	q := f.query()
	q.Kind = mtproto.MEDIA_PHOTOS_ONLY
	positions, total, err := Positions(context.Background(), f.db, q.Source(), 0, 3)
	if err != nil || total != 10 {
		t.Fatalf("%v %d %v", positions, total, err)
	}
	if len(positions) > 3 || positions[0].Offset != 0 || positions[0].Id != ids[9] {
		t.Fatalf("%+v", positions)
	}
	for i, p := range positions {
		if p.Id != ids[9-int(p.Offset)] || (i > 0 && p.Offset <= positions[i-1].Offset) {
			t.Fatalf("position %d: %+v", i, p)
		}
	}
	all, _, err := Positions(context.Background(), f.db, q.Source(), ids[5], 100)
	if err != nil || len(all) != 5 || all[0].Id != ids[4] || all[0].Offset != 5 {
		t.Fatalf("below an id: %+v %v", all, err)
	}
}

// The archive narrows the box.
func TestArchivedPeers(t *testing.T) {
	f := newBoxFixture(t)
	chat, other := itId(), itId()
	if _, err := f.db.Exec(context.Background(),
		"INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, folder_id, date2) VALUES (?, ?, ?, ?, '{}', 1, 1), (?, ?, ?, ?, '{}', 1, 1)",
		f.user, mtproto.PEER_USER, f.peer, mtproto.MakePeerDialogId(mtproto.PEER_USER, f.peer),
		f.user, mtproto.PEER_CHAT, chat, mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chat)); err != nil {
		t.Fatal(err)
	}
	inArchive := f.add(rowOpts{text: "слово"})
	outside := f.add(rowOpts{text: "слово", peerType: mtproto.PEER_USER, peerId: other})
	set, err := ArchivedPeers(context.Background(), f.db, f.user)
	if err != nil || len(set.Users) != 1 || len(set.Chats) != 1 {
		t.Fatalf("%+v %v", set, err)
	}
	q := BoxQuery{UserId: f.user, PeerType: mtproto.PEER_EMPTY, Kind: KindEmpty, Q: "слово"}
	inc := q
	set.NarrowBox(&inc.Extra, true)
	if got := f.ids(inc); !slices.Equal(got, []int32{inArchive}) {
		t.Errorf("the archive: %v", got)
	}
	exc := q
	set.NarrowBox(&exc.Extra, false)
	if got := f.ids(exc); !slices.Equal(got, []int32{outside}) {
		t.Errorf("not the archive: %v", got)
	}
}

// The daily quota: a new query takes a slot, a repeated one does not,
// the last one is taken, then FLOOD_WAIT; the next UTC day starts anew.
func TestPostsQuota(t *testing.T) {
	f := newBoxFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st, err := PostsFlood(ctx, f.db, f.user, "", now)
	if err != nil || st.GetRemains() != PostsDaily || st.GetQueryIsFree() {
		t.Fatalf("%v %v", st, err)
	}
	st, err = TakePostsSlot(ctx, f.db, f.user, "Кот", now)
	if err != nil || st.GetRemains() != PostsDaily-1 {
		t.Fatalf("first: %v %v", st, err)
	}
	st, err = TakePostsSlot(ctx, f.db, f.user, "  кот ", now)
	if err != nil || st.GetRemains() != PostsDaily-1 || !st.GetQueryIsFree() {
		t.Fatalf("the same again: %v %v", st, err)
	}
	if free, _ := PostsFlood(ctx, f.db, f.user, "кот", now); !free.GetQueryIsFree() {
		t.Errorf("not free after a search: %v", free)
	}
	for i := 0; i < PostsDaily-1; i++ {
		if _, err := TakePostsSlot(ctx, f.db, f.user, fmt.Sprintf("query %d", i), now); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	st, err = PostsFlood(ctx, f.db, f.user, "", now)
	if err != nil || st.GetRemains() != 0 || st.GetWaitTill().GetValue() == 0 {
		t.Fatalf("exhausted: %v %v", st, err)
	}
	_, err = TakePostsSlot(ctx, f.db, f.user, "one more", now)
	if err == nil || !strings.Contains(err.Error(), "_OR_STARS_0") {
		t.Fatalf("over the quota: %v", err)
	}
	if st, err = TakePostsSlot(ctx, f.db, f.user, "кот", now); err != nil || !st.GetQueryIsFree() {
		t.Fatalf("a known query at the limit: %v %v", st, err)
	}
	tomorrow := now.Add(24 * time.Hour)
	if st, err = TakePostsSlot(ctx, f.db, f.user, "new day", tomorrow); err != nil || st.GetRemains() != PostsDaily-1 {
		t.Fatalf("a new day: %v %v", st, err)
	}
}
