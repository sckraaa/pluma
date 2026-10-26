//go:build integration

package core

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The seq journal in the difference (ADR 0020).

// seqState sets the user's state.
func seqState(t *testing.T, userId int64, pts, seq int32) {
	t.Helper()
	if _, err := itRaw.Exec("INSERT INTO user_state (user_id, pts, seq) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE pts = VALUES(pts), seq = VALUES(seq)", userId, pts, seq); err != nil {
		t.Fatal(err)
	}
}

// seqRow writes a row of the seq journal: updateNotifySettings of peer
// (the seq as mute_until, to tell them apart) at pts, written at date.
func seqRow(t *testing.T, userId int64, seq, pts int32, date int64) {
	t.Helper()
	u := mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
		Peer_NOTIFYPEER: mtproto.MakeTLNotifyPeer(&mtproto.NotifyPeer{Peer: mtproto.MakePeerUser(int64(100 + seq))}).To_NotifyPeer(),
		NotifySettings:  mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: mtproto.MakeFlagsInt32(seq)}).To_PeerNotifySettings(),
	}).To_Update()
	data, err := jsonx.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = itRaw.Exec("INSERT INTO user_seq_updates (user_id, seq, pts, update_type, update_data, date2) VALUES (?, ?, ?, ?, ?, ?)",
		userId, seq, pts, u.PredicateName, string(data), date); err != nil {
		t.Fatal(err)
	}
}

func cursorOf(t *testing.T, userId, keyId int64) (int32, bool) {
	t.Helper()
	seq, ok, err := itSvc.Dao.SeqCursor(context.Background(), userId, keyId)
	if err != nil {
		t.Fatal(err)
	}
	return seq, ok
}

// seqOf writes other_updates as N for new messages' ids and sN for updates
// with seq.
func seqOf(d *updates.Difference) string {
	var out []string
	for _, m := range d.GetNewMessages() {
		out = append(out, fmt.Sprint(m.GetId()))
	}
	for _, u := range d.GetOtherUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNotifySettings:
			out = append(out, fmt.Sprintf("s%d", u.GetNotifySettings().GetMuteUntil().GetValue()))
		default:
			out = append(out, strings.TrimPrefix(u.GetPredicateName(), "update"))
		}
	}
	return strings.Join(out, " ")
}

const key = int64(77)

func TestDifferenceSeq(t *testing.T) {
	t.Run("by the date, among the journal updates", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 0, 2, 3)
		seqState(t, u, 3, 5)
		seqRow(t, u, 2, 1, 100)  // long before the date
		seqRow(t, u, 3, 1, 980)  // within the margin before it
		seqRow(t, u, 4, 2, 2000) // between pts 2 and 3
		seqRow(t, u, 5, 3, 2000)
		seqRow(t, u, 6, 3, 2000) // committed after the state was read
		d := differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: 990})
		if d.GetPredicateName() != updates.Predicate_difference || d.GetState().GetSeq() != 5 {
			t.Fatalf("%s, state %v", d.GetPredicateName(), d.GetState())
		}
		// The new messages, then the updates with seq in commit order.
		if got := seqOf(d); got != "2 3 s3 s4 s5" {
			t.Errorf("difference %q", got)
		}
	})
	t.Run("only updates with seq", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 2)
		seqRow(t, u, 2, userbox.BasePts, 1000)
		d := differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: 1000})
		if d.GetPredicateName() != updates.Predicate_difference || seqOf(d) != "s2" || d.GetState().GetSeq() != 2 {
			t.Fatalf("%s %q, state %v", d.GetPredicateName(), seqOf(d), d.GetState())
		}
	})
	t.Run("the cursor finds what a moved date misses", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 2)
		c := New(context.Background(), itSvc)
		if _, err := c.UpdatesGetStateV2(&updates.TLUpdatesGetStateV2{AuthKeyId: key, UserId: u}); err != nil {
			t.Fatal(err)
		}
		if seq, ok := cursorOf(t, u, key); !ok || seq != 2 {
			t.Fatalf("cursor after getState %d, %v", seq, ok)
		}
		seqRow(t, u, 3, userbox.BasePts, 1000)
		seqState(t, u, userbox.BasePts, 3)
		// The device's date moved past the row (Telegram Desktop takes it
		// from any update).
		in := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key, UserId: u, Pts: userbox.BasePts, Date: 5000}
		if d := differenceWith(t, in); d.GetPredicateName() != updates.Predicate_difference || seqOf(d) != "s3" {
			t.Fatalf("%s %q", d.GetPredicateName(), seqOf(d))
		}
		if seq, _ := cursorOf(t, u, key); seq != 3 {
			t.Errorf("cursor after the difference %d, want 3", seq)
		}
		if d := differenceWith(t, in); d.GetPredicateName() != updates.Predicate_differenceEmpty {
			t.Errorf("again: %s %q", d.GetPredicateName(), seqOf(d))
		}
		// Another authorization of the account has its own cursor.
		other := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key + 1, UserId: u, Pts: userbox.BasePts, Date: 990}
		if d := differenceWith(t, other); seqOf(d) != "s3" {
			t.Errorf("another key: %q", seqOf(d))
		}
	})
	t.Run("date INT32_MAX asks for pts only", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 2)
		seqRow(t, u, 2, userbox.BasePts, 1000)
		in := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key, UserId: u, Pts: userbox.BasePts, Date: math.MaxInt32}
		if d := differenceWith(t, in); d.GetPredicateName() != updates.Predicate_differenceEmpty || d.GetState().GetSeq() != 2 {
			t.Fatalf("%s %q, state %v", d.GetPredicateName(), seqOf(d), d.GetState())
		}
		if _, ok := cursorOf(t, u, key); ok {
			t.Error("the cursor moved without the updates")
		}
	})
	t.Run("a cursor above the state is not trusted", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 3)
		seqRow(t, u, 2, userbox.BasePts, 1000)
		seqRow(t, u, 3, userbox.BasePts, 1000)
		if err := itSvc.Dao.SetSeqCursor(context.Background(), u, key, 50, 0); err != nil {
			t.Fatal(err)
		}
		in := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key, UserId: u, Pts: userbox.BasePts, Date: 990}
		if d := differenceWith(t, in); seqOf(d) != "s2 s3" {
			t.Errorf("difference %q", seqOf(d))
		}
		if seq, _ := cursorOf(t, u, key); seq != 3 {
			t.Errorf("cursor %d, want 3", seq)
		}
	})
	t.Run("a client ahead gets them with the state", func(t *testing.T) {
		u := newUser()
		seqState(t, u, 3, 2)
		seqRow(t, u, 2, 3, 1000)
		d := differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: 10, Date: 1000})
		if d.GetPredicateName() != updates.Predicate_difference || d.GetState().GetPts() != 3 || seqOf(d) != "s2" {
			t.Fatalf("%s %q, state %v", d.GetPredicateName(), seqOf(d), d.GetState())
		}
	})
	t.Run("slices: on the last page", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 0, 2, 3)
		seqState(t, u, 3, 3)
		seqRow(t, u, 2, 1, 1000)
		seqRow(t, u, 3, 2, 1000)
		defer func(n int32) { sliceRows = n }(sliceRows)
		sliceRows = 1
		in := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key, UserId: u, Pts: userbox.BasePts, Date: 1000}
		d := differenceWith(t, in)
		if d.GetPredicateName() != updates.Predicate_differenceSlice || seqOf(d) != "2" {
			t.Fatalf("first page: %s %q", d.GetPredicateName(), seqOf(d))
		}
		if st := d.GetIntermediateState(); st.GetDate() != 1000 || st.GetPts() != 2 {
			t.Errorf("intermediate state %v: want the date of the request", st)
		}
		if _, ok := cursorOf(t, u, key); ok {
			t.Error("the cursor moved on a page without the updates with seq")
		}
		in.Pts = 2
		if d = differenceWith(t, in); d.GetPredicateName() != updates.Predicate_difference || seqOf(d) != "3 s2 s3" {
			t.Fatalf("last page: %s %q", d.GetPredicateName(), seqOf(d))
		}
		if seq, _ := cursorOf(t, u, key); seq != 3 {
			t.Errorf("cursor %d, want 3", seq)
		}
	})
}
