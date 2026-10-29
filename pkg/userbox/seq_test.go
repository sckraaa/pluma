package userbox

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func notifySettings(userId int64) *mtproto.Update {
	return mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
		Peer_NOTIFYPEER: mtproto.MakeTLNotifyPeer(&mtproto.NotifyPeer{Peer: mtproto.MakePeerUser(userId)}).To_NotifyPeer(),
		NotifySettings:  mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(),
	}).To_Update()
}

// setNotifySettings is the fn of a write with seq: a row of fn and one
// update with seq.
func setNotifySettings(peer int64) func(b *Box) error {
	return func(b *Box) error {
		if _, err := b.Tx().Exec("UPDATE user_notify_settings SET mute_until = 1 WHERE user_id = ?", b.UserId()); err != nil {
			return err
		}
		b.AddSeqUpdate(notifySettings(peer))
		return nil
	}
}

func TestRunAllocatesSeq(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	ctx := context.Background()

	var seqs []int32
	res, err := Run(ctx, db, 7, func(b *Box) error {
		b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
		seqs = append(seqs, b.AddSeqUpdate(notifySettings(1)))
		b.AddPtsUpdate(newMessage(b.NextMessageId()), 1)
		seqs = append(seqs, b.AddSeqUpdate(notifySettings(2)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{BaseSeq + 1, BaseSeq + 2}; !slices.Equal(seqs, want) {
		t.Errorf("seq %v, want %v", seqs, want)
	}
	if res.State.Seq != BaseSeq+2 || res.State.Pts != BasePts+2 {
		t.Errorf("result state %+v", res.State)
	}
	if st, _ := s.state(7); st.Seq != BaseSeq+2 {
		t.Errorf("committed seq %d, want %d", st.Seq, BaseSeq+2)
	}
	// Each row keeps the pts of the box where it was added.
	var got [][2]int32
	for _, r := range s.seqRows(7) {
		got = append(got, [2]int32{r.seq, r.pts})
		if r.updateType != mtproto.Predicate_updateNotifySettings {
			t.Errorf("seq row %d: type %q", r.seq, r.updateType)
		}
	}
	if want := [][2]int32{{BaseSeq + 1, BasePts + 1}, {BaseSeq + 2, BasePts + 2}}; !slices.Equal(got, want) {
		t.Errorf("seq rows (seq, pts) %v, want %v", got, want)
	}

	// A transaction with seq only writes the counters too, and the next
	// continues from the committed seq.
	if _, err = Run(ctx, db, 7, setNotifySettings(3)); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.state(7); st.Seq != BaseSeq+3 || st.Pts != BasePts+2 {
		t.Errorf("committed state %+v, want seq %d and pts %d", st, BaseSeq+3, BasePts+2)
	}
	if n := len(s.seqRows(7)); n != 3 {
		t.Errorf("%d seq rows, want 3", n)
	}
}

// An update that one authorization must not get (ADR 0022): its row keeps
// the key, and the other rows keep 0.
func TestRunSeqExcept(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	login := mtproto.MakeTLUpdateNewAuthorization(&mtproto.Update{Unconfirmed: true, Hash: 5}).To_Update()
	if _, err := Run(context.Background(), db, 7, func(b *Box) error {
		b.AddSeqUpdate(notifySettings(1))
		if seq := b.AddSeqUpdateExcept(login, 7001); seq != BaseSeq+2 {
			t.Errorf("seq %d, want %d", seq, BaseSeq+2)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got [][2]int64
	for _, r := range s.seqRows(7) {
		got = append(got, [2]int64{int64(r.seq), r.except})
	}
	if want := [][2]int64{{BaseSeq + 1, 0}, {BaseSeq + 2, 7001}}; !slices.Equal(got, want) {
		t.Errorf("seq rows (seq, except) %v, want %v", got, want)
	}
}

// The updates with seq: states of the account, those another user's
// change sets included (ADR 0021); never an update with pts, a draft, a
// service notification or typing.
func TestSeqUpdate(t *testing.T) {
	for _, p := range []string{
		mtproto.Predicate_updateNotifySettings,
		mtproto.Predicate_updatePeerSettings,
		mtproto.Predicate_updateUserName,
		mtproto.Predicate_updateUser,
		mtproto.Predicate_updateDialogPinned,
		mtproto.Predicate_updateChatParticipants,
		mtproto.Predicate_updateChat,
		mtproto.Predicate_updateChatDefaultBannedRights,
		mtproto.Predicate_updatePendingJoinRequests,
		mtproto.Predicate_updateNewAuthorization,
	} {
		if !SeqUpdate(p) {
			t.Errorf("%s goes without seq", p)
		}
	}
	for _, p := range []string{
		mtproto.Predicate_updateNewMessage,
		mtproto.Predicate_updateDeleteMessages,
		mtproto.Predicate_updateReadHistoryInbox,
		mtproto.Predicate_updateDraftMessage,
		mtproto.Predicate_updateServiceNotification,
		mtproto.Predicate_updateUserTyping,
		mtproto.Predicate_updateUserStatus,
		mtproto.Predicate_updateNewChannelMessage,
		"",
	} {
		if SeqUpdate(p) {
			t.Errorf("%q goes with seq", p)
		}
	}
}

func TestAddSeqUpdateMisuse(t *testing.T) {
	for _, c := range []struct {
		name string
		u    *mtproto.Update
	}{
		{"nil update", nil},
		{"pts update", newMessage(1)},
		{"draft", mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{}).To_Update()},
		{"typing", mtproto.MakeTLUpdateUserTyping(&mtproto.Update{}).To_Update()},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newFakeServer()
			db := s.open(t)
			var seq int32
			_, err := Run(context.Background(), db, 7, func(b *Box) error {
				b.AddSeqUpdate(notifySettings(1))
				seq = b.AddSeqUpdate(c.u)
				return nil
			})
			if !errors.Is(err, ErrMisuse) {
				t.Fatalf("error %v, want %v", err, ErrMisuse)
			}
			if seq != 0 {
				t.Errorf("seq %d given to a wrong update", seq)
			}
			if s.count("commit") != 0 || len(s.seqRows(7)) != 0 {
				t.Errorf("statements %v: want no commit", s.statements())
			}
		})
	}
}

// Another writer moved seq under the lock: the conditional UPDATE finds
// nothing to change, and nothing is committed.
func TestRunSeqChanged(t *testing.T) {
	s := newFakeServer()
	db := s.open(t)
	if _, err := Run(context.Background(), db, 7, setNotifySettings(1)); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), db, 7, func(b *Box) error {
		s.mu.Lock()
		st := s.states[7]
		st.Seq += 10
		s.states[7] = st
		s.mu.Unlock()
		return setNotifySettings(2)(b)
	})
	if !errors.Is(err, errStateChanged) {
		t.Fatalf("error %v, want %v", err, errStateChanged)
	}
	if rows := s.seqRows(7); len(rows) != 1 {
		t.Errorf("seq rows %+v: the second transaction's row was committed", rows)
	}
}

// A COMMIT whose reply is lost is checked by its seq rows when it wrote no
// journal rows.
func TestRunSeqCommitOutcome(t *testing.T) {
	broken := errors.New("connection reset")
	t.Run("applied", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		s.failNext("commit", injection{err: broken, afterApply: true, breakConn: true})
		res, err := Run(context.Background(), db, 7, setNotifySettings(1))
		if err != nil {
			t.Fatal(err)
		}
		if res.State.Seq != BaseSeq+1 {
			t.Errorf("result %+v", res.State)
		}
		if s.count("verify_lock") != 1 || s.count("verify_seq_row") != 1 || s.count("lock") != 1 {
			t.Errorf("statements %v: want one attempt and a check", s.statements())
		}
	})
	t.Run("not applied", func(t *testing.T) {
		s := newFakeServer()
		db := s.open(t)
		if _, err := Run(context.Background(), db, 7, setNotifySettings(1)); err != nil {
			t.Fatal(err)
		}
		s.failNext("commit", injection{err: broken, breakConn: true})
		res, err := Run(context.Background(), db, 7, setNotifySettings(2))
		if err != nil {
			t.Fatal(err)
		}
		if res.State.Seq != BaseSeq+2 {
			t.Errorf("seq %d, want %d", res.State.Seq, BaseSeq+2)
		}
		if rows := s.seqRows(7); len(rows) != 2 || rows[1].seq != BaseSeq+2 {
			t.Errorf("seq rows %+v: want one row per operation", rows)
		}
		if s.count("verify_lock") != 1 || s.count("lock") != 3 {
			t.Errorf("statements %v: want a check and a second attempt", s.statements())
		}
	})
}
