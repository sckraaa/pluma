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

// Package dao reads what the statistics of channels and groups count (task
// 103, ADR 0066, p. 2): every query takes one channel and a range of dates
// over an index (channel, date) and groups the rows into buckets of time in
// the database, so a channel with a long history costs the rows of its
// window, not of its life. Nothing here writes.
//
// A bucket is FLOOR(date / seconds): the day, the hour or the five minutes
// of pkg/statsgraph (Scale.Bucket) for the unix times after 1970.
//
// The accounts of the support ghosts (ADR 0048) are never counted: their
// events are not written (pkg/channelstats), and the rows of the tables
// that do not know them (reactions, forwards, stories) leave them out here.
package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/ghost"
)

type Dao struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Dao {
	return &Dao{db: db}
}

// Count is the number of rows of key K in bucket B.
type Count struct {
	B int64 `db:"b"`
	K int64 `db:"k"`
	N int64 `db:"n"`
}

// KeyCount is the number of rows of the text key K (a reaction) in bucket B.
type KeyCount struct {
	B int64  `db:"b"`
	K string `db:"k"`
	N int64  `db:"n"`
}

// Range is the unix times [From, To) a query counts, in buckets of Bucket
// seconds.
type Range struct {
	From, To int64
	Bucket   int64
}

func (r Range) check() error {
	if r.Bucket <= 0 || r.To < r.From {
		return fmt.Errorf("dao: range [%d, %d) in buckets of %d", r.From, r.To, r.Bucket)
	}
	return nil
}

// notGhost is the condition that the account in column is no ghost.
func notGhost(column string) string {
	return "NOT EXISTS (SELECT 1 FROM users g WHERE g.id = " + column + " AND g.ghost = 1)"
}

// isPost is the condition that the row m of channel_messages is a message,
// not a service message (msg stores the predicate in message_data).
const isPost = "m.deleted = 0 AND JSON_UNQUOTE(JSON_EXTRACT(m.message_data, '$.predicate_name')) = 'message'"

func placeholders(n int) string {
	return "?" + strings.Repeat(", ?", n-1)
}

// Since is since when the events of kind are recorded (stats_collection,
// set once by the migration).
func (d *Dao) Since(ctx context.Context, kind channelstats.Kind) (int64, error) {
	return channelstats.Since(ghost.OnDB(ctx, d.db), kind)
}
