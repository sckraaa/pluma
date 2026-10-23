//go:build integration

package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The relay on MySQL and Kafka (ADR 0012, pp. 3-4).

var (
	itDB    *testinfra.MySQL
	itKafka *testinfra.Kafka
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itKafka, errs[1] = testinfra.StartKafka(ctx) }()
	wg.Wait()
	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	_ = itDB.Terminate(ctx)
	_ = itKafka.Terminate(ctx)
	os.Exit(code)
}

var itUserSeq atomic.Int64

// itUser gives each test its own recipients: the tests share the table and
// the topics, and run in any order and again.
func itUser() int64 { return time.Now().UnixMilli()%1_000_000*1000 + itUserSeq.Add(1) }

func boxPool(t testing.TB, maxConns int) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", itDB.DSN()+"&innodb_lock_wait_timeout=2")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })
	if err = userbox.Check(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

// startRelay runs a relay of db until the test ends.
func startRelay(t testing.TB, db *sql.DB, wrap func(store) store) *Relay {
	t.Helper()
	var st store = newSQLStore(db)
	if wrap != nil {
		st = wrap(st)
	}
	r := newRelay(st, dialSarama, RelayConf{Brokers: itKafka.Brokers, Groups: []GroupConf{{Topic: "Inbox-T", Group: "it-inbox"}}}, "it-relay")
	r.Start()
	t.Cleanup(r.Stop)
	return r
}

// record is a message of a topic.
type record struct {
	topic     string
	partition int32
	offset    int64
	key       string
	eventId   int64
	initiator int64
	value     []byte
}

// topicReader reads every partition of topics from the oldest offset.
type topicReader struct {
	mu      sync.Mutex
	records []record
	stop    func()
}

func readTopics(t testing.TB, topics ...string) *topicReader {
	t.Helper()
	cfg := sarama.NewConfig()
	cfg.Version = KafkaVersion
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	c, err := sarama.NewConsumer(itKafka.Brokers, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tr := &topicReader{}
	var wg sync.WaitGroup
	var pcs []sarama.PartitionConsumer
	for _, topic := range topics {
		for p := range int32(Partitions) {
			pc, err := c.ConsumePartition(topic, p, sarama.OffsetOldest)
			if err != nil {
				t.Fatal(err)
			}
			pcs = append(pcs, pc)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for m := range pc.Messages() {
					r := record{topic: m.Topic, partition: m.Partition, offset: m.Offset, key: string(m.Key), value: m.Value}
					for _, h := range m.Headers {
						switch string(h.Key) {
						case HeaderEventId:
							r.eventId, _ = strconv.ParseInt(string(h.Value), 10, 64)
						case HeaderInitiator:
							r.initiator, _ = strconv.ParseInt(string(h.Value), 10, 64)
						}
					}
					tr.mu.Lock()
					tr.records = append(tr.records, r)
					tr.mu.Unlock()
				}
			}()
		}
	}
	tr.stop = func() {
		for _, pc := range pcs {
			pc.AsyncClose()
		}
		wg.Wait()
		_ = c.Close()
	}
	t.Cleanup(tr.stop)
	return tr
}

// of returns the records with key, in the order of their partition.
func (tr *topicReader) of(key string) []record {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var out []record
	for _, r := range tr.records {
		if r.key == key {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b record) int { return int(a.offset - b.offset) })
	return out
}

func eventually(t testing.TB, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v", what, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func countRows(t testing.TB, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// emit writes one event of initiator to recipient in a box transaction.
func emit(t testing.TB, db *sql.DB, initiator int64, ev userbox.Event) {
	t.Helper()
	if _, err := userbox.Run(context.Background(), db, initiator, func(b *userbox.Box) error {
		b.Emit(ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	Notify()
}

// The events of each initiator reach the topic in the order its
// transactions committed, also when a transaction with a smaller id commits
// after rows with greater ones are visible.
func TestRelayOrderPerInitiator(t *testing.T) {
	db := boxPool(t, 64)
	tr := readTopics(t, "Sync-T")
	startRelay(t, db, nil)
	recipient := itUser()
	initiators := []int64{itUser(), itUser()}
	const perGoroutine = 4

	// A row whose transaction commits late: its id is smaller than rows
	// committed meanwhile.
	late, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	res, err := late.Exec("INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, created_at) VALUES (?, 'Sync-T', ?, 'late', '{}', ?)",
		itUser(), recipient, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	lateId, _ := res.LastInsertId()

	var wg sync.WaitGroup
	for _, initiator := range initiators {
		for g := range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range perGoroutine {
					emit(t, db, initiator, userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: fmt.Sprintf("%d-%d", g, i), Data: []byte("{}")})
				}
			}()
		}
	}
	time.Sleep(300 * time.Millisecond)
	if err = late.Commit(); err != nil {
		t.Fatal(err)
	}
	Notify()
	wg.Wait()

	total := 2*32*perGoroutine + 1
	key := strconv.FormatInt(recipient, 10)
	eventually(t, 30*time.Second, "all events published", func() bool { return len(tr.of(key)) >= total })
	byInitiator := map[int64][]int64{}
	seen := map[int64]bool{}
	for _, r := range tr.of(key) {
		if seen[r.eventId] {
			t.Errorf("event %d published twice", r.eventId)
		}
		seen[r.eventId] = true
		byInitiator[r.initiator] = append(byInitiator[r.initiator], r.eventId)
	}
	for _, initiator := range initiators {
		ids := byInitiator[initiator]
		// Each initiator's transactions commit one at a time under its box
		// lock, so its ids are its commit order.
		if !slices.IsSorted(ids) || len(ids) != 32*perGoroutine {
			t.Errorf("initiator %d: %d events, in order %v", initiator, len(ids), slices.IsSorted(ids))
		}
	}
	if !seen[lateId] {
		t.Error("the row committed late was never published")
	}
	eventually(t, 10*time.Second, "push rows deleted", func() bool { return countRows(t, db, "recipient_id = ?", recipient) == 0 })
}

// failingFinish loses every record of the broker's answers: the relay
// stopped between the answers and the finisher.
type failingFinish struct {
	store
	calls atomic.Int64
}

func (f *failingFinish) Finish(context.Context, finish) error {
	f.calls.Add(1)
	return &mysql.MySQLError{Number: 2013, Message: "Lost connection"}
}

// The relay stops after the broker took the rows and before it recorded
// that: the rows are published again by the next relay, and nothing else
// happens than repeated copies.
func TestRelayCrashBeforeFinish(t *testing.T) {
	db := boxPool(t, 16)
	tr := readTopics(t, "Inbox-T")
	recipient := itUser()
	initiator := itUser()
	for i := range 5 {
		emit(t, db, initiator, userbox.Event{Topic: "Inbox-T", Recipient: recipient, Method: fmt.Sprint(i), Data: []byte("{}")})
	}
	key := strconv.FormatInt(recipient, 10)
	var ff *failingFinish
	r := startRelay(t, db, func(s store) store { ff = &failingFinish{store: s}; return ff })
	eventually(t, 20*time.Second, "first copies", func() bool { return len(tr.of(key)) >= 5 })
	r.Stop()
	if ff.calls.Load() == 0 {
		t.Fatal("the finisher never ran")
	}
	if n := countRows(t, db, "recipient_id = ? AND state = ?", recipient, userbox.EventPending); n != 5 {
		t.Fatalf("%d rows pending after the lost records, want 5", n)
	}

	startRelay(t, db, nil)
	eventually(t, 20*time.Second, "published", func() bool {
		return countRows(t, db, "recipient_id = ? AND state = ?", recipient, userbox.EventPublished) == 5
	})
	// The rows turn published on the broker's answer; the test's reader
	// may still be fetching the second copies.
	eventually(t, 20*time.Second, "second copies", func() bool { return len(tr.of(key)) >= 10 })
	ids := map[int64]int{}
	for _, r := range tr.of(key) {
		ids[r.eventId]++
	}
	if len(ids) != 5 {
		t.Errorf("copies of %d events, want 5", len(ids))
	}
	for id, n := range ids {
		if n < 2 {
			t.Errorf("event %d published %d times, want again after the crash", id, n)
		}
	}
}

// connectionId returns the id of the connection that holds the relay's lock.
func lockHolder(t testing.TB, db *sql.DB) int64 {
	t.Helper()
	var id sql.NullInt64
	if err := db.QueryRow("SELECT IS_USED_LOCK(" + lockName + ")").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id.Int64
}

// Two relays: one leads. Its connection killed, the other publishes within
// the standby's retry.
func TestRelayFailover(t *testing.T) {
	db := boxPool(t, 16)
	tr := readTopics(t, "Sync-T")
	a := startRelay(t, db, nil)
	eventually(t, 10*time.Second, "a leader", func() bool { return lockHolder(t, db) != 0 })
	b := startRelay(t, db, nil)
	time.Sleep(200 * time.Millisecond)
	holder := lockHolder(t, db)

	root, err := sql.Open("mysql", itDB.RootDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = root.Exec(fmt.Sprintf("KILL %d", holder)); err != nil {
		t.Fatal(err)
	}
	killed := time.Now()
	recipient := itUser()
	emit(t, db, itUser(), userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: "after", Data: []byte("{}")})
	key := strconv.FormatInt(recipient, 10)
	eventually(t, leaderRetry+5*time.Second, "published after the leader died", func() bool { return len(tr.of(key)) == 1 })
	t.Logf("published %v after the leader's connection was killed", time.Since(killed).Round(10*time.Millisecond))
	if h := lockHolder(t, db); h == holder || h == 0 {
		t.Errorf("lock holder %d", h)
	}
	_, _ = a, b
}

// Two databases of one MySQL server: a leader each.
func TestRelayTwoDatabases(t *testing.T) {
	root, err := sql.Open("mysql", itDB.RootDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("teamgram_%d", itUser())
	for _, q := range []string{
		"CREATE DATABASE " + name,
		"CREATE TABLE " + name + ".mq_outbox LIKE " + testinfra.MySQLDatabase + ".mq_outbox",
		"CREATE TABLE " + name + ".user_state LIKE " + testinfra.MySQLDatabase + ".user_state",
	} {
		if _, err = root.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _, _ = root.Exec("DROP DATABASE " + name) }()
	cfg, err := mysql.ParseDSN(itDB.RootDSN())
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	other, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	db := boxPool(t, 8)
	ctx := context.Background()
	s1, s2 := newSQLStore(db), newSQLStore(other)
	defer s1.Resign()
	defer s2.Resign()
	ok1, err1 := s1.Lead(ctx)
	ok2, err2 := s2.Lead(ctx)
	if err1 != nil || err2 != nil || !ok1 || !ok2 {
		t.Fatalf("leaders %v %v (%v, %v)", ok1, ok2, err1, err2)
	}
	if ok, _ := newSQLStore(db).Lead(ctx); ok {
		t.Error("two leaders of one database")
	}
}

// The broker frozen: the round ends at its limit, and publication goes on
// after, without a restart of the relay.
func TestRelayBrokerPaused(t *testing.T) {
	db := boxPool(t, 16)
	tr := readTopics(t, "Sync-T")
	startRelay(t, db, nil)
	warm := itUser()
	emit(t, db, itUser(), userbox.Event{Topic: "Sync-T", Recipient: warm, Method: "warm", Data: []byte("{}")})
	eventually(t, 20*time.Second, "warm-up", func() bool { return len(tr.of(strconv.FormatInt(warm, 10))) == 1 })

	ctx := context.Background()
	if err := testinfra.Pause(ctx, itKafka.Container); err != nil {
		t.Fatal(err)
	}
	paused := true
	defer func() {
		if paused {
			_ = testinfra.Unpause(ctx, itKafka.Container)
		}
	}()
	recipient := itUser()
	emit(t, db, itUser(), userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: "during", Data: []byte("{}")})
	time.Sleep(RoundTimeout + 3*time.Second)
	if err := testinfra.Unpause(ctx, itKafka.Container); err != nil {
		t.Fatal(err)
	}
	paused = false
	unpaused := time.Now()
	key := strconv.FormatInt(recipient, 10)
	eventually(t, 60*time.Second, "published after the pause", func() bool { return len(tr.of(key)) >= 1 })
	t.Logf("published %v after the broker came back", time.Since(unpaused).Round(10*time.Millisecond))
	eventually(t, 10*time.Second, "row deleted", func() bool { return countRows(t, db, "recipient_id = ?", recipient) == 0 })
}

// The broker stopped and started again: the rows written meanwhile wait and
// are published once it is back.
func TestRelayKafkaRestart(t *testing.T) {
	db := boxPool(t, 16)
	startRelay(t, db, nil)
	ctx := context.Background()
	stop := 10 * time.Second
	if err := itKafka.Container.Stop(ctx, &stop); err != nil {
		t.Fatal(err)
	}
	recipient := itUser()
	for i := range 20 {
		emit(t, db, itUser(), userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: fmt.Sprint(i), Data: []byte("{}")})
	}
	time.Sleep(5 * time.Second)
	if n := countRows(t, db, "recipient_id = ?", recipient); n != 20 {
		t.Fatalf("%d rows wait, want 20", n)
	}
	if err := itKafka.Container.Start(ctx); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	eventually(t, 90*time.Second, "backlog published", func() bool { return countRows(t, db, "recipient_id = ?", recipient) == 0 })
	t.Logf("backlog of 20 published %v after the broker started", time.Since(started).Round(100*time.Millisecond))
	tr := readTopics(t, "Sync-T")
	eventually(t, 30*time.Second, "copies in the topic", func() bool { return len(tr.of(strconv.FormatInt(recipient, 10))) == 20 })
}

// A row the broker refuses for good among ordinary ones: the others are
// delivered without delay; an inbox row goes dead and a push row is
// dropped, and neither is tried again.
func TestRelayRefusedRows(t *testing.T) {
	ctx := context.Background()
	db := boxPool(t, 16)
	const smallInbox = "Small-Inbox-T"
	if err := Register(Topics{Inbox: []string{smallInbox}}); err != nil {
		t.Fatal(err)
	}
	if err := itKafka.Topics(ctx, "--create", "--if-not-exists", "--topic", smallInbox, "--partitions", strconv.Itoa(Partitions),
		"--replication-factor", "1", "--config", "max.message.bytes=200"); err != nil {
		t.Fatal(err)
	}
	tr := readTopics(t, "Sync-T", smallInbox)
	startRelay(t, db, nil)
	recipient := itUser()

	// A push over the client's limit, refused before it reaches the broker.
	huge := bytes.Repeat([]byte("x"), MaxMessageBytes+1)
	res, err := db.Exec("INSERT INTO mq_outbox (initiator_id, topic, recipient_id, method, payload, created_at) VALUES (0, 'Sync-T', ?, 'huge', ?, ?)",
		recipient, huge, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	hugeId, _ := res.LastInsertId()
	// An inbox copy the topic refuses (its headers are over 200 bytes).
	if _, err = Insert(ctx, db, userbox.Event{Topic: smallInbox, Recipient: recipient, Method: string(bytes.Repeat([]byte("m"), 120)), Data: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		emit(t, db, itUser(), userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: fmt.Sprint(i), Data: []byte("{}")})
	}
	key := strconv.FormatInt(recipient, 10)
	eventually(t, 20*time.Second, "ordinary rows delivered", func() bool { return len(tr.of(key)) == 3 })
	eventually(t, 20*time.Second, "refused rows settled", func() bool {
		return countRows(t, db, "id = ?", hugeId) == 0 &&
			countRows(t, db, "recipient_id = ? AND topic = ? AND state = ?", recipient, smallInbox, userbox.EventDead) == 1
	})
	time.Sleep(time.Second)
	if n := len(tr.of(key)); n != 3 {
		t.Errorf("%d records: something was published twice", n)
	}
}

// Every statement of the relay that changes rows does it by primary key.
func TestRelayPlans(t *testing.T) {
	db := boxPool(t, 4)
	recipient := itUser()
	for range 100 {
		emit(t, db, itUser(), userbox.Event{Topic: "Inbox-T", Recipient: recipient, Method: "m", Data: []byte("{}")})
	}
	var a, b int64
	if err := db.QueryRow("SELECT MIN(id), MAX(id) FROM mq_outbox WHERE recipient_id = ?", recipient).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	// The rows exist in these revisions: the plans are of real lookups.
	if _, err := db.Exec("UPDATE mq_outbox SET state = 1 WHERE id IN (?, ?)", a, b); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		query string
		args  []any
	}{
		{fmt.Sprintf(finishPublished, values(finishPublishedRow, 2)), []any{a, 0, 1, 1, 1, b, 0, 1, 2, 1}},
		{fmt.Sprintf(finishDeleted, values(finishDeletedRow, 2)), []any{a, 0, b, 0}},
		{fmt.Sprintf(resetQuery, values(finishDeletedRow, 2)), []any{a, 0, b, 0}},
		{redeliverQuery, []any{a, 0}},
		{postponeQuery, []any{1, a, 0}},
	} {
		keys := explain(t, db, c.query, c.args...)
		key, ok := keys["o"]
		if !ok {
			key, ok = keys["mq_outbox"]
		}
		if !ok || key != "PRIMARY" {
			t.Errorf("%s: plan %v, want mq_outbox by PRIMARY", c.query, keys)
		}
	}
	if _, err := db.Exec("DELETE FROM mq_outbox WHERE recipient_id = ?", recipient); err != nil {
		t.Fatal(err)
	}
}

// explain returns the key of each table of the plan of query.
func explain(t *testing.T, db *sql.DB, query string, args ...any) map[string]string {
	t.Helper()
	rows, err := db.Query("EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		keys[vals[slices.Index(cols, "table")].String] = vals[slices.Index(cols, "key")].String
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return keys
}

// After a restore from a backup the ids of new events go above every id
// given before: a copy in Kafka never names a new event.
func TestRaiseIds(t *testing.T) {
	ctx := context.Background()
	db := boxPool(t, 4)
	recipient := itUser()
	ev := userbox.Event{Topic: "Sync-T", Recipient: recipient, Method: "m", Data: []byte("{}")}
	before, err := Insert(ctx, db, ev)
	if err != nil {
		t.Fatal(err)
	}
	next, err := RaiseIds(ctx, db, IdMargin)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Insert(ctx, db, ev)
	if err != nil {
		t.Fatal(err)
	}
	if next <= before+IdMargin || after < next {
		t.Errorf("ids %d, then %d after raising to %d", before, after, next)
	}
	// It never lowers the counter.
	again, err := RaiseIds(ctx, db, 0)
	if err != nil || again <= after {
		t.Errorf("raise by 0: %d, %v; the last id is %d", again, err, after)
	}
	if _, err = db.Exec("DELETE FROM mq_outbox WHERE recipient_id = ?", recipient); err != nil {
		t.Fatal(err)
	}
}
