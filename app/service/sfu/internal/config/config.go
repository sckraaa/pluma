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
	"time"

	"github.com/zeromicro/go-zero/core/logx"

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
	}

	// Plaintext: the node takes group and channel calls, which are not
	// end-to-end encrypted; it must stand in the trusted zone of the core.
	// E2E: the node takes end-to-end encrypted conferences (ADR 0059 §3).
	Plaintext bool `json:",default=true"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	E2E       bool `json:",optional"`     //nolint:staticcheck // a go-zero conf option, not encoding/json\'s

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

// Validate checks what the service cannot start without.
func (c *Config) Validate() error {
	if c.Control.ListenOn == "" || c.Control.CertFile == "" || c.Control.KeyFile == "" || c.Control.ClientCAFile == "" {
		return errors.New("config: Control needs ListenOn, CertFile, KeyFile and ClientCAFile: the API is served over mutual TLS only")
	}
	if !c.Plaintext && !c.E2E {
		return errors.New("config: the node takes no calls: set Plaintext or E2E")
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
	return nil
}

func port(p int) (uint16, error) {
	if p <= 0 || p > 65535 {
		return 0, errors.New("config: port out of range: " + strconv.Itoa(p))
	}
	return uint16(p), nil
}
