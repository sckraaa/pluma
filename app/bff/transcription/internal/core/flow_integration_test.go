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

package core

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
)

const itTopic = "Sync-T"

var (
	itMySQL *testinfra.MySQL
	itDB    *sql.DB
	// itVoice is a real voice message: two seconds of Ogg/Opus.
	itVoice []byte
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The worker decodes with the ffmpeg of the server image.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		fail(fmt.Errorf("the integration tests of transcription need ffmpeg: %w", err))
	}
	root, err := testinfra.RepoRoot()
	if err != nil {
		fail(err)
	}
	if itVoice, err = os.ReadFile(filepath.Join(root, "test/e2e/testdata/voice.ogg")); err != nil {
		fail(err)
	}
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fail(err)
	}
	if itDB, err = userbox.Open(itMySQL.DSN()); err == nil {
		err = outbox.Register(outbox.Topics{Push: []string{itTopic}})
	}
	if err != nil {
		fail(err)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// fakeEngine is the speech engine of a test.
type fakeEngine struct {
	calls atomic.Int32
	// answer gives the result of the call number n (from 1); it may block.
	answer func(ctx context.Context, n int32) (string, error)
	// The last sound it was given, and the time it had for it; the size
	// of every sound.
	mu       sync.Mutex
	header   []byte
	duration time.Duration
	deadline time.Time
	sizes    []int
}

func (e *fakeEngine) Transcribe(ctx context.Context, wav io.Reader, duration time.Duration) (string, error) {
	n := e.calls.Add(1)
	b, err := io.ReadAll(wav)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	e.header, e.duration = b[:min(len(b), 44)], duration
	e.deadline, _ = ctx.Deadline()
	e.sizes = append(e.sizes, len(b))
	e.mu.Unlock()
	return e.answer(ctx, n)
}

func says(text string) func(context.Context, int32) (string, error) {
	return func(context.Context, int32) (string, error) { return text, nil }
}

// fakeFiles serves documents by id, as dfs does: a chunk per call, no bytes
// past the end.
type fakeFiles struct {
	docs  map[int64][]byte
	calls atomic.Int32
}

func (f *fakeFiles) DfsDownloadFile(_ context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	f.calls.Add(1)
	data := f.docs[in.GetLocation().GetId()]
	from := min(in.GetOffset(), int64(len(data)))
	to := min(from+int64(in.GetLimit()), int64(len(data)))
	return mtproto.MakeTLUploadFile(&mtproto.Upload_File{Bytes: data[from:to]}).To_Upload_File(), nil
}

type stand struct {
	t      *testing.T
	svc    *svc.ServiceContext
	engine *fakeEngine
	files  *fakeFiles
}

// boxes are the boxes of the users of a test.
type boxes map[int64]fakeMessages

func (b boxes) MessageGetUserMessage(ctx context.Context, in *messagepb.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	return b[in.UserId].MessageGetUserMessage(ctx, in)
}

// newStand is a service context over empty tables, without a worker. me and
// friend have voice message 5 (document 9001) and round video 7 (9002) in
// their chat, each in its own box; me has the forward 6 of the voice
// message in Saved Messages.
func newStand(t *testing.T, cfg transcribe.Config, answer func(context.Context, int32) (string, error)) *stand {
	t.Helper()
	for _, table := range []string{"transcription_jobs", "transcription_requests", "mq_outbox"} {
		if _, err := itDB.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	cfg.URL, cfg.Model = "http://engine.test", "model-a"
	voice := withDocument(voiceDocument(voiceDoc, 2, int64(len(itVoice))))
	round := withDocument(roundDocument(9002, 2, int64(len(itVoice))))
	s := &stand{t: t, engine: &fakeEngine{answer: answer}, files: &fakeFiles{docs: map[int64][]byte{voiceDoc: itVoice, 9002: itVoice}}}
	s.svc = &svc.ServiceContext{
		Config: config.Config{Transcribe: cfg.WithDefaults(), SyncTopic: itTopic},
		Store:  transcribe.NewStore(itDB),
		Messages: boxes{
			me: {
				5: box(mtproto.PEER_USER, friend, voice),
				6: box(mtproto.PEER_USER, me, voice),
				7: box(mtproto.PEER_USER, friend, round),
			},
			friend: {
				5: received(friend, me, voice),
				7: received(friend, me, round),
			},
		},
		Channels: &fakeChannels{},
		Files:    s.files,
		Engine:   s.engine,
		TempDir:  t.TempDir(),
	}
	return s
}

// work starts the worker of the stand; it is stopped with the test.
func (s *stand) work() *Worker {
	w := StartWorker(s.svc)
	s.t.Cleanup(w.Stop)
	return w
}

func (s *stand) as(user int64) *TranscriptionCore {
	ctx := context.Background()
	return &TranscriptionCore{ctx: ctx, svcCtx: s.svc, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: user}}
}

func (s *stand) transcribe(user int64, peer *mtproto.InputPeer, msg int32) (*mtproto.Messages_TranscribedAudio, error) {
	return s.as(user).MessagesTranscribeAudio(&mtproto.TLMessagesTranscribeAudio{Peer: peer, MsgId: msg})
}

func (s *stand) job(doc int64) *transcribe.Job {
	s.t.Helper()
	j, err := s.svc.Store.Job(context.Background(), doc, "model-a")
	if err != nil {
		s.t.Fatal(err)
	}
	return j
}

// pushed returns the updateTranscribedAudio written to the outbox for user.
func (s *stand) pushed(user int64) []*mtproto.Update {
	s.t.Helper()
	rows, err := itDB.Query("SELECT method, payload FROM mq_outbox WHERE recipient_id = ? AND topic = ? ORDER BY id", user, itTopic)
	if err != nil {
		s.t.Fatal(err)
	}
	defer rows.Close()
	var out []*mtproto.Update
	for rows.Next() {
		var (
			method  string
			payload []byte
		)
		if err = rows.Scan(&method, &payload); err != nil {
			s.t.Fatal(err)
		}
		var push syncpb.TLSyncPushUpdates
		if method != "sync.TL_sync_pushUpdates" {
			s.t.Fatalf("an event %s in the outbox", method)
		}
		if err = jsonx.Unmarshal(payload, &push); err != nil {
			s.t.Fatal(err)
		}
		if push.GetUserId() != user {
			s.t.Fatalf("a push of user %d in the lane of %d", push.GetUserId(), user)
		}
		out = append(out, push.GetUpdates().GetUpdates()...)
	}
	if err = rows.Err(); err != nil {
		s.t.Fatal(err)
	}
	return out
}

// eventually waits until ok holds.
func eventually(t *testing.T, limit time.Duration, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("%s: not within %s", what, limit)
}

func final(t *testing.T, r *mtproto.Messages_TranscribedAudio, err error, text string) int64 {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPending() || r.GetText() != text || r.GetTranscriptionId() == 0 {
		t.Fatalf("answer %v, want the final text %q", r, text)
	}
	// Never the fields of the trial: they lock the button on iOS.
	if r.GetTrialRemainsNum() != nil || r.GetTrialRemainsUntilDate() != nil {
		t.Fatalf("trial fields in %v", r)
	}
	return r.GetTranscriptionId()
}

func pending(t *testing.T, r *mtproto.Messages_TranscribedAudio, err error) int64 {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !r.GetPending() || r.GetText() != "" || r.GetTranscriptionId() == 0 {
		t.Fatalf("answer %v, want pending without a text", r)
	}
	if r.GetTrialRemainsNum() != nil || r.GetTrialRemainsUntilDate() != nil {
		t.Fatalf("trial fields in %v", r)
	}
	return r.GetTranscriptionId()
}

func TestTextInTheAnswer(t *testing.T) {
	s := newStand(t, transcribe.Config{}, says("  Привет, это голосовое.  "))
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := final(t, r, err, "Привет, это голосовое.")

	// The engine got what every engine reads: a WAV, 16 kHz mono, of the
	// two seconds of the record.
	s.engine.mu.Lock()
	header, duration := s.engine.header, s.engine.duration
	s.engine.mu.Unlock()
	if !bytes.HasPrefix(header, []byte("RIFF")) || !bytes.Equal(header[8:16], []byte("WAVEfmt ")) || !bytes.Equal(header[36:40], []byte("data")) {
		t.Fatalf("the engine got %q", header)
	}
	if duration < 1900*time.Millisecond || duration > 2200*time.Millisecond {
		t.Errorf("duration %s", duration)
	}

	// Asked again: the same id and text, no second recognition.
	r, err = s.transcribe(me, inputUser(friend), 5)
	if again := final(t, r, err, "Привет, это голосовое."); again != id {
		t.Errorf("id %d, then %d", id, again)
	}
	// The forward in Saved Messages and the copy of another reader: the
	// text at once, ids of their own.
	r, err = s.transcribe(me, inputSelf, 6)
	forward := final(t, r, err, "Привет, это голосовое.")
	r, err = s.transcribe(friend, inputUser(me), 5)
	other := final(t, r, err, "Привет, это голосовое.")
	if forward == id || other == id || forward == other {
		t.Errorf("ids %d, %d, %d", id, forward, other)
	}
	if n := s.engine.calls.Load(); n != 1 {
		t.Fatalf("%d recognitions of one record", n)
	}
	// Nobody was told "pending": no update is sent.
	if u := append(s.pushed(me), s.pushed(friend)...); len(u) != 0 {
		t.Fatalf("updates %v", u)
	}
	// The record is gone from the disk.
	if left, _ := os.ReadDir(s.svc.TempDir); len(left) != 0 {
		t.Errorf("%d files left in the temporary directory", len(left))
	}
}

func TestPendingThenExactlyOneUpdate(t *testing.T) {
	release := make(chan struct{})
	s := newStand(t, transcribe.Config{SyncWait: 300 * time.Millisecond, DeliverDelay: 400 * time.Millisecond}, func(ctx context.Context, _ int32) (string, error) {
		select {
		case <-release:
			return "Долгая запись.", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	s.work()
	start := time.Now()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	if waited := time.Since(start); waited < 300*time.Millisecond || waited > 3*time.Second {
		t.Errorf("answered after %s", waited)
	}
	answered := time.Now()
	// Asked again while it runs: pending again, the same id.
	r, err = s.transcribe(me, inputUser(friend), 5)
	if again := pending(t, r, err); again != id {
		t.Errorf("id %d, then %d", id, again)
	}
	if u := s.pushed(me); len(u) != 0 {
		t.Fatalf("an update before the text: %v", u)
	}
	close(release)
	eventually(t, 10*time.Second, "the final update", func() bool { return len(s.pushed(me)) > 0 })
	if early := time.Since(answered); early < 300*time.Millisecond {
		t.Errorf("the update went %s after the answer, before the client could know the id", early)
	}
	// Later sweeps add nothing.
	time.Sleep(2*workTick + 500*time.Millisecond)
	u := s.pushed(me)
	if len(u) != 1 {
		t.Fatalf("%d updates", len(u))
	}
	if u[0].GetPredicateName() != mtproto.Predicate_updateTranscribedAudio || u[0].GetPending() || u[0].GetTranscriptionId() != id ||
		u[0].GetText() != "Долгая запись." || u[0].GetMsgId_INT32() != 5 || u[0].GetPeer_PEER().GetUserId() != friend {
		t.Fatalf("update %v", u[0])
	}
	// And the text is in the answer from now on.
	r, err = s.transcribe(me, inputUser(friend), 5)
	if again := final(t, r, err, "Долгая запись."); again != id {
		t.Errorf("id %d, then %d", id, again)
	}
	if len(s.pushed(me)) != 1 {
		t.Fatal("the cached answer sent an update")
	}
}

// A recognition that takes longer than the write of its result may take is
// still written: the time of the write starts when the engine answers.
func TestLongRecognitionIsWritten(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 200 * time.Millisecond, DeliverDelay: 200 * time.Millisecond}, func(ctx context.Context, _ int32) (string, error) {
		select {
		case <-time.After(endWrite + time.Second):
			return "Очень долгая запись.", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	eventually(t, endWrite+10*time.Second, "the final update", func() bool { return len(s.pushed(me)) > 0 })
	u := s.pushed(me)
	if len(u) != 1 || u[0].GetTranscriptionId() != id || u[0].GetText() != "Очень долгая запись." {
		t.Fatalf("updates %v", u)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateDone || j.Attempts != 1 {
		t.Fatalf("job %+v", j)
	}
}

func TestTwoReadersAtOnceOneRecognition(t *testing.T) {
	s := newStand(t, transcribe.Config{}, func(context.Context, int32) (string, error) {
		time.Sleep(300 * time.Millisecond)
		return "Одна на всех.", nil
	})
	s.work()
	var (
		wg  sync.WaitGroup
		ids [2]int64
	)
	for i, user := range []int64{me, friend} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			peer := inputUser(friend)
			if user == friend {
				peer = inputUser(me)
			}
			r, err := s.transcribe(user, peer, 5)
			if err != nil || r.GetPending() || r.GetText() != "Одна на всех." {
				t.Errorf("user %d: %v, %v", user, r, err)
				return
			}
			ids[i] = r.GetTranscriptionId()
		}()
	}
	wg.Wait()
	if ids[0] == ids[1] {
		t.Errorf("both readers got the id %d", ids[0])
	}
	if n := s.engine.calls.Load(); n != 1 {
		t.Fatalf("%d recognitions", n)
	}
}

func TestRejectedSoundFailsAtOnce(t *testing.T) {
	s := newStand(t, transcribe.Config{}, func(context.Context, int32) (string, error) {
		return "", fmt.Errorf("%w: status 400", transcribe.ErrRejected)
	})
	s.work()
	if _, err := s.transcribe(me, inputUser(friend), 5); !isError(err, mtproto.ErrTranscriptionFailed) {
		t.Fatalf("got %v", err)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateFailed || !j.Permanent || j.Attempts != 1 {
		t.Fatalf("job %+v", j)
	}
	// Asked again: the same failure, no second recognition.
	if _, err := s.transcribe(friend, inputUser(me), 5); !isError(err, mtproto.ErrTranscriptionFailed) {
		t.Fatalf("got %v", err)
	}
	if n := s.engine.calls.Load(); n != 1 {
		t.Fatalf("%d recognitions of a record that cannot be read", n)
	}
	if u := s.pushed(me); len(u) != 0 {
		t.Fatalf("updates %v after an error in the answer", u)
	}
}

func TestUndecodableRecordFailsForGood(t *testing.T) {
	s := newStand(t, transcribe.Config{}, says("never"))
	garbage := bytes.Repeat([]byte("not a media file\n"), 64)
	s.files.docs[voiceDoc] = garbage
	s.svc.Messages = boxes{me: {5: box(mtproto.PEER_USER, friend, withDocument(voiceDocument(voiceDoc, 2, int64(len(garbage)))))}}
	s.work()
	if _, err := s.transcribe(me, inputUser(friend), 5); !isError(err, mtproto.ErrTranscriptionFailed) {
		t.Fatalf("got %v", err)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateFailed || !j.Permanent {
		t.Fatalf("job %+v", j)
	}
	if n := s.engine.calls.Load(); n != 0 {
		t.Fatal("the engine was given what ffmpeg refused")
	}
}

// An engine that takes the sound and fails on it, attempt after attempt,
// ends the record with an empty update; a new request queues it again.
func TestEngineFailuresEndWithAnEmptyUpdate(t *testing.T) {
	// One attempt, so that the test does not sit through the pauses.
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond, MaxAttempts: 1, FailedTTL: 1500 * time.Millisecond}, func(ctx context.Context, _ int32) (string, error) {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
		}
		return "", fmt.Errorf("%w: status 500", transcribe.ErrUnavailable)
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	eventually(t, 10*time.Second, "the final update", func() bool { return len(s.pushed(me)) > 0 })
	u := s.pushed(me)
	if len(u) != 1 || u[0].GetPending() || u[0].GetText() != "" || u[0].GetTranscriptionId() != id {
		t.Fatalf("updates %v", u)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateFailed || j.Permanent {
		t.Fatalf("job %+v: a failure of the engine is not the record's", j)
	}
	// Asked again within FailedTTL: "pending", not the failure (Android
	// would keep that for good), and the record is tried once FailedTTL
	// has passed; the engine is not asked at every tap meanwhile.
	failedAt := time.Now()
	r, err = s.transcribe(me, inputUser(friend), 5)
	if again := pending(t, r, err); again != id {
		t.Fatalf("id %d, then %d", id, again)
	}
	if n := s.engine.calls.Load(); n != 1 {
		t.Fatalf("%d recognitions within FailedTTL", n)
	}
	eventually(t, 10*time.Second, "the second final update", func() bool { return len(s.pushed(me)) == 2 })
	if early := time.Since(failedAt); early < time.Second {
		t.Errorf("tried again %s after the failure", early)
	}
	if n := s.engine.calls.Load(); n != 2 {
		t.Fatalf("%d recognitions", n)
	}
}

func TestFailureIsRetriedBeforeItIsFinal(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond}, func(_ context.Context, n int32) (string, error) {
		if n == 1 {
			return "", fmt.Errorf("%w: status 502", transcribe.ErrUnavailable)
		}
		return "Со второй попытки.", nil
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	// The first failure is not told to anyone: the record waits its pause.
	time.Sleep(time.Second)
	if j := s.job(voiceDoc); j.State != transcribe.StateQueued || j.Attempts != 1 || len(s.pushed(me)) != 0 {
		t.Fatalf("after the first failure: job %+v, %d updates", j, len(s.pushed(me)))
	}
	eventually(t, 15*time.Second, "the text of the second attempt", func() bool { return len(s.pushed(me)) > 0 })
	if u := s.pushed(me); len(u) != 1 || u[0].GetText() != "Со второй попытки." || u[0].GetTranscriptionId() != id {
		t.Fatalf("updates %v", u)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateDone || j.Attempts != 2 {
		t.Fatalf("job %+v", j)
	}
}

func TestBusyEngineIsNotAnAttempt(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond}, func(_ context.Context, n int32) (string, error) {
		if n <= 2 {
			return "", fmt.Errorf("%w (status 503)", transcribe.ErrBusy)
		}
		return "Дождались.", nil
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	pending(t, r, err)
	eventually(t, 15*time.Second, "the text", func() bool { return s.job(voiceDoc).State == transcribe.StateDone })
	if j := s.job(voiceDoc); j.Attempts != 1 || j.Text != "Дождались." {
		t.Fatalf("job %+v", j)
	}
	// The record was read once, not for every 503.
	if n := s.files.calls.Load(); n != 1 {
		t.Fatalf("the record was read %d times", n)
	}
}

// An engine that cannot be used (stopped, or refusing the configuration) is
// waited for with the decoded sound: no attempt is spent, nobody is told
// "no words", and the text comes once the engine is back.
func TestEngineDownIsWaitedFor(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond, MaxAttempts: 1}, func(_ context.Context, n int32) (string, error) {
		switch n {
		case 1:
			return "", fmt.Errorf("%w: dial tcp: connection refused", transcribe.ErrDown)
		case 2:
			return "", fmt.Errorf("%w (status 404)", transcribe.ErrDown)
		}
		return "Движок вернулся.", nil
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	eventually(t, 20*time.Second, "the text", func() bool { return len(s.pushed(me)) > 0 })
	if u := s.pushed(me); len(u) != 1 || u[0].GetText() != "Движок вернулся." || u[0].GetTranscriptionId() != id {
		t.Fatalf("updates %v", u)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateDone || j.Attempts != 1 {
		t.Fatalf("job %+v", j)
	}
	if n := s.files.calls.Load(); n != 1 {
		t.Fatalf("the record was read %d times", n)
	}
	// After the engine was down, it was sent a sound of no samples until it
	// took one (the second refused it, the third was taken), and only then
	// the record: a refused configuration does not cost the whole record
	// on the wire every time.
	s.engine.mu.Lock()
	sizes := s.engine.sizes
	s.engine.mu.Unlock()
	if len(sizes) != 4 || sizes[1] != 44 || sizes[2] != 44 || sizes[0] <= 44 || sizes[3] != sizes[0] {
		t.Fatalf("the engine was sent %v bytes", sizes)
	}
}

// A text no client could be given (Android drops a packet over 2 MiB) ends
// the record for good, as one that cannot be read: the reader told
// "pending" gets the final update without a text, never one that breaks
// its connection, and the text is not kept.
func TestTextLongerThanAClientTakes(t *testing.T) {
	long := strings.Repeat("я", maxText/2+1)
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond}, func(context.Context, int32) (string, error) {
		// Past the wait of the answer: the reader is told "pending".
		time.Sleep(500 * time.Millisecond)
		return long, nil
	})
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	eventually(t, 10*time.Second, "the final update", func() bool { return len(s.pushed(me)) > 0 })
	if u := s.pushed(me); len(u) != 1 || u[0].GetText() != "" || u[0].GetTranscriptionId() != id || u[0].GetPending() {
		t.Fatalf("updates %v", u)
	}
	if j := s.job(voiceDoc); j.State != transcribe.StateFailed || !j.Permanent || j.Text != "" || j.Attempts != 1 {
		t.Fatalf("job %+v", j)
	}
}

// With too little free disk for the copy of a record, the record waits:
// nothing is read, the engine is not asked, and no attempt is spent.
func TestTooLittleDiskIsWaitedFor(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, MaxAttempts: 1}, says("never"))
	// A document larger than any disk of the stand.
	s.svc.Messages = boxes{me: {5: box(mtproto.PEER_USER, friend, withDocument(voiceDocument(voiceDoc, 2, 1<<50)))}}
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	pending(t, r, err)
	time.Sleep(2*workTick + 500*time.Millisecond)
	if j := s.job(voiceDoc); j.State != transcribe.StateQueued || j.Attempts != 0 {
		t.Fatalf("job %+v", j)
	}
	var retryAt int64
	if err = itDB.QueryRow("SELECT retry_at FROM transcription_jobs WHERE document_id = ?", voiceDoc).Scan(&retryAt); err != nil {
		t.Fatal(err)
	}
	if wait := time.Until(time.UnixMilli(retryAt)); wait < noRoomPause-5*time.Second {
		t.Fatalf("tried again in %s", wait)
	}
	if n := s.files.calls.Load(); n != 0 {
		t.Fatalf("the record was read %d times", n)
	}
	if n := s.engine.calls.Load(); n != 0 {
		t.Fatalf("the engine was asked %d times", n)
	}
	if u := s.pushed(me); len(u) != 0 {
		t.Fatalf("updates %v", u)
	}
}

// A busy engine is never held against a record, however long it waited in
// the queue before.// The engine gets the time of the sound as decoded, not of the length the
// client declared, and the lease of the attempt covers it.
func TestEngineTimeIsOfTheDecodedSound(t *testing.T) {
	s := newStand(t, transcribe.Config{Timeout: "1s+20x"}, says("Длина по звуку."))
	// The client says one second; the record is two.
	s.svc.Messages = boxes{me: {5: box(mtproto.PEER_USER, friend, withDocument(voiceDocument(voiceDoc, 1, int64(len(itVoice)))))}}
	var lease atomic.Int64
	s.engine.answer = func(context.Context, int32) (string, error) {
		var until int64
		if err := itDB.QueryRow("SELECT lease_until FROM transcription_jobs WHERE document_id = ?", voiceDoc).Scan(&until); err != nil {
			return "", err
		}
		lease.Store(until)
		return "Длина по звуку.", nil
	}
	s.work()
	asked := time.Now()
	r, err := s.transcribe(me, inputUser(friend), 5)
	final(t, r, err, "Длина по звуку.")
	s.engine.mu.Lock()
	duration, deadline := s.engine.duration, s.engine.deadline
	s.engine.mu.Unlock()
	// 1 s + 20 x the decoded two seconds, as the configuration says; the
	// declared second would give 21 s.
	want := time.Second + 20*duration
	if got := deadline.Sub(asked); got < want-5*time.Second || got > want+5*time.Second {
		t.Fatalf("the engine had %s for %s of sound, want %s", got, duration, want)
	}
	if got := time.UnixMilli(lease.Load()).Sub(asked); got < want+leaseSlack-5*time.Second {
		t.Fatalf("the attempt held the job for %s, its engine call may take %s", got, want)
	}
}

// A job of another model is left to the instance that runs it: it is not
// failed, and its readers are not told anything.
func TestJobOfAnotherModelIsLeftAlone(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond}, says("never"))
	ctx := context.Background()
	m := transcribe.Message{UserId: me, PeerType: mtproto.PEER_USER, PeerId: friend, MsgId: 5}
	id, err := s.svc.Store.Request(ctx, m, voiceDoc, "model-b")
	if err == nil {
		_, err = s.svc.Store.Enqueue(ctx, transcribe.Record{DocumentId: voiceDoc, AccessHash: voiceDoc + 1, Duration: 2, FileSize: int64(len(itVoice))}, "model-b", time.Minute)
	}
	if err == nil {
		err = s.svc.Store.Await(ctx, id, time.Now())
	}
	if err != nil {
		t.Fatal(err)
	}
	s.work()
	time.Sleep(2*workTick + 500*time.Millisecond)
	j, err := s.svc.Store.Job(ctx, voiceDoc, "model-b")
	if err != nil || j == nil || j.State != transcribe.StateQueued {
		t.Fatalf("the job of the other model %+v, err %v", j, err)
	}
	if u := s.pushed(me); len(u) != 0 {
		t.Fatalf("updates %v", u)
	}
	if n := s.engine.calls.Load(); n != 0 {
		t.Fatalf("%d recognitions of another model's job", n)
	}
}

func TestShortReadOfTheRecordIsRetried(t *testing.T) {
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond}, says("never"))
	// dfs answers a failure of the storage with no bytes and no error.
	s.files.docs[voiceDoc] = itVoice[:len(itVoice)/2]
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	pending(t, r, err)
	eventually(t, 5*time.Second, "the first attempt", func() bool { return s.job(voiceDoc).Attempts == 1 && s.job(voiceDoc).State == transcribe.StateQueued })
	if n := s.engine.calls.Load(); n != 0 {
		t.Fatal("half a record was given to the engine")
	}
}

func TestWorkerRestartInTheMiddle(t *testing.T) {
	entered := make(chan struct{}, 1)
	var broken atomic.Bool
	broken.Store(true)
	s := newStand(t, transcribe.Config{SyncWait: 100 * time.Millisecond, DeliverDelay: 100 * time.Millisecond}, func(ctx context.Context, _ int32) (string, error) {
		if !broken.Load() {
			return "После перезапуска.", nil
		}
		entered <- struct{}{}
		<-ctx.Done() // holds the job until the process stops
		return "", ctx.Err()
	})
	first := StartWorker(s.svc)
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := pending(t, r, err)
	<-entered
	first.Stop()
	// The job is back in the queue as it was: the stop is not held
	// against the record.
	if j := s.job(voiceDoc); j.State != transcribe.StateQueued || j.Attempts != 0 {
		t.Fatalf("after the stop: %+v", j)
	}
	if len(s.pushed(me)) != 0 {
		t.Fatal("an update for a job that did not end")
	}
	broken.Store(false)
	s.work()
	eventually(t, 10*time.Second, "the final update", func() bool { return len(s.pushed(me)) > 0 })
	time.Sleep(workTick + 500*time.Millisecond)
	if u := s.pushed(me); len(u) != 1 || u[0].GetText() != "После перезапуска." || u[0].GetTranscriptionId() != id {
		t.Fatalf("updates %v", u)
	}
}

// Nothing is refused for volume (the owner's decision of 2026-11-12): one
// user transcribes record after record, and a record a client declares four
// hours long is transcribed like any other.
func TestNothingIsRefusedForVolume(t *testing.T) {
	s := newStand(t, transcribe.Config{}, says("Без пределов."))
	const long = int64(9003)
	s.files.docs[long] = itVoice
	s.svc.Messages.(boxes)[me][8] = box(mtproto.PEER_USER, friend, withDocument(voiceDocument(long, 4*3600, int64(len(itVoice)))))
	s.work()
	for _, msg := range []int32{5, 7, 8} {
		r, err := s.transcribe(me, inputUser(friend), msg)
		final(t, r, err, "Без пределов.")
	}
	for i := 0; i < 50; i++ {
		r, err := s.transcribe(me, inputUser(friend), 8)
		final(t, r, err, "Без пределов.")
	}
	if n := s.engine.calls.Load(); n != 3 {
		t.Fatalf("%d recognitions of three records", n)
	}
}

func TestRating(t *testing.T) {
	s := newStand(t, transcribe.Config{}, says("Оцените."))
	s.work()
	r, err := s.transcribe(me, inputUser(friend), 5)
	id := final(t, r, err, "Оцените.")
	rate := func(user int64, peer *mtproto.InputPeer, msg int32, id int64, good bool) error {
		ok, err := s.as(user).MessagesRateTranscribedAudio(&mtproto.TLMessagesRateTranscribedAudio{
			Peer: peer, MsgId: msg, TranscriptionId: id, Good: mtproto.ToBool(good),
		})
		if err == nil && !mtproto.FromBool(ok) {
			return errors.New("answered boolFalse")
		}
		return err
	}
	if err = rate(me, inputUser(friend), 5, id, true); err != nil {
		t.Fatal(err)
	}
	if err = rate(me, inputUser(friend), 5, id, false); err != nil {
		t.Fatalf("a changed mind: %v", err)
	}
	var rating sql.NullBool
	if err = itDB.QueryRow("SELECT rating FROM transcription_requests WHERE id = ?", id).Scan(&rating); err != nil || !rating.Valid || rating.Bool {
		t.Fatalf("stored %v, %v", rating, err)
	}
	for name, err := range map[string]error{
		"somebody else's id":  rate(friend, inputUser(me), 5, id, true),
		"another message":     rate(me, inputSelf, 6, id, true),
		"an id never given":   rate(me, inputUser(friend), 5, id+100, true),
		"another dialog":      rate(me, inputChat(300), 5, id, true),
		"zero for an id":      rate(me, inputUser(friend), 5, 0, true),
		"a channel it is not": rate(me, inputChannel(channelId, channelHash), 5, id, true),
	} {
		if !isError(err, mtproto.ErrMsgIdInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if err = rate(me, mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), 5, id, true); !isError(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("no peer: got %v", err)
	}
}
