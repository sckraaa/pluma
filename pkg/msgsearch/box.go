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

package msgsearch

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// MaxLimit is the most messages one page of a search gives
// (core.telegram.org/method/messages.search allows limit up to 100).
const MaxLimit = 100

// Querier reads rows: *sqlx.DB and a transaction.
type Querier interface {
	QueryRowsPartial(ctx context.Context, v interface{}, query string, args ...interface{}) error
}

// Execer writes rows.
type Execer interface {
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// Page is the paging of messages.search: add_offset messages below
// offset_id are skipped, a negative one takes that many newer ones; min_id
// and max_id bound the ids.
type Page struct {
	OffsetId, AddOffset, Limit, MaxId, MinId int32
}

// BoxQuery is what a search of a user's box asks for: one dialog, or every
// one with PeerType PEER_EMPTY; every other part is optional.
type BoxQuery struct {
	UserId   int64
	PeerType int32
	PeerId   int64

	Q                string
	Kind             int32
	FromId           int64
	HasSaved         bool
	SavedPeerType    int32
	SavedPeerId      int64
	MinDate, MaxDate int32

	// Extra narrows the search further: the kinds of chats of a global
	// search, its folders.
	Extra Where
	// Cursor is where a page of a merged list starts: it narrows the page
	// and not the count of all that match.
	Cursor Where
}

var boxColumns = Columns{}

// Source is one queryable list of messages: the rows of Table with their
// id in the column Id and the conditions Where.
type Source struct {
	Table string
	Id    string
	Where string
	Args  []any
}

// Source is the list q asks for.
func (q BoxQuery) Source() Source {
	var w Where
	if q.PeerType != mtproto.PEER_EMPTY {
		did := mtproto.MakeDialogId(q.UserId, q.PeerType, q.PeerId)
		w.And("dialog_id1 = ?", did.A)
		w.And("dialog_id2 = ?", did.B)
	}
	boxColumns.Kind(&w, q.Kind)
	if q.Kind == KindMyMentions {
		w.And("mentioned = 1")
	}
	boxColumns.Text(&w, q.Kind, q.Q)
	if q.FromId != 0 {
		w.And("sender_user_id = ?", q.FromId)
	}
	if q.HasSaved {
		w.And("saved_peer_type = ?", q.SavedPeerType)
		w.And("saved_peer_id = ?", q.SavedPeerId)
	}
	boxColumns.Dates(&w, q.MinDate, q.MaxDate)
	return Source{
		Table: "messages",
		Id:    "user_message_box_id",
		Where: "user_id = ? AND deleted = 0" + w.SQL + q.Extra.SQL,
		Args:  append(append([]any{q.UserId}, w.Args...), q.Extra.Args...),
	}
}

// SearchBox is a page of the user's box that matches q, newest first, as
// box ids, with the count of all that match. A limit of 0 gives the count
// alone.
func SearchBox(ctx context.Context, db Querier, q BoxQuery, pg Page) ([]int32, int32, error) {
	src := q.Source()
	limit := min(max(pg.Limit, 0), MaxLimit)
	var ids []int32
	if limit > 0 {
		var err error
		from := src
		from.Where += q.Cursor.SQL
		from.Args = append(slices.Clone(src.Args), q.Cursor.Args...)
		if ids, err = page(ctx, db, from, pg, limit); err != nil {
			return nil, 0, err
		}
	}
	count, err := Count(ctx, db, src)
	if err != nil {
		return nil, 0, err
	}
	if ids == nil {
		ids = []int32{}
	}
	return ids, count, nil
}

// Count is how many messages src has.
func Count(ctx context.Context, db Querier, src Source) (int32, error) {
	var n []int32
	if err := db.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM "+src.Table+" WHERE "+src.Where, src.Args...); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

func page(ctx context.Context, db Querier, src Source, pg Page, limit int32) ([]int32, error) {
	addOffset := max(pg.AddOffset, -limit)
	offsetId := pg.OffsetId
	if offsetId <= 0 {
		offsetId = math.MaxInt32
	}
	upper := int32(math.MaxInt32)
	if pg.MaxId > 0 {
		upper = pg.MaxId
	}
	where := src.Where + " AND " + src.Id + " > ? AND " + src.Id + " < ?"
	args := append(slices.Clone(src.Args), max(pg.MinId, 0), upper)

	var ids []int32
	if addOffset < 0 {
		var newer []int32
		if err := db.QueryRowsPartial(ctx, &newer,
			"SELECT "+src.Id+" FROM "+src.Table+" WHERE "+where+" AND "+src.Id+" >= ? ORDER BY "+src.Id+" LIMIT ?",
			append(slices.Clone(args), offsetId, -addOffset)...); err != nil {
			return nil, err
		}
		slices.Reverse(newer)
		ids = newer
	}
	if rest := limit - int32(len(ids)); rest > 0 {
		var older []int32
		if err := db.QueryRowsPartial(ctx, &older,
			"SELECT "+src.Id+" FROM "+src.Table+" WHERE "+where+" AND "+src.Id+" < ? ORDER BY "+src.Id+" DESC LIMIT ? OFFSET ?",
			append(slices.Clone(args), offsetId, rest, max(addOffset, 0))...); err != nil {
			return nil, err
		}
		ids = append(ids, older...)
	}
	return ids, nil
}

// CalendarDays is the most days one answer of
// messages.getSearchResultsCalendar has.
const CalendarDays = 100

// Day is the messages of src on one UTC day: the first second of the day,
// the ids and the earliest date.
type Day struct {
	Date    int32 `db:"d"`
	MinId   int32 `db:"min_id"`
	MaxId   int32 `db:"max_id"`
	MinDate int32 `db:"min_date"`
	Count   int32 `db:"n"`
}

// Calendar is the days of src, newest first, up to CalendarDays of them,
// from the messages below offset_id (and not after offset_date), with the
// count of all of src. A day is always whole: the next page starts from
// the oldest day's min_id. Clients ask for the messages of a calendar with
// the min_msg_id of the last answer (tdesktop data_search_calendar.cpp,
// iOS SparseMessageCalendar, Android CalendarActivity).
func Calendar(ctx context.Context, db Querier, src Source, offsetId, offsetDate int32) ([]Day, int32, error) {
	where := src.Where
	args := slices.Clone(src.Args)
	if offsetId > 0 {
		where += " AND " + src.Id + " < ?"
		args = append(args, offsetId)
	}
	if offsetDate > 0 {
		where += " AND date2 <= ?"
		args = append(args, offsetDate)
	}
	var days []Day
	if err := db.QueryRowsPartial(ctx, &days,
		"SELECT FLOOR(date2 / 86400) * 86400 AS d, MIN("+src.Id+") AS min_id, MAX("+src.Id+") AS max_id,"+
			" MIN(date2) AS min_date, COUNT(*) AS n FROM "+src.Table+" WHERE "+where+" GROUP BY d ORDER BY d DESC LIMIT ?",
		append(args, CalendarDays)...); err != nil {
		return nil, 0, err
	}
	total, err := Count(ctx, db, src)
	return days, total, err
}

// PositionsMax is the most positions one answer of
// messages.getSearchResultsPositions has (iOS asks for 1000:
// SparseMessageList.swift).
const PositionsMax = 1000

// Position is one message of src with its 0-based place in the whole of
// src, newest first.
type Position struct {
	Id     int32 `db:"id"`
	Date   int32 `db:"date2"`
	Offset int32 `db:"pos"`
}

// Positions are up to limit messages of src spread evenly over it, from
// the place of offset_id down, with the count of all of src: the sparse
// positions the shared media scrollbars use
// (core.telegram.org/method/messages.getSearchResultsPositions).
func Positions(ctx context.Context, db Querier, src Source, offsetId, limit int32) ([]Position, int32, error) {
	limit = min(max(limit, 1), PositionsMax)
	total, err := Count(ctx, db, src)
	if err != nil || total == 0 {
		return []Position{}, total, err
	}
	step := max((total+limit-1)/limit, 1)
	inner := "SELECT " + src.Id + " AS id, date2, ROW_NUMBER() OVER (ORDER BY " + src.Id + " DESC) - 1 AS pos FROM " + src.Table + " WHERE " + src.Where
	args := slices.Clone(src.Args)
	where := "MOD(pos, ?) = 0"
	args = append(args, step)
	if offsetId > 0 {
		where += " AND id < ?"
		args = append(args, offsetId)
	}
	var out []Position
	err = db.QueryRowsPartial(ctx, &out, "SELECT id, date2, pos FROM ("+inner+") ranked WHERE "+where+" ORDER BY id DESC LIMIT ?",
		append(args, limit)...)
	return out, total, err
}

// PeerSet is a list of chats by type.
type PeerSet struct {
	Users, Chats, Channels []int64
}

// ArchivedPeers are the chats of the user's archive (folder 1).
func ArchivedPeers(ctx context.Context, db Querier, userId int64) (PeerSet, error) {
	type row struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
	}
	var rows []row
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT peer_type, peer_id FROM dialogs WHERE user_id = ? AND folder_id = 1 AND deleted = 0", userId); err != nil {
		return PeerSet{}, err
	}
	var s PeerSet
	for _, r := range rows {
		switch r.PeerType {
		case mtproto.PEER_USER:
			s.Users = append(s.Users, r.PeerId)
		case mtproto.PEER_CHAT:
			s.Chats = append(s.Chats, r.PeerId)
		case mtproto.PEER_CHANNEL:
			s.Channels = append(s.Channels, r.PeerId)
		}
	}
	return s, nil
}

// NarrowBox narrows box rows to the user and basic-group dialogs of s
// (include), or to all the others.
func (s PeerSet) NarrowBox(w *Where, include bool) {
	var parts []string
	var args []any
	add := func(peerType int32, ids []int64) {
		if len(ids) == 0 {
			return
		}
		parts = append(parts, "(peer_type = ? AND peer_id IN ("+marks(len(ids))+"))")
		args = append(args, peerType)
		for _, id := range ids {
			args = append(args, id)
		}
	}
	add(mtproto.PEER_USER, s.Users)
	add(mtproto.PEER_CHAT, s.Chats)
	switch {
	case len(parts) == 0 && include:
		w.And("1 = 0")
	case len(parts) == 0:
	case include:
		w.And("("+strings.Join(parts, " OR ")+")", args...)
	default:
		w.And("NOT ("+strings.Join(parts, " OR ")+")", args...)
	}
}

func marks(n int) string {
	return "?" + strings.Repeat(", ?", n-1)
}
