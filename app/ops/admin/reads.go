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
	"sort"
	"strconv"
	"strings"
	"time"
)

// What the panel reads of the other services' tables: the database user has
// only SELECT on these columns (docs/ops/admin-panel.md). Writes go through
// the services.

// userRow is a user of the panel's lists and cards: never a message.
type userRow struct {
	Id        int64  `db:"id" json:"id"`
	FirstName string `db:"first_name" json:"first_name"`
	LastName  string `db:"last_name" json:"last_name"`
	Username  string `db:"username" json:"username"`
	Phone     string `db:"phone" json:"phone"`
	About     string `db:"about" json:"about"`
	PhotoId   int64  `db:"photo_id" json:"photo_id"`
	IsBot     bool   `db:"is_bot" json:"is_bot"`
	Deleted   bool   `db:"deleted" json:"deleted"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
	// The restriction in force, if any (kind 0: none).
	RestrictKind  int8  `db:"restrict_kind" json:"restrict_kind"`
	RestrictUntil int32 `db:"restrict_until" json:"restrict_until"`
	// LastSeen is the last presence, unix seconds, 0 if never.
	LastSeen int64 `db:"last_seen" json:"last_seen"`
}

const userColumns = "u.id, u.first_name, u.last_name, u.username, u.phone, u.about, u.photo_id, u.is_bot, u.deleted," +
	" UNIX_TIMESTAMP(u.created_at) AS created_at, COALESCE(r.kind, 0) AS restrict_kind, COALESCE(r.until_date, 0) AS restrict_until," +
	" COALESCE((SELECT MAX(p.last_seen_at) FROM user_presences p WHERE p.user_id = u.id), 0) AS last_seen"

const userFrom = " FROM users u LEFT JOIN moderation_restrictions r ON r.user_id = u.id"

func (s *store) user(ctx context.Context, id int64) (*userRow, error) {
	var rows []userRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+userColumns+userFrom+" WHERE u.id = ?", id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// likeEscape escapes the wildcards of a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// userSearchWhere is the condition of a search, by phone or id for digits,
// by @username, otherwise by name.
func userSearchWhere(q string) (string, []any) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return "", nil
	case strings.HasPrefix(q, "@"):
		return " WHERE u.username LIKE ? ESCAPE '\\\\'", []any{likeEscape(q[1:]) + "%"}
	}
	digits := strings.TrimPrefix(q, "+")
	if id, err := strconv.ParseInt(digits, 10, 64); err == nil && digits != "" {
		return " WHERE u.id = ? OR u.phone LIKE ?", []any{id, digits + "%"}
	}
	pattern := "%" + likeEscape(q) + "%"
	return " WHERE (u.first_name LIKE ? ESCAPE '\\\\' OR u.last_name LIKE ? ESCAPE '\\\\' OR u.username LIKE ? ESCAPE '\\\\')", []any{pattern, pattern, likeEscape(q) + "%"}
}

// searchUsers finds users by the query, the newest first; an empty query
// lists the newest.
func (s *store) searchUsers(ctx context.Context, q string, limit int) ([]userRow, error) {
	where, args := userSearchWhere(q)
	args = append(args, limit)
	var rows []userRow
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+userColumns+userFrom+where+" ORDER BY u.id DESC LIMIT ?", args...)
	return rows, err
}

// peerRow is a channel, supergroup or basic group of the panel's lists.
type peerRow struct {
	Kind         string `db:"kind" json:"kind"` // channel or chat
	Id           int64  `db:"id" json:"id"`
	Title        string `db:"title" json:"title"`
	Username     string `db:"username" json:"username"`
	Broadcast    bool   `db:"broadcast" json:"broadcast"`
	Megagroup    bool   `db:"megagroup" json:"megagroup"`
	CreatorId    int64  `db:"creator_user_id" json:"creator_user_id"`
	Participants int32  `db:"participants" json:"participants"`
	Blocked      bool   `db:"blocked" json:"blocked"`
	Deleted      bool   `db:"deleted" json:"deleted"`
	Date         int64  `db:"date" json:"date"`
}

const channelSelect = "SELECT 'channel' AS kind, c.id, c.title, COALESCE(u.username, '') AS username, c.broadcast, c.megagroup," +
	" c.creator_user_id, c.participants_count AS participants, c.blocked, c.deleted, c.date" +
	" FROM channels c LEFT JOIN username u ON u.peer_type = 4 AND u.peer_id = c.id AND u.deleted = 0"

const chatSelect = "SELECT 'chat' AS kind, c.id, c.title, '' AS username, FALSE AS broadcast, FALSE AS megagroup," +
	" c.creator_user_id, c.participant_count AS participants, c.blocked, (c.deactivated AND NOT c.blocked) AS deleted, c.date" +
	" FROM chats c"

func (s *store) peer(ctx context.Context, kind string, id int64) (*peerRow, error) {
	query := channelSelect + " WHERE c.id = ?"
	if kind == peerChat {
		query = chatSelect + " WHERE c.id = ?"
	}
	var rows []peerRow
	if err := s.db.QueryRowsPartial(ctx, &rows, query, id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// searchPeers finds channels and groups by id, @username or title, the
// newest first.
func (s *store) searchPeers(ctx context.Context, q string, limit int) ([]peerRow, error) {
	q = strings.TrimSpace(q)
	var (
		chWhere, ctWhere string
		chArgs, ctArgs   []any
	)
	switch {
	case q == "":
	case strings.HasPrefix(q, "@"):
		chWhere, chArgs = " WHERE u.username LIKE ? ESCAPE '\\\\'", []any{likeEscape(q[1:]) + "%"}
		ctWhere = " WHERE 1 = 0"
	default:
		pattern := "%" + likeEscape(q) + "%"
		if id, err := strconv.ParseInt(q, 10, 64); err == nil {
			chWhere, chArgs = " WHERE c.id = ? OR c.title LIKE ? ESCAPE '\\\\'", []any{id, pattern}
			ctWhere, ctArgs = chWhere, chArgs
		} else {
			chWhere, chArgs = " WHERE c.title LIKE ? ESCAPE '\\\\' OR u.username LIKE ? ESCAPE '\\\\'", []any{pattern, likeEscape(q) + "%"}
			ctWhere, ctArgs = " WHERE c.title LIKE ? ESCAPE '\\\\'", []any{pattern}
		}
	}
	var channels, chats []peerRow
	if err := s.db.QueryRowsPartial(ctx, &channels, channelSelect+chWhere+" ORDER BY c.id DESC LIMIT ?", append(chArgs, limit)...); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowsPartial(ctx, &chats, chatSelect+ctWhere+" ORDER BY c.id DESC LIMIT ?", append(ctArgs, limit)...); err != nil {
		return nil, err
	}
	all := append(channels, chats...)
	sort.Slice(all, func(i, j int) bool { return all[i].Date > all[j].Date })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// counters are the numbers of the overview.
type counters struct {
	Users         int64 `json:"users"`
	Online        int64 `json:"online"`
	Registrations int64 `json:"registrations"`
	Messages      int64 `json:"messages"`
	OpenReports   int64 `json:"open_reports"`
}

// onlineWindow is how recent a presence counts as being online: the time a
// client that went away keeps showing as online.
const onlineWindow = 5 * 60

func (s *store) counters(ctx context.Context) (*counters, error) {
	day := time.Now().Add(-24 * time.Hour).Unix()
	var c counters
	for _, q := range []struct {
		out   *int64
		query string
		args  []any
	}{
		{&c.Users, "SELECT COUNT(*) FROM users WHERE deleted = 0 AND id <> 777000", nil},
		{&c.Online, "SELECT COUNT(*) FROM user_presences WHERE last_seen_at >= ?", []any{time.Now().Unix() - onlineWindow}},
		{&c.Registrations, "SELECT COUNT(*) FROM users WHERE created_at >= FROM_UNIXTIME(?)", []any{day}},
		// A message counted once: the box of its sender.
		{&c.Messages, "SELECT COUNT(*) FROM messages WHERE date2 >= ? AND sender_user_id = user_id", []any{day}},
		{&c.OpenReports, "SELECT COUNT(*) FROM moderation_reports WHERE status = 0", nil},
	} {
		if err := s.db.QueryRow(ctx, q.out, q.query, q.args...); err != nil {
			return nil, err
		}
	}
	var channelMessages int64
	if err := s.db.QueryRow(ctx, &channelMessages, "SELECT COUNT(*) FROM channel_messages WHERE date2 >= ?", day); err != nil {
		return nil, err
	}
	c.Messages += channelMessages
	return &c, nil
}
