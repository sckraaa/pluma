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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/premium"
)

// RecommendationsLimit is the most channels channels.getChannelRecommendations
// gives: the Premium limit, which everybody has (ADR 0041;
// recommended_channels_limit_premium of help.getAppConfig).
const RecommendationsLimit = premium.RecommendedChannelsLimit

// The recommendations of channels (ADR 0047). A candidate is a broadcast
// channel that is public (it has its editable username), neither deleted
// nor blocked by the owner, and in which the account has no row at all: it
// is not in it, did not leave it and was not banned from it. The people
// a recommendation is about are the readers of the channel asked about, or
// for the account the members of every channel and supergroup it is in;
// the account itself, bots and deleted accounts are not counted. A
// candidate's overlap is how many of these people are in it. Candidates go
// by overlap, then by size (participants_count), then by id, the oldest
// first: the same data always gives the same list.
const (
	// recommendWhere is what makes a channel a candidate.
	recommendWhere = " WHERE c.broadcast = 1 AND c.deleted = 0 AND c.blocked = 0 AND c.id <> ?" +
		" AND EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1)" +
		" AND NOT EXISTS (SELECT 1 FROM channel_participants m WHERE m.channel_id = c.id AND m.user_id = ?)"
	// recommendPeople keeps people who count: not the account, no bots,
	// no deleted accounts.
	recommendPeople = " AND q.user_id <> ? AND NOT EXISTS (SELECT 1 FROM users x WHERE x.id = q.user_id AND (x.is_bot = 1 OR x.deleted = 1))"
	// recommendOverlap counts, for every channel, its readers among the
	// people of the query given to overlapOf.
	recommendOverlap = "(SELECT p.channel_id, COUNT(DISTINCT p.user_id) AS overlap FROM channel_participants p WHERE p.state = 0 AND p.user_id IN ("
	recommendOrder   = " ORDER BY COALESCE(o.overlap, 0) DESC, c.participants_count DESC, c.id LIMIT ?"
)

// overlapOf is the derived table o of the overlap with people.
func overlapOf(people string) string {
	return recommendOverlap + people + ") GROUP BY p.channel_id) o ON o.channel_id = c.id"
}

// SimilarChannels are the ids of the channels like channelId for selfId:
// the candidates with at least one of channelId's readers, in order.
func (d *Dao) SimilarChannels(ctx context.Context, selfId, channelId int64, limit int) ([]int64, error) {
	people := "SELECT q.user_id FROM channel_participants q WHERE q.channel_id = ? AND q.state = 0" + recommendPeople
	var ids []int64
	err := d.Mysql.QueryRowsPartial(ctx, &ids,
		"SELECT c.id FROM channels c JOIN "+overlapOf(people)+recommendWhere+recommendOrder,
		channelId, selfId,
		channelId, mtproto.PEER_CHANNEL, selfId,
		limit)
	return ids, err
}

// RecommendedChannels are the ids of the channels recommended to selfId on
// its own: the candidates with the members of its channels and supergroups
// first, then the others by size, so that an account in no channel yet is
// shown the public channels there are.
func (d *Dao) RecommendedChannels(ctx context.Context, selfId int64, limit int) ([]int64, error) {
	people := "SELECT q.user_id FROM channel_participants q JOIN channel_participants me ON me.channel_id = q.channel_id" +
		" WHERE me.user_id = ? AND me.state = 0 AND q.state = 0" + recommendPeople
	var ids []int64
	err := d.Mysql.QueryRowsPartial(ctx, &ids,
		"SELECT c.id FROM channels c LEFT JOIN "+overlapOf(people)+recommendWhere+recommendOrder,
		selfId, selfId,
		0, mtproto.PEER_CHANNEL, selfId,
		limit)
	return ids, err
}
