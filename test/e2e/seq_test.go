//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The seq journal (task 010, ADR 0020): an update with seq reaches every
// device of the account, the one that made the change included, in a
// container with the next seq; a device that missed it finds it in
// updates.getDifference, by its date or, when its date moved past the
// update (Telegram Desktop moves it with any update), by the state it was
// last given.

func muteUsers(ctx context.Context, t *testing.T, u *user, until int32) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
		Constructor: u.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
		Peer:        mtproto.MakeTLInputNotifyUsers(nil).To_InputNotifyPeer(),
		Settings: mtproto.MakeTLInputPeerNotifySettings(&mtproto.InputPeerNotifySettings{
			MuteUntil: mtproto.MakeFlagsInt32(until),
		}).To_InputPeerNotifySettings(),
	}); err != nil {
		t.Fatalf("account.updateNotifySettings: %v", err)
	}
}

// waitSeq waits on u for the push of updateNotifySettings of the class of
// users with mute_until until, and returns the seq of its container.
func waitSeq(ctx context.Context, t *testing.T, u *user, until int32) int32 {
	t.Helper()
	up, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateNotifySettings &&
				x.GetPeer_NOTIFYPEER().GetPredicateName() == mtproto.Predicate_notifyUsers &&
				x.GetNotifySettings().GetMuteUntil().GetValue() == until {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("no push of updateNotifySettings %d: %v", until, err)
	}
	if up.GetPredicateName() != mtproto.Predicate_updates || len(up.GetUpdates()) != 1 {
		t.Errorf("push %s with %d updates, want updates with one", up.GetPredicateName(), len(up.GetUpdates()))
	}
	return up.GetSeq()
}

// mutedIn returns the mute_until of the class of users in the updates of d
// with seq, in order.
func mutedIn(d *mtproto.Updates_Difference) []int32 {
	var out []int32
	for _, x := range updatesOfType(d, mtproto.Predicate_updateNotifySettings) {
		if x.GetPeer_NOTIFYPEER().GetPredicateName() == mtproto.Predicate_notifyUsers {
			out = append(out, x.GetNotifySettings().GetMuteUntil().GetValue())
		}
	}
	return out
}

func TestSeqJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	phoneA := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	stA := getState(ctx, t, phoneA)
	stB := getState(ctx, t, laptop)
	if stA.GetSeq() != stB.GetSeq() {
		t.Fatalf("seq of the two devices: %d and %d, want one of the account", stA.GetSeq(), stB.GetSeq())
	}

	// Both devices get the push with the next seq, the phone that made
	// the change too.
	muteUsers(ctx, t, phoneA, 1000)
	if seq := waitSeq(ctx, t, phoneA, 1000); seq != stA.GetSeq()+1 {
		t.Errorf("phone: seq %d, want %d", seq, stA.GetSeq()+1)
	}
	if seq := waitSeq(ctx, t, laptop, 1000); seq != stB.GetSeq()+1 {
		t.Errorf("laptop: seq %d, want %d", seq, stB.GetSeq()+1)
	}

	// The laptop lost the push: the difference from its state has it.
	d := differenceOnce(ctx, t, laptop, stB)
	if d.GetPredicateName() != mtproto.Predicate_updates_difference {
		t.Fatalf("difference %s, want updates.difference", d.GetPredicateName())
	}
	if got := mutedIn(d); len(got) != 1 || got[0] != 1000 {
		t.Errorf("updates with seq in the difference %v, want [1000]", got)
	}
	if d.GetState().GetSeq() != stB.GetSeq()+1 {
		t.Errorf("state seq %d, want %d", d.GetState().GetSeq(), stB.GetSeq()+1)
	}

	// The laptop moved its date past the next change with another update
	// and lost that push too: its cursor finds it.
	muteUsers(ctx, t, phoneA, 2000)
	waitSeq(ctx, t, laptop, 2000)
	moved := &mtproto.TLUpdatesState{Data2: &mtproto.Updates_State{
		Pts:  d.GetState().GetPts(),
		Qts:  d.GetState().GetQts(),
		Seq:  d.GetState().GetSeq(),
		Date: int32(time.Now().Add(time.Hour).Unix()),
	}}
	d = differenceOnce(ctx, t, laptop, moved)
	if got := mutedIn(d); len(got) != 1 || got[0] != 2000 {
		t.Errorf("updates with seq after a moved date %v (%s), want [2000]", got, d.GetPredicateName())
	}
	if d = differenceOnce(ctx, t, laptop, moved); d.GetPredicateName() != mtproto.Predicate_updates_differenceEmpty {
		t.Errorf("difference once more: %s, want updates.differenceEmpty", d.GetPredicateName())
	}

	// From the first state, both changes fold into the last one.
	if got := mutedIn(differenceOnce(ctx, t, phoneA, stA)); len(got) != 1 || got[0] != 2000 {
		t.Errorf("phone from its first state: %v, want [2000]", got)
	}
}
