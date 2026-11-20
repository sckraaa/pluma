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

// Package config is the configuration of the SFU service.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// Config of the SFU. Addresses that clients see are separate from the
// listening ones, so that the SFU may stand behind a forwarding VPS
// (ADR 0057 §2, ADR 0059 §3).
type Config struct {
	Log logx.LogConf

	Control struct {
		// ListenOn is the address of the gRPC control API.
		ListenOn string
		// CertFile and KeyFile are the server's certificate; ClientCAFile
		// signs the certificates of the signalling side.
		CertFile     string
		KeyFile      string
		ClientCAFile string
	}

	// MetricsListenOn serves /metrics; empty turns it off.
	MetricsListenOn string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s

	Media struct {
		// UDPListen is the one UDP port of all clients, IPv4 and IPv6.
		UDPListen string `json:",default=:20600"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// TCPListen is the ICE-TCP port; empty turns ICE-TCP off. Not
		// 443 on the current server, where Caddy has it.
		TCPListen string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// PublicIPs are the addresses clients reach the SFU at, IPv4
		// and IPv6.
		PublicIPs []string
		// PublicUDPPort and PublicTCPPort are the ports clients reach
		// the SFU at; 0 means the listening port.
		PublicUDPPort int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		PublicTCPPort int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// Relays are forwarding addresses in front of this node, each a
		// route of its own (SFU_RELAYS, task 116, ADR 0074): entries
		// "<name>=<IPv4 or [IPv6]>:<UDP port>/<TCP port>" separated by
		// commas, e.g. "ru=194.87.226.185:20600/20601". The relay passes
		// packets to the node's own ports as they are; ICE takes its
		// address as the client's. Empty: none.
		Relays string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	}

	Limits struct {
		MaxParticipants int `json:",default=100"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// MaxVideos: how many senders' videos are forwarded at once.
		MaxVideos int `json:",default=30"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// LastN speakers are forwarded at once, each kept for AudioHold.
		LastN     int           `json:",default=5"`      //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		AudioHold time.Duration `json:",default=1500ms"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// UplinkMbps caps what the node sends; 0 is no cap.
		UplinkMbps float64 `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		// PacketCache: packets kept per video layer for retransmissions.
		PacketCache int `json:",default=512"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	}

	ICE struct {
		StartTimeout        time.Duration `json:",default=20s"`  //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		ConnectTimeout      time.Duration `json:",default=15s"`  //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		DisconnectedTimeout time.Duration `json:",default=5s"`   //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		FailedTimeout       time.Duration `json:",default=25s"`  //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		KeepaliveInterval   time.Duration `json:",default=2s"`   //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
		Renomination        bool          `json:",default=true"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	}
}

// Addresses returns the public candidates: every public IP with the UDP
// port, and with the TCP port when ICE-TCP is on.
func (c *Config) Addresses(udpPort, tcpPort int) ([]tgcallsjson.Address, error) {
	if len(c.Media.PublicIPs) == 0 {
		return nil, errors.New("config: Media.PublicIPs is empty")
	}
	if c.Media.PublicUDPPort != 0 {
		udpPort = c.Media.PublicUDPPort
	}
	if c.Media.PublicTCPPort != 0 {
		tcpPort = c.Media.PublicTCPPort
	}
	var out []tgcallsjson.Address
	for _, s := range c.Media.PublicIPs {
		ip, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("config: public IP %q: %w", s, err)
		}
		up, err := port(udpPort)
		if err != nil {
			return nil, err
		}
		out = append(out, tgcallsjson.Address{IP: ip, Port: up})
	}
	if c.Media.TCPListen != "" {
		tp, err := port(tcpPort)
		if err != nil {
			return nil, err
		}
		for _, s := range c.Media.PublicIPs {
			out = append(out, tgcallsjson.Address{IP: netip.MustParseAddr(s), Port: tp, TCP: true})
		}
	}
	return out, nil
}

// Relay is an entry of Media.Relays: a forwarding address in front of the
// node, whose candidates a join of the route Name gets.
type Relay struct {
	Name    string
	IP      netip.Addr
	UDPPort uint16
	TCPPort uint16
}

// ParseRelays parses Media.Relays. A name is a route (pb.ValidRoute),
// never "direct", which is the node's own addresses, and not twice; the
// address is a unicast IPv4 or an IPv6 in brackets with both ports.
func ParseRelays(s string) ([]Relay, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []Relay
	for entry := range strings.SplitSeq(s, ",") {
		entry = strings.TrimSpace(entry)
		name, addr, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: want <name>=<address>:<UDP port>/<TCP port>", entry)
		}
		if !pb.ValidRoute(name) {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: the name must be 1 to 16 of a-z, 0-9 and '-', starting with a letter", entry)
		}
		if name == pb.RouteDirect {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS): the name %q is the node's own addresses", name)
		}
		for _, r := range out {
			if r.Name == name {
				return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) names %q twice", name)
			}
		}
		hostPort, tcp, ok := strings.Cut(addr, "/")
		if !ok {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: want <address>:<UDP port>/<TCP port>", entry)
		}
		ap, err := netip.ParseAddrPort(hostPort)
		if err != nil || ap.Port() == 0 {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: %q is not <IPv4>:<port> or [<IPv6>]:<port>", entry, hostPort)
		}
		ip := ap.Addr().Unmap()
		// Loopback is the e2e stand's relay.
		if ip.Zone() != "" || !ip.IsGlobalUnicast() && !ip.IsLoopback() {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: %s is not a unicast address", entry, ip)
		}
		tp, err := strconv.ParseUint(tcp, 10, 16)
		if err != nil || tp == 0 {
			return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) entry %q: TCP port %q", entry, tcp)
		}
		out = append(out, Relay{Name: name, IP: ip, UDPPort: ap.Port(), TCPPort: uint16(tp)})
	}
	if len(out) >= pb.MaxRoutes {
		return nil, fmt.Errorf("config: Media.Relays (SFU_RELAYS) has %d entries, at most %d", len(out), pb.MaxRoutes-1)
	}
	return out, nil
}

// RelayAddresses returns the candidates of each relay of Media.Relays by
// its name: the UDP address, and the TCP one when ICE-TCP is on.
func (c *Config) RelayAddresses() (map[string][]tgcallsjson.Address, error) {
	relays, err := ParseRelays(c.Media.Relays)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]tgcallsjson.Address, len(relays))
	for _, r := range relays {
		addrs := []tgcallsjson.Address{{IP: r.IP, Port: r.UDPPort}}
		if c.Media.TCPListen != "" {
			addrs = append(addrs, tgcallsjson.Address{IP: r.IP, Port: r.TCPPort, TCP: true})
		}
		out[r.Name] = addrs
	}
	return out, nil
}

// Validate checks what the service cannot start without.
func (c *Config) Validate() error {
	if c.Control.ListenOn == "" || c.Control.CertFile == "" || c.Control.KeyFile == "" || c.Control.ClientCAFile == "" {
		return errors.New("config: Control needs ListenOn, CertFile, KeyFile and ClientCAFile: the API is served over mutual TLS only")
	}
	if c.Limits.UplinkMbps < 0 || c.Limits.MaxVideos < 0 || c.Limits.LastN < 0 {
		return errors.New("config: negative limit")
	}
	if _, _, err := net.SplitHostPort(c.Media.UDPListen); err != nil {
		return fmt.Errorf("config: Media.UDPListen: %w", err)
	}
	if c.Media.TCPListen != "" {
		if _, _, err := net.SplitHostPort(c.Media.TCPListen); err != nil {
			return fmt.Errorf("config: Media.TCPListen: %w", err)
		}
	}
	relays, err := ParseRelays(c.Media.Relays)
	if err != nil {
		return err
	}
	// A join may name every route: the answer must stay within what Web
	// takes (tgcallsjson.MaxCandidates).
	perAddress := 1
	if c.Media.TCPListen != "" {
		perAddress = 2
	}
	if n := (len(c.Media.PublicIPs) + len(relays)) * perAddress; n > tgcallsjson.MaxCandidates {
		return fmt.Errorf("config: Media.PublicIPs and Media.Relays make %d candidates, at most %d", n, tgcallsjson.MaxCandidates)
	}
	return nil
}

func port(p int) (uint16, error) {
	if p <= 0 || p > 65535 {
		return 0, errors.New("config: port out of range: " + strconv.Itoa(p))
	}
	return uint16(p), nil
}
