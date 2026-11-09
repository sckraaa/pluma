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

// Package ice makes the phoneConnectionWebrtc list of a call (task 068, ADR
// 0004): STUN and TURN servers from the config, with TURN credentials made
// per call from coturn's static-auth-secret (use-auth-secret), so no
// password is stored or shared between calls.
package ice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // the TURN REST credential is HMAC-SHA1 by definition
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Server is one configured STUN/TURN endpoint. Host6 is the IPv6 address of
// a Host given as an IPv4 address.
type Server struct {
	Host  string
	Host6 string
	Port  int32
	Stun  bool
	Turn  bool
}

// Servers are the endpoints; the zero value has none.
type Servers struct {
	list     []Server
	secret   []byte
	now      func() time.Time
	resolver func(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Parse reads "host:port:stun+turn,host:port:turn". A host may be an
// IPv4 address, a host name or an IPv6 address in square brackets; the
// flag stun or turn or both separated by "+" says what it serves. The TURN
// secret must be set if any entry has turn. An IPv6 address is one more
// address of the entry with an IPv4 address and the same port and flags: a
// connection needs an IPv4 address (see Connections).
func Parse(cfg, secret string) (*Servers, error) {
	s := &Servers{secret: []byte(secret), now: time.Now, resolver: net.DefaultResolver.LookupIPAddr}
	for _, entry := range strings.Split(cfg, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		i := strings.LastIndexByte(entry, ':')
		if i <= 0 {
			return nil, fmt.Errorf("%q: want host:port:stun+turn", entry)
		}
		hostPort, kinds := entry[:i], entry[i+1:]
		host, port, err := net.SplitHostPort(hostPort)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", entry, err)
		}
		p, err := strconv.Atoi(port)
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("%q: bad port", entry)
		}
		srv := Server{Host: host, Port: int32(p)}
		for _, k := range strings.Split(kinds, "+") {
			switch k {
			case "stun":
				srv.Stun = true
			case "turn":
				srv.Turn = true
			default:
				return nil, fmt.Errorf("%q: unknown kind %q", entry, k)
			}
		}
		if srv.Turn && secret == "" {
			return nil, errors.New("a TURN server needs a TurnSecret")
		}
		s.list = append(s.list, srv)
	}
	if err := s.pairIPv6(); err != nil {
		return nil, err
	}
	return s, nil
}

// pairIPv6 folds every entry with an IPv6 address into its IPv4 entry.
func (s *Servers) pairIPv6() error {
	out := s.list[:0:0]
	for _, srv := range s.list {
		if !isIPv6(srv.Host) {
			out = append(out, srv)
		}
	}
	for _, srv := range s.list {
		if !isIPv6(srv.Host) {
			continue
		}
		paired := false
		for i := range out {
			o := &out[i]
			if o.Host6 == "" && o.Port == srv.Port && o.Stun == srv.Stun && o.Turn == srv.Turn && net.ParseIP(o.Host) != nil {
				o.Host6, paired = srv.Host, true
				break
			}
		}
		if !paired {
			return fmt.Errorf("[%s]:%d: an IPv6 address needs an IPv4 entry with the same port and kinds", srv.Host, srv.Port)
		}
	}
	s.list = out
	return nil
}

func isIPv6(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

// Empty tells whether no server is configured: calls cannot start.
func (s *Servers) Empty() bool { return len(s.list) == 0 }

// resolveTimeout bounds the name lookup of a call's servers.
const resolveTimeout = 2 * time.Second

// Connections returns the connections of callId. The clients take numeric
// addresses only (ADR 0004: names are resolved before they are handed out).
// A host without an IPv4 address is left out: Android reads only ip for such
// a connection (jni/voip/org_telegram_messenger_voip_Instance.cpp:854), Web K
// makes an URL of it unchecked (p2P/utils.ts:169,175) and tdesktop a TURN
// server (calls_call.cpp:155). A TURN credential lives as long as a call may.
func (s *Servers) Connections(ctx context.Context, callId int64, lifetime time.Duration) []*mtproto.PhoneConnection {
	out := make([]*mtproto.PhoneConnection, 0, len(s.list))
	expires := s.now().Add(lifetime).Unix()
	username := strconv.FormatInt(expires, 10) + ":" + strconv.FormatInt(callId, 10)
	for i, srv := range s.list {
		ipv4, ipv6 := s.addresses(ctx, srv.Host)
		if ipv4 == "" {
			continue
		}
		if srv.Host6 != "" {
			ipv6 = srv.Host6
		}
		conn := &mtproto.PhoneConnection{
			Id:   int64(i + 1),
			Ip:   ipv4,
			Ipv6: ipv6,
			Port: srv.Port,
			Stun: srv.Stun,
			Turn: srv.Turn,
		}
		if srv.Turn {
			conn.Username = username
			conn.Password = credential(s.secret, username)
		}
		out = append(out, mtproto.MakeTLPhoneConnectionWebrtc(conn).To_PhoneConnection())
	}
	return out
}

// credential is the password of the TURN REST API: base64(HMAC-SHA1(secret,
// username)), as coturn's use-auth-secret checks it.
func credential(secret []byte, username string) string {
	h := hmac.New(sha1.New, secret)
	h.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (s *Servers) addresses(ctx context.Context, host string) (ipv4, ipv6 string) {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String(), ""
		}
		return "", ip.String()
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	ips, err := s.resolver(ctx, host)
	if err != nil {
		return "", ""
	}
	for _, a := range ips {
		if ip4 := a.IP.To4(); ip4 != nil {
			if ipv4 == "" {
				ipv4 = ip4.String()
			}
		} else if ipv6 == "" {
			ipv6 = a.IP.String()
		}
	}
	return ipv4, ipv6
}
