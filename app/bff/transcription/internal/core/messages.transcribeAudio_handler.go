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
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
)

// waitPoll is how often a waiting request looks at its job when nothing
// in this process woke it: the job may be another instance's.
const waitPoll = 250 * time.Millisecond

// MessagesTranscribeAudio
// messages.transcribeAudio#269e9a49 peer:InputPeer msg_id:int = messages.TranscribedAudio;
//
// The text of a voice message or a round video (ADR 0056, p. 6). The
// record is transcribed once for every reader and chat that has it (the
// job is the document's); the caller gets the text in the answer when it
// is there or comes within SyncWait, and otherwise pending with an empty
// text, followed by exactly one updateTranscribedAudio with the result.
//
// What the answer must be for the clients:
//
//   - transcription_id is the caller's and the message's own, the same in
//     the answer, the update and a rating: tdesktop keeps one message per
//     id and finds the message of an update by it alone
//     (api_transcribes.cpp:152-171, 214);
//   - a pending answer has no text, and no pending update follows it:
//     Android skips those (MessagesController.java:20806), tdesktop and
//     the round video of Web K wait for the final one without a timeout
//     (appMessagesManager.ts:3156-3192);
//   - never FLOOD_WAIT, PREMIUM_ACCOUNT_REQUIRED or trial_remains_*:
//     Premium is everyone's (ADR 0041), and the first and the third lock
//     the button on iOS for the time they name (Transcription.swift:32-36,
//     59-62, 71-77);
//   - an error is kept by Android for the message for good
//     (TranscribeButton.java:679-685, 741-754), so a record that only
//     waits for the engine stays pending, however long the engine is
//     away, a failure that may pass is queued again rather than answered
//     (Store.Enqueue), and TRANSCRIPTION_FAILED is for what has really
//     failed: a record that cannot be decoded or that the engine rejects.
func (c *TranscriptionCore) MessagesTranscribeAudio(in *mtproto.TLMessagesTranscribeAudio) (*mtproto.Messages_TranscribedAudio, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	at, rec, err := c.voice(in.GetPeer(), in.GetMsgId())
	if err != nil {
		transcribe.CountRequest(transcribe.RequestRefused)
		return nil, err
	}
	cfg := c.svcCtx.Config.Transcribe
	if c.svcCtx.Engine == nil {
		transcribe.CountRequest(transcribe.RequestOff)
		return nil, mtproto.ErrTranscriptionFailed
	}
	id, err := c.svcCtx.Store.Request(c.ctx, at, rec.DocumentId, cfg.Model)
	if err != nil {
		return nil, c.internal("request", err)
	}
	// Nothing is refused for volume: however long the record and however
	// many wait, it joins the queue (the owner's decision of 2026-11-12).
	job, err := c.svcCtx.Store.Enqueue(c.ctx, rec, cfg.Model, cfg.FailedTTL)
	if err != nil {
		return nil, c.internal("enqueue", err)
	}
	if job.State == transcribe.StateDone {
		transcribe.CountRequest(transcribe.RequestCached)
		return transcribed(id, job.Text), nil
	}
	if !job.Final() {
		c.svcCtx.Queued.Broadcast()
		if job, err = c.wait(job, cfg.SyncWait); err != nil {
			return nil, c.internal("wait", err)
		}
	}
	if !job.Final() {
		// Not in time: the update is owed from now on. The job is read
		// again after that, so that one that ended in between is answered
		// here and not left to an update nobody would send.
		if err = c.svcCtx.Store.Await(c.ctx, id, time.Now().Add(cfg.DeliverDelay)); err != nil {
			return nil, c.internal("await", err)
		}
		if job, err = c.svcCtx.Store.Job(c.ctx, rec.DocumentId, cfg.Model); err != nil || job == nil {
			return nil, c.internal("job", err)
		}
		if !job.Final() {
			transcribe.CountRequest(transcribe.RequestPending)
			return mtproto.MakeTLMessagesTranscribedAudio(&mtproto.Messages_TranscribedAudio{
				Pending:         true,
				TranscriptionId: id,
				Text:            "",
			}).To_Messages_TranscribedAudio(), nil
		}
		if _, err = c.svcCtx.Store.Unawait(c.ctx, id); err != nil {
			return nil, c.internal("unawait", err)
		}
	}
	if job.State == transcribe.StateFailed {
		transcribe.CountRequest(transcribe.RequestFailed)
		return nil, mtproto.ErrTranscriptionFailed
	}
	transcribe.CountRequest(transcribe.RequestDone)
	return transcribed(id, job.Text), nil
}

// transcribed is the final answer. trial_remains_* are never set.
func transcribed(id int64, text string) *mtproto.Messages_TranscribedAudio {
	return mtproto.MakeTLMessagesTranscribedAudio(&mtproto.Messages_TranscribedAudio{
		Pending:         false,
		TranscriptionId: id,
		Text:            text,
	}).To_Messages_TranscribedAudio()
}

func (c *TranscriptionCore) internal(step string, err error) error {
	transcribe.CountRequest(transcribe.RequestInternal)
	if err == nil {
		err = errors.New("the job is gone")
	}
	c.Logger.Errorf("messages.transcribeAudio - %s: %v", step, err)
	return mtproto.ErrInternalServerError
}

// wait returns the job as it is once it has ended, or after limit.
func (c *TranscriptionCore) wait(job *transcribe.Job, limit time.Duration) (*transcribe.Job, error) {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	poll := time.NewTicker(waitPoll)
	defer poll.Stop()
	for {
		finished := c.svcCtx.Finished.Wait()
		j, err := c.svcCtx.Store.Job(c.ctx, job.DocumentId, job.Model)
		if err != nil {
			return nil, err
		}
		if j == nil {
			return nil, errors.New("the job is gone")
		}
		if j.Final() {
			return j, nil
		}
		select {
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		case <-deadline.C:
			return j, nil
		case <-finished:
		case <-poll.C:
		}
	}
}
