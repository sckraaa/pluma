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

// Package channelstats records the events the statistics of channels and
// supergroups are counted from (task 103, ADR 0066, p. 3), which no other
// table keeps: joins and leaves with their source, changes of a member's
// muting, where a post was viewed from, the forwards of posts and stories
// into channels, who read a supergroup on which day and what its admins did.
//
// Every event is written in the transaction of the change it records, so
// that a change and its event are committed together, and a transaction
// run again (userbox.Run) writes it again instead of twice. A ghost (ADR
// 0048) is never written: it is not seen among the members, nor counted.
//
// The history of the events starts when the migration made their tables
// (stats_collection, Since); nothing before it is made up.
package channelstats

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/ghost"
)

// Tx is the transaction an event is written in: *sqlx.Tx of the change.
type Tx interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRowsPartial(v any, query string, args ...any) error
}

// Kind is a kind of events, as stats_collection names it.
type Kind string

const (
	MemberEvents   Kind = "member_events"
	MuteEvents     Kind = "mute_events"
	ViewSources    Kind = "view_sources"
	PublicForwards Kind = "public_forwards"
	Readers        Kind = "readers"
	AdminActions   Kind = "admin_actions"
)

// Kinds are every kind of events, each with a row of stats_collection.
var Kinds = []Kind{MemberEvents, MuteEvents, ViewSources, PublicForwards, Readers, AdminActions}

// JoinSource is how a member came in (channel_stats_member_events.source).
type JoinSource int8

const (
	// JoinOther: none of the others, such as the owner coming back to its
	// private channel without a link.
	JoinOther JoinSource = 0
	// JoinLink: by an invite link that needs no approval.
	JoinLink JoinSource = 1
	// JoinFolder: by the link of a shared folder (chatlist).
	JoinFolder JoinSource = 2
	// JoinRequest: a request to join approved by an admin.
	JoinRequest JoinSource = 3
	// JoinPublic: by itself into a public channel (its name, a search).
	JoinPublic JoinSource = 4
	// JoinAdded: added by a member (channels.inviteToChannel), or a bot
	// added by being made an admin.
	JoinAdded JoinSource = 5
)

// ViewSource is where a post was viewed from (channel_message_views.source).
type ViewSource int8

const (
	// ViewOther: not by a subscriber in the channel itself.
	ViewOther ViewSource = 0
	// ViewSubscriber: by a member of the channel, in the channel.
	ViewSubscriber ViewSource = 1
	// ViewChannel: a forwarded copy in another broadcast channel.
	ViewChannel ViewSource = 2
	// ViewGroup: a forwarded copy in a basic group or a supergroup.
	ViewGroup ViewSource = 3
	// ViewPrivate: a forwarded copy in a private chat.
	ViewPrivate ViewSource = 4
)

// ForwardKind is what was forwarded (channel_stats_public_forwards.src_kind).
type ForwardKind int8

const (
	// ForwardPost: a post of a broadcast channel.
	ForwardPost ForwardKind = 1
	// ForwardStory: a story of a channel.
	ForwardStory ForwardKind = 2
)

// Action is a kind of the admins' actions counted per day
// (channel_stats_admin_actions).
type Action int8

const (
	// Deleted: messages of others deleted.
	Deleted Action = iota + 1
	// Kicked: users removed (banned from viewing the messages).
	Kicked
	// Banned: members restricted.
	Banned
)

// MuteForever is mute_until of a member muted for good, as the clients send
// it (Int32.max, core.telegram.org/constructor/inputPeerNotifySettings).
const MuteForever = math.MaxInt32

// ErrNotCollected: the kind of events has no row of stats_collection, the
// migration of task 103 did not run.
var ErrNotCollected = errors.New("channelstats: events not collected")

// Day is the UTC day of the unix time t: the days of the readers and of the
// admins' actions, and the buckets of the daily graphs.
func Day(t int64) int32 {
	d := t / 86400
	if t < 0 && t%86400 != 0 {
		d--
	}
	return int32(d)
}

// MemberJoined records that userId became a member of the channel at date,
// in tx; inviterId is who let it in (the link's admin, the approving admin,
// the member who added it), 0 when nobody.
func MemberJoined(tx Tx, channelId, userId, date int64, source JoinSource, inviterId int64) error {
	return MembersJoined(tx, channelId, []int64{userId}, date, source, inviterId)
}

// MembersJoined records that users became members of the channel at date,
// in tx, by one source and inviter (MemberJoined). The ghosts among them
// are left out.
func MembersJoined(tx Tx, channelId int64, users []int64, date int64, source JoinSource, inviterId int64) error {
	if source < JoinOther || source > JoinAdded {
		return fmt.Errorf("channelstats: join source %d", source)
	}
	return memberEvents(tx, channelId, users, date, true, source, inviterId)
}

// MemberLeft records that userId stopped being a member of the channel at
// date, in tx: it left, or was removed.
func MemberLeft(tx Tx, channelId, userId, date int64) error {
	return memberEvents(tx, channelId, []int64{userId}, date, false, JoinOther, 0)
}

func memberEvents(tx Tx, channelId int64, users []int64, date int64, joined bool, source JoinSource, inviterId int64) error {
	users, err := visible(tx, users)
	if err != nil || len(users) == 0 {
		return err
	}
	args := make([]any, 0, 6*len(users))
	for _, id := range users {
		args = append(args, channelId, id, date, joined, int8(source), inviterId)
	}
	_, err = tx.Exec("INSERT INTO channel_stats_member_events (channel_id, user_id, `date`, joined, source, inviter_id) VALUES "+
		rows("(?, ?, ?, ?, ?, ?)", len(users)), args...)
	return err
}

// MuteChanged records that userId's muting of the channel came to be
// muteUntil at date, in tx: 0 when its notifications are on, a later date
// when muted until it, MuteForever when muted for good. The caller writes
// it only when the muting in effect changed.
func MuteChanged(tx Tx, channelId, userId, date int64, muteUntil int32) error {
	if muteUntil < 0 {
		return fmt.Errorf("channelstats: mute_until %d", muteUntil)
	}
	users, err := visible(tx, []int64{userId})
	if err != nil || len(users) == 0 {
		return err
	}
	_, err = tx.Exec("INSERT INTO channel_stats_mute_events (channel_id, user_id, `date`, mute_until) VALUES (?, ?, ?, ?)",
		channelId, userId, date, muteUntil)
	return err
}

// Reader records that userId read the supergroup on the day of date, in
// tx: it read its history or wrote to it. A day counts a reader once.
func Reader(tx Tx, channelId, userId, date int64) error {
	users, err := visible(tx, []int64{userId})
	if err != nil || len(users) == 0 {
		return err
	}
	_, err = tx.Exec("INSERT IGNORE INTO channel_stats_readers (channel_id, `day`, user_id) VALUES (?, ?, ?)",
		channelId, Day(date), userId)
	return err
}

// AdminAction records n actions of the kind by the admin adminId in the
// channel on the day of date, in tx. Nothing is written for n below 1.
func AdminAction(tx Tx, channelId, adminId, date int64, action Action, n int) error {
	var column string
	switch action {
	case Deleted:
		column = "deleted"
	case Kicked:
		column = "kicked"
	case Banned:
		column = "banned"
	default:
		return fmt.Errorf("channelstats: admin action %d", action)
	}
	if n < 1 {
		return nil
	}
	users, err := visible(tx, []int64{adminId})
	if err != nil || len(users) == 0 {
		return err
	}
	_, err = tx.Exec("INSERT INTO channel_stats_admin_actions (channel_id, `day`, admin_id, "+column+") VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE "+column+" = "+column+" + VALUES("+column+")",
		channelId, Day(date), adminId, n)
	return err
}

// PublicForward records that the post or story srcId of the peer
// (srcPeerType, srcPeerId) was forwarded at date into the channel or
// supergroup dstChannelId as its message dstMessageId, in tx. It is
// recorded for any channel: whether the copy is public is read when the
// forwards are listed, as a channel's name comes and goes. The same copy is
// recorded once.
func PublicForward(tx Tx, kind ForwardKind, srcPeerType int32, srcPeerId int64, srcId int32, dstChannelId int64, dstMessageId int32, date int64) error {
	if kind != ForwardPost && kind != ForwardStory {
		return fmt.Errorf("channelstats: forward kind %d", kind)
	}
	_, err := tx.Exec("INSERT IGNORE INTO channel_stats_public_forwards (src_kind, src_peer_type, src_peer_id, src_id, dst_channel_id, dst_message_id, `date`)"+
		" VALUES (?, ?, ?, ?, ?, ?, ?)", int8(kind), srcPeerType, srcPeerId, srcId, dstChannelId, dstMessageId, date)
	return err
}

// Querier reads rows: a transaction, or a database (ghost.OnDB).
type Querier = ghost.Querier

// Since is the unix time the events of kind are recorded from
// (stats_collection); ErrNotCollected when they are not.
func Since(q Querier, kind Kind) (int64, error) {
	var since []int64
	if err := q.QueryRowsPartial(&since, "SELECT since FROM stats_collection WHERE kind = ?", string(kind)); err != nil {
		return 0, err
	}
	if len(since) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrNotCollected, kind)
	}
	return since[0], nil
}

// visible are the users among users, without the ghosts (ADR 0048).
func visible(tx Tx, users []int64) ([]int64, error) {
	ghosts, err := ghost.Ghosts(tx, users)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(users))
	for _, id := range users {
		if id > 0 && !slices.Contains(ghosts, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// rows is n tuples of row for a VALUES list.
func rows(row string, n int) string {
	return row + strings.Repeat(", "+row, n-1)
}
