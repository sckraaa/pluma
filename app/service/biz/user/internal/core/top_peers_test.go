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
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
)

func TestRateTopPeers(t *testing.T) {
	const (
		now   = int64(10_000_000)
		day   = int64(86400)
		alice = 11
		bob   = 12
		bot   = 13
		gone  = 14
		group = 21
	)
	kinds := map[int64]peerKind{alice: peerPerson, bob: peerPerson, bot: peerBot, gone: peerIgnored}
	kind := func(peerType int32, peerId int64) peerKind {
		if peerType == mtproto.PEER_CHAT {
			return peerGroup
		}
		return kinds[peerId]
	}
	sent := func(peerType int32, peerId, date int64, forward bool) dao.SentMessage {
		return dao.SentMessage{PeerType: peerType, PeerId: peerId, Date: date, Forward: forward}
	}
	messages := []dao.SentMessage{
		// Bob: three messages a month ago; Alice: one today and one
		// forwarded yesterday.
		sent(mtproto.PEER_USER, bob, now-30*day, false),
		sent(mtproto.PEER_USER, bob, now-30*day, false),
		sent(mtproto.PEER_USER, bob, now-30*day, false),
		sent(mtproto.PEER_USER, alice, now, false),
		sent(mtproto.PEER_USER, alice, now-day, true),
		sent(mtproto.PEER_USER, bot, now, false),
		sent(mtproto.PEER_USER, gone, now, false),
		sent(mtproto.PEER_CHAT, group, now-2*day, true),
	}
	rated := rateTopPeers(messages, now, kind, nil)

	people := rated[topPeerCorrespondents]
	if len(people) != 2 || people[0].GetPeer().GetUserId() != alice || people[1].GetPeer().GetUserId() != bob {
		t.Fatalf("correspondents %v", people)
	}
	wantAlice := 1 + math.Exp(-float64(day)/ratingDecay)
	wantBob := 3 * math.Exp(-float64(30*day)/ratingDecay)
	if math.Abs(people[0].GetRating()-wantAlice) > 1e-9 || math.Abs(people[1].GetRating()-wantBob) > 1e-9 {
		t.Errorf("ratings %v, %v; want %v, %v", people[0].GetRating(), people[1].GetRating(), wantAlice, wantBob)
	}
	if f := rated[topPeerForwardUsers]; len(f) != 1 || f[0].GetPeer().GetUserId() != alice {
		t.Errorf("forward_users %v", f)
	}
	if b := rated[topPeerBotsPM]; len(b) != 1 || b[0].GetPeer().GetUserId() != bot {
		t.Errorf("bots_pm %v", b)
	}
	if g := rated[topPeerGroups]; len(g) != 1 || g[0].GetPeer().GetChatId() != group {
		t.Errorf("groups %v", g)
	}
	if f := rated[topPeerForwardChats]; len(f) != 1 || f[0].GetPeer().GetChatId() != group {
		t.Errorf("forward_chats %v", f)
	}
	for _, p := range rated[topPeerCorrespondents] {
		if p.GetPeer().GetUserId() == gone {
			t.Errorf("a gone user is rated")
		}
	}

	// A reset of Alice today drops her messages until now from the
	// correspondents, not from forward_users.
	rated = rateTopPeers(messages, now, kind, map[topPeerKey]int64{
		{topPeerCorrespondents, mtproto.PEER_USER, alice}: now,
	})
	if people := rated[topPeerCorrespondents]; len(people) != 1 || people[0].GetPeer().GetUserId() != bob {
		t.Errorf("after the reset: %v", people)
	}
	if f := rated[topPeerForwardUsers]; len(f) != 1 {
		t.Errorf("forward_users after the reset of correspondents: %v", f)
	}
}

func TestRateTopPeersKeepsTheTop(t *testing.T) {
	const now = int64(10_000_000)
	var messages []dao.SentMessage
	for i := range MaxTopPeers + 20 {
		// Peer i wrote to i+1 times: the later peers rate higher.
		for range i + 1 {
			messages = append(messages, dao.SentMessage{PeerType: mtproto.PEER_USER, PeerId: int64(1000 + i), Date: now})
		}
	}
	rated := rateTopPeers(messages, now, func(int32, int64) peerKind { return peerPerson }, nil)[topPeerCorrespondents]
	if len(rated) != MaxTopPeers {
		t.Fatalf("%d peers", len(rated))
	}
	if rated[0].GetPeer().GetUserId() != int64(1000+MaxTopPeers+19) {
		t.Errorf("first %v", rated[0])
	}
	for i := 1; i < len(rated); i++ {
		if rated[i].GetRating() > rated[i-1].GetRating() {
			t.Fatalf("not sorted at %d", i)
		}
	}
}

// The clients add exp(Δt / rating_e_decay) of help.getConfig to the
// ratings they got: the server's decay must be the same.
func TestRatingDecayIsTheConfigs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "teamgramd", "bin", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Data2 struct {
			RatingEDecay int `json:"rating_e_decay"`
		} `json:"data2"`
	}
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Data2.RatingEDecay != ratingDecay {
		t.Errorf("rating_e_decay of config.json %d, ratingDecay %d", config.Data2.RatingEDecay, ratingDecay)
	}
}
