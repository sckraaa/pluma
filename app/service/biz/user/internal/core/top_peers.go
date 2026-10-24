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
	"math"
	"sort"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
)

// Top peers (task 011, ADR 0015). The rating of a peer in a category is
//
//	rating = Σ exp((t − now) / decay)
//
// over the messages the user sent to the peer at times t after the user
// turned top peers on and after the last reset of the peer in the
// category: every message adds at most 1, and its weight halves every
// decay·ln 2 (about 19 days). It is the formula of the clients, which add
// exp((t − t_load) / decay) for each message sent after loading the
// ratings (Android MediaDataController.increasePeerRaiting, tdesktop
// TopPeers), so the server's ratings and the clients' additions add up.
const (
	// ratingDecay is rating_e_decay of help.getConfig (teamgramd/bin/
	// config.json), in seconds.
	ratingDecay = 2419200
	// topPeersScan is how many of the newest messages of a box the ratings
	// are computed from: a bound on the cost of one request; older
	// messages weigh little anyway.
	topPeersScan = 10000
	// MaxTopPeers is the most peers a category returns.
	MaxTopPeers = 100
)

// Categories of top peers, as user_top_peer_resets keeps them.
const (
	topPeerCorrespondents int32 = iota + 1
	topPeerBotsPM
	topPeerBotsInline
	topPeerPhoneCalls
	topPeerForwardUsers
	topPeerForwardChats
	topPeerGroups
	topPeerChannels
	topPeerBotsApp
	topPeerBotsGuestChat
)

var topPeerCategories = map[string]int32{
	mtproto.Predicate_topPeerCategoryCorrespondents: topPeerCorrespondents,
	mtproto.Predicate_topPeerCategoryBotsPM:         topPeerBotsPM,
	mtproto.Predicate_topPeerCategoryBotsInline:     topPeerBotsInline,
	mtproto.Predicate_topPeerCategoryPhoneCalls:     topPeerPhoneCalls,
	mtproto.Predicate_topPeerCategoryForwardUsers:   topPeerForwardUsers,
	mtproto.Predicate_topPeerCategoryForwardChats:   topPeerForwardChats,
	mtproto.Predicate_topPeerCategoryGroups:         topPeerGroups,
	mtproto.Predicate_topPeerCategoryChannels:       topPeerChannels,
	mtproto.Predicate_topPeerCategoryBotsApp:        topPeerBotsApp,
	mtproto.Predicate_topPeerCategoryBotsGuestChat:  topPeerBotsGuestChat,
}

// topPeerCategory returns the id of a category.
func topPeerCategory(c *mtproto.TopPeerCategory) (int32, bool) {
	id, ok := topPeerCategories[c.GetPredicateName()]
	return id, ok
}

// peerKind is what a peer the user wrote to is, for the categories.
type peerKind uint8

const (
	peerIgnored peerKind = iota // gone, the user, unknown
	peerPerson                  // a user who is not a bot
	peerBot
	peerGroup // a basic group
)

type topPeerKey struct {
	category int32
	peerType int32
	peerId   int64
}

// rateTopPeers computes the ratings of the peers of the categories from
// the user's sent messages: correspondents and bots_pm are the users and
// bots the user writes to, groups the basic groups, forward_users and
// forward_chats where the user forwards messages. The other categories
// have no data on this server (inline bots, bot apps, calls, channels).
// resets are the times of the resets by category and peer. Each category
// is sorted by rating, the highest first, at most MaxTopPeers.
func rateTopPeers(messages []dao.SentMessage, now int64, kind func(peerType int32, peerId int64) peerKind, resets map[topPeerKey]int64) map[int32][]*mtproto.TopPeer {
	ratings := make(map[topPeerKey]float64)
	add := func(category int32, m dao.SentMessage, w float64) {
		k := topPeerKey{category, m.PeerType, m.PeerId}
		if m.Date <= resets[k] {
			return
		}
		ratings[k] += w
	}
	for _, m := range messages {
		w := math.Exp(float64(m.Date-now) / ratingDecay)
		switch kind(m.PeerType, m.PeerId) {
		case peerPerson:
			add(topPeerCorrespondents, m, w)
			if m.Forward {
				add(topPeerForwardUsers, m, w)
			}
		case peerBot:
			add(topPeerBotsPM, m, w)
		case peerGroup:
			add(topPeerGroups, m, w)
			if m.Forward {
				add(topPeerForwardChats, m, w)
			}
		}
	}

	byCategory := make(map[int32][]topPeerKey)
	for k := range ratings {
		byCategory[k.category] = append(byCategory[k.category], k)
	}
	out := make(map[int32][]*mtproto.TopPeer, len(byCategory))
	for category, keys := range byCategory {
		sort.Slice(keys, func(i, j int) bool {
			ri, rj := ratings[keys[i]], ratings[keys[j]]
			if ri != rj {
				return ri > rj
			}
			if keys[i].peerType != keys[j].peerType {
				return keys[i].peerType < keys[j].peerType
			}
			return keys[i].peerId < keys[j].peerId
		})
		if len(keys) > MaxTopPeers {
			keys = keys[:MaxTopPeers]
		}
		peers := make([]*mtproto.TopPeer, 0, len(keys))
		for _, k := range keys {
			peers = append(peers, mtproto.MakeTLTopPeer(&mtproto.TopPeer{
				Peer:   mtproto.MakePeer(k.peerType, k.peerId),
				Rating: ratings[k],
			}).To_TopPeer())
		}
		out[category] = peers
	}
	return out
}
