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
//
// A TURN server may have a secret of its own, named by a label (task 109,
// ADR 0072 §8): a node that holds only its own secret can make credentials
// for itself and for no other TURN server of the list.
//
// Both sides of a call get the whole list, and every client hands every
// TURN server of it to WebRTC, each with its own credentials, so ICE
// gathers relay candidates on all of them and picks the pair that works;
// no client takes only the first (Android 13.2.1, tdesktop v7.4.1, iOS
// release-13.2, Web K aa74832e):
//   - Android: VoIPService.java:3538-3542 makes an endpoint of every
//     connection, org_telegram_messenger_voip_Instance.cpp:954-976 an
//     RtcServer of every endpoint with its username and password;
//   - tdesktop: calls_call.cpp:1237-1238 and :172-183 (AppendServer), a
//     TURN RtcServer of every phoneConnectionWebrtc with turn;
//   - iOS: CallSessionManager.swift:1232 keeps all connections,
//     OngoingCallContext.swift:1011-1035 and :92-104 map each of them,
//     OngoingCallThreadLocalContext.mm:1969-1992 an RtcServer of each;
//   - tgcalls (Android 545f5ae, tdesktop 1c236c0, iOS c1de088): every TURN
//     RtcServer goes to the port allocator (v2/NativeNetworkingImpl.cpp:612-626,
//     NetworkManager.cpp:158-176) or to the peer connection's servers
//     (v2/InstanceV2ReferenceImpl.cpp:749-802 at 545f5ae and c1de088,
//     :717-770 at 1c236c0);
//   - Web K: callInstance.ts:735-747 keeps every phoneConnectionWebrtc,
//     p2P/utils.ts:173-203 (buildIceServers) makes an RTCIceServer of each.
package ice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // the TURN REST credential is HMAC-SHA1 by definition
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Server is one configured STUN/TURN endpoint. Host6 is the IPv6 address of
// a Host given as an IPv4 address. Label names the secret of a TURN server
// in Secrets; empty: the shared TurnSecret.
type Server struct {
	Host  string
	Host6 string
	Port  int32
	Stun  bool
	Turn  bool
	Label string

	secret []byte
}

// Servers are the endpoints; the zero value has none.
type Servers struct {
	list     []Server
	now      func() time.Time
	resolver func(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Secrets are the TURN secrets by label; the zero value has none.
type Secrets struct {
	byLabel map[string][]byte
}

var (
	labelRe = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
	// secretRe is what the node's deploy script accepts for its secret
	// (teamgramd/deploy/ru/deploy.sh, turn_secret): a secret the node would
	// refuse is refused here too, not found out at the node's deploy.
	secretRe = regexp.MustCompile(`^[A-Za-z0-9]{32,128}$`)
)

// ParseSecrets reads "label=secret,label=secret": a label is 1 to 32
// lowercase letters and digits, a secret 32 to 128 letters and digits
// (openssl rand -hex 32). Errors never quote a secret. A secret no entry
// uses is allowed: taking a node out of the server list leaves its secret.
func ParseSecrets(cfg string) (Secrets, error) {
	s := Secrets{byLabel: map[string][]byte{}}
	for i, entry := range strings.Split(cfg, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		label, secret, ok := strings.Cut(entry, "=")
		if !ok {
			return Secrets{}, fmt.Errorf("entry %d: want label=secret", i+1)
		}
		if !labelRe.MatchString(label) {
			return Secrets{}, fmt.Errorf("entry %d: a label is 1 to 32 lowercase letters and digits", i+1)
		}
		if _, dup := s.byLabel[label]; dup {
			return Secrets{}, fmt.Errorf("the label %q is set twice", label)
		}
		if !secretRe.MatchString(secret) {
			return Secrets{}, fmt.Errorf("the secret of %q must be 32 to 128 letters and digits (openssl rand -hex 32)", label)
		}
		s.byLabel[label] = []byte(secret)
	}
	return s, nil
}

// Parse reads "host:port:stun+turn,host:port:turn@label". A host may be an
// IPv4 address, a host name or an IPv6 address in square brackets; the
// flag stun or turn or both separated by "+" says what it serves. A TURN
// entry's credentials are made from the secret of its label in secrets,
// which must be there, or from the shared secret, which must then be set.
// An IPv6 address is one more address of the entry with an IPv4 address
// and the same port, flags and label: a connection needs an IPv4 address
// (see Connections).
func Parse(cfg, secret string, secrets Secrets) (*Servers, error) {
	s := &Servers{now: time.Now, resolver: net.DefaultResolver.LookupIPAddr}
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
		srv := Server{Host: host, Port: int32(p)} //nolint:gosec // G109: p is at most 65535, checked above
		kinds, label, labelled := strings.Cut(kinds, "@")
		if labelled && !labelRe.MatchString(label) {
			return nil, fmt.Errorf("%q: a label is 1 to 32 lowercase letters and digits", entry)
		}
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
		switch {
		case labelled && !srv.Turn:
			return nil, fmt.Errorf("%q: a label names the secret of a TURN server", entry)
		case labelled:
			sec, ok := secrets.byLabel[label]
			if !ok {
				return nil, fmt.Errorf("%q: TurnSecrets has no secret labelled %q", entry, label)
			}
			srv.Label, srv.secret = label, sec
		case srv.Turn && secret == "":
			return nil, errors.New("a TURN server without a label needs a TurnSecret")
		case srv.Turn:
			srv.secret = []byte(secret)
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
			if o.Host6 == "" && o.Port == srv.Port && o.Stun == srv.Stun && o.Turn == srv.Turn && o.Label == srv.Label && net.ParseIP(o.Host) != nil {
				o.Host6, paired = srv.Host, true
				break
			}
		}
		if !paired {
			return fmt.Errorf("[%s]:%d: an IPv6 address needs an IPv4 entry with the same port, kinds and label", srv.Host, srv.Port)
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
// A host without an IPv4 address is left out: Android takes the server's
// host from ip (jni/voip/org_telegram_messenger_voip_Instance.cpp:960), Web K
// makes an URL of it unchecked (p2P/utils.ts:179) and tdesktop a TURN
// server (calls_call.cpp:182). A TURN credential lives as long as a call
// may; each TURN server's is made from its own secret.
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
			conn.Password = credential(srv.secret, username)
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
