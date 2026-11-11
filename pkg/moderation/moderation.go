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

// Package moderation is the storage of reports and restrictions and the
// decisions on them (task 025, ADR 0040). The reports are written by bff,
// the decisions are made in the internal panel (app/ops/admin) or with
// pluma-admin, and the restrictions are read where a user acts: msg for
// sending, bff for logging in.
package moderation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Kinds of a report, by the method that made it.
const (
	KindMessage      int8 = 1 // messages.report
	KindPeer         int8 = 2 // account.reportPeer
	KindProfilePhoto int8 = 3 // account.reportProfilePhoto
	KindSpam         int8 = 4 // messages.reportSpam
	KindChannelSpam  int8 = 5 // channels.reportSpam
	// KindAntispamFalsePositive: a group's admin says the antispam deleted
	// a message that was no spam (channels.reportAntiSpamFalsePositive,
	// task 082); the target is the message's sender.
	KindAntispamFalsePositive int8 = 6
)

// Statuses of a report.
const (
	StatusOpen    int8 = 0
	StatusDecided int8 = 1
)

// Kinds of a restriction. A higher one is the stricter.
const (
	RestrictLimited int8 = 1
	RestrictBanned  int8 = 2
)

// ServiceUserId is the service notifications account (777000), a support
// account (ADR 0026), the one chat a restricted user always has.
const ServiceUserId = 777000

const (
	// MaxCommentChars is the longest comment of a report: the field of
	// Android has 1024 (ReportBottomSheet, EditTextCell).
	MaxCommentChars = 1024
	// MaxReportMessages is the most messages a report keeps.
	MaxReportMessages = 100
	// ReportsPerHour is the most reports one user makes an hour.
	ReportsPerHour = 60
)

// SnapshotUser is what a moderator sees of a user: no phone (ADR 0040,
// minimum of data).
type SnapshotUser struct {
	Id        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	About     string `json:"about,omitempty"`
	Bot       bool   `json:"bot,omitempty"`
}

// SnapshotMedia locates the media of a message for the panel: enough to
// build its InputFileLocation.
type SnapshotMedia struct {
	Kind       string `json:"kind"`
	PhotoId    int64  `json:"photo_id,omitempty"`
	DocumentId int64  `json:"document_id,omitempty"`
	AccessHash int64  `json:"access_hash,omitempty"`
	FileRef    []byte `json:"file_ref,omitempty"`
	ThumbSize  string `json:"thumb_size,omitempty"`
	Mime       string `json:"mime,omitempty"`
}

// SnapshotMessage is one reported message at the moment of the report.
type SnapshotMessage struct {
	Id     int32          `json:"id"`
	Date   int32          `json:"date"`
	FromId int64          `json:"from_id,omitempty"`
	Text   string         `json:"text,omitempty"`
	Media  *SnapshotMedia `json:"media,omitempty"`
}

// Snapshot is what a report was about at the moment it was made.
type Snapshot struct {
	PeerTitle string            `json:"peer_title,omitempty"`
	Users     []SnapshotUser    `json:"users,omitempty"`
	Messages  []SnapshotMessage `json:"messages,omitempty"`
	PhotoId   int64             `json:"photo_id,omitempty"`
}

// Report is a report of a user.
type Report struct {
	Id              int64  `db:"id" json:"id"`
	ReporterUserId  int64  `db:"reporter_user_id" json:"reporter_user_id"`
	Kind            int8   `db:"kind" json:"kind"`
	PeerType        int32  `db:"peer_type" json:"peer_type"`
	PeerId          int64  `db:"peer_id" json:"peer_id"`
	TargetUserId    int64  `db:"target_user_id" json:"target_user_id"`
	Reason          string `db:"reason" json:"reason"`
	Comment         string `db:"comment" json:"comment"`
	SnapshotJSON    string `db:"snapshot" json:"-"`
	Status          int8   `db:"status" json:"status"`
	Decision        string `db:"decision" json:"decision,omitempty"`
	DecidedBy       string `db:"decided_by" json:"decided_by,omitempty"`
	DecisionReason  string `db:"decision_reason" json:"decision_reason,omitempty"`
	DecisionUntil   int32  `db:"decision_until" json:"decision_until,omitempty"`
	DecidedAt       int64  `db:"decided_at" json:"decided_at,omitempty"`
	EffectsStage    int8   `db:"effects_stage" json:"effects_stage"`
	EffectsTotal    int8   `db:"effects_total" json:"effects_total"`
	EffectsAttempts int32  `db:"effects_attempts" json:"effects_attempts"`
	EffectsError    string `db:"effects_error" json:"effects_error,omitempty"`
	CreatedAt       int64  `db:"created_at" json:"created_at"`
}

// reportColumns are the columns of a Report, in a SELECT.
const reportColumns = "id, reporter_user_id, kind, peer_type, peer_id, target_user_id, reason, comment, snapshot, status," +
	" decision, decided_by, decision_reason, decision_until, decided_at, effects_stage, effects_total, effects_attempts, effects_error, created_at"

// Snapshot decodes the snapshot of the report.
func (r *Report) Snapshot() (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal([]byte(r.SnapshotJSON), &s); err != nil {
		return nil, fmt.Errorf("snapshot of report %d: %w", r.Id, err)
	}
	return &s, nil
}

// Restriction is a restriction of a user.
type Restriction struct {
	UserId    int64  `db:"user_id" json:"user_id"`
	Kind      int8   `db:"kind" json:"kind"`
	UntilDate int32  `db:"until_date" json:"until_date"`
	ReportId  int64  `db:"report_id" json:"report_id,omitempty"`
	Reason    string `db:"reason" json:"reason,omitempty"`
	CreatedBy string `db:"created_by" json:"created_by,omitempty"`
}

// Active tells whether the restriction still holds at now.
func (r *Restriction) Active(now int64) bool {
	return r != nil && (r.UntilDate == 0 || int64(r.UntilDate) > now)
}

// DAO reads and writes the moderation tables.
type DAO struct {
	db *sqlx.DB
}

// New returns a DAO on db.
func New(db *sqlx.DB) *DAO {
	return &DAO{db: db}
}

// TruncateComment cuts a comment to MaxCommentChars characters.
func TruncateComment(comment string) string {
	if utf8.RuneCountInString(comment) <= MaxCommentChars {
		return comment
	}
	return string([]rune(comment)[:MaxCommentChars])
}

// dedupKey identifies one report of the same thing by the same reporter.
func dedupKey(r *Report, ids []int32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%d|%d|%d|%s", r.ReporterUserId, r.Kind, r.PeerType, r.PeerId, r.Reason)
	for _, id := range ids {
		fmt.Fprintf(&b, "|%d", id)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ReportsSince counts the reports reporter made after the time since.
func (d *DAO) ReportsSince(ctx context.Context, reporter, since int64) (int, error) {
	var n int
	err := d.db.QueryRow(ctx, &n, "SELECT COUNT(*) FROM moderation_reports WHERE reporter_user_id = ? AND created_at > ?", reporter, since)
	return n, err
}

// AddReport stores a report. The same report of the same reporter while it
// is open keeps its first snapshot and takes the newer comment, if any:
// clients repeat a request whose answer was lost.
func (d *DAO) AddReport(ctx context.Context, r *Report, ids []int32, snapshot *Snapshot) error {
	query, args, err := addReport(r, ids, snapshot)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(ctx, query, args...)
	return err
}

// AddReportTx is AddReport in tx, for a report that goes with the other
// writes of a transaction or not at all.
func AddReportTx(tx *sqlx.Tx, r *Report, ids []int32, snapshot *Snapshot) error {
	query, args, err := addReport(r, ids, snapshot)
	if err != nil {
		return err
	}
	_, err = tx.Exec(query, args...)
	return err
}

func addReport(r *Report, ids []int32, snapshot *Snapshot) (string, []any, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil, fmt.Errorf("snapshot: %w", err)
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().Unix()
	}
	return "INSERT INTO moderation_reports (reporter_user_id, kind, peer_type, peer_id, target_user_id, reason, comment, snapshot, dedup_key, created_at)" +
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)" +
			" ON DUPLICATE KEY UPDATE comment = IF(VALUES(comment) = '', comment, VALUES(comment))",
		[]any{r.ReporterUserId, r.Kind, r.PeerType, r.PeerId, r.TargetUserId, r.Reason, TruncateComment(r.Comment), string(data), dedupKey(r, ids), r.CreatedAt},
		nil
}

// Restriction returns the active restriction of the user, nil if none.
func (d *DAO) Restriction(ctx context.Context, userId int64) (*Restriction, error) {
	var rows []Restriction
	if err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, kind, until_date, report_id, reason, created_by FROM moderation_restrictions WHERE user_id = ?", userId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || !rows[0].Active(time.Now().Unix()) {
		return nil, nil
	}
	return &rows[0], nil
}

// PhoneBanned tells whether the account of the phone is banned now.
func (d *DAO) PhoneBanned(ctx context.Context, phone string) (bool, error) {
	var n int
	err := d.db.QueryRow(ctx, &n,
		"SELECT COUNT(*) FROM users u JOIN moderation_restrictions r ON r.user_id = u.id"+
			" WHERE u.phone = ? AND r.kind = ? AND (r.until_date = 0 OR r.until_date > ?)",
		phone, RestrictBanned, time.Now().Unix())
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// singleInt64 reads the first value of a one-column query, 0 if no row.
func (d *DAO) singleInt64(ctx context.Context, query string, arg any) (int64, error) {
	var rows []struct {
		V int64 `db:"v"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows, query, arg); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].V, nil
}

// ChannelCreator returns the creator of the channel, 0 if there is none.
func (d *DAO) ChannelCreator(ctx context.Context, channelId int64) (int64, error) {
	return d.singleInt64(ctx, "SELECT creator_user_id AS v FROM channels WHERE id = ?", channelId)
}

// ChatTitle returns the title of a basic group, "" if there is none.
func (d *DAO) ChatTitle(ctx context.Context, chatId int64) (string, error) {
	return d.title(ctx, "SELECT title AS v FROM chats WHERE id = ?", chatId)
}

// ChannelTitle returns the title of a channel, "" if there is none.
func (d *DAO) ChannelTitle(ctx context.Context, channelId int64) (string, error) {
	return d.title(ctx, "SELECT title AS v FROM channels WHERE id = ?", channelId)
}

func (d *DAO) title(ctx context.Context, query string, id int64) (string, error) {
	var rows []struct {
		V string `db:"v"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows, query, id); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].V, nil
}
