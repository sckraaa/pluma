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

package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// Actions of a decision on a report.
const (
	ActionDismiss  = "dismiss"
	ActionDelete   = "delete"
	ActionRestrict = "restrict"
	ActionBan      = "ban"
)

// Steps of a decision done after its transaction, in this order: the
// message about a restriction goes before the sessions end.
const (
	StepNotify = "notify"
	StepDelete = "delete"
	StepRevoke = "revoke"
)

// MaxEffectsAttempts is how many times the worker tries the steps of a
// decision. After that the report waits for a moderator to retry it.
const MaxEffectsAttempts = 10

// effectsClaimFor is how long a runner holds the steps of a decision: past
// it, after a crash, another runner takes them over.
const effectsClaimFor = 2 * time.Minute

// maxReason is the longest stored reason of a decision.
const maxReason = 512

// ErrPeerFlood is PEER_FLOOD, the answer to a limited account writing to a
// stranger (mtproto has no such error of its own).
var ErrPeerFlood = status.Error(mtproto.ErrBadRequest, "PEER_FLOOD")

var (
	// ErrReportNotFound is a decision on a report that does not exist.
	ErrReportNotFound = errors.New("report not found")
	// ErrAlreadyDecided is a decision on a report already decided.
	ErrAlreadyDecided = errors.New("report already decided")
	// ErrNoTarget is a restriction of a report that names no user.
	ErrNoTarget = errors.New("report names no user to restrict")
	// ErrNothingToDelete is the deletion from a report with no messages.
	ErrNothingToDelete = errors.New("report has no messages to delete")
	// ErrBadAction is an action that does not exist.
	ErrBadAction = errors.New("unknown action")
)

// Decision is a moderator's decision on a report.
type Decision struct {
	ReportId int64
	Actor    string
	Action   string
	Reason   string
	// UntilDate ends a restriction or ban; 0 is no end.
	UntilDate int32
}

// StepsOf is the steps of action, in order.
func StepsOf(action string) ([]string, error) {
	switch action {
	case ActionDismiss:
		return nil, nil
	case ActionDelete:
		return []string{StepDelete}, nil
	case ActionRestrict:
		return []string{StepNotify}, nil
	case ActionBan:
		return []string{StepNotify, StepRevoke}, nil
	}
	return nil, ErrBadAction
}

func cutReason(reason string) string {
	if utf8.RuneCountInString(reason) <= maxReason {
		return reason
	}
	return string([]rune(reason)[:maxReason])
}

// Audit appends to the journal outside a transaction.
func (d *DAO) Audit(ctx context.Context, actor, action, target, detail string) error {
	if _, err := d.db.Exec(ctx, "INSERT INTO admin_audit (at, actor, action, target, detail) VALUES (?, ?, ?, ?, ?)",
		time.Now().Unix(), actor, action, target, cutDetail(detail)); err != nil {
		return err
	}
	auditLog(ctx, actor, action, target)
	return nil
}

func auditTx(tx *sqlx.Tx, actor, action, target, detail string) error {
	_, err := tx.Exec("INSERT INTO admin_audit (at, actor, action, target, detail) VALUES (?, ?, ?, ?, ?)",
		time.Now().Unix(), actor, action, target, cutDetail(detail))
	return err
}

// auditLog is the copy of a journal row for Loki: who, what, on what. The
// detail stays in the database: it may hold a moderator's words about a
// user.
func auditLog(ctx context.Context, actor, action, target string) {
	logx.WithContext(ctx).Infof("moderation audit: actor=%s action=%s target=%s", actor, action, target)
}

func cutDetail(detail string) string {
	if utf8.RuneCountInString(detail) <= 1024 {
		return detail
	}
	return string([]rune(detail)[:1024])
}

// restrictTx sets the restriction of a user unless a stricter one holds:
// a limit does not soften a ban. changed is false when the user keeps a
// stricter one.
func restrictTx(tx *sqlx.Tx, userId int64, kind int8, until int32, reportId int64, reason, actor string) (changed bool, err error) {
	var cur []Restriction
	if err = tx.QueryRowsPartial(&cur,
		"SELECT user_id, kind, until_date, report_id, reason, created_by FROM moderation_restrictions WHERE user_id = ? FOR UPDATE", userId); err != nil {
		return false, err
	}
	if len(cur) == 1 && cur[0].Active(time.Now().Unix()) && cur[0].Kind > kind {
		return false, nil
	}
	_, err = tx.Exec("INSERT INTO moderation_restrictions (user_id, kind, until_date, report_id, reason, created_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE kind = VALUES(kind), until_date = VALUES(until_date), report_id = VALUES(report_id), reason = VALUES(reason),"+
		" created_by = VALUES(created_by), updated_at = VALUES(updated_at)",
		userId, kind, until, reportId, reason, actor, time.Now().Unix())
	return err == nil, err
}

// Decide applies a decision: in one transaction the report closes, the
// restriction takes effect and the journal gets the row. What happens
// outside the database (Runner) follows from effects_stage, also after a
// restart.
func (d *DAO) Decide(ctx context.Context, dn Decision) error {
	steps, err := StepsOf(dn.Action)
	if err != nil {
		return err
	}
	dn.Reason = cutReason(dn.Reason)
	var audited bool
	err = d.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var rows []Report
		if err := tx.QueryRowsPartial(&rows, "SELECT "+reportColumns+" FROM moderation_reports WHERE id = ? FOR UPDATE", dn.ReportId); err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrReportNotFound
		}
		r := &rows[0]
		if r.Status != StatusOpen {
			return ErrAlreadyDecided
		}
		switch dn.Action {
		case ActionDelete:
			snapshot, err := r.Snapshot()
			if err != nil {
				return err
			}
			if len(snapshot.Messages) == 0 {
				return ErrNothingToDelete
			}
		case ActionRestrict, ActionBan:
			if r.TargetUserId == 0 {
				return ErrNoTarget
			}
			kind := RestrictLimited
			if dn.Action == ActionBan {
				kind = RestrictBanned
			}
			changed, err := restrictTx(tx, r.TargetUserId, kind, dn.UntilDate, r.Id, dn.Reason, dn.Actor)
			if err != nil {
				return err
			}
			if !changed {
				steps = nil
			}
		}
		if _, err := tx.Exec("UPDATE moderation_reports SET status = ?, decision = ?, decided_by = ?, decision_reason = ?, decision_until = ?,"+
			" decided_at = ?, dedup_key = NULL, effects_stage = 0, effects_total = ?, effects_attempts = 0, effects_error = ''"+
			" WHERE id = ?",
			StatusDecided, dn.Action, dn.Actor, dn.Reason, dn.UntilDate, time.Now().Unix(), len(steps), r.Id); err != nil {
			return err
		}
		audited = true
		return auditTx(tx, dn.Actor, "decide_"+dn.Action, fmt.Sprintf("report:%d", r.Id), dn.Reason)
	})
	if err != nil {
		return err
	}
	if audited {
		auditLog(ctx, dn.Actor, "decide_"+dn.Action, fmt.Sprintf("report:%d", dn.ReportId))
	}
	return nil
}

// ApplyDirect restricts or bans a user without a report: pluma-admin, the
// emergency access. changed is false when a stricter restriction holds.
func (d *DAO) ApplyDirect(ctx context.Context, userId int64, kind int8, until int32, reason, actor string) (changed bool, err error) {
	reason = cutReason(reason)
	err = d.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var err error
		if changed, err = restrictTx(tx, userId, kind, until, 0, reason, actor); err != nil {
			return err
		}
		return auditTx(tx, actor, "restrict_direct", fmt.Sprintf("user:%d", userId), reason)
	})
	if err == nil {
		auditLog(ctx, actor, "restrict_direct", fmt.Sprintf("user:%d", userId))
	}
	return changed, err
}

// Lift removes the restriction of a user. lifted is false when it had
// none.
func (d *DAO) Lift(ctx context.Context, userId int64, reason, actor string) (lifted bool, err error) {
	reason = cutReason(reason)
	err = d.db.Transact(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.Exec("DELETE FROM moderation_restrictions WHERE user_id = ?", userId)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		lifted = n > 0
		return auditTx(tx, actor, "restriction_lift", fmt.Sprintf("user:%d", userId), reason)
	})
	if err == nil {
		auditLog(ctx, actor, "restriction_lift", fmt.Sprintf("user:%d", userId))
	}
	return lifted, err
}

// Report returns a report, nil if there is none.
func (d *DAO) Report(ctx context.Context, id int64) (*Report, error) {
	var rows []Report
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT "+reportColumns+" FROM moderation_reports WHERE id = ?", id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// Queue returns the reports of a status, the oldest open first, the newest
// decided first; after pages by id (0 starts).
func (d *DAO) Queue(ctx context.Context, status int8, after int64, limit int) ([]Report, error) {
	query := "SELECT " + reportColumns + " FROM moderation_reports WHERE status = ?"
	args := []any{status}
	switch {
	case status == StatusOpen:
		query += " AND id > ? ORDER BY id ASC LIMIT ?"
		args = append(args, after, limit)
	case after > 0:
		query += " AND id < ? ORDER BY id DESC LIMIT ?"
		args = append(args, after, limit)
	default:
		query += " ORDER BY id DESC LIMIT ?"
		args = append(args, limit)
	}
	var rows []Report
	err := d.db.QueryRowsPartial(ctx, &rows, query, args...)
	return rows, err
}

// ReportsOfTarget lists the reports about a user, the newest first.
func (d *DAO) ReportsOfTarget(ctx context.Context, userId int64, limit int) ([]Report, error) {
	var rows []Report
	err := d.db.QueryRowsPartial(ctx, &rows, "SELECT "+reportColumns+" FROM moderation_reports WHERE target_user_id = ? ORDER BY id DESC LIMIT ?", userId, limit)
	return rows, err
}

// PendingEffects lists the decided reports whose steps are not finished
// and that the worker may still try.
func (d *DAO) PendingEffects(ctx context.Context, limit int) ([]int64, error) {
	var rows []struct {
		Id int64 `db:"id"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT id FROM moderation_reports WHERE status = ? AND effects_stage < effects_total AND effects_attempts < ? AND effects_claim_until < ? ORDER BY id LIMIT ?",
		StatusDecided, MaxEffectsAttempts, time.Now().Unix(), limit); err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.Id
	}
	return ids, nil
}

// ClaimEffects takes the steps of a decision for one runner. A false claim
// means another one holds them or nothing is left to do.
func (d *DAO) ClaimEffects(ctx context.Context, id int64) (bool, error) {
	now := time.Now()
	r, err := d.db.Exec(ctx,
		"UPDATE moderation_reports SET effects_claim_until = ? WHERE id = ? AND status = ? AND effects_stage < effects_total AND effects_claim_until < ?",
		now.Add(effectsClaimFor).Unix(), id, StatusDecided, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// AdvanceEffects records that the step at the index stage is done.
func (d *DAO) AdvanceEffects(ctx context.Context, id int64, stage int8) error {
	_, err := d.db.Exec(ctx, "UPDATE moderation_reports SET effects_stage = ? WHERE id = ?", stage+1, id)
	return err
}

// ReleaseEffects gives up the claim. A failed runner records the error,
// which counts as an attempt; a finished one clears it.
func (d *DAO) ReleaseEffects(ctx context.Context, id int64, failure string) error {
	if failure == "" {
		_, err := d.db.Exec(ctx, "UPDATE moderation_reports SET effects_claim_until = 0, effects_error = '' WHERE id = ?", id)
		return err
	}
	if utf8.RuneCountInString(failure) > 512 {
		failure = string([]rune(failure)[:512])
	}
	_, err := d.db.Exec(ctx,
		"UPDATE moderation_reports SET effects_claim_until = 0, effects_attempts = effects_attempts + 1, effects_error = ? WHERE id = ?", failure, id)
	return err
}

// RetryEffects lets the worker try the steps of a decision again.
func (d *DAO) RetryEffects(ctx context.Context, id int64, actor string) error {
	if _, err := d.db.Exec(ctx, "UPDATE moderation_reports SET effects_attempts = 0 WHERE id = ?", id); err != nil {
		return err
	}
	return d.Audit(ctx, actor, "effects_retry", fmt.Sprintf("report:%d", id), "")
}

// AuditEntry is one row of the journal.
type AuditEntry struct {
	Id     int64  `db:"id" json:"id"`
	At     int64  `db:"at" json:"at"`
	Actor  string `db:"actor" json:"actor"`
	Action string `db:"action" json:"action"`
	Target string `db:"target" json:"target"`
	Detail string `db:"detail" json:"detail"`
}

// AuditLog returns the journal, the newest first, before the id before (0
// starts).
func (d *DAO) AuditLog(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	query := "SELECT id, at, actor, action, target, detail FROM admin_audit"
	args := []any{}
	if before > 0 {
		query += " WHERE id < ?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	var rows []AuditEntry
	err := d.db.QueryRowsPartial(ctx, &rows, query, args...)
	return rows, err
}
