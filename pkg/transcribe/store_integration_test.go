//go:build integration

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

package transcribe

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const (
	itTopic = "Sync-T"
	modelA  = "model-a"
	modelB  = "model-b"
)

var (
	itMySQL *testinfra.MySQL
	itDB    *sql.DB
	// itTTL is FailedTTL of the tests.
	itTTL = 10 * time.Minute
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if itDB, err = userbox.Open(itMySQL.DSN()); err == nil {
		err = outbox.Register(outbox.Topics{Push: []string{itTopic}})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// clock is the time of a store under test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// itStore returns a store over empty tables, with a clock of its own.
func itStore(t *testing.T) (*Store, *clock) {
	t.Helper()
	for _, table := range []string{"transcription_jobs", "transcription_requests", "mq_outbox"} {
		if _, err := itDB.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	c := &clock{t: time.UnixMilli(1_800_000_000_000)}
	s := NewStore(itDB)
	s.now = c.now
	return s, c
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := itDB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func message(user int64, msg int32) Message {
	return Message{UserId: user, PeerType: 2, PeerId: 777, MsgId: msg}
}

func record(doc int64) Record {
	return Record{DocumentId: doc, AccessHash: doc * 3, Duration: 12, FileSize: 4096}
}

// lease is the lease of a claim in the tests.
const lease = time.Minute

// ask is one messages.transcribeAudio as far as the store goes.
func ask(t *testing.T, s *Store, m Message, doc int64, model string, failedTTL time.Duration) (int64, *Job, error) {
	t.Helper()
	ctx := context.Background()
	id, err := s.Request(ctx, m, doc, model)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Enqueue(ctx, record(doc), model, failedTTL)
	return id, j, err
}

// claim is a claim of a worker of modelA.
func claim(t *testing.T, s *Store) *Job {
	t.Helper()
	return claimOf(t, s, modelA)
}

func claimOf(t *testing.T, s *Store, model string) *Job {
	t.Helper()
	j, err := s.Claim(context.Background(), model, lease)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// must checks that an attempt wrote its end: must(t)(s.Complete(...)).
func must(t *testing.T) func(held bool, err error) {
	return func(held bool, err error) {
		t.Helper()
		if err != nil || !held {
			t.Fatalf("held %v, err %v", held, err)
		}
	}
}

func TestMigrationRunsTwice(t *testing.T) {
	path, err := testinfra.SchemaFile("migrate-20261115-voice-transcription.sql")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := itStore(t)
	if _, _, err = ask(t, s, message(1, 1), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = itMySQL.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("run %d: %v", i+2, err)
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_jobs"); n != 1 {
		t.Fatalf("%d jobs after the migration ran again", n)
	}
}

func TestRequestIdIsTheReadersAndTheMessages(t *testing.T) {
	s, _ := itStore(t)
	ctx := context.Background()
	first, err := s.Request(ctx, message(1, 5), 10, modelA)
	if err != nil || first == 0 {
		t.Fatalf("id %d, err %v", first, err)
	}
	// Asked again: the same id.
	if again, _ := s.Request(ctx, message(1, 5), 10, modelA); again != first {
		t.Errorf("the second request got %d, the first %d", again, first)
	}
	// The same record in another message of the reader (a forward), and in
	// the copy of another reader: ids of their own.
	forward, _ := s.Request(ctx, message(1, 6), 10, modelA)
	other, _ := s.Request(ctx, message(2, 5), 10, modelA)
	if forward == first || other == first || forward == other || forward == 0 || other == 0 {
		t.Errorf("ids %d, %d, %d", first, forward, other)
	}
}

func TestRequestForgetsWhatWasForAnotherRecord(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	m := message(1, 5)
	id, _, err := ask(t, s, m, 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Await(ctx, id, c.now()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.Rate(ctx, m, id, true); err != nil || !ok {
		t.Fatalf("rate: ok %v, err %v", ok, err)
	}
	// The model changed: the id stays, the rating and the update owed were
	// for the old text.
	again, err := s.Request(ctx, m, 10, modelB)
	if err != nil || again != id {
		t.Fatalf("id %d, want %d (%v)", again, id, err)
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_requests WHERE id = ? AND model = ? AND delivery = 0 AND rating IS NULL", id, modelB); n != 1 {
		t.Error("the request kept the rating or the update of the old model")
	}
}

func TestTwoRequestsAtOnceMakeOneJob(t *testing.T) {
	s, _ := itStore(t)
	const readers = 8
	var (
		wg   sync.WaitGroup
		ids  = make([]int64, readers)
		errs = make([]error, readers)
	)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var j *Job
			ids[i], j, errs[i] = ask(t, s, message(int64(100+i), 5), 10, modelA, itTTL)
			if errs[i] == nil && j.State != StateQueued {
				errs[i] = fmt.Errorf("state %d", j.State)
			}
		}()
	}
	wg.Wait()
	seen := map[int64]bool{}
	for i := range ids {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[ids[i]] {
			t.Errorf("id %d given twice", ids[i])
		}
		seen[ids[i]] = true
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_jobs"); n != 1 {
		t.Fatalf("%d jobs for one record", n)
	}
}

func TestOneWorkerGetsAJob(t *testing.T) {
	s, _ := itStore(t)
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	const workers = 6
	var (
		wg  sync.WaitGroup
		got = make([]*Job, workers)
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = s.Claim(context.Background(), modelA, lease)
		}()
	}
	wg.Wait()
	n := 0
	for _, j := range got {
		if j != nil {
			n++
			if j.State != StateRunning || j.Attempts != 1 || j.DocumentId != 10 || j.AccessHash != 30 || j.FileSize != 4096 || j.Duration != 12 {
				t.Errorf("claimed %+v", j)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d workers got the job", n)
	}
}

func TestExpiredLeaseIsTakenOver(t *testing.T) {
	s, c := itStore(t)
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	dead := claim(t, s)
	if dead == nil {
		t.Fatal("no job")
	}
	c.add(30 * time.Second)
	if j := claim(t, s); j != nil {
		t.Fatal("a job was taken from an attempt that still holds it")
	}
	c.add(31 * time.Second) // the lease of a minute is over
	next := claim(t, s)
	if next == nil || next.Attempts != 2 {
		t.Fatalf("after the lease: %+v", next)
	}
	// The first attempt comes back late: it may not write.
	if held, err := s.Complete(context.Background(), dead, "the late text"); err != nil || held {
		t.Fatalf("the attempt that lost its lease wrote: held %v, err %v", held, err)
	}
	must(t)(s.Complete(context.Background(), next, "the text"))
	j, err := s.Job(context.Background(), 10, modelA)
	if err != nil || j.State != StateDone || j.Text != "the text" {
		t.Fatalf("job %+v, err %v", j, err)
	}
}

func TestRetriesAndFailures(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	j := claim(t, s)
	// The engine was busy: back in the queue, the attempt does not count.
	must(t)(s.Retry(ctx, j, 2*time.Second, false))
	if claim(t, s) != nil {
		t.Fatal("claimed before its time")
	}
	c.add(2 * time.Second)
	if j = claim(t, s); j == nil || j.Attempts != 1 {
		t.Fatalf("after a busy engine: %+v", j)
	}
	// A real failure: counted, tried again after the pause.
	must(t)(s.Retry(ctx, j, 5*time.Second, true))
	c.add(5 * time.Second)
	if j = claim(t, s); j == nil || j.Attempts != 2 {
		t.Fatalf("after a failure: %+v", j)
	}
	must(t)(s.Fail(ctx, j, false))

	// A failure that may pass is queued again by the next request, which
	// is told "pending", not the failure; it is tried when FailedTTL has
	// passed since it failed.
	_, j, err := ask(t, s, message(2, 5), 10, modelA, itTTL)
	if err != nil || j.State != StateQueued || j.Attempts != 0 {
		t.Fatalf("within the TTL: %+v, %v", j, err)
	}
	c.add(itTTL - time.Second)
	if claim(t, s) != nil {
		t.Fatal("a failed record was tried again before FailedTTL")
	}
	// Asking again meanwhile changes nothing.
	if _, again, err := ask(t, s, message(3, 5), 10, modelA, itTTL); err != nil || again.State != StateQueued {
		t.Fatalf("joining the queued record: %+v, %v", again, err)
	}
	c.add(time.Second)
	if j = claim(t, s); j == nil || j.Attempts != 1 {
		t.Fatalf("after the TTL: %+v", j)
	}

	// A record that cannot be read is never tried again.
	must(t)(s.Fail(ctx, j, true))
	c.add(24 * time.Hour)
	if _, j, err := ask(t, s, message(3, 5), 10, modelA, itTTL); err != nil || j.State != StateFailed || !j.Permanent {
		t.Fatalf("a permanent failure a day later: %+v, %v", j, err)
	}
	if claim(t, s) != nil {
		t.Fatal("a permanently failed job was claimed")
	}
}

// Nothing is refused for volume (the owner's decision of 2026-11-12): one
// user may queue any number of records, of any length, and they are served
// in the order they were asked for; a text that is there is served at once.
func TestNoLimitsAndTheOrderOfTheQueue(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	const records = 200
	for doc := int64(1); doc <= records; doc++ {
		rec := record(doc)
		rec.Duration, rec.FileSize = 4*3600, 400<<20 // four hours, as a client may declare
		if _, err := s.Request(ctx, message(1, int32(doc)), doc, modelA); err != nil {
			t.Fatal(err)
		}
		j, err := s.Enqueue(ctx, rec, modelA, itTTL)
		if err != nil || j.State != StateQueued {
			t.Fatalf("record %d: %+v, %v", doc, j, err)
		}
		c.add(time.Millisecond)
	}
	if depth, _, err := s.Queue(ctx); err != nil || depth != records {
		t.Fatalf("depth %d, err %v", depth, err)
	}
	for doc := int64(1); doc <= 3; doc++ {
		j := claim(t, s)
		if j == nil || j.DocumentId != doc {
			t.Fatalf("claim %d: %+v", doc, j)
		}
		must(t)(s.Complete(ctx, j, "ready"))
	}
	// Another user asks for a record that is done: it is there at once.
	if _, j, err := ask(t, s, message(2, 1), 1, modelA, itTTL); err != nil || j.State != StateDone {
		t.Fatalf("a ready text: %+v, %v", j, err)
	}
}

func TestAnotherModelTranscribesAgain(t *testing.T) {
	s, _ := itStore(t)
	ctx := context.Background()
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	must(t)(s.Complete(ctx, claim(t, s), "the text of model A"))

	_, j, err := ask(t, s, message(1, 5), 10, modelB, itTTL)
	if err != nil || j.State != StateQueued || j.Model != modelB || j.Text != "" {
		t.Fatalf("under the new model: %+v, %v", j, err)
	}
	// A worker of model A does not take it.
	if claim(t, s) != nil {
		t.Fatal("a worker took a job of another model")
	}
	must(t)(s.Complete(ctx, claimOf(t, s, modelB), "the text of model B"))
	// Back to the old name: its text is still there.
	if _, j, err = ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil || j.State != StateDone || j.Text != "the text of model A" {
		t.Fatalf("back under the old model: %+v, %v", j, err)
	}
}

func event(user int64) userbox.Event {
	return userbox.Event{Topic: itTopic, Recipient: user, Method: "sync.TLSyncPushUpdates", Data: []byte(`{}`)}
}

func TestFinalUpdateIsOwedOnce(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	id, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	// A reader who got the text in the answer is owed nothing.
	quick, _, _ := ask(t, s, message(2, 5), 10, modelA, itTTL)
	j := claim(t, s)
	if err = s.Await(ctx, id, c.now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	owed := func() []Owed {
		t.Helper()
		o, err := s.Owed(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	if len(owed()) != 0 {
		t.Fatal("an update is owed while the job runs")
	}
	must(t)(s.Complete(ctx, j, "the text"))
	if len(owed()) != 0 {
		t.Fatal("the update may go before the answer had its second")
	}
	c.add(time.Second)
	o := owed()
	if len(o) != 1 || o[0].RequestId != id || o[0].Message != message(1, 5) || o[0].Text != "the text" {
		t.Fatalf("owed %+v (the request answered at once is %d)", o, quick)
	}

	// An event that cannot be written leaves the update owed.
	if sent, err := s.Deliver(ctx, id, userbox.Event{Topic: "no-such-topic", Recipient: 1, Method: "m"}); err == nil || sent {
		t.Fatalf("sent %v, err %v", sent, err)
	}
	if len(owed()) != 1 || count(t, "SELECT COUNT(*) FROM mq_outbox") != 0 {
		t.Fatal("a failed delivery changed something")
	}
	// Two sweeps at once: one update.
	var (
		wg   sync.WaitGroup
		sent [4]bool
	)
	for i := range sent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sent[i], _ = s.Deliver(ctx, id, event(1))
		}()
	}
	wg.Wait()
	n := 0
	for _, ok := range sent {
		if ok {
			n++
		}
	}
	if n != 1 || count(t, "SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = 1") != 1 {
		t.Fatalf("%d deliveries, %d rows of the outbox", n, count(t, "SELECT COUNT(*) FROM mq_outbox"))
	}
	if len(owed()) != 0 {
		t.Fatal("still owed after it was sent")
	}
}

func TestFailedJobOwesAnEmptyUpdate(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	id, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	j := claim(t, s)
	if err = s.Await(ctx, id, c.now()); err != nil {
		t.Fatal(err)
	}
	must(t)(s.Fail(ctx, j, false))
	o, err := s.Owed(ctx, 10)
	if err != nil || len(o) != 1 || o[0].RequestId != id || o[0].Text != "" {
		t.Fatalf("owed %+v, err %v", o, err)
	}
}

func TestUnawait(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	id, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Await(ctx, id, c.now()); err != nil {
		t.Fatal(err)
	}
	if owed, err := s.Unawait(ctx, id); err != nil || !owed {
		t.Fatalf("owed %v, err %v", owed, err)
	}
	must(t)(s.Complete(ctx, claim(t, s), "the text"))
	if o, _ := s.Owed(ctx, 10); len(o) != 0 {
		t.Fatalf("owed %+v after the answer carried the text", o)
	}
	if owed, err := s.Unawait(ctx, id); err != nil || owed {
		t.Fatalf("a second time: owed %v, err %v", owed, err)
	}
}

func TestRate(t *testing.T) {
	s, _ := itStore(t)
	ctx := context.Background()
	m := message(1, 5)
	id, _, err := ask(t, s, m, 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	model, changed, ok, err := s.Rate(ctx, m, id, true)
	if err != nil || !ok || !changed || model != modelA {
		t.Fatalf("model %q, changed %v, ok %v, err %v", model, changed, ok, err)
	}
	// The same rating again is not a new one.
	if _, changed, ok, _ = s.Rate(ctx, m, id, true); !ok || changed {
		t.Errorf("again: changed %v, ok %v", changed, ok)
	}
	if _, changed, ok, _ = s.Rate(ctx, m, id, false); !ok || !changed {
		t.Errorf("changed mind: changed %v, ok %v", changed, ok)
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_requests WHERE id = ? AND rating = 0", id); n != 1 {
		t.Error("the rating is not stored")
	}
	// Somebody else's id, another message, an id that does not exist.
	for name, bad := range map[string]struct {
		m  Message
		id int64
	}{
		"another reader":  {message(2, 5), id},
		"another message": {message(1, 6), id},
		"no such id":      {m, id + 1000},
	} {
		if _, _, ok, err := s.Rate(ctx, bad.m, bad.id, true); err != nil || ok {
			t.Errorf("%s: ok %v, err %v", name, ok, err)
		}
	}
}

func TestQueueAge(t *testing.T) {
	s, c := itStore(t)
	if depth, oldest, err := s.Queue(context.Background()); err != nil || depth != 0 || oldest != 0 {
		t.Fatalf("an empty queue: %d, %s, %v", depth, oldest, err)
	}
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	c.add(90 * time.Second)
	if _, _, err := ask(t, s, message(1, 6), 11, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	if depth, oldest, err := s.Queue(context.Background()); err != nil || depth != 2 || oldest != 90*time.Second {
		t.Fatalf("%d, %s, %v", depth, oldest, err)
	}
}

// A message whose media is edited again and again is one request with one
// id, and each record of it is a job of its own: the text of one record is
// never served for another, and the readers of the old record keep theirs.
func TestEditedMediaKeepsTheIdAndTheTexts(t *testing.T) {
	s, _ := itStore(t)
	ctx := context.Background()
	m := message(1, 5)
	first, _, err := ask(t, s, m, 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	must(t)(s.Complete(ctx, claim(t, s), "the first record"))
	// A forward of the first record keeps it; the message itself is edited.
	forward, _, err := ask(t, s, message(2, 9), 10, modelA, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	for i, doc := range []int64{11, 12, 13} {
		id, j, err := ask(t, s, m, doc, modelA, itTTL)
		if err != nil || id != first {
			t.Fatalf("edit %d: id %d, want %d (%v)", i+1, id, first, err)
		}
		if j.DocumentId != doc || j.State != StateQueued || j.Text != "" {
			t.Fatalf("edit %d: the job %+v", i+1, j)
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_requests WHERE id = ? AND document_id = 13", first); n != 1 {
		t.Fatal("the request does not name the record the message has now")
	}
	if _, j, err := ask(t, s, message(2, 9), 10, modelA, itTTL); err != nil || j.State != StateDone || j.Text != "the first record" {
		t.Fatalf("the forward of the first record: %+v, %v", j, err)
	}
	if again, _ := s.Request(ctx, message(2, 9), 10, modelA); again != forward || forward == first {
		t.Fatalf("ids %d, %d, %d", first, forward, again)
	}
	// Each record is transcribed for itself, in the order of the edits.
	for _, doc := range []int64{11, 12, 13} {
		j := claim(t, s)
		if j == nil || j.DocumentId != doc {
			t.Fatalf("claim of %d: %+v", doc, j)
		}
		must(t)(s.Complete(ctx, j, fmt.Sprintf("record %d", doc)))
	}
	if _, j, err := ask(t, s, m, 13, modelA, itTTL); err != nil || j.Text != "record 13" {
		t.Fatalf("the message as it is now: %+v, %v", j, err)
	}
}

func TestExtend(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	if _, _, err := ask(t, s, message(1, 5), 10, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	j := claim(t, s)
	// The record turned out long: the attempt holds the job for longer
	// than its claim gave it.
	if held, err := s.Extend(ctx, j, 10*time.Minute); err != nil || !held {
		t.Fatalf("extend: held %v, err %v", held, err)
	}
	c.add(lease + time.Second)
	if claim(t, s) != nil {
		t.Fatal("a job was taken from an attempt that extended its lease")
	}
	// Twice in the same millisecond is still a change.
	if held, err := s.Extend(ctx, j, 10*time.Minute); err != nil || !held {
		t.Fatalf("extend again: held %v, err %v", held, err)
	}
	if held, err := s.Extend(ctx, j, 10*time.Minute); err != nil || !held {
		t.Fatalf("extend in the same millisecond: held %v, err %v", held, err)
	}
	must(t)(s.Complete(ctx, j, "the text"))

	// An attempt that lost its lease cannot extend it.
	if _, _, err := ask(t, s, message(1, 6), 11, modelA, itTTL); err != nil {
		t.Fatal(err)
	}
	dead := claim(t, s)
	c.add(lease)
	next := claim(t, s)
	if dead == nil || next == nil {
		t.Fatalf("claims %+v, %+v", dead, next)
	}
	if held, err := s.Extend(ctx, dead, 10*time.Minute); err != nil || held {
		t.Fatalf("the attempt that lost its lease extended it: held %v, err %v", held, err)
	}
}

// After STT_MODEL changed, the readers who wait for a job of the old model
// get the text of the new one, never an empty update; a job of a model that
// another instance still runs is left to it while it is taken in time.
func TestRetireHandsTheReadersOver(t *testing.T) {
	s, c := itStore(t)
	ctx := context.Background()
	const grace = time.Minute
	waiting, _, err := ask(t, s, message(1, 5), 10, modelB, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Await(ctx, waiting, c.now()); err != nil {
		t.Fatal(err)
	}
	// A reader who was answered already is not moved.
	told, _, err := ask(t, s, message(2, 5), 10, modelB, itTTL)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.Retire(ctx, modelA, grace); err != nil || n != 0 {
		t.Fatalf("retired %d within the grace, err %v", n, err)
	}
	c.add(grace)
	if n, err := s.Retire(ctx, modelA, grace); err != nil || n != 1 {
		t.Fatalf("retired %d, err %v", n, err)
	}
	if j, _ := s.Job(ctx, 10, modelB); j == nil || j.State != StateFailed || j.Permanent {
		t.Fatalf("the old job %+v", j)
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_requests WHERE id = ? AND model = ? AND delivery = ?", waiting, modelA, deliveryAwaiting); n != 1 {
		t.Fatal("the waiting reader was not moved to the new model")
	}
	if n := count(t, "SELECT COUNT(*) FROM transcription_requests WHERE id = ? AND model = ?", told, modelB); n != 1 {
		t.Fatal("a reader who waits for nothing was moved")
	}
	// Nothing is owed until the new model has the text.
	if owed, err := s.Owed(ctx, 10); err != nil || len(owed) != 0 {
		t.Fatalf("owed %+v, err %v", owed, err)
	}
	j := claim(t, s)
	if j == nil || j.DocumentId != 10 || j.AccessHash != 30 || j.FileSize != 4096 {
		t.Fatalf("the job of the new model %+v", j)
	}
	must(t)(s.Complete(ctx, j, "the text of model A"))
	owed, err := s.Owed(ctx, 10)
	if err != nil || len(owed) != 1 || owed[0].RequestId != waiting || owed[0].Text != "the text of model A" {
		t.Fatalf("owed %+v, err %v", owed, err)
	}

	// A job of the other model that its instance runs, with a lease, stays.
	if _, _, err = ask(t, s, message(1, 6), 11, modelB, itTTL); err != nil {
		t.Fatal(err)
	}
	running := claimOf(t, s, modelB)
	c.add(grace)
	if n, err := s.Retire(ctx, modelA, grace); err != nil || n != 0 {
		t.Fatalf("retired %d running jobs, err %v", n, err)
	}
	must(t)(s.Complete(ctx, running, "the text of model B"))
}
