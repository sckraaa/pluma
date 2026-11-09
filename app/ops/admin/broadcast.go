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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicemsg"

	"github.com/zeromicro/go-zero/core/breaker"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A broadcast is a job of the panel: a text from the service account to an
// audience, which the worker sends one by one and resumes after a restart
// from its cursor (admin_broadcasts). A crash between a send and the cursor
// writes one duplicate at most, the same trade as the decisions' steps
// (ADR 0040, p. 10).

// The states of a broadcast.
const (
	broadcastRunning  int8 = 0
	broadcastFinished int8 = 1
	broadcastCanceled int8 = 2
)

// The audiences of a broadcast.
const (
	audienceAll     = "all"
	audienceIds     = "ids"
	audienceChannel = "channel"
)

const (
	broadcastBatch   = 200
	broadcastPause   = 80 * time.Millisecond
	broadcastIdsMax  = 10000
	broadcastLease   = 5 * time.Minute
	broadcastPollGap = 5 * time.Second
	broadcastErrMax  = 512 // characters of admin_broadcasts.last_error
)

// broadcast is a row of admin_broadcasts.
type broadcast struct {
	Id         int64  `db:"id" json:"id"`
	Text       string `db:"text" json:"text"`
	Audience   string `db:"audience" json:"audience"`
	AudienceId int64  `db:"audience_id" json:"audience_id"`
	Ids        string `db:"ids" json:"-"`
	State      int8   `db:"state" json:"state"`
	Total      int64  `db:"total" json:"total"`
	Sent       int64  `db:"sent" json:"sent"`
	Failed     int64  `db:"failed" json:"failed"`
	Cursor     int64  `db:"cursor_id" json:"-"`
	LastError  string `db:"last_error" json:"last_error"`
	CreatedBy  string `db:"created_by" json:"created_by"`
	CreatedAt  int64  `db:"created_at" json:"created_at"`
	DoneAt     int64  `db:"done_at" json:"done_at"`
	ClaimUntil int64  `db:"claim_until" json:"-"`
}

// recipientsAfter lists the next recipients of an audience after a cursor,
// by id.
func (s *store) recipientsAfter(ctx context.Context, audience string, audienceId int64, ids []int64, cursor int64, limit int) ([]int64, error) {
	switch audience {
	case audienceAll:
		var rows []int64
		err := s.db.QueryRowsPartial(ctx, &rows,
			"SELECT id FROM users WHERE id > ? AND deleted = 0 AND is_bot = 0 AND id <> 777000 ORDER BY id LIMIT ?", cursor, limit)
		return rows, err
	case audienceChannel:
		var rows []int64
		err := s.db.QueryRowsPartial(ctx, &rows,
			"SELECT p.user_id FROM channel_participants p JOIN users u ON u.id = p.user_id"+
				" WHERE p.channel_id = ? AND p.state = 0 AND p.user_id > ? AND u.deleted = 0 AND u.is_bot = 0 AND p.user_id <> 777000 ORDER BY p.user_id LIMIT ?",
			audienceId, cursor, limit)
		return rows, err
	case audienceIds:
		i, _ := slices.BinarySearch(ids, cursor+1)
		var page []int64
		for ; i < len(ids) && len(page) < limit; i++ {
			page = append(page, ids[i])
		}
		return page, nil
	}
	return nil, fmt.Errorf("unknown audience %q", audience)
}

// countRecipients tells how many users the audience has at most.
func (s *store) countRecipients(ctx context.Context, audience string, audienceId int64, ids []int64) (int64, error) {
	var n int64
	switch audience {
	case audienceAll:
		err := s.db.QueryRow(ctx, &n, "SELECT COUNT(*) FROM users WHERE deleted = 0 AND is_bot = 0 AND id <> 777000")
		return n, err
	case audienceChannel:
		err := s.db.QueryRow(ctx, &n,
			"SELECT COUNT(*) FROM channel_participants p JOIN users u ON u.id = p.user_id WHERE p.channel_id = ? AND p.state = 0 AND u.deleted = 0 AND u.is_bot = 0 AND p.user_id <> 777000", audienceId)
		return n, err
	case audienceIds:
		return int64(len(ids)), nil
	}
	return 0, fmt.Errorf("unknown audience %q", audience)
}

// broadcastBody is what a request to start one carries.
type broadcastBody struct {
	Text       string `json:"text"`
	Audience   string `json:"audience"`
	AudienceId int64  `json:"audience_id"`
	Ids        string `json:"ids"`
}

// parseAudience checks the request and reads its explicit list of ids.
func parseAudience(b *broadcastBody) ([]int64, error) {
	switch b.Audience {
	case audienceAll:
		return nil, nil
	case audienceChannel:
		if b.AudienceId <= 0 {
			return nil, errors.New("no channel")
		}
		return nil, nil
	case audienceIds:
		var ids []int64
		for _, f := range strings.FieldsFunc(b.Ids, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' || r == '\t' }) {
			id, err := strconv.ParseInt(f, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("bad id %q", f)
			}
			ids = append(ids, id)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if len(ids) == 0 || len(ids) > broadcastIdsMax {
			return nil, errors.New("the list of ids is empty or too long")
		}
		return ids, nil
	}
	return nil, errors.New("bad audience")
}

func (s *server) handleBroadcasts(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.broadcasts(r.Context(), 30)
	if err != nil {
		s.fail(w, r, "broadcasts", err)
		return
	}
	writeJSON(w, map[string]any{"broadcasts": rows})
}

// handleBroadcastPreview tells how many users a start would reach.
func (s *server) handleBroadcastPreview(w http.ResponseWriter, r *http.Request, a *actor) {
	req, ok := s.readBody(w, r)
	if !ok {
		return
	}
	var body broadcastBody
	if err := parsePayload(req.Payload, &body); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	ids, err := parseAudience(&body)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.store.countRecipients(r.Context(), body.Audience, body.AudienceId, ids)
	if err != nil {
		s.fail(w, r, "broadcast preview", err)
		return
	}
	writeJSON(w, map[string]any{"count": n})
}

func (s *server) handleBroadcastStart(w http.ResponseWriter, r *http.Request, a *actor) {
	var body broadcastBody
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	n := utf8.RuneCountInString(strings.TrimSpace(body.Text))
	if n == 0 || n > messageLengthMax {
		httpError(w, http.StatusBadRequest, "bad text")
		return
	}
	ids, err := parseAudience(&body)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	total, err := s.store.countRecipients(r.Context(), body.Audience, body.AudienceId, ids)
	if err != nil {
		s.fail(w, r, "broadcast count", err)
		return
	}
	if total == 0 {
		httpError(w, http.StatusConflict, "no recipients")
		return
	}
	idsJSON := ""
	if body.Audience == audienceIds {
		data, err := json.Marshal(ids)
		if err != nil {
			s.fail(w, r, "broadcast ids", err)
			return
		}
		idsJSON = string(data)
	}
	id, err := s.store.createBroadcast(r.Context(), &broadcast{
		Text: strings.TrimSpace(body.Text), Audience: body.Audience, AudienceId: body.AudienceId, Ids: idsJSON,
		Total: total, CreatedBy: a.login, CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		s.fail(w, r, "broadcast", err)
		return
	}
	// The journal has the audience and the numbers, the text stays in the
	// row of the broadcast. The row exists stopped until the journal has it:
	// the journal names its id, and what it did not record does not go out.
	if !s.journal(w, r, a, "broadcast_start", fmt.Sprintf("broadcast:%d", id), fmt.Sprintf("audience=%s total=%d", body.Audience, total)) {
		return
	}
	if err := s.store.startBroadcast(r.Context(), id); err != nil {
		s.fail(w, r, "broadcast start", err)
		return
	}
	writeJSON(w, map[string]any{"id": id})
}

func (s *server) handleBroadcastCancel(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct{}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if !s.journal(w, r, a, "broadcast_cancel", fmt.Sprintf("broadcast:%d", id), "") {
		return
	}
	stopped, err := s.store.cancelBroadcast(r.Context(), id)
	if err != nil {
		s.fail(w, r, "broadcast cancel", err)
		return
	}
	if !stopped {
		httpError(w, http.StatusConflict, "the broadcast is over")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// store of the broadcasts.

const broadcastColumns = "id, `text`, audience, audience_id, ids, state, total, sent, failed, cursor_id, last_error, created_by, created_at, done_at, claim_until"

// createBroadcast writes a broadcast stopped, for the worker not to take it
// before startBroadcast.
func (s *store) createBroadcast(ctx context.Context, b *broadcast) (int64, error) {
	r, err := s.db.Exec(ctx, "INSERT INTO admin_broadcasts (`text`, audience, audience_id, ids, state, total, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		b.Text, b.Audience, b.AudienceId, b.Ids, broadcastCanceled, b.Total, b.CreatedBy, b.CreatedAt)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *store) startBroadcast(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "UPDATE admin_broadcasts SET state = ? WHERE id = ? AND state = ?", broadcastRunning, id, broadcastCanceled)
	return err
}

func (s *store) broadcasts(ctx context.Context, limit int) ([]broadcast, error) {
	var rows []broadcast
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+broadcastColumns+" FROM admin_broadcasts ORDER BY id DESC LIMIT ?", limit)
	return rows, err
}

func (s *store) cancelBroadcast(ctx context.Context, id int64) (bool, error) {
	r, err := s.db.Exec(ctx, "UPDATE admin_broadcasts SET state = ?, done_at = ? WHERE id = ? AND state = ?", broadcastCanceled, time.Now().Unix(), id, broadcastRunning)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// claimBroadcast takes the oldest running broadcast for broadcastLease, nil
// when none is free: a second process, or the one before a crash, must let
// its lease expire.
func (s *store) claimBroadcast(ctx context.Context) (*broadcast, error) {
	now := time.Now().Unix()
	var rows []broadcast
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+broadcastColumns+" FROM admin_broadcasts WHERE state = ? AND claim_until <= ? ORDER BY id LIMIT 1", broadcastRunning, now); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	b := rows[0]
	r, err := s.db.Exec(ctx, "UPDATE admin_broadcasts SET claim_until = ? WHERE id = ? AND state = ? AND claim_until = ?",
		now+int64(broadcastLease.Seconds()), b.Id, broadcastRunning, b.ClaimUntil)
	if err != nil {
		return nil, err
	}
	if n, err := r.RowsAffected(); err != nil || n == 0 {
		return nil, err
	}
	return &b, nil
}

// advanceBroadcast records one recipient done, renewing the lease.
func (s *store) advanceBroadcast(ctx context.Context, id, cursor int64, failed bool, lastError string) error {
	failedN := 0
	sentN := 1
	if failed {
		failedN, sentN = 1, 0
	}
	_, err := s.db.Exec(ctx,
		"UPDATE admin_broadcasts SET cursor_id = ?, sent = sent + ?, failed = failed + ?, last_error = IF(? = '', last_error, ?), claim_until = ? WHERE id = ?",
		cursor, sentN, failedN, lastError, lastError, time.Now().Add(broadcastLease).Unix(), id)
	return err
}

func (s *store) finishBroadcast(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "UPDATE admin_broadcasts SET state = ?, done_at = ?, claim_until = 0 WHERE id = ? AND state = ?",
		broadcastFinished, time.Now().Unix(), id, broadcastRunning)
	return err
}

// broadcastState reads a broadcast's state, to stop a canceled one between
// sends.
func (s *store) broadcastState(ctx context.Context, id int64) (int8, error) {
	var state int8
	err := s.db.QueryRow(ctx, &state, "SELECT state FROM admin_broadcasts WHERE id = ?", id)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	return state, nil
}

// serviceDown tells a message service that did not answer (a restart, the
// client's circuit breaker) from an answer refusing the recipient.
func serviceDown(err error) bool {
	if errors.Is(err, breaker.ErrServiceUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
		return true
	}
	return false
}

// cutError fits an error into admin_broadcasts.last_error.
func cutError(s string) string {
	if r := []rune(s); len(r) > broadcastErrMax {
		return string(r[:broadcastErrMax])
	}
	return s
}

// runBroadcasts sends the running broadcasts, one at a time, until ctx ends.
func runBroadcasts(ctx context.Context, st *store, msg servicemsg.Msg) {
	for {
		if err := sendNext(ctx, st, msg); err != nil {
			logx.Errorf("admin: broadcast: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(broadcastPollGap):
		}
	}
}

// sendNext takes one broadcast and sends what is left of it, stopping when
// it is canceled.
func sendNext(ctx context.Context, st *store, msg servicemsg.Msg) error {
	b, err := st.claimBroadcast(ctx)
	if err != nil || b == nil {
		return err
	}
	var ids []int64
	if b.Audience == audienceIds {
		if err = json.Unmarshal([]byte(b.Ids), &ids); err != nil {
			return fmt.Errorf("broadcast %d: ids: %w", b.Id, err)
		}
	}
	cursor := b.Cursor
	for {
		page, err := st.recipientsAfter(ctx, b.Audience, b.AudienceId, ids, cursor, broadcastBatch)
		if err != nil {
			return fmt.Errorf("broadcast %d: recipients: %w", b.Id, err)
		}
		if len(page) == 0 {
			return st.finishBroadcast(ctx, b.Id)
		}
		for _, userId := range page {
			if state, err := st.broadcastState(ctx, b.Id); err != nil {
				return err
			} else if state != broadcastRunning {
				return nil
			}
			var failed bool
			var lastError string
			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := servicemsg.Send(sendCtx, msg, userId, time.Now(), b.Text, nil)
			cancel()
			if err != nil {
				if serviceDown(err) {
					// Not this recipient's failure: the broadcast stays where
					// it is and goes on when its lease ends.
					return fmt.Errorf("broadcast %d: the message service: %w", b.Id, err)
				}
				failed = true
				lastError = cutError(fmt.Sprintf("user %d: %v", userId, err))
				logx.Errorf("admin: broadcast %d: %s", b.Id, lastError)
			}
			if err := st.advanceBroadcast(ctx, b.Id, userId, failed, lastError); err != nil {
				return err
			}
			cursor = userId
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(broadcastPause):
			}
		}
	}
}
