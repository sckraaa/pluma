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
	if st.GetPts() != userbox.BasePts || st.GetSeq() != -1 {
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
		"CREATE TABLE IF NOT EXISTS lagging.auth_seq_updates LIKE teamgram.auth_seq_updates",
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
	d, err := New(context.Background(), withReplica).UpdatesGetDifferenceV2(&updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts})
	if err != nil {
		t.Fatal(err)
	}
	if d.GetState().GetPts() != 4 || fmt.Sprint(messageIds(d)) != "[2 3 4]" {
		t.Errorf("%s, state %v, messages %v", d.GetPredicateName(), d.GetState(), messageIds(d))
	}
}
