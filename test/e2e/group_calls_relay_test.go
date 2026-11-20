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

//go:build e2e

package e2e

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// Group calls from Russia through the relay of the Russian node (task 116,
// ADR 0074). On the stand the node is rufiles, whose nginx relays with the
// stream.conf deploy.sh renders to the SFU's ports; the SFU knows it as
// SFU_RELAYS=ru=127.0.0.1:<port>/<port>, and bff routes GROUPCALLS_ROUTES=
// RU:ru;*:direct. The clients' countries are those of countryClients: the
// one from Russia joins through the origin of the Gcore edge, which hands
// the gateway its address (5.8.16.41), as task 113's tests do.

// relayPort is the port of the relay "ru" of the stand's SFU_RELAYS.
func relayPort(t *testing.T) string {
	t.Helper()
	m := regexp.MustCompile(`(?:^|,)ru=127\.0\.0\.1:(\d+)/(\d+)(?:,|$)`).FindStringSubmatch(os.Getenv("PLUMA_E2E_SFU_RELAYS"))
	if m == nil || m[1] != m[2] {
		t.Skip("PLUMA_E2E_SFU_RELAYS has no relay ru (run.sh test sets it)")
	}
	return m[1]
}

// answerCandidates are the candidates of the params of
// updateGroupCallConnection as ip:port/protocol.
func answerCandidates(t *testing.T, who, params string) []string {
	t.Helper()
	resp, err := tgcallsjson.ParseJoinResponse([]byte(params))
	if err != nil {
		t.Fatalf("%s: the answer: %v", who, err)
	}
	var out []string
	for _, c := range resp.Transport.Candidates {
		out = append(out, c.IP+":"+c.Port+"/"+c.Protocol)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no candidates", who)
	}
	return out
}

// onlyRelay checks that every candidate is the relay's, and noRelay that
// none is.
func onlyRelay(t *testing.T, who string, cands []string, port string) {
	t.Helper()
	for _, c := range cands {
		if !strings.HasPrefix(c, "127.0.0.1:"+port+"/") {
			t.Fatalf("%s: candidate %s of %v is not the relay's (port %s)", who, c, cands, port)
		}
	}
}

func noRelay(t *testing.T, who string, cands []string, port string) {
	t.Helper()
	for _, c := range cands {
		if strings.Contains(c, ":"+port+"/") {
			t.Fatalf("%s: candidate %s of %v is the relay's", who, c, cands)
		}
	}
}

var icePairsRe = regexp.MustCompile(`(?m)^sfu_ice_pairs_total\{([^}]*)\} (\d+)$`)

// sfuPairs is the sum of sfu_ice_pairs_total of the route on the stand's
// SFU, read from its metrics port through bff's container.
func sfuPairs(t *testing.T, route, network string) int {
	t.Helper()
	out := stackOut(t, "exec", "-T", "bff", "bash", "-c",
		`exec 3<>/dev/tcp/sfu/20501 && printf 'GET /metrics HTTP/1.0\r\nHost: sfu\r\n\r\n' >&3 && cat <&3`)
	n := 0
	for _, m := range icePairsRe.FindAllStringSubmatch(out, -1) {
		if strings.Contains(m[1], `route="`+route+`"`) && strings.Contains(m[1], `network="`+network+`"`) {
			v, _ := strconv.Atoi(m[2])
			n += v
		}
	}
	return n
}

// A call of a supergroup with a client of no country, one from Germany
// and one from Russia: the one from Russia gets the relay's candidates
// alone and connects through it, the others the SFU's own; they all hear
// each other. The screen of the one from Russia goes through the relay
// too.
func TestGroupCallThroughRelay(t *testing.T) {
	needEdges(t)
	port := relayPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	direct, germany, russia := countryClients(ctx, t, "relay")
	_, ch := createChannel(ctx, t, direct, "group call through the relay", true)
	_, input := channelInputs(direct, ch)
	inviteToChannel(ctx, t, direct, input, germany, russia)
	for _, u := range []*user{direct, germany, russia} {
		getState(ctx, t, u)
	}
	peer, _ := channelInputs(direct, ch)
	gc := createGroupCall(ctx, t, direct, peer, "Through the relay", 0)
	pairs := sfuPairs(t, "ru", "udp4")

	md := joinMember(ctx, t, direct, groupCallInput(direct, gc), memberOptions{})
	mg := joinMember(ctx, t, germany, groupCallInput(germany, gc), memberOptions{})
	mr := joinMember(ctx, t, russia, groupCallInput(russia, gc), memberOptions{})
	onlyRelay(t, "Russia", answerCandidates(t, "Russia", mr.answer), port)
	noRelay(t, "no country", answerCandidates(t, "no country", md.answer), port)
	noRelay(t, "Germany", answerCandidates(t, "Germany", mg.answer), port)

	for _, m := range []*member{md, mg, mr} {
		speakMember(ctx, t, m)
	}
	hearsMember(ctx, t, md, mr)
	hearsMember(ctx, t, mr, md)
	hearsMember(ctx, t, mr, mg)
	hearsMember(ctx, t, mg, mr)
	if got := sfuPairs(t, "ru", "udp4"); got < pairs+1 {
		t.Errorf("sfu_ice_pairs_total{route=ru,network=udp4}: %d after the join, %d before", got, pairs)
	}

	// The screen of the one from Russia.
	screen, err := groupcallpeer.New(groupcallpeer.Config{VideoLayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Close)
	params, err := screen.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	o, err := russia.c.Invoke(ctx, &mtproto.TLPhoneJoinGroupCallPresentation{
		Constructor: russia.c.Constructor(mtproto.Predicate_phone_joinGroupCallPresentation),
		Call:        groupCallInput(russia, gc),
		Params:      mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	})
	if err != nil {
		t.Fatalf("phone.joinGroupCallPresentation: %v", err)
	}
	var conn *mtproto.Update
	for _, x := range updatesOf(t, "phone.joinGroupCallPresentation", o).GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCallConnection && x.GetPresentation() {
			conn = x
		}
	}
	if conn == nil {
		t.Fatal("phone.joinGroupCallPresentation without the connection of the screen")
	}
	onlyRelay(t, "the screen from Russia", answerCandidates(t, "the screen", conn.GetParams().GetData()), port)
	screen.SetE2E(mr.peer().E2E())
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	if err := screen.Connect(cctx, []byte(conn.GetParams().GetData())); err != nil {
		t.Fatalf("the screen from Russia connects: %v", err)
	}
}

// A client in Russia on a network without UDP: ICE-TCP through the relay.
func TestGroupCallThroughRelayTCPOnly(t *testing.T) {
	needEdges(t)
	port := relayPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	direct, _, russia := countryClients(ctx, t, "relay tcp")
	_, ch := createChannel(ctx, t, direct, "group call through the relay over TCP", true)
	_, input := channelInputs(direct, ch)
	inviteToChannel(ctx, t, direct, input, russia)
	for _, u := range []*user{direct, russia} {
		getState(ctx, t, u)
	}
	peer, _ := channelInputs(direct, ch)
	gc := createGroupCall(ctx, t, direct, peer, "Through the relay over TCP", 0)
	pairs := sfuPairs(t, "ru", "tcp4")

	md := joinMember(ctx, t, direct, groupCallInput(direct, gc), memberOptions{})
	mr := joinMember(ctx, t, russia, groupCallInput(russia, gc), memberOptions{cfg: groupcallpeer.Config{Networks: []ice.NetworkType{ice.NetworkTypeTCP4}}})
	onlyRelay(t, "Russia", answerCandidates(t, "Russia", mr.answer), port)
	if n := mr.peer().PairNetwork(); n != "tcp4" {
		t.Fatalf("the pair of the one from Russia is %q, want tcp4", n)
	}
	speakMember(ctx, t, md)
	speakMember(ctx, t, mr)
	hearsMember(ctx, t, md, mr)
	hearsMember(ctx, t, mr, md)
	if got := sfuPairs(t, "ru", "tcp4"); got < pairs+1 {
		t.Errorf("sfu_ice_pairs_total{route=ru,network=tcp4}: %d after the join, %d before", got, pairs)
	}
}
