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

// Package transport connects the SFU with one client: ICE (role chosen per
// client), DTLS with the SFU as the client, SRTP and the SCTP data channel.
// All clients share one UDP port and one ICE-TCP port (Mux).
package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	"github.com/pion/ice/v4"
)

// MuxConfig says where the SFU listens for media.
type MuxConfig struct {
	// UDP is the address of the UDP socket, such as ":20600". An
	// unspecified IP listens on IPv4 and IPv6 at once.
	UDP string
	// TCP is the address of the ICE-TCP listener; empty turns ICE-TCP off.
	TCP string
	// IncludeLoopback keeps loopback addresses among the local candidates.
	// Only tests on one machine need it.
	IncludeLoopback bool
	// TCPWriteBuffer is the most bytes queued for one ICE-TCP connection;
	// beyond it packets are dropped instead of blocking the sender.
	TCPWriteBuffer int
}

// Mux owns the shared sockets.
//
// It also sees every connectivity check before the ICE agents do. An agent
// reads nothing until it starts (pion ice candidate_base.go recvLoop waits
// for the start), and it must start in the role the client's first check
// asks for; so the mux hands the checks of endpoints that wait for their
// role to them (roles), while the shared mux keeps the packets for the
// agent.
type Mux struct {
	udpConn     *net.UDPConn
	udpMux      *ice.UDPMuxDefault
	tcpListener net.Listener
	tcpMux      *ice.TCPMuxDefault
	loopback    bool
	roles       *roleRegistry

	closeOnce sync.Once
}

// NewMux opens the sockets.
func NewMux(cfg MuxConfig) (*Mux, error) {
	if cfg.UDP == "" {
		return nil, errors.New("transport: no UDP address")
	}
	addr, err := net.ResolveUDPAddr("udp", cfg.UDP)
	if err != nil {
		return nil, fmt.Errorf("transport: UDP address: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: listen UDP: %w", err)
	}
	logger := newLoggerFactory().NewLogger("ice-mux")
	roles := &roleRegistry{pending: map[string]*roleSniffer{}}
	m := &Mux{
		udpConn: udpConn,
		udpMux: ice.NewUDPMuxDefault(ice.UDPMuxParams{
			UDPConn: &observedUDPConn{UDPConn: udpConn, roles: roles},
			Logger:  logger,
		}),
		loopback: cfg.IncludeLoopback,
		roles:    roles,
	}
	if cfg.TCP != "" {
		l, err := net.Listen("tcp", cfg.TCP)
		if err != nil {
			_ = m.Close()
			return nil, fmt.Errorf("transport: listen TCP: %w", err)
		}
		writeBuffer := cfg.TCPWriteBuffer
		if writeBuffer <= 0 {
			writeBuffer = 4 << 20
		}
		m.tcpListener = l
		m.tcpMux = ice.NewTCPMuxDefault(ice.TCPMuxParams{
			Listener:        &observedListener{Listener: l, roles: roles},
			Logger:          logger,
			ReadBufferSize:  64,
			WriteBufferSize: writeBuffer,
		})
	}
	return m, nil
}

// UDPAddr is the local address of the UDP socket.
func (m *Mux) UDPAddr() *net.UDPAddr {
	addr, _ := m.udpConn.LocalAddr().(*net.UDPAddr)
	return addr
}

// TCPAddr is the local address of the ICE-TCP listener, nil without one.
func (m *Mux) TCPAddr() *net.TCPAddr {
	if m.tcpListener == nil {
		return nil
	}
	addr, _ := m.tcpListener.Addr().(*net.TCPAddr)
	return addr
}

// Close closes the sockets; endpoints using them fail.
func (m *Mux) Close() error {
	var errs []error
	m.closeOnce.Do(func() {
		if m.tcpMux != nil {
			errs = append(errs, m.tcpMux.Close())
		}
		if m.tcpListener != nil {
			if err := m.tcpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
		}
		if err := m.udpMux.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	})
	return errors.Join(errs...)
}

// roleRegistry holds the sniffers of the endpoints that wait for their
// role, by the SFU's ufrag.
type roleRegistry struct {
	mu      sync.RWMutex
	pending map[string]*roleSniffer
}

func (r *roleRegistry) add(localUfrag string, s *roleSniffer) {
	r.mu.Lock()
	r.pending[localUfrag] = s
	r.mu.Unlock()
}

func (r *roleRegistry) remove(localUfrag string) {
	r.mu.Lock()
	delete(r.pending, localUfrag)
	r.mu.Unlock()
}

// observe looks at a packet that arrived on a shared socket. Only STUN
// messages (first two bits 0, RFC 7983) while some endpoint waits are
// parsed.
func (r *roleRegistry) observe(p []byte) {
	if len(p) < 20 || p[0] > 3 {
		return
	}
	r.mu.RLock()
	empty := len(r.pending) == 0
	r.mu.RUnlock()
	if empty {
		return
	}
	localUfrag, ok := stunLocalUfrag(p)
	if !ok {
		return
	}
	r.mu.RLock()
	s := r.pending[localUfrag]
	r.mu.RUnlock()
	if s != nil {
		s.observe(p)
	}
}

// observedUDPConn passes what the UDP socket reads to the registry. It keeps
// the allocation-free AddrPort methods the mux prefers (ice addr.go
// AddrPortReaderWriter).
type observedUDPConn struct {
	*net.UDPConn
	roles *roleRegistry
}

func (c *observedUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.UDPConn.ReadFrom(p)
	if err == nil {
		c.roles.observe(p[:n])
	}
	return n, addr, err
}

func (c *observedUDPConn) ReadFromAddrPort(p []byte) (int, netip.AddrPort, error) {
	n, addr, err := c.UDPConn.ReadFromUDPAddrPort(p)
	if err == nil {
		c.roles.observe(p[:n])
	}
	return n, addr, err
}

func (c *observedUDPConn) WriteToAddrPort(p []byte, addr netip.AddrPort) (int, error) {
	return c.UDPConn.WriteToUDPAddrPort(p, addr)
}

// observedListener wraps the ICE-TCP connections it accepts.
type observedListener struct {
	net.Listener
	roles *roleRegistry
}

func (l *observedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &observedTCPConn{Conn: conn, roles: l.roles}, nil
}

// observedTCPConn passes the first framed packet of an ICE-TCP connection
// (RFC 4571 §2: a 16-bit length, then the packet) to the registry. The
// first packet is the client's check; the mux routes the connection by it
// too (ice tcp_mux.go).
type observedTCPConn struct {
	net.Conn
	roles *roleRegistry
	head  []byte
	done  bool
}

// maxFirstFrame bounds what is kept of the first frame; a check is about
// 100 bytes.
const maxFirstFrame = 1500

func (c *observedTCPConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && !c.done {
		c.head = append(c.head, p[:n]...)
		if len(c.head) >= 2 {
			size := int(binary.BigEndian.Uint16(c.head))
			switch {
			case size > maxFirstFrame:
				c.done, c.head = true, nil
			case len(c.head) >= 2+size:
				c.roles.observe(c.head[2 : 2+size])
				c.done, c.head = true, nil
			}
		}
	}
	return n, err
}

// stunLocalUfrag reads the SFU's ufrag from the USERNAME of a STUN message
// ("local:remote", RFC 8445 §7.2.2) without decoding the rest.
func stunLocalUfrag(p []byte) (string, bool) {
	const headerSize = 20
	if len(p) < headerSize || binary.BigEndian.Uint32(p[4:8]) != 0x2112A442 {
		return "", false
	}
	end := headerSize + int(binary.BigEndian.Uint16(p[2:4]))
	if end > len(p) {
		return "", false
	}
	for off := headerSize; off+4 <= end; {
		typ := binary.BigEndian.Uint16(p[off:])
		size := int(binary.BigEndian.Uint16(p[off+2:]))
		if off+4+size > end {
			return "", false
		}
		if typ == 0x0006 { // USERNAME
			v := p[off+4 : off+4+size]
			for i, b := range v {
				if b == ':' {
					return string(v[:i]), true
				}
			}
			return "", false
		}
		off += 4 + (size+3)&^3
	}
	return "", false
}
