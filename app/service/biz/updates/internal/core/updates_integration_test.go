//go:build integration

package core

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// updates.getState and updates.getDifference read the committed state of
// the box (user_state) and the journal up to it (ADR 0011, p. 7).

var (
	itDB      *testinfra.MySQL
	itSvc     *svc.ServiceContext
	itRaw     *sql.DB
	itUserSeq atomic.Int64
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	itSvc = svc.NewServiceContext(config.Config{Mysql: itDB.Config()})
	if itRaw, err = sql.Open("mysql", itDB.DSN()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itRaw.Close()
	_ = itDB.Terminate(ctx)
	os.Exit(code)
}

func newUser() int64 { return 40_000_000 + itUserSeq.Add(1) }

// fixture writes the user's state and journal rows of updateNewMessage with
// the given pts (pts_count 1), as box transactions would have.
func fixture(t *testing.T, userId int64, statePts int32, journalPts ...int32) {
	t.Helper()
	if statePts > 0 {
		if _, err := itRaw.Exec("INSERT INTO user_state (user_id, pts) VALUES (?, ?)", userId, statePts); err != nil {
			t.Fatal(err)
		}
	}
	for _, pts := range journalPts {
		u := mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
			Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: pts, Message: fmt.Sprint(pts)}).To_Message(),
			Pts_INT32:       pts,
			PtsCount:        1,
		}).To_Update()
		data, err := jsonx.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = itRaw.Exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, ?, 1, ?, ?, 0)",
			userId, pts, mtproto.PTS_UPDATE_NEW_MESSAGE, string(data)); err != nil {
			t.Fatal(err)
		}
	}
}

// journal writes rows of updates with their own pts, and the state at
// statePts (0: no state row).
func journal(t *testing.T, userId int64, statePts int32, rows ...*mtproto.Update) {
	t.Helper()
	if statePts > 0 {
		if _, err := itRaw.Exec("INSERT INTO user_state (user_id, pts) VALUES (?, ?) ON DUPLICATE KEY UPDATE pts = VALUES(pts)", userId, statePts); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range rows {
		data, err := jsonx.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = itRaw.Exec("INSERT INTO user_pts_updates (user_id, pts, pts_count, update_type, update_data, date2) VALUES (?, ?, ?, ?, ?, 0)",
			userId, u.GetPts_INT32(), u.GetPtsCount(), mtproto.GetUpdateType(u), string(data)); err != nil {
			t.Fatal(err)
		}
	}
}

// messageRow writes the box copy boxId of userId, sent by senderId with
// randomId.
func messageRow(t *testing.T, userId int64, boxId int32, senderId, randomId int64) {
	t.Helper()
	if _, err := itRaw.Exec("INSERT INTO messages (id, user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		userId*1000+int64(boxId), userId, boxId, userId, userId, userId*1000+int64(boxId), senderId, mtproto.PEER_USER, userId, randomId); err != nil {
		t.Fatal(err)
	}
}

// incomingRow writes userId's copy boxId of a private message from fromId,
// added at pts.
func incomingRow(t *testing.T, userId int64, boxId int32, fromId int64, pts int32, deleted bool) {
	t.Helper()
	did := mtproto.MakeDialogId(userId, mtproto.PEER_USER, fromId)
	if _, err := itRaw.Exec("INSERT INTO messages (id, user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, pts, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		userId*1000+int64(boxId), userId, boxId, did.A, did.B, userId*1000+int64(boxId), fromId, mtproto.PEER_USER, userId, pts, deleted); err != nil {
		t.Fatal(err)
	}
}

func difference(t *testing.T, userId int64, pts, limit int32) *updates.Difference {
	t.Helper()
	in := &updates.TLUpdatesGetDifferenceV2{UserId: userId, Pts: pts}
	if limit > 0 {
		in.PtsTotalLimit = &wrapperspb.Int32Value{Value: limit}
	}
	d, err := New(context.Background(), itSvc).UpdatesGetDifferenceV2(in)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func messageIds(d *updates.Difference) []int32 {
	var ids []int32
	for _, m := range d.GetNewMessages() {
		ids = append(ids, m.GetId())
	}
	return ids
}

func TestGetState(t *testing.T) {
	c := New(context.Background(), itSvc)
	fresh := newUser()
	st, err := c.UpdatesGetStateV2(&updates.TLUpdatesGetStateV2{UserId: fresh})
	if err != nil {
		t.Fatal(err)
	}
	if st.GetPts() != userbox.BasePts || st.GetSeq() != userbox.BaseSeq {
		t.Errorf("state of a new user %+v", st)
	}

	u := newUser()
	fixture(t, u, 3, 2, 3)
	if st, err = c.UpdatesGetStateV2(&updates.TLUpdatesGetStateV2{UserId: u}); err != nil || st.GetPts() != 3 {
		t.Errorf("state %v, %v", st, err)
	}
}

func TestGetDifference(t *testing.T) {
	t.Run("rows committed after the state are not read", func(t *testing.T) {
		u := newUser()
		// Rows 6 and 7 stand for a transaction committing after the state
		// was read.
		fixture(t, u, 5, 2, 3, 4, 5, 6, 7)
		d := difference(t, u, userbox.BasePts, 0)
		if d.GetPredicateName() != updates.Predicate_difference || d.GetState().GetPts() != 5 {
			t.Fatalf("%s, state %v", d.GetPredicateName(), d.GetState())
		}
		if got := messageIds(d); fmt.Sprint(got) != "[2 3 4 5]" {
			t.Errorf("messages %v", got)
		}
	})
	t.Run("slices", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 6, 2, 3, 4, 5, 6)
		d := difference(t, u, userbox.BasePts, 3)
		if d.GetPredicateName() != updates.Predicate_differenceSlice || d.GetIntermediateState().GetPts() != 4 {
			t.Fatalf("%s, intermediate %v", d.GetPredicateName(), d.GetIntermediateState())
		}
		d = difference(t, u, 4, 3)
		if d.GetPredicateName() != updates.Predicate_difference || d.GetState().GetPts() != 6 || fmt.Sprint(messageIds(d)) != "[5 6]" {
			t.Fatalf("%s, state %v, messages %v", d.GetPredicateName(), d.GetState(), messageIds(d))
		}
	})
	t.Run("a jump of the state is a difference without rows", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 1_000_003, 2, 3)
		d := difference(t, u, 3, 0)
		if d.GetPredicateName() != updates.Predicate_difference || d.GetState().GetPts() != 1_000_003 || len(d.GetNewMessages()) != 0 {
			t.Fatalf("%s, state %v", d.GetPredicateName(), d.GetState())
		}
	})
	t.Run("up to date", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 3, 2, 3)
		if d := difference(t, u, 3, 0); d.GetPredicateName() != updates.Predicate_differenceEmpty {
			t.Fatalf("%s", d.GetPredicateName())
		}
	})
}

// otherUpdates writes other_updates as predicate(details).
func otherUpdates(d *updates.Difference) string {
	var out []string
	for _, u := range d.GetOtherUpdates() {
		s := strings.TrimPrefix(u.GetPredicateName(), "update")
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateMessageID:
			s += fmt.Sprintf("(%d,%d)", u.GetId_INT32(), u.GetRandomId())
		case mtproto.Predicate_updateDeleteMessages:
			s += fmt.Sprint(u.GetMessages())
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// The journal of the Telegram fixture saved_messages: a message sent to
// Saved Messages and read at once, edited, deleted.
func TestGetDifferenceFolds(t *testing.T) {
	u := newUser()
	self := mtproto.MakePeerUser(u)
	sent := mtproto.MakeTLMessage(&mtproto.Message{Id: 1, Message: "sent", PeerId: self}).To_Message()
	edited := mtproto.MakeTLMessage(&mtproto.Message{Id: 1, Message: "edited", PeerId: self, EditDate: wrapperspb.Int32(1700000000)}).To_Message()
	messageRow(t, u, 1, u, 555)
	journal(t, u, 4,
		mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: sent, Pts_INT32: 2, PtsCount: 1}).To_Update(),
		mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: self, MaxId: 1, Pts_INT32: 3, PtsCount: 1}).To_Update(),
		mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: edited, Pts_INT32: 4, PtsCount: 1}).To_Update(),
	)
	c := New(context.Background(), itSvc)
	st, err := c.UpdatesGetStateV2(&updates.TLUpdatesGetStateV2{UserId: u})
	if err != nil {
		t.Fatal(err)
	}

	d := difference(t, u, userbox.BasePts, 0)
	if ms := d.GetNewMessages(); len(ms) != 1 || ms[0].GetMessage() != "edited" || ms[0].GetEditDate() == nil {
		t.Errorf("after the edit, new messages %v", ms)
	}
	if got := otherUpdates(d); got != "MessageID(1,555) ReadHistoryInbox" {
		t.Errorf("after the edit, other updates %s", got)
	}
	if d.GetState().GetSeq() != st.GetSeq() || st.GetSeq() != userbox.BaseSeq {
		t.Errorf("seq of the difference %d, of the state %d; want both %d", d.GetState().GetSeq(), st.GetSeq(), userbox.BaseSeq)
	}

	journal(t, u, 5, mtproto.MakeTLUpdateDeleteMessages(&mtproto.Update{Messages: []int32{1}, Pts_INT32: 5, PtsCount: 1}).To_Update())
	d = difference(t, u, userbox.BasePts, 0)
	if len(d.GetNewMessages()) != 0 || otherUpdates(d) != "DeleteMessages[1] ReadHistoryInbox" {
		t.Errorf("after the deletion, new messages %v, other updates %s", d.GetNewMessages(), otherUpdates(d))
	}
	if d = difference(t, u, 5, 0); d.GetPredicateName() != updates.Predicate_differenceEmpty || d.GetState().GetSeq() != st.GetSeq() {
		t.Errorf("up to date: %s, seq %d", d.GetPredicateName(), d.GetState().GetSeq())
	}
	if d = difference(t, u, userbox.BasePts, 2); d.GetPredicateName() != updates.Predicate_differenceSlice || d.GetIntermediateState().GetSeq() != st.GetSeq() {
		t.Errorf("slice: %s, seq %d", d.GetPredicateName(), d.GetIntermediateState().GetSeq())
	}

	// A copy received from another sender keeps the sender's random_id, but
	// it is not the account's own message.
	r := newUser()
	messageRow(t, r, 1, u, 777)
	journal(t, r, 2, mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
		Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: 1, Message: "hi", PeerId: mtproto.MakePeerUser(u)}).To_Message(), Pts_INT32: 2, PtsCount: 1,
	}).To_Update())
	if d = difference(t, r, userbox.BasePts, 0); len(d.GetNewMessages()) != 1 || otherUpdates(d) != "" {
		t.Errorf("incoming: new messages %v, other updates %s", d.GetNewMessages(), otherUpdates(d))
	}
}

// The read history of a dialog comes last in the difference, so its
// still_unread_count is the dialog's at the end: messages after the read
// count, deleted ones do not.
func TestGetDifferenceUnreadCount(t *testing.T) {
	u, alice := newUser(), newUser()
	peer := mtproto.MakePeerUser(alice)
	incoming := func(id, pts int32) *mtproto.Update {
		return mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
			Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: id, PeerId: peer}).To_Message(), Pts_INT32: pts, PtsCount: 1,
		}).To_Update()
	}
	// 1 and 2 arrive, the phone reads up to 1 (1 unread), 3 arrives, 2 is
	// deleted: 3 is unread at the end.
	incomingRow(t, u, 1, alice, 2, false)
	incomingRow(t, u, 2, alice, 3, true)
	incomingRow(t, u, 3, alice, 5, false)
	journal(t, u, 6,
		incoming(1, 2), incoming(2, 3),
		mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: peer, MaxId: 1, StillUnreadCount: 1, Pts_INT32: 4, PtsCount: 1}).To_Update(),
		incoming(3, 5),
		mtproto.MakeTLUpdateDeleteMessages(&mtproto.Update{Messages: []int32{2}, Pts_INT32: 6, PtsCount: 1}).To_Update(),
	)
	d := difference(t, u, userbox.BasePts, 0)
	var read *mtproto.Update
	for _, up := range d.GetOtherUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateReadHistoryInbox {
			read = up
		}
	}
	if read == nil || read.GetMaxId() != 1 || read.GetStillUnreadCount() != 1 {
		t.Fatalf("read %v, want max_id 1 with 1 unread (message 3)", read)
	}
	// A slice up to the read: 3 comes after it and does not count. 2 is
	// deleted only after the slice, but the messages table keeps that it is
	// deleted, not when, so it no longer counts; the next slice deletes it.
	if d = difference(t, u, userbox.BasePts, 3); d.GetPredicateName() != updates.Predicate_differenceSlice {
		t.Fatalf("%s", d.GetPredicateName())
	}
	for _, up := range d.GetOtherUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateReadHistoryInbox && up.GetStillUnreadCount() != 0 {
			t.Errorf("slice read %v, want 0 unread", up)
		}
	}
}

// TestGetDifferenceWithReadReplica: with a read replica configured
// (ReadDSN) the journal still comes from where the state does. The replica
// here is an empty copy of the tables: it lags behind everything.
func TestGetDifferenceWithReadReplica(t *testing.T) {
	root, err := sql.Open("mysql", itDB.RootDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, q := range []string{
		"CREATE DATABASE IF NOT EXISTS lagging",
		"CREATE TABLE IF NOT EXISTS lagging.user_pts_updates LIKE teamgram.user_pts_updates",
		"CREATE TABLE IF NOT EXISTS lagging.messages LIKE teamgram.messages",
		"GRANT SELECT ON lagging.* TO '" + testinfra.MySQLUser + "'@'%'",
	} {
		if _, err = root.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	c := itDB.Config()
	c.ReadDSN = []string{strings.Replace(itDB.DSN(), "/"+testinfra.MySQLDatabase+"?", "/lagging?", 1)}
	withReplica := svc.NewServiceContext(config.Config{Mysql: c})

	u := newUser()
	fixture(t, u, 4, 2, 3, 4)
	// The random_id of an own message comes from there too.
	messageRow(t, u, 3, u, 333)
	d, err := New(context.Background(), withReplica).UpdatesGetDifferenceV2(&updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts})
	if err != nil {
		t.Fatal(err)
	}
	if d.GetState().GetPts() != 4 || fmt.Sprint(messageIds(d)) != "[2 3 4]" || otherUpdates(d) != "MessageID(3,333)" {
		t.Errorf("%s, state %v, messages %v, other updates %s", d.GetPredicateName(), d.GetState(), messageIds(d), otherUpdates(d))
	}
}
