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
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/dtls/v3"
)

// sctpConn lends the DTLS connection to one SCTP association. An
// association closes its connection when it ends (pion sctp
// Association.close), but DTLS has to outlive it: tgcalls starts a new
// association on the same DTLS when its SCTP transport fails
// (SctpDataChannelProviderInterfaceImpl.cpp:118-124, GroupNetworkManager.cpp:451-485).
// Closing an sctpConn only ends the association's reading, through the
// DTLS read deadline, which DTLS keeps to itself (pion dtls Conn.SetReadDeadline).
type sctpConn struct {
	*dtls.Conn
	once   sync.Once
	closed atomic.Bool
}

// lendSCTP prepares conn for a new association.
func lendSCTP(conn *dtls.Conn) *sctpConn {
	_ = conn.SetReadDeadline(time.Time{})
	return &sctpConn{Conn: conn}
}

func (c *sctpConn) Read(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	n, err := c.Conn.Read(p)
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return n, err
}

func (c *sctpConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}

func (c *sctpConn) Close() error {
	c.once.Do(func() {
		c.closed.Store(true)
		_ = c.Conn.SetReadDeadline(time.Now())
	})
	return nil
}
