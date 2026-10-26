//go:build integration

package userbox

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// setNotify writes an update with seq and nothing else.
func setNotify(peer int64) func(b *Box) error {
	return func(b *Box) error {
		b.AddSeqUpdate(notifySettings(peer))
		return nil
	}
}

type seqEntry struct {
	seq, pts int32
	date     int64
}

// seqJournalOf returns the user's seq rows in seq order.
func seqJournalOf(t testing.TB, db *sql.DB, userId int64) []seqEntry {
	t.Helper()
	rows, err := db.Query("SELECT seq, pts, date2 FROM user_seq_updates WHERE user_id = ? ORDER BY seq", userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []seqEntry
	for rows.Next() {
		var e seqEntry
		if err := rows.Scan(&e.seq, &e.pts, &e.date); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Writers of pts and seq to one user at once: seq goes from BaseSeq+1 without
// gaps or repeats, and the pts kept with each seq row follows the order of
// commits.
func TestSeqConcurrentOperations(t *testing.T) {
	const workers, perWorker = 16, 20
	pools(t, func(t *testing.T, db *sql.DB) {
		u := itUser()
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			seqs []int32
			errs []error
		)
		for w := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range perWorker {
					var got int32
					_, err := Run(context.Background(), db, u, func(b *Box) error {
						got = 0
						if (w+i)%2 == 0 {
							if err := send(b); err != nil {
								return err
							}
						}
						got = b.AddSeqUpdate(notifySettings(int64(w)))
						return nil
					})
					mu.Lock()
					if err != nil {
						errs = append(errs, err)
					} else {
						seqs = append(seqs, got)
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if len(errs) > 0 {
			t.Fatalf("%d operations failed, first: %v", len(errs), errs[0])
		}

		const total = workers * perWorker
		slices.Sort(seqs)
		if !slices.Equal(seqs, seq(BaseSeq+1, BaseSeq+total)) {
			t.Errorf("seq returned are not %d..%d without gaps or repeats", BaseSeq+1, BaseSeq+total)
		}
		st := mustRead(t, db, u)
		if st.Seq != BaseSeq+total || st.Pts != BasePts+total/2 {
			t.Errorf("committed state %+v, want seq %d, pts %d", st, BaseSeq+total, BasePts+total/2)
		}
		rep, err := CheckSeqJournal(context.Background(), db, u, BaseSeq)
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() || rep.Rows != total {
			t.Errorf("seq journal: %d rows, gaps %v", rep.Rows, rep.Gaps)
		}
		rows := seqJournalOf(t, db, u)
		for i := 1; i < len(rows); i++ {
			if rows[i].pts < rows[i-1].pts {
				t.Errorf("seq %d at pts %d after seq %d at pts %d", rows[i].seq, rows[i].pts, rows[i-1].seq, rows[i-1].pts)
				break
			}
		}
		mustCheckJournal(t, db, u)
	})
}

// A seq row already there (a writer bypassing Run) fails the transaction
// with the duplicate key, and nothing of it is written.
func TestSeqTakenFails(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 2)
	u := itUser()
	mustRun(t, db, u, setNotify(1))
	if _, err := db.Exec("INSERT INTO user_seq_updates (user_id, seq, pts, update_type, update_data, date2) VALUES (?, ?, 1, 'updateNotifySettings', '{}', 0)", u, BaseSeq+2); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), db, u, func(b *Box) error {
		if err := send(b); err != nil {
			return err
		}
		b.AddSeqUpdate(notifySettings(2))
		return nil
	})
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1062 {
		t.Fatalf("error %v, want a duplicate key", err)
	}
	if st := mustRead(t, db, u); st.Seq != BaseSeq+1 || st.Pts != BasePts || st.LastMessageBoxId != 0 {
		t.Errorf("state %+v: the failed transaction changed it", st)
	}
	if n := len(journalOf(t, db, u)); n != 0 {
		t.Errorf("%d journal rows written", n)
	}
}

// Raise lifts seq above the seq journal too: a client that saw a seq the
// restored rows lost gets a gap, not a repeat.
func TestRaiseSeq(t *testing.T) {
	ctx := context.Background()
	db := openPool(t, boxDSN(itDB.DSN()), 2)
	u := itUser()
	for i := range 3 {
		mustRun(t, db, u, setNotify(int64(i)))
	}
	if _, err := db.Exec("UPDATE user_state SET seq = ? WHERE user_id = ?", BaseSeq+1, u); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Raise(ctx, db, 100, u); err != nil {
		t.Fatal(err)
	}
	if st := mustRead(t, db, u); st.Seq != BaseSeq+3+100 {
		t.Errorf("seq after raise %d, want %d", st.Seq, BaseSeq+3+100)
	}
	rep, err := CheckSeqJournal(ctx, db, u, BaseSeq)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Rows != 3 || !slices.Equal(rep.Gaps, []Range{{From: BaseSeq + 3, To: BaseSeq + 3 + 100}}) {
		t.Errorf("seq journal after the jump: %+v", rep)
	}
	res := mustRun(t, db, u, setNotify(9))
	if res.State.Seq != BaseSeq+3+100+1 {
		t.Errorf("seq after the jump %d", res.State.Seq)
	}

	// A user with seq rows and no user_state row gets one above them.
	u2 := itUser()
	if _, err = db.Exec("INSERT INTO user_seq_updates (user_id, seq, pts, update_type, update_data, date2) VALUES (?, 5, 1, 'updateNotifySettings', '{}', 0)", u2); err != nil {
		t.Fatal(err)
	}
	if _, added, err := Raise(ctx, db, 10, u2); err != nil || added != 1 {
		t.Fatalf("raise: %d added, %v", added, err)
	}
	if st := mustRead(t, db, u2); st.Seq != 5+10 || st.Pts != BasePts+10 {
		t.Errorf("u2 added: %+v", st)
	}
}

// CheckSeqJournal reports seq without a row.
func TestCheckSeqJournalFindsGaps(t *testing.T) {
	db := openPool(t, boxDSN(itDB.DSN()), 2)
	u := itUser()
	if _, err := db.Exec("INSERT INTO user_state (user_id, seq) VALUES (?, 7)", u); err != nil {
		t.Fatal(err)
	}
	for _, s := range []int{2, 3, 5} {
		if _, err := db.Exec("INSERT INTO user_seq_updates (user_id, seq, pts, update_type, update_data, date2) VALUES (?, ?, 1, 'updateNotifySettings', '{}', 0)", u, s); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := CheckSeqJournal(context.Background(), db, u, BaseSeq)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Range{{From: 3, To: 4}, {From: 5, To: 7}}; rep.OK() || rep.Rows != 3 || !slices.Equal(rep.Gaps, want) {
		t.Errorf("report %+v, want gaps %v", rep, want)
	}
}
