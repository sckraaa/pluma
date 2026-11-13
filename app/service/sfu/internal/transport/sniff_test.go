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

package transport

import (
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v4"
)

const (
	sfuUfrag    = "sfuU"
	sfuPwd      = "sfu-password-of-24-chars"
	clientUfrag = "cliU"
)

// check builds a connectivity check of the client as RFC 8445 §7.1.2
// describes it.
func check(t *testing.T, username, pwd string, role stun.Setter) []byte {
	t.Helper()
	setters := []stun.Setter{stun.BindingRequest, stun.TransactionID, stun.NewUsername(username)}
	if role != nil {
		setters = append(setters, role)
	}
	setters = append(setters, stun.NewShortTermIntegrity(pwd), stun.Fingerprint)
	m, err := stun.Build(setters...)
	if err != nil {
		t.Fatal(err)
	}
	return m.Raw
}

func TestRoleFromFirstCheck(t *testing.T) {
	controlled := ice.AttrControlled(1)
	controlling := ice.AttrControlling(2)
	tests := []struct {
		name  string
		first []byte
		want  Role
	}{
		// tgcalls: ICE-controlled (GroupNetworkManager.cpp:419-421).
		{"native client", check(t, sfuUfrag+":"+clientUfrag, sfuPwd, controlled), RoleControlling},
		// A PeerConnection that took the controlling role.
		{"peer connection", check(t, sfuUfrag+":"+clientUfrag, sfuPwd, controlling), RoleControlled},
		{"no role attribute", check(t, sfuUfrag+":"+clientUfrag, sfuPwd, nil), RoleControlling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Role
			s := newRoleSniffer(sfuUfrag, clientUfrag, sfuPwd, func(r Role) { got = append(got, r) })
			s.observe(tt.first)
			// Later checks never change the decision.
			s.observe(check(t, sfuUfrag+":"+clientUfrag, sfuPwd, controlling))
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("decided %v, want [%v]", got, tt.want)
			}
		})
	}
}

func TestRoleIgnoresForgedChecks(t *testing.T) {
	var got []Role
	s := newRoleSniffer(sfuUfrag, clientUfrag, sfuPwd, func(r Role) { got = append(got, r) })
	s.observe(check(t, sfuUfrag+":"+clientUfrag, "wrong-password-of-24-chr", ice.AttrControlling(2)))
	s.observe(check(t, sfuUfrag+":other", sfuPwd, ice.AttrControlling(2)))
	resp, err := stun.Build(stun.BindingSuccess, stun.TransactionID, stun.NewUsername(sfuUfrag+":"+clientUfrag), stun.NewShortTermIntegrity(sfuPwd))
	if err != nil {
		t.Fatal(err)
	}
	s.observe(resp.Raw)
	s.observe([]byte{0x80, 0x60, 0, 1}) // RTP
	if len(got) != 0 {
		t.Fatalf("decided %v on checks that are not the client's", got)
	}
}

func TestStunLocalUfrag(t *testing.T) {
	raw := check(t, sfuUfrag+":"+clientUfrag, sfuPwd, ice.AttrControlled(1))
	if u, ok := stunLocalUfrag(raw); !ok || u != sfuUfrag {
		t.Fatalf("%q %v", u, ok)
	}
	if _, ok := stunLocalUfrag(raw[:30]); ok {
		t.Error("truncated message parsed")
	}
	bad := append([]byte(nil), raw...)
	binary.BigEndian.PutUint32(bad[4:8], 0)
	if _, ok := stunLocalUfrag(bad); ok {
		t.Error("message without the magic cookie parsed")
	}
	noUser, err := stun.Build(stun.BindingRequest, stun.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stunLocalUfrag(noUser.Raw); ok {
		t.Error("message without USERNAME parsed")
	}
}

func TestRegistryRoutesByUfrag(t *testing.T) {
	r := &roleRegistry{pending: map[string]*roleSniffer{}}
	var got []Role
	r.add(sfuUfrag, newRoleSniffer(sfuUfrag, clientUfrag, sfuPwd, func(role Role) { got = append(got, role) }))
	r.observe(check(t, "else:"+clientUfrag, sfuPwd, ice.AttrControlling(2)))
	if len(got) != 0 {
		t.Fatal("decided on another endpoint's check")
	}
	r.observe(check(t, sfuUfrag+":"+clientUfrag, sfuPwd, ice.AttrControlling(2)))
	if len(got) != 1 || got[0] != RoleControlled {
		t.Fatalf("decided %v", got)
	}
	r.remove(sfuUfrag)
	if len(r.pending) != 0 {
		t.Error("not removed")
	}
}

func TestObservedTCPConnFirstFrame(t *testing.T) {
	r := &roleRegistry{pending: map[string]*roleSniffer{}}
	var got []Role
	r.add(sfuUfrag, newRoleSniffer(sfuUfrag, clientUfrag, sfuPwd, func(role Role) { got = append(got, role) }))
	msg := check(t, sfuUfrag+":"+clientUfrag, sfuPwd, ice.AttrControlled(1))
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(msg)))
	frame = append(frame, msg...)
	conn := &observedTCPConn{Conn: &chunkConn{data: frame, chunk: 7}, roles: r}
	buf := make([]byte, 5)
	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}
	}
	if len(got) != 1 || got[0] != RoleControlling {
		t.Fatalf("decided %v", got)
	}
}

// chunkConn returns its data a few bytes per Read, as TCP may.
type chunkConn struct {
	net.Conn
	data  []byte
	chunk int
}

func (c *chunkConn) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(c.chunk, len(p), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}
