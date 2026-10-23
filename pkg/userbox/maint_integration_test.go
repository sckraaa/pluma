//go:build integration

package userbox

import (
	"context"
	"strconv"
	"testing"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// TestMaintenance: Raise lifts stale rows above the journal and the
// messages and adds missing rows; CheckJournal then sees the jump as a gap;
// ExportLegacy writes the counters for a version that kept them in Redis.
func TestMaintenance(t *testing.T) {
	ctx := context.Background()
	db := openPool(t, boxDSN(itDB.DSN()), 4)

	// u1: a row behind what was written.
	u1 := itUser()
	for range 3 {
		mustRun(t, db, u1, send)
	}
	if _, err := db.Exec("UPDATE user_state SET pts = 2, last_message_box_id = 1 WHERE user_id = ?", u1); err != nil {
		t.Fatal(err)
	}
	raised, added, err := Raise(ctx, db, 1000, u1)
	if err != nil || raised != 1 || added != 0 {
		t.Fatalf("raise one: %d raised, %d added, %v", raised, added, err)
	}
	if st := mustRead(t, db, u1); st.Pts != BasePts+3+1000 || st.LastMessageBoxId != 3+1000 {
		t.Errorf("u1 after raise: %+v", st)
	}

	// The jump shows as one gap; the next transaction follows it.
	rep, err := CheckJournal(ctx, db, u1, BasePts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || len(rep.Gaps) != 1 || rep.Gaps[0] != (Range{From: BasePts + 3, To: BasePts + 3 + 1000}) {
		t.Errorf("journal report after the jump: %+v", rep)
	}
	res := mustRun(t, db, u1, send)
	if res.State.Pts != BasePts+3+1000+1 {
		t.Errorf("pts after the jump %d", res.State.Pts)
	}

	// u2: written to without a row (a backup taken before its first box
	// transaction): Raise of all users adds it.
	u2 := itUser()
	if _, err = db.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, date2) VALUES (?, 5, 0, 0, 5, ?, 2, 0, 0)", u2, u2); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, 7, 1, 1, '{}', 0)", u2); err != nil {
		t.Fatal(err)
	}
	if raised, added, err = Raise(ctx, db, 10, 0); err != nil || raised < 1 || added < 1 {
		t.Fatalf("raise all: %d raised, %d added, %v", raised, added, err)
	}
	if st := mustRead(t, db, u2); st.Pts != 7+10 || st.LastMessageBoxId != 5+10 {
		t.Errorf("u2 added: %+v", st)
	}
	u1State := mustRead(t, db, u1)
	if u1State.Pts != BasePts+3+1000+1+10 {
		t.Errorf("u1 after raise of all: %+v", u1State)
	}

	rds := redis.MustNewRedis(itRedis.RedisConf())
	n, err := ExportLegacy(ctx, db, 100, func(ctx context.Context, key, value string) error {
		return rds.SetCtx(ctx, key, value)
	})
	if err != nil || n < 2 {
		t.Fatalf("export: %d users, %v", n, err)
	}
	for key, want := range map[string]int64{
		LegacyPtsKeyPrefix + strconv.FormatInt(u1, 10):        int64(u1State.Pts) + 100,
		LegacyMessageBoxKeyPrefix + strconv.FormatInt(u1, 10): int64(u1State.LastMessageBoxId) + 100,
		LegacyPtsKeyPrefix + strconv.FormatInt(u2, 10):        17 + 100,
	} {
		v, err := rds.Get(key)
		if err != nil || v != strconv.FormatInt(want, 10) {
			t.Errorf("%s = %q, %v; want %d", key, v, err, want)
		}
	}
}
