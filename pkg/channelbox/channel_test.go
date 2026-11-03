package channelbox

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func newMessage(channelId int64, id int32) *mtproto.Update {
	msg := mtproto.MakeTLMessage(&mtproto.Message{
		Id:     id,
		PeerId: mtproto.MakeTLPeerChannel(&mtproto.Peer{ChannelId: channelId}).To_Peer(),
	}).To_Message()
	return mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: msg}).To_Update()
}

func deleteMessages(channelId int64, ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: channelId, Messages: ids}).To_Update()
}

// send writes one message to the channel: a message id and one pts.
func send(channelId int64) func(b *userbox.Box) error {
	return func(b *userbox.Box) error {
		c, err := Open(b, channelId)
		if err != nil {
			return err
		}
		c.AddPtsUpdate(newMessage(channelId, c.NextMessageId()), 1)
		return nil
	}
}

func run(t *testing.T, db *sql.DB, userId int64, fn func(b *userbox.Box) error) (*userbox.Result, error) {
	t.Helper()
	return userbox.Run(context.Background(), db, userId, fn)
}

// TestCreate: a new channel starts in the base state, its first message is
// id 1 with pts BasePts+1, and the row, the counters and the journal commit
// together with the creator's transaction.
func TestCreate(t *testing.T) {
	s := newFakeServer()
	db := s.open(t, 2)
	res, err := run(t, db, 7, func(b *userbox.Box) error {
		c, err := Create(b, NewChannel{Id: 100, AccessHash: 1, CreatorUserId: 7, Title: "t", Megagroup: true, Date: 5})
		if err != nil {
			return err
		}
		if c.State() != (State{Pts: BasePts, Date: b.Date()}) {
			t.Errorf("state of a new channel %+v", c.State())
		}
		if id := c.NextMessageId(); id != 1 {
			t.Errorf("first message id %d, want 1", id)
		}
		c.AddPtsUpdate(newMessage(100, 1), 1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, rows, ok := s.channel(100)
	if !ok || st.Pts != BasePts+1 || st.LastMessageId != 1 || len(rows) != 1 || rows[0].pts != BasePts+1 {
		t.Fatalf("committed %+v, journal %+v", st, rows)
	}
	r := Results(res)
	if len(r) != 1 || r[0].ChannelId != 100 || r[0].State.Pts != BasePts+1 || r[0].State.LastMessageId != 1 ||
		len(r[0].Updates) != 1 || r[0].Updates[0].Pts_INT32 != BasePts+1 || r[0].Updates[0].PtsCount != 1 {
		t.Fatalf("results %+v", r)
	}

	// The id is taken.
	_, err = run(t, db, 7, func(b *userbox.Box) error {
		_, err := Create(b, NewChannel{Id: 100, Title: "again"})
		return err
	})
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1062 {
		t.Fatalf("second create: %v, want a duplicate key", err)
	}
}

// TestOpen: transactions of different users on one channel continue its
// pts and ids; a count of several pts covers them with one row; a
// transaction that only reads writes nothing.
func TestOpen(t *testing.T) {
	s := newFakeServer()
	s.addChannel(100)
	db := s.open(t, 2)
	for user := int64(1); user <= 3; user++ {
		if _, err := run(t, db, user, send(100)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := run(t, db, 4, func(b *userbox.Box) error {
		c, err := Open(b, 100)
		if err != nil {
			return err
		}
		c.AddPtsUpdate(deleteMessages(100, 1, 2), 2)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, rows, _ := s.channel(100)
	if st.Pts != BasePts+5 || st.LastMessageId != 3 || len(rows) != 4 || rows[3].pts != BasePts+5 || rows[3].ptsCount != 2 {
		t.Fatalf("committed %+v, journal %+v", st, rows)
	}

	updates := s.count("update")
	res, err := run(t, db, 5, func(b *userbox.Box) error {
		c, err := Open(b, 100)
		if err != nil {
			return err
		}
		if c.State().Pts != BasePts+5 {
			t.Errorf("locked state %+v", c.State())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.count("update") != updates {
		t.Error("a transaction that allocated nothing updated the channel")
	}
	if r := Results(res); len(r) != 1 || r[0].State.Pts != BasePts+5 || len(r[0].Updates) != 0 {
		t.Errorf("results of a read %+v", r)
	}
}

// TestRollback: an error of fn writes nothing and burns no pts or id.
func TestRollback(t *testing.T) {
	s := newFakeServer()
	s.addChannel(100)
	db := s.open(t, 2)
	fail := errors.New("business")
	_, err := run(t, db, 1, func(b *userbox.Box) error {
		if err := send(100)(b); err != nil {
			return err
		}
		return fail
	})
	if !errors.Is(err, fail) {
		t.Fatalf("error %v", err)
	}
	if st, rows, _ := s.channel(100); st.Pts != BasePts || st.LastMessageId != 0 || len(rows) != 0 {
		t.Fatalf("after a rollback %+v %+v", st, rows)
	}
	if _, err = run(t, db, 1, send(100)); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := s.channel(100); st.Pts != BasePts+1 || st.LastMessageId != 1 {
		t.Fatalf("after the next send %+v", st)
	}
}

// TestOpenMissing: no channel is ErrNoChannel, which fn may return.
func TestOpenMissing(t *testing.T) {
	db := newFakeServer().open(t, 2)
	_, err := run(t, db, 1, send(100))
	if !errors.Is(err, ErrNoChannel) {
		t.Fatalf("error %v, want ErrNoChannel", err)
	}
}

// TestMisuse: an update that is not of this channel's box, a bad count, a
// channel locked out of order or created twice roll the transaction back
// with ErrMisuse, which is deterministic.
func TestMisuse(t *testing.T) {
	cases := map[string]func(b *userbox.Box) error{
		"update of the common box": func(b *userbox.Box) error {
			c, err := Open(b, 100)
			if err != nil {
				return err
			}
			u := mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: newMessage(100, 1).Message_MESSAGE}).To_Update()
			c.AddPtsUpdate(u, 1)
			return nil
		},
		"update of another channel": func(b *userbox.Box) error {
			c, err := Open(b, 100)
			if err != nil {
				return err
			}
			c.AddPtsUpdate(deleteMessages(200, 1), 1)
			return nil
		},
		"pts_count 0": func(b *userbox.Box) error {
			c, err := Open(b, 100)
			if err != nil {
				return err
			}
			c.AddPtsUpdate(deleteMessages(100, 1), 0)
			return nil
		},
		"descending lock order": func(b *userbox.Box) error {
			if _, err := Open(b, 200); err != nil {
				return err
			}
			_, err := Open(b, 100)
			if !errors.Is(err, userbox.ErrMisuse) {
				t.Errorf("open out of order: %v", err)
			}
			return nil // the transaction fails anyway
		},
		"created twice": func(b *userbox.Box) error {
			if _, err := Create(b, NewChannel{Id: 300, Title: "t"}); err != nil {
				return err
			}
			_, err := Create(b, NewChannel{Id: 300, Title: "t"})
			if !errors.Is(err, userbox.ErrMisuse) {
				t.Errorf("second create: %v", err)
			}
			return nil
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			s := newFakeServer()
			s.addChannel(100)
			s.addChannel(200)
			db := s.open(t, 2)
			_, err := run(t, db, 1, fn)
			if !errors.Is(err, userbox.ErrMisuse) || !userbox.Deterministic(err) {
				t.Fatalf("error %v, want a deterministic misuse", err)
			}
			for _, id := range []int64{100, 200} {
				if st, rows, _ := s.channel(id); st.Pts != BasePts || len(rows) != 0 {
					t.Errorf("channel %d written: %+v %+v", id, st, rows)
				}
			}
			if _, _, ok := s.channel(300); ok {
				t.Error("channel 300 created")
			}
		})
	}
}

// TestAscendingOrder: channels opened in ascending id, and one opened
// again, are one transaction's.
func TestAscendingOrder(t *testing.T) {
	s := newFakeServer()
	s.addChannel(100)
	s.addChannel(200)
	db := s.open(t, 2)
	res, err := run(t, db, 1, func(b *userbox.Box) error {
		if err := send(100)(b); err != nil {
			return err
		}
		if err := send(200)(b); err != nil {
			return err
		}
		again, err := Open(b, 100)
		if err != nil {
			return err
		}
		if again.Pts() != BasePts+1 {
			t.Errorf("channel opened again at pts %d", again.Pts())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := Results(res); len(r) != 2 || r[0].ChannelId != 100 || r[1].ChannelId != 200 {
		t.Fatalf("results %+v", r)
	}
}

// TestCommitLost: a COMMIT whose reply is lost is checked on another
// connection by the channel's journal: applied is a success, not applied
// is tried again; either way the journal has each pts once. The user has
// written before, so the user's own row does not decide the outcome.
func TestCommitLost(t *testing.T) {
	lost := errors.New("connection reset")
	for _, applied := range []bool{true, false} {
		s := newFakeServer()
		s.addChannel(100)
		db := s.open(t, 2)
		if _, err := run(t, db, 1, send(100)); err != nil {
			t.Fatal(err)
		}
		s.failNext("commit", injection{err: lost, afterApply: applied})
		res, err := run(t, db, 1, send(100))
		if err != nil {
			t.Fatalf("applied %v: %v", applied, err)
		}
		st, rows, _ := s.channel(100)
		if st.Pts != BasePts+2 || len(rows) != 2 || Results(res)[0].State.Pts != BasePts+2 {
			t.Fatalf("applied %v: %+v %+v", applied, st, rows)
		}
		if n := s.count("verify_channel"); n != 1 {
			t.Errorf("applied %v: %d channel checks, want 1", applied, n)
		}
		if n := s.count("journal"); applied && n != 2 || !applied && n != 3 {
			t.Errorf("applied %v: %d journal writes", applied, n)
		}
	}
}

// TestApplied: the check of a lost COMMIT finds the channel's journal rows
// as written, and nothing else: no channel, a pts below the transaction's,
// or another row at its pts mean not applied.
func TestApplied(t *testing.T) {
	row := journalRow{pts: BasePts + 1, ptsCount: 1, updateType: 9, data: `{"id":1}`}
	c := &Channel{id: 100, state: State{Pts: BasePts}, pts: BasePts + 1, journal: []journalRow{row}}
	other := row
	other.data = `{"id":2}`
	cases := []struct {
		name string
		st   *State
		rows []journalRow
		want bool
	}{
		{"no channel", nil, nil, false},
		{"pts below", &State{Pts: BasePts}, nil, false},
		{"the row", &State{Pts: BasePts + 1}, []journalRow{row}, true},
		{"another row", &State{Pts: BasePts + 1}, []journalRow{other}, false},
		{"later commits", &State{Pts: BasePts + 3}, []journalRow{row, {pts: BasePts + 3, ptsCount: 2, updateType: 10, data: "{}"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeServer()
			if tc.st != nil {
				s.channels[100] = *tc.st
				s.journal[100] = tc.rows
			}
			db := s.open(t, 1)
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			got, err := c.Applied(context.Background(), tx)
			if err != nil || got != tc.want {
				t.Fatalf("applied %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// TestConcurrent: transactions of many users on one channel at once give
// each pts and id once, in commit order. It fails when the channel's row is
// read without its lock (FOR UPDATE): the transactions then read the same
// counters, and all but one fail on the conditional UPDATE or the journal's
// key.
func TestConcurrent(t *testing.T) {
	const users = 20
	s := newFakeServer()
	s.addChannel(100)
	db := s.open(t, users)
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []error
		ready  = make(chan struct{})
	)
	for u := int64(1); u <= users; u++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := userbox.Run(context.Background(), db, u, func(b *userbox.Box) error {
				<-ready
				return send(100)(b)
			})
			if err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
			}
		}()
	}
	close(ready)
	wg.Wait()
	if len(failed) > 0 {
		t.Fatalf("%d of %d failed: %v", len(failed), users, failed)
	}
	st, rows, _ := s.channel(100)
	if st.Pts != BasePts+users || st.LastMessageId != users || len(rows) != users {
		t.Fatalf("committed %+v, %d rows", st, len(rows))
	}
	for i, r := range rows {
		if r.pts != BasePts+int32(i)+1 {
			t.Fatalf("row %d has pts %d: commits out of pts order", i, r.pts)
		}
	}
}
