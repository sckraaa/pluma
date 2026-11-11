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

package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dal/dataobject"
)

// ParticipantsMax is the most rows of one page (core.telegram.org/method/
// channels.getParticipants: 200).
const ParticipantsMax = 200

// participantsColumns are those of makeParticipant, of channel_participants
// p.
const participantsColumns = "p.channel_id, p.user_id, p.role, p.state, p.admin_rights, p.`rank`, p.banned_rights, p.until_date," +
	" p.inviter_user_id, p.promoted_by, p.kicked_by, p.available_min_id, p.`date`, p.hidden"

// ErrUnknownFilter: a channelParticipantsFilter Participants does not know.
var ErrUnknownFilter = fmt.Errorf("channel: unknown participants filter")

// participantsWhere is the condition of a filter (the predicate of
// channelParticipantsFilter, api.tl) on channel_participants p and users u
// at now, with its arguments: members are those in the channel
// (ImmutableChannelParticipant.Member); kicked are those kicked whose ban
// runs, banned (restricted) the members whose restriction runs
// (channelperm.Restricted); q matches the beginning of a name or the
// username. The hidden rows of a ghost (ADR 0048) are left out of every
// list of members but the ghost's own; the kicked and the restricted lists
// show a ghost an admin banned or restricted, as the admin did it (and as
// kicked_count and banned_count count it).
func participantsWhere(filter, q string, selfId int64, now int32) (string, []any, error) {
	const (
		member = "p.state = 0"
		// The ban or restriction runs: until_date 0 or Int32.max is for
		// good.
		running = "(p.until_date = 0 OR p.until_date = 2147483647 OR p.until_date > ?)"
	)
	var (
		where string
		args  []any
	)
	switch filter {
	case mtproto.Predicate_channelParticipantsRecent:
		where = member
	case mtproto.Predicate_channelParticipantsAdmins:
		where = member + " AND p.role > 0"
	case mtproto.Predicate_channelParticipantsKicked:
		where, args = "p.state = 2 AND "+running, []any{now}
	case mtproto.Predicate_channelParticipantsBanned:
		where, args = member+" AND p.role = 0 AND p.banned_rights <> 0 AND "+running, []any{now}
	case mtproto.Predicate_channelParticipantsSearch, mtproto.Predicate_channelParticipantsMentions:
		where = member
	case mtproto.Predicate_channelParticipantsBots:
		where = member + " AND u.is_bot = 1"
	case mtproto.Predicate_channelParticipantsContacts:
		where, args = member+" AND EXISTS (SELECT 1 FROM user_contacts uc WHERE uc.owner_user_id = ? AND uc.contact_user_id = p.user_id AND uc.is_deleted = 0)",
			[]any{selfId}
	default:
		return "", nil, fmt.Errorf("%w: %q", ErrUnknownFilter, filter)
	}
	if filter != mtproto.Predicate_channelParticipantsKicked && filter != mtproto.Predicate_channelParticipantsBanned {
		where += " AND (p.hidden = 0 OR p.user_id = ?)"
		args = append(args, selfId)
	}
	if q = strings.TrimSpace(q); q != "" {
		like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		where += " AND (u.first_name LIKE ? OR u.last_name LIKE ? OR CONCAT(u.first_name, ' ', u.last_name) LIKE ? OR u.username LIKE ?)"
		args = append(args, like, like, like, like)
	}
	return where, args, nil
}

// Participants is a page of channelId's participants by filter and q, the
// creator and admins first and then the most recent, with the count of all
// that match.
func (d *Dao) Participants(ctx context.Context, channelId, selfId int64, filter, q string, offset, limit, now int32) ([]*channel.ImmutableChannelParticipant, int32, error) {
	where, args, err := participantsWhere(filter, q, selfId, now)
	if err != nil {
		return nil, 0, err
	}
	from := " FROM channel_participants p JOIN users u ON u.id = p.user_id WHERE p.channel_id = ? AND u.deleted = 0 AND " + where
	args = append([]any{channelId}, args...)
	var count []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*)"+from, args...); err != nil {
		return nil, 0, err
	}
	var dos []dataobject.ChannelParticipantsDO
	if err = d.Mysql.QueryRowsPartial(ctx, &dos,
		"SELECT "+participantsColumns+from+" ORDER BY p.role DESC, p.`date` DESC, p.user_id LIMIT ? OFFSET ?",
		append(args, min(max(limit, 0), ParticipantsMax), max(offset, 0))...); err != nil {
		return nil, 0, err
	}
	out := make([]*channel.ImmutableChannelParticipant, 0, len(dos))
	for i := range dos {
		out = append(out, makeParticipant(&dos[i]))
	}
	return out, first(count), nil
}
