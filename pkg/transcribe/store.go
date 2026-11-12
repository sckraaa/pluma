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
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// State is the state of a job, as transcription_jobs.state holds it.
type State int8

const (
	StateQueued  State = 1 // waits for its turn, or for the next attempt
	StateRunning State = 2 // an attempt holds the lease
	StateDone    State = 3 // the text is there
	StateFailed  State = 4
)

// delivery of the final update of a request, as
// transcription_requests.delivery holds it.
const (
	deliveryNone     = 0 // the answer carried the result, or nothing was asked yet
	deliveryAwaiting = 1 // the answer was "pending": an update is owed
	deliverySent     = 2
)

// retryPauses are the waits before the second, third and fourth attempt
// that reached the engine and came to nothing (an engine that is down or
// busy is waited for, not counted: app/bff/transcription). They are patient
// because the Android client keeps the first error it is given for a
// message for good (TranscribeButton.java:741-754 at 7be672ea): a record is
// better "pending" for a while than failed at once.
var retryPauses = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}

// AfterFailure says what follows the failure of the attempt number
// attempts (from 1): another attempt in retryIn, or the end.
func AfterFailure(attempts, maxAttempts int, permanent bool) (retryIn time.Duration, final bool) {
	if permanent || attempts >= maxAttempts || attempts < 1 || attempts > len(retryPauses) {
		return 0, true
	}
	return retryPauses[attempts-1], false
}

// Message names a message as its reader sees it: the reader, the dialog
// (PEER_SELF as the reader's own PEER_USER) and the id of the reader's copy.
type Message struct {
	UserId   int64
	PeerType int32
	PeerId   int64
	MsgId    int32
}

// Record is the document of a voice message or a round video.
type Record struct {
	DocumentId int64
	AccessHash int64
	// Duration is what the attribute of the document says, seconds: the
	// client's word, kept for the operator. Nothing is timed by it; the
	// worker times the engine by the length it decoded.
	Duration int32
	// FileSize is the size of the document as the server stored it.
	FileSize int64
}

// Job is the transcription of one document by one model: every copy and
// forward of a message has the same document, so a record is transcribed
// once for all its readers and chats.
type Job struct {
	Record
	Model     string
	State     State
	Text      string
	Permanent bool
	// Attempts started so far.
	Attempts int
	// lease is lease_until of the attempt that claimed the job: what the
	// attempt may write is guarded by it, so an attempt that outlived its
	// lease cannot overwrite the one that took over.
	lease      int64
	QueuedAt   time.Time
	FinishedAt time.Time
}

// Final reports whether nothing more will happen to the job by itself.
func (j *Job) Final() bool { return j.State == StateDone || j.State == StateFailed }

// passing: a failure that may pass, which a new request queues again.
func (j *Job) passing() bool {
	return j.State == StateFailed && !j.Permanent
}

// Store is the tables transcription_jobs and transcription_requests
// (teamgramd/deploy/sql/migrate-20261115-voice-transcription.sql). The jobs
// table is the queue, of any length, served in the order the records were
// asked for:
// nothing about a job is kept in a process, so a restart or a second
// instance of bff with the same model changes nothing. An instance takes
// only the jobs of its own model (Claim), and hands the readers of a model
// that nobody runs over to its own (Retire).
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns the store in db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

const jobColumns = "document_id, model, access_hash, state, `text`, permanent, attempts, duration, file_size, lease_until, queued_at, finished_at"

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	var (
		j                    Job
		text                 sql.NullString
		queuedAt, finishedAt int64
	)
	err := row.Scan(&j.DocumentId, &j.Model, &j.AccessHash, &j.State, &text, &j.Permanent, &j.Attempts, &j.Duration, &j.FileSize, &j.lease, &queuedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Text = text.String
	j.QueuedAt, j.FinishedAt = time.UnixMilli(queuedAt), time.UnixMilli(finishedAt)
	return &j, nil
}

// Job returns the job of the document and the model, nil without one.
func (s *Store) Job(ctx context.Context, documentId int64, model string) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM transcription_jobs WHERE document_id = ? AND model = ?", documentId, model))
}

// Request returns the id of the request of m, making it if it is the
// first: the transcription_id the client gets in the answer, in the update
// and sends back with a rating. It is one for a reader and a message
// however often they ask, and never shared by two messages: tdesktop keeps
// one message per id and finds the message of an update by the id alone
// (api_transcribes.cpp:152-171, 214 at v7.4.1).
//
// A message whose document or model changed (an edited media, a new
// STT_MODEL) keeps its id and forgets its rating and its pending update.
func (s *Store) Request(ctx context.Context, m Message, documentId int64, model string) (int64, error) {
	var (
		id         int64
		wasDoc     int64
		wasModel   string
		sameRecord bool
	)
	err := s.db.QueryRowContext(ctx,
		"SELECT id, document_id, model FROM transcription_requests WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND msg_id = ?",
		m.UserId, m.PeerType, m.PeerId, m.MsgId).Scan(&id, &wasDoc, &wasModel)
	switch {
	case err == nil:
		sameRecord = wasDoc == documentId && wasModel == model
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	if sameRecord {
		return id, nil
	}
	// The assignments run left to right: delivery and rating see the old
	// document and model.
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO transcription_requests (user_id, peer_type, peer_id, msg_id, document_id, model, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id),"+
			" delivery = IF(document_id = VALUES(document_id) AND model = VALUES(model), delivery, 0),"+
			" rating = IF(document_id = VALUES(document_id) AND model = VALUES(model), rating, NULL),"+
			" document_id = VALUES(document_id), model = VALUES(model)",
		m.UserId, m.PeerType, m.PeerId, m.MsgId, documentId, model, s.now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Enqueue makes sure the record is being transcribed by model, and returns
// its job as it is now. A job that is there is joined whatever its state,
// except a failure that may pass: that is queued again, and not before
// FailedTTL after it failed, so that a request is answered "pending", not
// with an error (Android keeps an error for the message for good,
// TranscribeButton.java:741-754 at 7be672ea), and an engine that is down
// is not asked again at every tap. Nothing is refused: however many
// records wait, a new one joins the end of the queue.
func (s *Store) Enqueue(ctx context.Context, rec Record, model string, failedTTL time.Duration) (*Job, error) {
	j, err := s.Job(ctx, rec.DocumentId, model)
	if err != nil {
		return nil, err
	}
	if j != nil && !j.passing() {
		return j, nil
	}
	now := s.now()
	if j == nil {
		// Two requests for one record at once: one row, the other joins.
		_, err = s.db.ExecContext(ctx,
			"INSERT IGNORE INTO transcription_jobs (document_id, model, access_hash, state, duration, file_size, retry_at, queued_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			rec.DocumentId, model, rec.AccessHash, StateQueued, rec.Duration, rec.FileSize, now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	} else {
		due := j.FinishedAt.Add(failedTTL)
		if due.Before(now) {
			due = now
		}
		_, err = s.db.ExecContext(ctx,
			"UPDATE transcription_jobs SET state = ?, attempts = 0, retry_at = ?, queued_at = ?, lease_until = 0, finished_at = 0"+
				" WHERE document_id = ? AND model = ? AND state = ? AND permanent = 0 AND finished_at = ?",
			StateQueued, due.UnixMilli(), now.UnixMilli(), rec.DocumentId, model, StateFailed, j.FinishedAt.UnixMilli())
	}
	if err != nil {
		return nil, err
	}
	if j, err = s.Job(ctx, rec.DocumentId, model); err != nil {
		return nil, err
	}
	if j == nil {
		return nil, fmt.Errorf("transcribe: no job of document %d after it was queued", rec.DocumentId)
	}
	return j, nil
}

// Await records that the request was answered "pending": the final update
// is owed to it, not before after.
func (s *Store) Await(ctx context.Context, requestId int64, after time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE transcription_requests SET delivery = ?, deliver_after = ? WHERE id = ?", deliveryAwaiting, after.UnixMilli(), requestId)
	return err
}

// Unawait takes Await back: the answer carries the result after all. It
// reports whether the update was still owed; false means it has been sent.
func (s *Store) Unawait(ctx context.Context, requestId int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE transcription_requests SET delivery = ? WHERE id = ? AND delivery = ?", deliveryNone, requestId, deliveryAwaiting)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Claim takes the next job of model that is due: one waiting for its turn
// or for its next attempt, or one whose attempt lost its lease (the
// process died). The attempt holds it for lease, and Extend moves that on.
// nil: no job is due. Two workers never get the same job. The jobs of
// other models are left to the instances that run them (Retire).
func (s *Store) Claim(ctx context.Context, model string, lease time.Duration) (*Job, error) {
	now := s.now().UnixMilli()
	candidates, err := s.due(ctx, model, now)
	if err != nil {
		return nil, err
	}
	until := now + lease.Milliseconds()
	for _, doc := range candidates {
		res, err := s.db.ExecContext(ctx,
			"UPDATE transcription_jobs SET state = ?, lease_until = ?, attempts = attempts + 1 WHERE document_id = ? AND model = ? AND "+dueJobs,
			StateRunning, until, doc, model, StateQueued, now, StateRunning, now)
		if err != nil {
			return nil, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if n == 0 {
			continue // another worker was first
		}
		j, err := s.Job(ctx, doc, model)
		if err != nil {
			return nil, err
		}
		if j != nil && j.State == StateRunning && j.lease == until {
			return j, nil
		}
	}
	return nil, nil
}

// dueJobs is the condition of a job an attempt may take at a time: queued
// and its time come, or running with a lease that ran out. Its arguments
// are StateQueued, the time, StateRunning, the time.
const dueJobs = "((state = ? AND retry_at <= ?) OR (state = ? AND lease_until <= ?))"

// due reads the documents of the first jobs of model that are due at now,
// the oldest first.
func (s *Store) due(ctx context.Context, model string, now int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT document_id FROM transcription_jobs WHERE model = ? AND "+dueJobs+" ORDER BY queued_at LIMIT 8",
		model, StateQueued, now, StateRunning, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []int64
	for rows.Next() {
		var doc int64
		if err = rows.Scan(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// Extend gives the attempt that holds j its lease until d from now, and
// reports whether it still held the job. An attempt learns how long it
// needs once it has decoded the record: the length a client declares is
// not to be trusted.
func (s *Store) Extend(ctx context.Context, j *Job, d time.Duration) (bool, error) {
	until := s.now().Add(d).UnixMilli()
	if until <= j.lease {
		// MySQL counts only the rows it changes.
		until = j.lease + 1
	}
	res, err := s.db.ExecContext(ctx, "UPDATE transcription_jobs SET lease_until = ? WHERE document_id = ? AND model = ? AND state = ? AND lease_until = ?",
		until, j.DocumentId, j.Model, StateRunning, j.lease)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	j.lease = until
	return true, nil
}

// Retire hands the readers who wait for a job of another model over to
// model, when nobody has run that job for grace: it was queued under an
// STT_MODEL that is configured no more. Each waiting request moves to the
// job of model, which is queued if it is not there, and gets its text from
// it; the old job ends without anyone waiting for it, so nobody is told
// "no words" for a record that was never heard. It returns how many jobs
// it retired.
func (s *Store) Retire(ctx context.Context, model string, grace time.Duration) (int, error) {
	now := s.now()
	cut := now.Add(-grace).UnixMilli()
	old, err := s.untaken(ctx, model, cut)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, j := range old {
		retired, err := s.retire(ctx, j, model, now, cut)
		if err != nil {
			return n, err
		}
		if retired {
			n++
		}
	}
	return n, nil
}

// untaken reads the first jobs of models other than model that nobody has
// run since cut.
func (s *Store) untaken(ctx context.Context, model string, cut int64) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+jobColumns+" FROM transcription_jobs WHERE model <> ? AND "+dueJobs+" ORDER BY queued_at LIMIT 8",
		model, StateQueued, cut, StateRunning, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var old []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		old = append(old, j)
	}
	return old, rows.Err()
}

// retire moves the waiting readers of j to model, in one transaction.
func (s *Store) retire(ctx context.Context, j *Job, model string, now time.Time, cut int64) (done bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx,
		"UPDATE transcription_jobs SET state = ?, `text` = NULL, permanent = 0, lease_until = 0, finished_at = ? WHERE document_id = ? AND model = ? AND "+dueJobs,
		StateFailed, now.UnixMilli(), j.DocumentId, j.Model, StateQueued, cut, StateRunning, cut)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err // taken by its instance after all
	}
	res, err = tx.ExecContext(ctx,
		"UPDATE transcription_requests SET model = ?, rating = NULL WHERE document_id = ? AND model = ? AND delivery = ?",
		model, j.DocumentId, j.Model, deliveryAwaiting)
	if err != nil {
		return false, err
	}
	waiting, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if waiting > 0 {
		if _, err = tx.ExecContext(ctx,
			"INSERT IGNORE INTO transcription_jobs (document_id, model, access_hash, state, duration, file_size, retry_at, queued_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			j.DocumentId, model, j.AccessHash, StateQueued, j.Duration, j.FileSize, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx,
			"UPDATE transcription_jobs SET state = ?, attempts = 0, retry_at = ?, queued_at = ?, lease_until = 0, finished_at = 0 WHERE document_id = ? AND model = ? AND state = ? AND permanent = 0",
			StateQueued, now.UnixMilli(), now.UnixMilli(), j.DocumentId, model, StateFailed); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// finish writes the end of the attempt that claimed j, and reports whether
// it was still the one holding the job.
func (s *Store) finish(ctx context.Context, j *Job, set string, args ...any) (bool, error) {
	args = append(args, j.DocumentId, j.Model, StateRunning, j.lease)
	//nolint:gosec // G202: set is a constant of the callers, the values are arguments
	res, err := s.db.ExecContext(ctx, "UPDATE transcription_jobs SET "+set+", lease_until = 0 WHERE document_id = ? AND model = ? AND state = ? AND lease_until = ?", args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Complete stores the text of j. The text may be empty: no speech.
func (s *Store) Complete(ctx context.Context, j *Job, text string) (bool, error) {
	return s.finish(ctx, j, "state = ?, `text` = ?, permanent = 0, finished_at = ?", StateDone, text, s.now().UnixMilli())
}

// Retry puts j back for another attempt in after. An attempt that never
// reached the engine's model (the engine was busy) is not counted.
func (s *Store) Retry(ctx context.Context, j *Job, after time.Duration, counted bool) (bool, error) {
	back := 1
	if counted {
		back = 0
	}
	return s.finish(ctx, j, "state = ?, retry_at = ?, attempts = attempts - ?", StateQueued, s.now().Add(after).UnixMilli(), back)
}

// Fail ends j without a text. permanent: the record itself cannot be
// transcribed, and is never tried again.
func (s *Store) Fail(ctx context.Context, j *Job, permanent bool) (bool, error) {
	return s.finish(ctx, j, "state = ?, `text` = NULL, permanent = ?, finished_at = ?", StateFailed, permanent, s.now().UnixMilli())
}

// Owed is a request that was answered "pending" and whose job has ended.
type Owed struct {
	RequestId int64
	Message
	Text string
}

// Owed returns up to limit requests whose final update is due. A failed
// job owes the update too, with an empty text: a client that was told
// "pending" waits until it is told otherwise.
func (s *Store) Owed(ctx context.Context, limit int) ([]Owed, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT r.id, r.user_id, r.peer_type, r.peer_id, r.msg_id, j.state, j.`text`"+
			" FROM transcription_requests r JOIN transcription_jobs j ON j.document_id = r.document_id AND j.model = r.model"+
			" WHERE r.delivery = ? AND r.deliver_after <= ? AND j.state IN (?, ?) ORDER BY r.deliver_after LIMIT ?",
		deliveryAwaiting, s.now().UnixMilli(), StateDone, StateFailed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Owed
	for rows.Next() {
		var (
			o     Owed
			state State
			text  sql.NullString
		)
		if err = rows.Scan(&o.RequestId, &o.UserId, &o.PeerType, &o.PeerId, &o.MsgId, &state, &text); err != nil {
			return nil, err
		}
		if state == StateDone {
			o.Text = text.String
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Deliver writes the final update of the request, the event, to the outbox
// and marks the request as told, in one transaction: the update goes out
// once, whatever fails and however many sweepers run. It reports whether
// this call was the one.
func (s *Store) Deliver(ctx context.Context, requestId int64, event userbox.Event) (sent bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if !sent {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, "UPDATE transcription_requests SET delivery = ? WHERE id = ? AND delivery = ?", deliverySent, requestId, deliveryAwaiting)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if _, err = outbox.InsertTx(ctx, tx, event); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	outbox.Notify()
	return true, nil
}

// Rate stores the reader's rating of the text it was given for m under the
// id. ok is false when the id is not the request of that reader and
// message. changed is true when the rating is new or different, with the
// model it rates.
func (s *Store) Rate(ctx context.Context, m Message, id int64, good bool) (model string, changed, ok bool, err error) {
	var was sql.NullBool
	err = s.db.QueryRowContext(ctx,
		"SELECT model, rating FROM transcription_requests WHERE id = ? AND user_id = ? AND peer_type = ? AND peer_id = ? AND msg_id = ?",
		id, m.UserId, m.PeerType, m.PeerId, m.MsgId).Scan(&model, &was)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if was.Valid && was.Bool == good {
		return model, false, true, nil
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE transcription_requests SET rating = ? WHERE id = ?", good, id); err != nil {
		return "", false, false, err
	}
	return model, true, true, nil
}

// Queue returns how many jobs wait or run, and how old the oldest is.
func (s *Store) Queue(ctx context.Context) (depth int, oldest time.Duration, err error) {
	var first sql.NullInt64
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*), MIN(queued_at) FROM transcription_jobs WHERE state IN (?, ?)", StateQueued, StateRunning).Scan(&depth, &first); err != nil {
		return 0, 0, err
	}
	if first.Valid {
		oldest = max(s.now().Sub(time.UnixMilli(first.Int64)), 0)
	}
	return depth, oldest, nil
}
