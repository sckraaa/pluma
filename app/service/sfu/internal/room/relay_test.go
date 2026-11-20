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

package room

// The relay of the Russian node in front of the SFU (task 116, ADR 0074):
// nginx stream on amf passes UDP and ICE-TCP to the SFU's ports as they
// are, one session per client address with an upstream port of its own.
// testRelay does the same in the process.

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// testRelay forwards UDP and TCP to the SFU's ports as nginx stream with
// reuseport does: every client address is a session with its own upstream
// socket, so the SFU sees each client at the relay's address and a port of
// its own.
type testRelay struct {
	t      *testing.T
	udp    *net.UDPConn
	tcp    net.Listener
	upUDP  *net.UDPAddr
	upTCP  string
	closed atomic.Bool

	mu       sync.Mutex
	sessions map[netip.AddrPort]*net.UDPConn
	tcpConns []net.Conn
	// ports are the upstream ports the sessions used, in order.
	ports []int

	toSFU, fromSFU atomic.Int64
}

func newTestRelay(t *testing.T, mux muxPorts) *testRelay {
	t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &testRelay{
		t: t, udp: udp, tcp: tcp, upUDP: mux.UDPAddr(), upTCP: mux.TCPAddr().String(),
		sessions: map[netip.AddrPort]*net.UDPConn{},
	}
	t.Cleanup(r.stop)
	go r.serveUDP()
	go r.serveTCP()
	return r
}

// addresses are the relay's candidates, as SFU_RELAYS makes them.
func (r *testRelay) addresses() []tgcallsjson.Address {
	lo := netip.MustParseAddr("127.0.0.1")
	return []tgcallsjson.Address{
		{IP: lo, Port: uint16(r.udp.LocalAddr().(*net.UDPAddr).Port)},
		{IP: lo, Port: uint16(r.tcp.Addr().(*net.TCPAddr).Port), TCP: true},
	}
}

func (r *testRelay) serveUDP() {
	buf := make([]byte, 1500)
	for {
		n, from, err := r.udp.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		r.mu.Lock()
		up := r.sessions[from]
		if up == nil && !r.closed.Load() {
			if up, err = net.DialUDP("udp4", nil, r.upUDP); err != nil {
				r.mu.Unlock()
				continue
			}
			r.sessions[from] = up
			r.ports = append(r.ports, up.LocalAddr().(*net.UDPAddr).Port)
			go r.back(up, from)
		}
		r.mu.Unlock()
		if up == nil {
			continue
		}
		if _, err := up.Write(buf[:n]); err == nil {
			r.toSFU.Add(int64(n))
		}
	}
}

// back passes what the SFU sends on a session to its client.
func (r *testRelay) back(up *net.UDPConn, client netip.AddrPort) {
	buf := make([]byte, 1500)
	for {
		n, err := up.Read(buf)
		if err != nil {
			return
		}
		if _, err := r.udp.WriteToUDPAddrPort(buf[:n], client); err == nil {
			r.fromSFU.Add(int64(n))
		}
	}
}

func (r *testRelay) serveTCP() {
	for {
		c, err := r.tcp.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp4", r.upTCP)
		if err != nil {
			_ = c.Close()
			continue
		}
		r.mu.Lock()
		r.tcpConns = append(r.tcpConns, c, up)
		r.ports = append(r.ports, up.LocalAddr().(*net.TCPAddr).Port)
		r.mu.Unlock()
		go func() {
			n, _ := io.Copy(up, c)
			r.toSFU.Add(n)
			_ = up.Close()
		}()
		go func() {
			n, _ := io.Copy(c, up)
			r.fromSFU.Add(n)
			_ = c.Close()
		}()
	}
}

// reset ends every UDP session, as a reload of nginx does: the next packet
// of a client opens a new one, with a new upstream port.
func (r *testRelay) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, up := range r.sessions {
		_ = up.Close()
		delete(r.sessions, k)
	}
}

// stop closes the relay: nothing passes any more.
func (r *testRelay) stop() {
	if r.closed.Swap(true) {
		return
	}
	_ = r.udp.Close()
	_ = r.tcp.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, up := range r.sessions {
		_ = up.Close()
	}
	for _, c := range r.tcpConns {
		_ = c.Close()
	}
}

func (r *testRelay) upstreamPorts() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.ports...)
}

// newRelayManager is a manager with the relay "ru" in front of it;
// startTimeout, when not 0, is the wait for a client's first check.
func newRelayManager(t *testing.T, startTimeout time.Duration) (*Manager, *recorder, *testRelay) {
	t.Helper()
	var relay *testRelay
	m, rec := newTestManager(t, func(c *Config) {
		// The direct addresses of newTestManager are the mux's ports.
		relay = newTestRelay(t, muxPorts{udp: int(c.Addresses[0].Port), tcp: int(c.Addresses[1].Port)})
		c.Relays = map[string][]tgcallsjson.Address{"ru": relay.addresses()}
		if startTimeout != 0 {
			c.Transport.StartTimeout = startTimeout
		}
	})
	return m, rec, relay
}

// muxPorts are the loopback ports of the SFU's mux.
type muxPorts struct{ udp, tcp int }

func (p muxPorts) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p.udp}
}

func (p muxPorts) TCPAddr() *net.TCPAddr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p.tcp}
}

// answerAddrs are the candidates of an answer as ip:port/protocol.
func answerAddrs(t *testing.T, res *JoinResult) []string {
	t.Helper()
	resp, err := tgcallsjson.ParseJoinResponse(res.Params)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range resp.Transport.Candidates {
		out = append(out, c.IP+":"+c.Port+"/"+c.Protocol)
	}
	return out
}

func addrStrings(addrs []tgcallsjson.Address) []string {
	var out []string
	for _, a := range addrs {
		p := "udp"
		if a.TCP {
			p = "tcp"
		}
		out = append(out, netip.AddrPortFrom(a.IP, a.Port).String()+"/"+p)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A client of the route "ru" gets the relay's candidates alone and talks
// through it with a client of no route; its connection counts under "ru".
func TestJoinThroughRelay(t *testing.T) {
	m, rec, relay := newRelayManager(t, 0)
	pairsBefore := testutil.ToFloat64(metrics.ICEPairs.WithLabelValues("udp4", "controlling", "ru"))

	ru, ruRes := joinPeerRoutes(t, m, 1, KindMain, groupcallpeer.Config{}, []string{"ru"})
	de, deRes := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	if got, want := answerAddrs(t, ruRes), addrStrings(relay.addresses()); !equalStrings(got, want) {
		t.Fatalf("the answer of ru: %v, want the relay's %v", got, want)
	}
	if got, want := answerAddrs(t, deRes), addrStrings(m.cfg.Addresses); !equalStrings(got, want) {
		t.Fatalf("the answer without routes: %v, want the direct %v", got, want)
	}
	rec.wait(t, 10*time.Second, func(ev Event) bool { return ev.Type == EventConnected && ev.Participant == 1 })
	sendAudio(t, ru, 20)
	sendAudio(t, de, 20)
	awaitRTP(t, de, 5*time.Second, bySSRC(ru.AudioSSRC()))
	awaitRTP(t, ru, 5*time.Second, bySSRC(de.AudioSSRC()))
	if relay.toSFU.Load() == 0 || relay.fromSFU.Load() == 0 {
		t.Fatalf("nothing passed the relay: %d to the SFU, %d back", relay.toSFU.Load(), relay.fromSFU.Load())
	}
	if got := testutil.ToFloat64(metrics.ICEPairs.WithLabelValues("udp4", "controlling", "ru")); got != pairsBefore+1 {
		t.Errorf("sfu_ice_pairs_total{route=ru}: %v, want %v", got, pairsBefore+1)
	}
	if got := testutil.ToFloat64(metrics.Connections.WithLabelValues("main", "connected", "ru")); got < 1 {
		t.Errorf("sfu_connections{state=connected,route=ru}: %v", got)
	}
}

// Two clients behind the relay's one address: each has its own session
// and upstream port, and the SFU keeps them apart.
func TestTwoClientsBehindRelay(t *testing.T) {
	m, _, relay := newRelayManager(t, 0)
	a, _ := joinPeerRoutes(t, m, 1, KindMain, groupcallpeer.Config{}, []string{"ru"})
	b, _ := joinPeerRoutes(t, m, 2, KindMain, groupcallpeer.Config{}, []string{"ru"})
	sendAudio(t, a, 20)
	sendAudio(t, b, 30)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	awaitRTP(t, a, 5*time.Second, bySSRC(b.AudioSSRC()))
	if ports := relay.upstreamPorts(); len(ports) < 2 || ports[0] == ports[1] {
		t.Fatalf("upstream ports %v: want one per client", ports)
	}
}

// ICE-TCP through the relay, for a client without UDP.
func TestRelayICETCPOnly(t *testing.T) {
	m, rec, relay := newRelayManager(t, 0)
	tcp := groupcallpeer.Config{Networks: []ice.NetworkType{ice.NetworkTypeTCP4}}
	a, _ := joinPeerRoutes(t, m, 1, KindMain, tcp, []string{"ru"})
	b, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ev := rec.wait(t, 10*time.Second, func(ev Event) bool { return ev.Type == EventConnected && ev.Participant == 1 })
	if ev.Network != "tcp4" || a.PairNetwork() != "tcp4" {
		t.Fatalf("networks: SFU %s, client %s", ev.Network, a.PairNetwork())
	}
	sendAudio(t, a, 20)
	sendAudio(t, b, 20)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	awaitRTP(t, a, 5*time.Second, bySSRC(b.AudioSSRC()))
	relay.mu.Lock()
	n := len(relay.tcpConns)
	relay.mu.Unlock()
	if n == 0 {
		t.Fatal("no TCP connection through the relay")
	}
}

// A new session of the relay in the middle of a call (a reload of nginx):
// the SFU sees the client at a new port; its checks are the client's, and
// the media go on over the new pair.
func TestRelaySessionChange(t *testing.T) {
	m, _, relay := newRelayManager(t, 0)
	ru, _ := joinPeerRoutes(t, m, 1, KindMain, groupcallpeer.Config{}, []string{"ru"})
	de, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	sendAudio(t, ru, 20)
	sendAudio(t, de, 20)
	awaitRTP(t, de, 5*time.Second, bySSRC(ru.AudioSSRC()))
	awaitRTP(t, ru, 5*time.Second, bySSRC(de.AudioSSRC()))

	relay.reset()
	time.Sleep(200 * time.Millisecond)
	drain(de)
	drain(ru)
	if ports := relay.upstreamPorts(); len(ports) < 2 {
		t.Fatalf("no new session after the reset: %v", ports)
	}
	// Both ways again, within the SFU's renomination and the client's
	// consent checks.
	awaitRTP(t, de, 15*time.Second, bySSRC(ru.AudioSSRC()))
	awaitRTP(t, ru, 15*time.Second, bySSRC(de.AudioSSRC()))
}

// A relay that passes nothing: the SFU never gets a check, and the
// failure counts under the relay's route.
func TestRelayDownNoCheck(t *testing.T) {
	m, rec, relay := newRelayManager(t, 2*time.Second)
	relay.stop()
	before := testutil.ToFloat64(metrics.ConnectFailures.WithLabelValues("no_check", "ru"))
	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Join(callID, 1, KindMain, params, false, "", []string{"ru"}); err != nil {
		t.Fatal(err)
	}
	ev := rec.wait(t, 10*time.Second, func(ev Event) bool { return ev.Type == EventDisconnected && ev.Participant == 1 })
	if ev.Reason != "no_check" {
		t.Fatalf("reason %s", ev.Reason)
	}
	if got := testutil.ToFloat64(metrics.ConnectFailures.WithLabelValues("no_check", "ru")); got != before+1 {
		t.Errorf("sfu_connect_failures_total{reason=no_check,route=ru}: %v, want %v", got, before+1)
	}
}

// The routes of a join choose the sets in their order; a route the node
// does not have is left out and counted, and with none left the answer
// has the direct addresses.
func TestJoinRoutes(t *testing.T) {
	m, _, relay := newRelayManager(t, 0)
	direct, ru := addrStrings(m.cfg.Addresses), addrStrings(relay.addresses())
	for _, tc := range []struct {
		routes []string
		want   []string
		route  string
	}{
		{nil, direct, "direct"},
		{[]string{"direct"}, direct, "direct"},
		{[]string{"ru"}, ru, "ru"},
		{[]string{"ru", "direct"}, append(append([]string(nil), ru...), direct...), "ru"},
		{[]string{"direct", "ru", "direct"}, append(append([]string(nil), direct...), ru...), "direct"},
		{[]string{"kz"}, direct, "direct"},
		{[]string{"kz", "ru"}, ru, "ru"},
	} {
		unknown := testutil.ToFloat64(metrics.RouteUnknown.WithLabelValues("kz"))
		addrs, route := m.addresses(tc.routes)
		if got := addrStrings(addrs); !equalStrings(got, tc.want) || route != tc.route {
			t.Errorf("%v: %v under %s, want %v under %s", tc.routes, got, route, tc.want, tc.route)
		}
		wantUnknown := unknown
		for _, r := range tc.routes {
			if r == "kz" {
				wantUnknown++
			}
		}
		if got := testutil.ToFloat64(metrics.RouteUnknown.WithLabelValues("kz")); got != wantUnknown {
			t.Errorf("%v: sfu_route_unknown_total{route=kz} %v, want %v", tc.routes, got, wantUnknown)
		}
	}

	// The answer of a join with both sets: the relay's first.
	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Join(callID, 1, KindMain, params, false, "", []string{"ru", "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := answerAddrs(t, res), append(append([]string(nil), ru...), direct...); !equalStrings(got, want) {
		t.Fatalf("answer %v, want %v", got, want)
	}
	if err := m.Leave(callID, 1, KindMain); err != nil && !errors.Is(err, ErrNotParticipant) {
		t.Fatal(err)
	}
}
