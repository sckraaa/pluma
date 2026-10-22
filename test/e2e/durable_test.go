//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The durable box of a user (task 002, ADR 0011): every pts a client sees is
// committed with its update, so updates.getDifference from any state gives
// every message, in order, and the state has no gaps; Redis holds none of
// it.

func getState(ctx context.Context, t *testing.T, u *user) *mtproto.TLUpdatesState {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetState{Constructor: u.c.Constructor(mtproto.Predicate_updates_getState)})
	if err != nil {
		t.Fatalf("updates.getState: %v", err)
	}
	st, ok := o.(*mtproto.TLUpdatesState)
	if !ok {
		t.Fatalf("updates.getState returned %T", o)
	}
	return st
}

// differenceFrom returns the new messages of u's updates.getDifference from
// st, following slices, and the final state.
func differenceFrom(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState) ([]*mtproto.Message, *mtproto.Updates_State) {
	t.Helper()
	var (
		msgs      []*mtproto.Message
		pts, date = st.GetPts(), st.GetDate()
	)
	for range 100 {
		o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
			Constructor: u.c.Constructor(mtproto.Predicate_updates_getDifference),
			Pts:         pts,
			Date:        date,
			Qts:         st.GetQts(),
		})
		if err != nil {
			t.Fatalf("updates.getDifference: %v", err)
		}
		d := o.(interface {
			To_Updates_Difference() *mtproto.Updates_Difference
		}).To_Updates_Difference()
		msgs = append(msgs, d.GetNewMessages()...)
		switch d.GetPredicateName() {
		case mtproto.Predicate_updates_differenceSlice:
			pts, date = d.GetIntermediateState().GetPts(), d.GetIntermediateState().GetDate()
		case mtproto.Predicate_updates_difference:
			return msgs, d.GetState()
		default:
			return msgs, &mtproto.Updates_State{Pts: pts, Date: date}
		}
	}
	t.Fatal("updates.getDifference: too many slices")
	return nil, nil
}

// incomingTagged keeps the incoming messages with text tag.
func incomingTagged(msgs []*mtproto.Message, tag string) []*mtproto.Message {
	var out []*mtproto.Message
	for _, m := range msgs {
		if !m.GetOut() && m.GetMessage() == tag {
			out = append(out, m)
		}
	}
	return out
}

// waitDifference polls u's difference from st until it holds n incoming
// messages tagged tag (delivery to the recipient's box is asynchronous).
func waitDifference(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState, tag string, n int) ([]*mtproto.Message, *mtproto.Updates_State) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		msgs, state := differenceFrom(ctx, t, u, st)
		if got := incomingTagged(msgs, tag); len(got) >= n || time.Now().After(deadline) {
			return msgs, state
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// checkDifference checks that the difference holds the n messages tagged
// tag with ascending ids, and that its state covers exactly its updates:
// every pts between the two states belongs to a message of the difference.
func checkDifference(t *testing.T, st *mtproto.TLUpdatesState, msgs []*mtproto.Message, state *mtproto.Updates_State, tag string, n int) {
	t.Helper()
	got := incomingTagged(msgs, tag)
	if len(got) != n {
		t.Fatalf("updates.getDifference has %d of the %d messages", len(got), n)
	}
	if !slices.IsSortedFunc(got, func(a, b *mtproto.Message) int { return int(a.GetId() - b.GetId()) }) {
		t.Errorf("message ids not ascending")
	}
	if want := st.GetPts() + int32(len(msgs)); state.GetPts() != want {
		t.Errorf("state pts %d after %d new messages from pts %d, want %d", state.GetPts(), len(msgs), st.GetPts(), want)
	}
}

// sendTo sends text from u to peer and returns the pts of the reply.
func sendTo(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	up, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("messages.sendMessage returned %T", o)
	}
	for _, u := range up.To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			return u.GetPts_INT32()
		}
	}
	t.Fatalf("messages.sendMessage reply without updateNewMessage: %s", up.To_Updates().GetPredicateName())
	return 0
}

// TestDifferenceWithoutPushes: Bob does not listen to pushes; the 20
// messages Alice sends are all in his updates.getDifference, in order.
func TestDifferenceWithoutPushes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	st := getState(ctx, t, bob)
	peer := peerOf(ctx, t, alice, bob)

	tag := fmt.Sprintf("diff %d", randomID())
	for range 20 {
		sendTo(ctx, t, alice, peer, tag)
	}
	msgs, state := waitDifference(ctx, t, bob, st, tag, 20)
	checkDifference(t, st, msgs, state, tag, 20)
}

// TestRedisFlushAll: losing Redis between two batches changes nothing for
// pts: Alice's replies keep growing, and Bob's difference has both batches.
func TestRedisFlushAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	st := getState(ctx, t, bob)
	peer := peerOf(ctx, t, alice, bob)

	tag := fmt.Sprintf("flush %d", randomID())
	var replies []int32
	for range 5 {
		replies = append(replies, sendTo(ctx, t, alice, peer, tag))
	}
	if out, err := exec.Command(runScript, "compose", "exec", "-T", "redis", "redis-cli", "FLUSHALL").CombinedOutput(); err != nil {
		t.Fatalf("FLUSHALL: %v\n%s", err, out)
	}
	for range 5 {
		replies = append(replies, sendTo(ctx, t, alice, peer, tag))
	}
	if !slices.IsSorted(replies) || len(slices.Compact(slices.Clone(replies))) != len(replies) {
		t.Errorf("pts of Alice's replies %v: not strictly growing", replies)
	}
	msgs, state := waitDifference(ctx, t, bob, st, tag, 10)
	checkDifference(t, st, msgs, state, tag, 10)
}

// historyCount returns the number of messages of u's history with peer.
func historyCount(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	m, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getHistory returned %T", o)
	}
	return len(m.To_Messages_Messages().GetMessages())
}

// TestDeleteHistoryInPortions: deleting a history of 1200 messages for
// both takes calls until offset 0; then both histories are empty.
func TestDeleteHistoryInPortions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	peer := peerOf(ctx, t, alice, bob)
	alicePeer := peerOf(ctx, t, bob, alice)

	tag := fmt.Sprintf("history %d", randomID())
	for range 1200 {
		sendTo(ctx, t, alice, peer, tag)
	}
	var offsets []int32
	for len(offsets) < 10 {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteHistory{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteHistory),
			Revoke:      true,
			Peer:        peer,
		})
		if err != nil {
			t.Fatalf("messages.deleteHistory: %v", err)
		}
		h, ok := o.(interface {
			To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
		})
		if !ok {
			t.Fatalf("messages.deleteHistory returned %T", o)
		}
		offsets = append(offsets, h.To_Messages_AffectedHistory().GetOffset())
		if offsets[len(offsets)-1] == 0 {
			break
		}
	}
	if len(offsets) != 2 || offsets[0] <= 0 || offsets[1] != 0 {
		t.Errorf("offsets %v, want one portion left after the first call", offsets)
	}
	if n := historyCount(ctx, t, alice, peer); n != 0 {
		t.Errorf("Alice's history keeps %d messages", n)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		n := historyCount(ctx, t, bob, alicePeer)
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Bob's history keeps %d messages", n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
