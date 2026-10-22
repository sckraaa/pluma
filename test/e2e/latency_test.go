//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Latency measurement of messages.sendMessage (task 002). It is not part of
// `make e2e`; run it on a started stack with
//
//	PLUMA_E2E_LATENCY=1 E2E_GOTEST_FLAGS='-run TestSendLatency' test/e2e/run.sh test
//
// PLUMA_E2E_LATENCY_USERS and PLUMA_E2E_LATENCY_SENDS change the number of
// users (at most 20: logins from one address are limited) and the messages
// each user sends in the parallel phase.

// peerOf returns the input peer of to for from, resolved by phone.
func peerOf(ctx context.Context, t *testing.T, from, to *user) *mtproto.InputPeer {
	t.Helper()
	o, err := from.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
		Constructor: from.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
		Phone:       strings.TrimPrefix(to.phone, "+"),
	})
	if err != nil {
		t.Fatalf("contacts.resolvePhone: %v", err)
	}
	resolved, ok := o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	})
	if !ok {
		t.Fatalf("contacts.resolvePhone returned %T", o)
	}
	for _, u := range resolved.To_Contacts_ResolvedPeer().GetUsers() {
		if u.GetId() == to.self.GetId() && u.GetAccessHash() != nil {
			return &mtproto.InputPeer{
				Constructor: from.c.Constructor(mtproto.Predicate_inputPeerUser),
				UserId:      u.GetId(),
				AccessHash:  u.GetAccessHash().GetValue(),
			}
		}
	}
	t.Fatalf("contacts.resolvePhone did not return user %d", to.self.GetId())
	return nil
}

// sendTimed sends text to peer and returns the time of the RPC.
func sendTimed(ctx context.Context, u *user, peer *mtproto.InputPeer, text string) (time.Duration, error) {
	start := time.Now()
	_, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		RandomId:    randomID(),
	})
	return time.Since(start), err
}

func envInt(name string, def, max int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return min(v, max)
	}
	return def
}

func percentiles(d []time.Duration) string {
	if len(d) == 0 {
		return "no samples"
	}
	s := slices.Clone(d)
	slices.Sort(s)
	p := func(q float64) time.Duration { return s[min(len(s)-1, int(q*float64(len(s))))] }
	return fmt.Sprintf("n=%d p50=%v p95=%v p99=%v max=%v",
		len(s), p(0.50).Round(10*time.Microsecond), p(0.95).Round(10*time.Microsecond),
		p(0.99).Round(10*time.Microsecond), s[len(s)-1].Round(10*time.Microsecond))
}

// TestSendLatency measures the time of messages.sendMessage: one user
// sending to another one message at a time, with the time until the
// recipient has the message pushed (delivery goes through Kafka and the
// recipient's box transaction after the sender's reply), then all users
// sending to random others at once. Afterwards every user's
// updates.getDifference must hold every message sent to it.
//
// One run on a developer machine is no measurement: the spread between runs
// of the same code is of the order of the 20% the task allows. Compare
// versions on the target host, alternating runs of each several times.
func TestSendLatency(t *testing.T) {
	if os.Getenv("PLUMA_E2E_LATENCY") == "" {
		t.Skip("set PLUMA_E2E_LATENCY=1 to measure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	n := envInt("PLUMA_E2E_LATENCY_USERS", 10, 20)
	sends := envInt("PLUMA_E2E_LATENCY_SENDS", 30, 1000)
	phones := testPhones(t, n)
	users := make([]*user, n)
	for i := range users {
		users[i] = login(ctx, t, phones[i], fmt.Sprintf("Load%d", i))
	}
	states := make([]*mtproto.TLUpdatesState, n)
	for i, u := range users {
		o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetState{Constructor: u.c.Constructor(mtproto.Predicate_updates_getState)})
		if err != nil {
			t.Fatalf("updates.getState: %v", err)
		}
		states[i] = o.(*mtproto.TLUpdatesState)
	}
	peers := make([][]*mtproto.InputPeer, n)
	for i := range users {
		peers[i] = make([]*mtproto.InputPeer, n)
		for j := range users {
			if i != j {
				peers[i][j] = peerOf(ctx, t, users[i], users[j])
			}
		}
	}
	tag := fmt.Sprintf("lat%d", randomID())
	received := make([]int, n)

	// Warm-up: the first message of each pair creates the dialogs.
	for i := range users {
		j := (i + 1) % n
		if _, err := sendTimed(ctx, users[i], peers[i][j], tag+"-w"); err != nil {
			t.Fatalf("messages.sendMessage: %v", err)
		}
		received[j]++
	}

	var seq, delivery []time.Duration
	for i := range sends {
		text := fmt.Sprintf("%s-s%d", tag, i)
		pushed := make(chan time.Time, 1)
		go func() {
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if _, err := users[1].c.WaitUpdate(wctx, func(u *mtproto.Updates) bool {
				for _, m := range newMessages(u) {
					if m.GetMessage() == text {
						return true
					}
				}
				return false
			}); err == nil {
				pushed <- time.Now()
			}
			close(pushed)
		}()
		start := time.Now()
		d, err := sendTimed(ctx, users[0], peers[0][1], text)
		if err != nil {
			t.Fatalf("messages.sendMessage: %v", err)
		}
		seq = append(seq, d)
		received[1]++
		at, ok := <-pushed
		if !ok {
			t.Fatalf("message %d not pushed to the recipient within 30s", i)
		}
		delivery = append(delivery, at.Sub(start))
	}
	t.Logf("sequential, one sender: %s", percentiles(seq))
	t.Logf("sequential, sent until pushed to the recipient: %s", percentiles(delivery))

	var (
		mu  sync.Mutex
		par []time.Duration
		wg  sync.WaitGroup
	)
	start := time.Now()
	for i := range users {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(i), uint64(time.Now().UnixNano())))
			for range sends {
				j := r.IntN(n - 1)
				if j >= i {
					j++
				}
				d, err := sendTimed(ctx, users[i], peers[i][j], tag+"-p")
				if err != nil {
					t.Errorf("messages.sendMessage: %v", err)
					return
				}
				mu.Lock()
				par = append(par, d)
				received[j]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	t.Logf("parallel, %d senders: %s, %.0f messages/s", n, percentiles(par), float64(len(par))/time.Since(start).Seconds())

	// Every message is in the recipient's difference. Delivery to the
	// recipients' boxes is asynchronous: wait for it.
	for i, u := range users {
		deadline := time.Now().Add(30 * time.Second)
		for {
			got, err := countIncoming(ctx, u, states[i], tag)
			if err != nil {
				t.Fatalf("updates.getDifference: %v", err)
			}
			if got == received[i] {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("user %d: %d messages in updates.getDifference, %d sent to it", i, got, received[i])
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// countIncoming counts the incoming messages of the run (their text starts
// with tag and a dash) in u's updates.getDifference from st, following
// slices.
func countIncoming(ctx context.Context, u *user, st *mtproto.TLUpdatesState, tag string) (int, error) {
	pts, date := st.GetPts(), st.GetDate()
	n := 0
	for {
		o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
			Constructor: u.c.Constructor(mtproto.Predicate_updates_getDifference),
			Pts:         pts,
			Date:        date,
			Qts:         st.GetQts(),
		})
		if err != nil {
			return 0, err
		}
		d := o.(interface {
			To_Updates_Difference() *mtproto.Updates_Difference
		}).To_Updates_Difference()
		for _, m := range d.GetNewMessages() {
			if !m.GetOut() && strings.HasPrefix(m.GetMessage(), tag+"-") {
				n++
			}
		}
		if d.GetPredicateName() != mtproto.Predicate_updates_differenceSlice {
			return n, nil
		}
		pts, date = d.GetIntermediateState().GetPts(), d.GetIntermediateState().GetDate()
	}
}
