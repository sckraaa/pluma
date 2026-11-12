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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/transcribe"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// workTick is how often the tables are looked at with nothing in this
	// process to wake the worker: jobs of another instance, attempts whose
	// time has come, leases that ran out.
	workTick = 2 * time.Second
	// sweepBatch bounds one pass of the updates owed.
	sweepBatch = 64
	// leaseSlack is the part of a lease the attempt does not use: the
	// attempt stops by itself before another may take the job.
	leaseSlack = 30 * time.Second
	// prepareBase and prepareRate are the part of an attempt before the
	// engine, reading the record and measuring its sound: a base, and a
	// time for every megabyte of the document, whose size the server knows
	// (the length a client declares is not to be trusted). An hour of a
	// voice message is 7 to 15 MB; ffmpeg decodes Opus hundreds of times
	// faster than it plays, and dfs reads a megabyte in milliseconds. The
	// engine's time is added once the decoded length is known
	// (Store.Extend).
	prepareBase = 90 * time.Second
	prepareRate = 4 * time.Second
	claimLease  = prepareBase + leaseSlack
	// busyPause is the wait after the engine said it was busy. downPause
	// is the first wait after it could not be used at all, doubled up to
	// downPauseMax while that lasts. Neither is an attempt, and there is
	// no end to them: the record waits for its engine with its decoded
	// sound, as long as it takes (the owner's decision of 2026-11-12).
	busyPause    = 2 * time.Second
	downPause    = 2 * time.Second
	downPauseMax = 30 * time.Second
	// retireAfter is how long a job of another model may lie untaken
	// before its readers are handed over to this instance's model: an
	// instance that runs that model would have taken it.
	retireAfter = time.Minute
	// downloadChunk is the most dfs gives in one answer.
	downloadChunk = 1 << 20
	// endWrite bounds the write of what came of an attempt.
	endWrite = 10 * time.Second
	// diskReserve is the free space the copy of a record must leave on the
	// disk of bff, which is the host's, MySQL's and Kafka's too. With less,
	// the record waits (noRoomPause) and no attempt is spent: a full disk
	// says nothing about the record.
	diskReserve = 1 << 30
	noRoomPause = downPauseMax
	// maxText is the longest text that is given to the clients, in bytes.
	// Android drops the connection on a packet over 2 MiB
	// (tgnet/Connection.cpp:216), and the text goes in one: the answer of
	// messages.transcribeAudio is not compressed (sendRpcResultToQueue).
	// 1 MiB is about eleven hours of Russian speech (45,682 characters an
	// hour measured); a record that says more fails for good, as one that
	// cannot be read: its text could reach no client.
	maxText = 1 << 20
)

var (
	// errLeaseLost: the attempt was too slow, and another holds the job now.
	errLeaseLost = errors.New("the attempt lost its lease")
	// errNoRoom: too little free disk to read the record now.
	errNoRoom = errors.New("too little free disk for the record")
	// errTooLong: a text no client can be given.
	errTooLong = errors.New("the text is longer than a client takes")
)

// Worker runs the jobs of transcription_jobs and sends the final updates
// of the requests that were answered "pending". Its state is the tables:
// what a stopped process leaves is taken over when the lease runs out, by
// this instance after a restart or by another.
type Worker struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartWorker starts the workers of the configuration and the sweep. The
// sweep runs without an engine too: updates may still be owed.
func StartWorker(svcCtx *svc.ServiceContext) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{svcCtx: svcCtx, cancel: cancel}
	if svcCtx.Engine != nil {
		for i := 0; i < svcCtx.Config.Transcribe.Workers; i++ {
			w.wg.Add(1)
			go func() {
				defer w.wg.Done()
				w.work(ctx)
			}()
		}
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.sweep(ctx)
	}()
	return w
}

// Stop ends the worker and waits for it. A job in flight goes back to the
// queue as it was.
func (w *Worker) Stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *Worker) work(ctx context.Context) {
	tick := time.NewTicker(workTick)
	defer tick.Stop()
	for ctx.Err() == nil {
		queued := w.svcCtx.Queued.Wait()
		job, err := w.svcCtx.Store.Claim(ctx, w.svcCtx.Config.Transcribe.Model, claimLease)
		if err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("transcription: claim a job: %v", err)
		}
		if job != nil {
			w.run(ctx, job)
			continue
		}
		select {
		case <-ctx.Done():
		case <-queued:
		case <-tick.C:
		}
	}
}

// run makes one attempt on job and writes what came of it.
func (w *Worker) run(ctx context.Context, job *transcribe.Job) {
	cfg := w.svcCtx.Config.Transcribe
	defer w.svcCtx.Finished.Broadcast()

	if job.Attempts > cfg.MaxAttempts {
		// Every attempt so far ended with its process.
		transcribe.CountJob(transcribe.JobFailed)
		logx.WithContext(ctx).Errorf("transcription: document %d: given up after %d attempts that did not end", job.DocumentId, job.Attempts-1)
		w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Fail(end, job, false) })
		return
	}

	text, err := w.transcribe(ctx, job)
	switch {
	case err == nil:
		transcribe.CountJob(transcribe.JobDone)
		w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Complete(end, job, text) })
	case ctx.Err() != nil:
		// The process stops: the attempt says nothing about the record.
		w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Retry(end, job, 0, false) })
	case errors.Is(err, errLeaseLost):
		// Whatever came of it, the attempt that holds the job now writes.
		transcribe.CountJob(transcribe.JobLost)
		logx.WithContext(ctx).Errorf("transcription: document %d: the attempt outlived its lease", job.DocumentId)
	case errors.Is(err, errNoRoom):
		transcribe.CountJob(transcribe.JobNoRoom)
		logx.WithContext(ctx).Errorf("transcription: document %d waits for free disk: %v", job.DocumentId, err)
		w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Retry(end, job, noRoomPause, false) })
	default:
		permanent := errors.Is(err, transcribe.ErrRejected) || errors.Is(err, transcribe.ErrUndecodable) || errors.Is(err, errTooLong)
		retryIn, final := transcribe.AfterFailure(job.Attempts, cfg.MaxAttempts, permanent)
		logx.WithContext(ctx).Errorf("transcription: document %d, attempt %d: %v", job.DocumentId, job.Attempts, err)
		switch {
		case !final:
			transcribe.CountJob(transcribe.JobRetry)
			w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Retry(end, job, retryIn, true) })
		case permanent:
			transcribe.CountJob(transcribe.JobRejected)
			w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Fail(end, job, true) })
		default:
			transcribe.CountJob(transcribe.JobFailed)
			w.write(ctx, job, func(end context.Context) (bool, error) { return w.svcCtx.Store.Fail(end, job, false) })
		}
	}
}

// write records what came of an attempt on job. Its time starts when the
// attempt is over, however long that took, and does not end with the
// process: what was done must be written while it stops.
func (w *Worker) write(ctx context.Context, job *transcribe.Job, store func(end context.Context) (bool, error)) {
	end, cancel := context.WithTimeout(context.WithoutCancel(ctx), endWrite)
	defer cancel()
	ok, err := store(end)
	switch {
	case err != nil:
		logx.WithContext(ctx).Errorf("transcription: document %d: write the result: %v", job.DocumentId, err)
	case !ok:
		transcribe.CountJob(transcribe.JobLost)
		logx.WithContext(ctx).Errorf("transcription: document %d: the attempt outlived its lease", job.DocumentId)
	}
}

// transcribe reads the record of job into a file, measures its sound and
// gives it to the engine. The file is the only thing on the disk, the size
// of the document; the decoded sound goes from ffmpeg to the engine and is
// never stored. The file does not outlive the call.
func (w *Worker) transcribe(ctx context.Context, job *transcribe.Job) (string, error) {
	took := prepareTime(job.FileSize)
	if held, err := w.svcCtx.Store.Extend(ctx, job, took+leaseSlack); err != nil {
		return "", err
	} else if !held {
		return "", errLeaseLost
	}
	tmp := w.svcCtx.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	if err := room(tmp, job.FileSize); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(tmp, "transcribe-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "record")
	prepare, stop := context.WithTimeout(ctx, took)
	err = w.download(prepare, job, src)
	var duration time.Duration
	if err == nil {
		duration, err = w.svcCtx.Decoder.Measure(prepare, src)
	}
	stop()
	if err != nil {
		return "", err
	}
	if duration == 0 {
		// A record without a sample of sound has no words.
		return "", nil
	}
	text, err := w.recognize(ctx, job, src, duration)
	if err == nil && len(text) > maxText {
		return "", fmt.Errorf("%w: %d bytes of %s of sound", errTooLong, len(text), duration)
	}
	return text, err
}

// room checks that the disk of dir has the space for a record of size
// bytes and diskReserve besides; errNoRoom if not.
func room(dir string, size int64) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return fmt.Errorf("the free space of %s: %w", dir, err)
	}
	// The types of the fields differ between systems.
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if need := uint64(max(size, 0)) + diskReserve; free < need {
		return fmt.Errorf("%w: %d MB free, %d MB needed", errNoRoom, free>>20, need>>20)
	}
	return nil
}

// prepareTime is how long reading and decoding a document of size bytes
// may take.
func prepareTime(size int64) time.Duration {
	return prepareBase + time.Duration(size>>20)*prepareRate
}

// recognize gives the sound of the record src to the engine, with the time
// its real length needs, whatever the client declared. An engine that is
// busy, down or misconfigured is asked again, for as long as that lasts:
// the record is not read again, and the wait is no attempt on it. Once the
// engine was down, it is probed with a sound of no samples until it takes
// one, and only then sent the record (transcribe.Probe).
func (w *Worker) recognize(ctx context.Context, job *transcribe.Job, src string, duration time.Duration) (string, error) {
	timeout := w.svcCtx.Config.Transcribe.EngineTimeout().For(duration)
	var down time.Duration // the next wait for an engine that is down; 0 while it is up
	for {
		if held, err := w.svcCtx.Store.Extend(ctx, job, timeout+leaseSlack); err != nil {
			return "", err
		} else if !held {
			return "", errLeaseLost
		}
		var (
			text string
			took time.Duration
			err  error
		)
		if down > 0 {
			err = transcribe.Probe(ctx, w.svcCtx.Engine)
			if !errors.Is(err, transcribe.ErrDown) && !errors.Is(err, transcribe.ErrBusy) {
				// The engine takes the configuration again; what it makes
				// of a sound of no samples says nothing about the record.
				err = nil
			}
		}
		if err == nil {
			text, took, err = w.ask(ctx, src, duration, timeout)
		}
		var wait time.Duration
		switch {
		case err == nil:
			transcribe.ObserveEngine(took.Seconds(), duration.Seconds())
			logx.WithContext(ctx).Infof("transcription: document %d: %.1f s of sound in %.2f s", job.DocumentId, duration.Seconds(), took.Seconds())
			// Whatever the engine pads its text with, the clients get it
			// trimmed; an empty text is "no words".
			return strings.TrimSpace(text), nil
		case errors.Is(err, transcribe.ErrBusy):
			transcribe.CountJob(transcribe.JobBusy)
			wait = busyPause
		case errors.Is(err, transcribe.ErrDown):
			transcribe.CountJob(transcribe.JobDown)
			if down == 0 {
				logx.WithContext(ctx).Errorf("transcription: document %d waits for the engine: %v", job.DocumentId, err)
				down = downPause
			}
			wait, down = down, min(2*down, downPauseMax)
		default:
			return "", err
		}
		pause := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			pause.Stop()
			return "", ctx.Err()
		case <-pause.C:
		}
	}
}

// ask makes one call to the engine with the sound of the record src,
// decoded as the engine reads it.
func (w *Worker) ask(ctx context.Context, src string, duration, timeout time.Duration) (string, time.Duration, error) {
	call, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sound := w.svcCtx.Decoder.Open(call, src)
	defer sound.Close()
	start := time.Now()
	text, err := w.svcCtx.Engine.Transcribe(call, sound, duration)
	return text, time.Since(start), err
}

// download writes the document of job to path. dfs answers a failure of
// the storage with no bytes and no error, so what was read is checked
// against the size of the document: a short file is an error to try again,
// never a record to decode.
func (w *Worker) download(ctx context.Context, job *transcribe.Job, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	location := mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
		Id:            job.DocumentId,
		AccessHash:    job.AccessHash,
		FileReference: []byte{},
	}).To_InputFileLocation()
	var offset int64
	for offset < job.FileSize {
		part, err := w.svcCtx.Files.DfsDownloadFile(ctx, &dfs.TLDfsDownloadFile{Location: location, Offset: offset, Limit: downloadChunk})
		if err != nil {
			return fmt.Errorf("read the document at %d: %w", offset, err)
		}
		data := part.GetBytes()
		if len(data) == 0 {
			break
		}
		if _, err = f.Write(data); err != nil {
			return err
		}
		offset += int64(len(data))
	}
	if offset != job.FileSize {
		return fmt.Errorf("the document is %d bytes, read %d", job.FileSize, offset)
	}
	return f.Close()
}

// sweep sends the updates owed and publishes the size of the queue.
func (w *Worker) sweep(ctx context.Context) {
	tick := time.NewTicker(workTick)
	defer tick.Stop()
	// again fires once the updates of the jobs that have just ended may
	// go: not before DeliverDelay after their answers.
	again := time.NewTimer(time.Hour)
	defer again.Stop()
	for ctx.Err() == nil {
		finished := w.svcCtx.Finished.Wait()
		w.retire(ctx)
		w.deliver(ctx)
		w.publish(ctx)
		select {
		case <-ctx.Done():
		case <-finished:
			again.Reset(w.svcCtx.Config.Transcribe.DeliverDelay + 50*time.Millisecond)
		case <-again.C:
		case <-tick.C:
		}
	}
}

// deliver sends the final update of every request that was answered
// "pending" and whose job has ended: exactly one, pending=false, with the
// text or, for a failure, without (ADR 0056, p. 6). Only this sends them,
// and not before deliver_after: the answer goes to the client directly and
// the update through the outbox, so the pause is what keeps the update
// behind the answer that gave the client its id (tdesktop drops an update
// with an id it does not know, api_transcribes.cpp:152-171).
func (w *Worker) deliver(ctx context.Context) {
	owed, err := w.svcCtx.Store.Owed(ctx, sweepBatch)
	if err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("transcription: updates owed: %v", err)
		}
		return
	}
	for _, o := range owed {
		event, err := sync_client.PushUpdatesEvent(w.svcCtx.Config.SyncTopic, &syncpb.TLSyncPushUpdates{
			UserId:  o.UserId,
			Updates: finalUpdate(o),
		})
		if err == nil {
			_, err = w.svcCtx.Store.Deliver(ctx, o.RequestId, event)
		}
		if err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("transcription: update of request %d: %v", o.RequestId, err)
		}
	}
}

// finalUpdate is updateTranscribedAudio of o for every device of its
// reader. It needs no users or chats: the reader has the message.
func finalUpdate(o transcribe.Owed) *mtproto.Updates {
	return mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateTranscribedAudio(&mtproto.Update{
		Pending:         false,
		Peer_PEER:       peerOf(o.Message),
		MsgId_INT32:     o.MsgId,
		TranscriptionId: o.RequestId,
		Text:            o.Text,
	}).To_Update())
}

// retire hands the readers of jobs of another model, which nobody has
// taken for retireAfter, over to this instance's model: after STT_MODEL
// changed they get the new model's text, not an empty update.
func (w *Worker) retire(ctx context.Context) {
	if w.svcCtx.Engine == nil {
		return // nothing would transcribe them here
	}
	n, err := w.svcCtx.Store.Retire(ctx, w.svcCtx.Config.Transcribe.Model, retireAfter)
	if err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorf("transcription: retire the jobs of other models: %v", err)
	}
	for i := 0; i < n; i++ {
		transcribe.CountJob(transcribe.JobRetired)
	}
	if n > 0 {
		w.svcCtx.Queued.Broadcast()
	}
}

func (w *Worker) publish(ctx context.Context) {
	depth, oldest, err := w.svcCtx.Store.Queue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("transcription: the queue: %v", err)
		}
		return
	}
	transcribe.SetQueue(depth, oldest.Seconds())
}
