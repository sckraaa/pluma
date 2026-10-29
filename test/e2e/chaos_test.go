// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Chaos tests of delivery (ADR 0012, p. 12): a service or the broker goes
// away in the middle of traffic, and afterwards every message is in the
// recipient's box exactly once and in order, pushed or through
// updates.getDifference. Each test brings the stack back before it ends.

// stack runs a docker compose command on the test project.
func stack(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command(runScript, append([]string{"compose"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("compose %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// restore starts what a test stopped and waits until the stack serves. A
// container that was stopped or recreated gets a new ephemeral host port:
// new clients use the current one of the gateway, the push tests that of
// fakepush.
func restore(t *testing.T) {
	t.Helper()
	out, err := exec.Command(runScript, "wait").CombinedOutput()
	if err != nil {
		t.Fatalf("restoring the stack: %v\n%s", err, out)
	}
	out, err = exec.Command(runScript, "compose", "port", "gnetway", "10443").Output()
	if err != nil {
		t.Fatalf("gateway port: %v", err)
	}
	if addr := strings.TrimSpace(string(out)); addr != "" {
		gatewayAddr = addr
	}
	if fakePushAddr != "" {
		out, err = exec.Command(runScript, "compose", "port", "fakepush", "8080").Output()
		if err != nil {
			t.Fatalf("fakepush port: %v", err)
		}
		if addr := strings.TrimSpace(string(out)); addr != "" {
			fakePushAddr = addr
		}
	}
}

// sendOnce sends text with a fixed random_id until the server answers: a
// retry after a lost reply must not create a second message.
func sendOnce(ctx context.Context, u *user, peer *mtproto.InputPeer, text string, rid int64) error {
	for {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := u.c.Invoke(cctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer,
			Message:     text,
			RandomId:    rid,
		})
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w (last error: %v)", text, ctx.Err(), err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// burst sends n messages "<tag> <i>" from u in order, retrying each with
// its own random_id.
func burst(ctx context.Context, u *user, peer *mtproto.InputPeer, tag string, from, n int) error {
	for i := from; i < from+n; i++ {
		if err := sendOnce(ctx, u, peer, fmt.Sprintf("%s %d", tag, i), randomID()); err != nil {
			return err
		}
	}
	return nil
}

// checkExactlyOnce checks that msgs hold every "<tag> <i>" for i < n once,
// with ascending ids.
func checkExactlyOnce(t *testing.T, msgs []*mtproto.Message, tag string, n int) {
	t.Helper()
	seen := map[string]int{}
	var ids []int32
	for _, m := range msgs {
		if strings.HasPrefix(m.GetMessage(), tag+" ") {
			seen[m.GetMessage()]++
			ids = append(ids, m.GetId())
		}
	}
	for i := range n {
		if c := seen[fmt.Sprintf("%s %d", tag, i)]; c != 1 {
			t.Errorf("%q delivered %d times, want once", fmt.Sprintf("%s %d", tag, i), c)
		}
	}
	if len(ids) != n {
		t.Errorf("%d tagged messages, want %d", len(ids), n)
	}
	if !slices.IsSorted(ids) {
		t.Errorf("message ids not ascending: %v", ids)
	}
}

// incomingPrefixed keeps the incoming messages whose text starts with
// "<tag> ".
func incomingPrefixed(msgs []*mtproto.Message, tag string) []*mtproto.Message {
	var out []*mtproto.Message
	for _, m := range msgs {
		if !m.GetOut() && strings.HasPrefix(m.GetMessage(), tag+" ") {
			out = append(out, m)
		}
	}
	return out
}

// allDelivered waits until u's difference from st has n tagged messages.
func allDelivered(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState, tag string, n int) []*mtproto.Message {
	t.Helper()
	for {
		msgs, _ := differenceFrom(ctx, t, u, st)
		if len(incomingPrefixed(msgs, tag)) >= n || ctx.Err() != nil {
			return msgs
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func twoUsers(ctx context.Context, t *testing.T) (alice, bob *user, peer *mtproto.InputPeer) {
	t.Helper()
	phones := testPhones(t, 2)
	alice = login(ctx, t, phones[0], "Alice")
	bob = login(ctx, t, phones[1], "Bob")
	return alice, bob, peerOf(ctx, t, alice, bob)
}

// waitServes retries a cheap call until u's device is served again.
func waitServes(ctx context.Context, t *testing.T, u *user) {
	t.Helper()
	for {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := u.c.Invoke(cctx, &mtproto.TLUpdatesGetState{Constructor: u.c.Constructor(mtproto.Predicate_updates_getState)})
		cancel()
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("device not served again: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// pushedTagged waits until n tagged messages arrived pushed to u, and returns
// the pts of the pushes in arrival order.
func pushedTagged(ctx context.Context, t *testing.T, u *user, tag string, n int) []int32 {
	t.Helper()
	var pts []int32
	got := 0
	for got < n {
		up, err := u.c.WaitUpdate(ctx, func(up *mtproto.Updates) bool {
			for _, m := range newMessages(up) {
				if strings.HasPrefix(m.GetMessage(), tag+" ") {
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("%d of %d messages pushed: %v", got, n, err)
		}
		for _, m := range newMessages(up) {
			if strings.HasPrefix(m.GetMessage(), tag+" ") {
				got++
			}
		}
		switch up.GetPredicateName() {
		case mtproto.Predicate_updateShortMessage:
			pts = append(pts, up.GetPts())
		case mtproto.Predicate_updateShort:
			pts = append(pts, up.GetUpdate().GetPts_INT32())
		default:
			for _, x := range up.GetUpdates() {
				if x.GetPts_INT32() > 0 {
					pts = append(pts, x.GetPts_INT32())
				}
			}
		}
	}
	return pts
}

// TestPushesResumeAfterSessionRestart: after session restarts, pushes reach
// the devices again without restarting sync.
func TestPushesResumeAfterSessionRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	alice, bob, peer := twoUsers(ctx, t)
	t.Cleanup(func() { restore(t) })

	stack(t, "restart", "session")
	restore(t)
	waitServes(ctx, t, alice)
	waitServes(ctx, t, bob)

	tag := fmt.Sprintf("after session restart %d", randomID())
	if err := burst(ctx, alice, peer, tag, 0, 5); err != nil {
		t.Fatal(err)
	}
	pctx, pcancel := context.WithTimeout(ctx, 30*time.Second)
	defer pcancel()
	pts := pushedTagged(pctx, t, bob, tag, 5)
	if !slices.IsSorted(pts) {
		t.Errorf("pushed pts out of order: %v", pts)
	}
}

// TestDeliveryAcrossKafkaOutage: sends succeed while Kafka is down; after it
// comes back every message reaches the recipient once and in order.
func TestDeliveryAcrossKafkaOutage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	alice, bob, peer := twoUsers(ctx, t)
	st := getState(ctx, t, bob)
	t.Cleanup(func() { restore(t) })

	tag := fmt.Sprintf("kafka outage %d", randomID())
	stack(t, "stop", "kafka")
	if err := burst(ctx, alice, peer, tag, 0, 10); err != nil {
		t.Fatalf("send while Kafka is down: %v", err)
	}
	stack(t, "start", "kafka")
	restore(t)

	dctx, dcancel := context.WithTimeout(ctx, 2*time.Minute)
	defer dcancel()
	checkExactlyOnce(t, incomingPrefixed(allDelivered(dctx, t, bob, st, tag, 10), tag), tag, 10)
}

// TestDeliveryAcrossMsgKill: msg is killed (SIGKILL) in the middle of a
// burst. Clients retry with the same random_id; afterwards every message is
// in the recipient's box exactly once.
func TestDeliveryAcrossMsgKill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	alice, bob, peer := twoUsers(ctx, t)
	st := getState(ctx, t, bob)
	t.Cleanup(func() { restore(t) })

	tag := fmt.Sprintf("msg kill %d", randomID())
	const n = 40
	errc := make(chan error, 1)
	go func() { errc <- burst(ctx, alice, peer, tag, 0, n) }()
	time.Sleep(300 * time.Millisecond)
	stack(t, "kill", "msg")
	stack(t, "start", "msg")
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	restore(t)

	dctx, dcancel := context.WithTimeout(ctx, 2*time.Minute)
	defer dcancel()
	checkExactlyOnce(t, incomingPrefixed(allDelivered(dctx, t, bob, st, tag, n), tag), tag, n)
}

// TestDeliveryAcrossSyncRestart: sync restarts during a burst; the recipient
// has every message once (pushed or caught up with getDifference).
func TestDeliveryAcrossSyncRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	alice, bob, peer := twoUsers(ctx, t)
	st := getState(ctx, t, bob)
	t.Cleanup(func() { restore(t) })

	tag := fmt.Sprintf("sync restart %d", randomID())
	const n = 30
	errc := make(chan error, 1)
	go func() { errc <- burst(ctx, alice, peer, tag, 0, n) }()
	time.Sleep(200 * time.Millisecond)
	stack(t, "restart", "sync")
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	restore(t)

	dctx, dcancel := context.WithTimeout(ctx, 2*time.Minute)
	defer dcancel()
	checkExactlyOnce(t, incomingPrefixed(allDelivered(dctx, t, bob, st, tag, n), tag), tag, n)
}

// TestOrderManySenders: 1000 messages to one user from three senders at once
// arrive pushed in strictly increasing pts, and getDifference has all of
// them in order.
func TestOrderManySenders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	bob := login(ctx, t, phones[0], "Bob")
	st := getState(ctx, t, bob)
	senders := []*user{
		login(ctx, t, phones[1], "Alice"),
		login(ctx, t, phones[2], "Carol"),
		login(ctx, t, phones[3], "Dave"),
	}

	tag := fmt.Sprintf("order %d", randomID())
	const total = 1000
	per := []int{334, 333, 333}
	var wg sync.WaitGroup
	errs := make([]error, len(senders))
	from := 0
	for i, s := range senders {
		peer := peerOf(ctx, t, s, bob)
		wg.Add(1)
		go func(i, from int) {
			defer wg.Done()
			errs[i] = burst(ctx, s, peer, tag, from, per[i])
		}(i, from)
		from += per[i]
	}
	pts := pushedTagged(ctx, t, bob, tag, total)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 1; i < len(pts); i++ {
		if pts[i] <= pts[i-1] {
			t.Fatalf("push %d has pts %d after %d: not strictly increasing", i, pts[i], pts[i-1])
		}
	}
	msgs, _ := differenceFrom(ctx, t, bob, st)
	checkExactlyOnce(t, incomingPrefixed(msgs, tag), tag, total)
	if d := bob.c.Duplicates(); d != 0 {
		t.Errorf("Bob got %d duplicate messages from the server", d)
	}
}
