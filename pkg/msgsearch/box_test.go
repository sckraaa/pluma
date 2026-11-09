package msgsearch

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestBoxSource(t *testing.T) {
	q := BoxQuery{UserId: 7, PeerType: mtproto.PEER_USER, PeerId: 9, Kind: KindEmpty, Q: "кот", FromId: 9}
	s := q.Source()
	for _, want := range []string{"user_id = ?", "dialog_id1 = ?", "REGEXP_LIKE(message, ?, 'i')", "sender_user_id = ?", "'$.predicate_name'"} {
		if !strings.Contains(s.Where, want) {
			t.Errorf("no %q in %q", want, s.Where)
		}
	}
	if strings.Count(s.Where, "?") != len(s.Args) {
		t.Errorf("%d placeholders, %d args: %q", strings.Count(s.Where, "?"), len(s.Args), s.Where)
	}
	all := BoxQuery{UserId: 7, PeerType: mtproto.PEER_EMPTY, Kind: mtproto.MEDIA_PHONE_CALL}.Source()
	if strings.Contains(all.Where, "dialog_id") {
		t.Errorf("the whole box has a dialog: %q", all.Where)
	}
}

func TestNarrowBox(t *testing.T) {
	var w Where
	PeerSet{Users: []int64{1, 2}, Chats: []int64{3}}.NarrowBox(&w, false)
	if !strings.HasPrefix(w.SQL, " AND NOT (") || len(w.Args) != 5 {
		t.Errorf("%q %v", w.SQL, w.Args)
	}
	var in Where
	PeerSet{}.NarrowBox(&in, true)
	if in.SQL != " AND 1 = 0" {
		t.Errorf("an empty include: %q", in.SQL)
	}
	var out Where
	PeerSet{}.NarrowBox(&out, false)
	if out.SQL != "" {
		t.Errorf("an empty exclude: %q", out.SQL)
	}
}

func TestKindOf(t *testing.T) {
	for pred, want := range map[string]int32{
		mtproto.Predicate_inputMessagesFilterEmpty:      KindEmpty,
		mtproto.Predicate_inputMessagesFilterPoll:       KindPoll,
		mtproto.Predicate_inputMessagesFilterGeo:        KindGeo,
		mtproto.Predicate_inputMessagesFilterRoundVoice: mtproto.MEDIA_AUDIO,
		mtproto.Predicate_inputMessagesFilterVoice:      mtproto.MEDIA_VOICE_FILE,
		mtproto.Predicate_inputMessagesFilterRoundVideo: mtproto.MEDIA_ROUND_FILE,
	} {
		if got, ok := KindOf(&mtproto.MessagesFilter{PredicateName: pred}); !ok || got != want {
			t.Errorf("%s: %d %v", pred, got, ok)
		}
	}
	if got, ok := KindOf(&mtproto.MessagesFilter{PredicateName: mtproto.Predicate_inputMessagesFilterPhoneCalls, Missed: true}); !ok || got != KindMissedCalls {
		t.Errorf("missed calls: %d %v", got, ok)
	}
	if _, ok := KindOf(&mtproto.MessagesFilter{PredicateName: "other"}); ok {
		t.Error("an unknown filter")
	}
}

func TestDayEnd(t *testing.T) {
	now := time.Date(2026, 11, 9, 23, 59, 30, 0, time.UTC)
	if got := secondsToDayEnd(now); got != 30 {
		t.Errorf("seconds %d", got)
	}
	f := makeFlood(false, 0, now)
	if f.GetWaitTill().GetValue() != int32(now.Unix())+30 || f.GetTotalDaily() != PostsDaily {
		t.Errorf("flood %v", f)
	}
}

// quotaRace is a store where the slot of the query is taken by a parallel
// request between the check and the insert.
type quotaRace struct {
	hash    int64
	checked int
}

type noRows struct{ sql.Result }

func (noRows) RowsAffected() (int64, error) { return 0, nil }

func (q *quotaRace) Exec(context.Context, string, ...interface{}) (sql.Result, error) {
	return noRows{}, nil
}

func (q *quotaRace) QueryRowsPartial(_ context.Context, v interface{}, _ string, _ ...interface{}) error {
	if q.checked++; q.checked > 1 {
		*v.(*[]int64) = []int64{q.hash}
	}
	return nil
}

func TestTakePostsSlotSameQueryInParallel(t *testing.T) {
	st, err := TakePostsSlot(context.Background(), &quotaRace{hash: queryHash("кот")}, 7, "кот", time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatalf("a parallel search of the same query: %v", err)
	}
	if !st.GetQueryIsFree() {
		t.Errorf("%v", st)
	}
}
