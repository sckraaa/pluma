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
	"sync"

	"github.com/pion/stun/v4"
)

// Role is the ICE role the SFU takes towards one client.
type Role int

const (
	// RoleUnknown: no connectivity check has arrived yet.
	RoleUnknown Role = iota
	// RoleControlling: the client is ICE-controlled, as every native client
	// is (tgcalls GroupNetworkManager.cpp:419-421); the SFU nominates.
	RoleControlling
	// RoleControlled: the client is a PeerConnection that took the
	// controlling role, as Web does.
	RoleControlled
)

func (r Role) String() string {
	switch r {
	case RoleControlling:
		return "controlling"
	case RoleControlled:
		return "controlled"
	}
	return "unknown"
}

// roleSniffer picks the SFU's role from the first authentic connectivity
// check of the client: a check with ICE-CONTROLLED means the SFU controls,
// one with ICE-CONTROLLING means the client does (RFC 8445 §7.1.3). The
// client only learns the SFU's candidates from the answer and gives none of
// its own (tgcalls sends none in the join params), so its checks always
// come first. A check is authentic when its USERNAME is ours and its
// MESSAGE-INTEGRITY verifies with our password, so a stranger who knows the
// address cannot choose the role.
type roleSniffer struct {
	username string
	key      stun.MessageIntegrity

	once   sync.Once
	decide func(Role)
}

func newRoleSniffer(localUfrag, remoteUfrag, localPwd string, decide func(Role)) *roleSniffer {
	return &roleSniffer{
		username: localUfrag + ":" + remoteUfrag,
		key:      stun.NewShortTermIntegrity(localPwd),
		decide:   decide,
	}
}

// observe looks at one packet the agent received.
func (s *roleSniffer) observe(p []byte) {
	if !stun.IsMessage(p) {
		return
	}
	m := &stun.Message{Raw: append([]byte(nil), p...)}
	if err := m.Decode(); err != nil {
		return
	}
	if m.Type != stun.BindingRequest {
		return
	}
	var username stun.Username
	if err := username.GetFrom(m); err != nil || username.String() != s.username {
		return
	}
	if err := s.key.Check(m); err != nil {
		return
	}
	role := RoleControlling
	if m.Contains(stun.AttrICEControlling) {
		role = RoleControlled
	}
	s.once.Do(func() { s.decide(role) })
}
