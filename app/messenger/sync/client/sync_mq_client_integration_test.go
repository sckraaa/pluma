//go:build integration

package sync_client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	gosync "sync"
	"testing"
	"time"

	kafka "github.com/teamgram/marmota/pkg/mq"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"google.golang.org/protobuf/proto"
)

var itKafka *testinfra.Kafka

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itKafka, err = testinfra.StartKafka(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itKafka.Terminate(ctx)
	os.Exit(code)
}

type received struct {
	method, key string
	push        *sync.TLSyncPushUpdates
}

// TestSyncMqRoundTrip sends sync.pushUpdates through the producer msg and bff
// use (marmota mq, SyncMqClient) and reads them with the consumer group sync
// uses, decoding them as the sync mq server does (app/messenger/sync/internal/server/mq):
// the method header names the request, the key is the user id, the value is
// its JSON, and the updates of one user arrive in the order they were sent.
func TestSyncMqRoundTrip(t *testing.T) {
	const topic = "Sync-T"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	producer := kafka.MustKafkaProducer(&kafka.KafkaProducerConf{Brokers: itKafka.Brokers, Topic: topic})
	defer producer.Close()
	client := NewSyncMqClient(producer)

	consumer := kafka.MustKafkaConsumer(&kafka.KafkaConsumerConf{
		Brokers: itKafka.Brokers,
		Topics:  []string{topic},
		Group:   "Sync-IntegrationTest-S",
	})
	got := make(chan received, 256)
	consumer.RegisterHandlers(topic, func(ctx context.Context, method, key string, value []byte) {
		r := new(sync.TLSyncPushUpdates)
		if err := json.Unmarshal(value, r); err != nil {
			t.Errorf("decode %s: %v", method, err)
			return
		}
		got <- received{method: method, key: key, push: r}
	})
	var wg gosync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); consumer.Start() }()
	defer func() {
		_ = consumer.Close()
		wg.Wait()
	}()

	push := func(userId int64, text string) *sync.TLSyncPushUpdates {
		in := &sync.TLSyncPushUpdates{
			Constructor: sync.CRC32_sync_pushUpdates,
			UserId:      userId,
			Updates: mtproto.MakeTLUpdateShortMessage(&mtproto.Updates{
				Id:       int32(len(text)),
				UserId:   userId + 1,
				Message:  text,
				Pts:      7,
				PtsCount: 1,
				Date:     int32(time.Now().Unix()),
			}).To_Updates(),
		}
		if _, err := client.SyncPushUpdates(ctx, in); err != nil {
			t.Fatalf("SyncPushUpdates: %v", err)
		}
		return in
	}

	// The consumer starts at the newest offset once it has joined the group:
	// probe until it receives, then send the messages under test.
	const probeUser = int64(1)
	ready := false
	for !ready {
		push(probeUser, "probe")
		select {
		case <-got:
			ready = true
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("the consumer received nothing")
		}
	}
	drain := time.After(time.Second)
	for draining := true; draining; {
		select {
		case <-got:
		case <-drain:
			draining = false
		}
	}

	const perUser = 20
	users := []int64{1001, 1002, 1003}
	sent := map[string]*sync.TLSyncPushUpdates{}
	for i := 0; i < perUser; i++ {
		for _, u := range users {
			text := fmt.Sprintf("user %d message %02d", u, i)
			sent[text] = push(u, text)
		}
	}

	next := map[int64]int{}
	for n := 0; n < perUser*len(users); n++ {
		var r received
		select {
		case r = <-got:
		case <-ctx.Done():
			t.Fatalf("received %d of %d messages", n, perUser*len(users))
		}
		if r.method != string(proto.MessageName(&sync.TLSyncPushUpdates{})) {
			t.Fatalf("method header %q", r.method)
		}
		userId := r.push.GetUserId()
		if r.key != strconv.FormatInt(userId, 10) {
			t.Fatalf("key %q for user %d: messages of a user must share the partition key", r.key, userId)
		}
		text := r.push.GetUpdates().GetMessage()
		want := fmt.Sprintf("user %d message %02d", userId, next[userId])
		if text != want {
			t.Fatalf("user %d got %q, want %q: per-user order lost", userId, text, want)
		}
		next[userId]++
		if in := sent[text]; !proto.Equal(in, r.push) {
			t.Fatalf("decoded %v, sent %v", r.push, in)
		}
	}
}
