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

package webpage

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
)

// The server fetches pages users name in their messages: it must never
// reach the networks it sits in (the services, the cloud's metadata
// endpoint, the operator's LAN). An address is checked when it is dialled,
// after DNS, so a name that resolves, or later re-resolves, to an internal
// address is refused too (DNS rebinding); a link that names an internal
// address is refused before any request, and Telegram then answers without
// a preview (messageMediaEmpty, observed 2026-11-02 for http://192.168.0.1).

// ErrNotPublic is the error of an address outside the public internet.
var ErrNotPublic = errors.New("webpage: not a public address")

// notPublic lists the ranges that are not the public internet beyond what
// netip's predicates cover (IANA special-purpose registries).
var notPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: may lead to any IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("100::/64"),        // discard
	netip.MustParsePrefix("2001::/23"),       // IETF protocol assignments (Teredo among them)
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: may lead to any IPv4
	netip.MustParsePrefix("5f00::/16"),       // segment routing
}

// publicAddr reports whether a is an address of the public internet.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// dialControl refuses a connection to an address that is not public. It
// runs for every address the dialer tries, after the name is resolved.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !publicAddr(a) {
		return fmt.Errorf("%w: %s", ErrNotPublic, a)
	}
	return nil
}

// fetchable checks a link before any request: http or https, a host, no
// user info, and a host that is not an internal address or name.
func fetchable(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webpage: scheme %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("webpage: user info in the link")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return errors.New("webpage: no host")
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if !publicAddr(a) {
			return fmt.Errorf("%w: %s", ErrNotPublic, a)
		}
		return nil
	}
	// Names that only resolve inside a network.
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home.arpa") ||
		!strings.Contains(host, ".") {
		return fmt.Errorf("%w: %s", ErrNotPublic, host)
	}
	return nil
}
