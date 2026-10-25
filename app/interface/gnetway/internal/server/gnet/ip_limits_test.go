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

package gnet

import (
	"testing"
	"time"
)

func TestIPSubject(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7":          "203.0.113.7",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"::ffff:203.0.113.7":   "203.0.113.7",
		"not an address":       "not an address",
	} {
		if got := ipSubject(in); got != want {
			t.Errorf("ipSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIPLimitsConnections(t *testing.T) {
	l := newIPLimits(2, 60)
	a, b := ipSubject("203.0.113.7"), ipSubject("203.0.113.8")
	for i := 0; i < 2; i++ {
		if !l.openConn(a) {
			t.Fatalf("connection %d within the limit refused", i+1)
		}
	}
	if l.openConn(a) {
		t.Fatal("a third connection of one address accepted")
	}
	if !l.openConn(b) {
		t.Fatal("another address refused")
	}
	l.closeConn(a)
	if !l.openConn(a) {
		t.Fatal("a connection after a close refused")
	}
}

func TestIPLimitsKeyExchanges(t *testing.T) {
	l := newIPLimits(10, 60) // 60 a minute: one a second, a burst of 60
	a := ipSubject("2001:db8::1")
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 60; i++ {
		if !l.allowKeyExchange(a, now) {
			t.Fatalf("key exchange %d of the burst refused", i+1)
		}
	}
	if l.allowKeyExchange(a, now) {
		t.Fatal("a key exchange over the burst accepted")
	}
	// Another address of the same /64 shares the budget.
	if l.allowKeyExchange(ipSubject("2001:db8::2"), now) {
		t.Fatal("the same /64 got a budget of its own")
	}
	if !l.allowKeyExchange(a, now.Add(time.Second)) {
		t.Fatal("a key exchange a second later refused")
	}
	// The budget outlives the connections: reconnecting does not refill it.
	l.openConn(a)
	l.closeConn(a)
	if l.allowKeyExchange(a, now.Add(time.Second)) {
		t.Fatal("the budget was refilled by a reconnect")
	}
}
