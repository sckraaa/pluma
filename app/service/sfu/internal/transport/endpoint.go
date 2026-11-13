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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/ice/v4"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sctp"
	"github.com/pion/srtp/v3"
)

// Errors an endpoint closes with.
var (
	ErrStartTimeout = errors.New("transport: no connectivity check from the client")
	ErrICEFailed    = errors.New("transport: ICE failed")
	ErrDTLSFailed   = errors.New("transport: DTLS failed")
	ErrClosed       = errors.New("transport: closed")
	ErrNotConnected = errors.New("transport: not connected")
)

// Certificate is the DTLS certificate of the SFU and the sha-256 digest of
// it that goes into the answers.
type Certificate struct {
	TLS    tls.Certificate
	SHA256 []byte
}

// NewCertificate makes a self-signed ECDSA certificate, as WebRTC endpoints
// do (tgcalls makes one per call, GroupNetworkManager.cpp:357).
func NewCertificate() (*Certificate, error) {
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(cert.Certificate[0])
	return &Certificate{TLS: cert, SHA256: digest[:]}, nil
}

// Handler receives what an endpoint gets. Its methods run on the endpoint's
// goroutines and must not block for long: OnRTP and OnRTCP hold up the
// reading of the client's packets.
type Handler interface {
	// OnConnected: DTLS is done and media can flow.
	OnConnected(e *Endpoint)
	// OnRTP gets a decrypted packet; the endpoint does not touch it again.
	OnRTP(e *Endpoint, pkt *rtp.Packet, arrival time.Time)
	OnRTCP(e *Endpoint, pkts []rtcp.Packet, arrival time.Time)
	// OnDataOpen: the client opened the data channel.
	OnDataOpen(e *Endpoint)
	// OnData gets a text message of the data channel.
	OnData(e *Endpoint, msg []byte)
	// OnClosed is called once, with the reason.
	OnClosed(e *Endpoint, reason error)
}

// EndpointConfig is one client's side of the connection.
type EndpointConfig struct {
	RemoteUfrag string
	RemotePwd   string
	// RemoteFingerprint is the sha-256 digest of the client's certificate.
	RemoteFingerprint []byte
	Certificate       *Certificate
	Handler           Handler

	// StartTimeout bounds the wait for the client's first check.
	StartTimeout time.Duration
	// ConnectTimeout bounds ICE and DTLS after that.
	ConnectTimeout      time.Duration
	DisconnectedTimeout time.Duration
	FailedTimeout       time.Duration
	KeepaliveInterval   time.Duration
	// Renomination lets the SFU, when it controls, move to a better pair
	// later (draft-thatcher-ice-renomination, the NOMINATION attribute
	// 0xC001 that libwebrtc reads on the controlled side whatever its own
	// settings, webrtc p2p/base/connection.cc:642-660 in Android's copy).
	Renomination bool
}

const (
	defaultStartTimeout   = 20 * time.Second
	defaultConnectTimeout = 15 * time.Second
	receiveMTU            = 1500
	dtlsQueue             = 64
	// renominationInterval is how often the SFU, when it controls, looks
	// for a better pair, such as one over a new network of the client.
	renominationInterval = 3 * time.Second
	// replayWindow covers reordering and retransmissions of a few seconds
	// of video.
	replayWindow = 1024
)

// Endpoint is the SFU's connection with one client.
type Endpoint struct {
	cfg        EndpointConfig
	roles      *roleRegistry
	localUfrag string
	localPwd   string
	agent      *ice.Agent

	ctx    context.Context
	cancel context.CancelFunc

	roleCh chan Role
	role   atomic.Int32

	// iceConn is set before readLoop and the writers start.
	iceConn *ice.Conn
	writeMu sync.Mutex
	// mu guards dtlsIn and dtlsConn, which Close reads.
	mu        sync.Mutex
	dtlsIn    *dtlsPacketConn
	dtlsConn  *dtls.Conn
	connected atomic.Bool

	srtpMu   sync.Mutex
	local    *srtp.Context
	remote   *srtp.Context
	encBuf   []byte
	plainBuf []byte

	dcMu sync.Mutex
	dc   *datachannel.DataChannel

	closeOnce sync.Once
	done      chan struct{}
}

// NewEndpoint prepares the connection with a client and gathers the local
// candidates. Start begins it.
func (m *Mux) NewEndpoint(cfg EndpointConfig) (*Endpoint, error) {
	if cfg.Handler == nil || cfg.Certificate == nil || len(cfg.RemoteFingerprint) != sha256.Size {
		return nil, errors.New("transport: incomplete endpoint config")
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = defaultStartTimeout
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultConnectTimeout
	}
	ufrag, err := randomICEString(8)
	if err != nil {
		return nil, err
	}
	pwd, err := randomICEString(24)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Endpoint{
		cfg:        cfg,
		localUfrag: ufrag,
		localPwd:   pwd,
		ctx:        ctx,
		cancel:     cancel,
		roleCh:     make(chan Role, 1),
		done:       make(chan struct{}),
	}
	e.roles = m.roles
	m.roles.add(ufrag, newRoleSniffer(ufrag, cfg.RemoteUfrag, pwd, func(r Role) {
		m.roles.remove(ufrag)
		select {
		case e.roleCh <- r:
		default:
		}
	}))

	networks := []ice.NetworkType{ice.NetworkTypeUDP4, ice.NetworkTypeUDP6}
	opts := []ice.AgentOption{
		ice.WithLocalCredentials(ufrag, pwd),
		ice.WithUDPMux(m.udpMux),
		ice.WithCandidateTypes([]ice.CandidateType{ice.CandidateTypeHost}),
		ice.WithMulticastDNSMode(ice.MulticastDNSModeDisabled),
		// The SFU only listens: it learns the client's addresses from its
		// checks (peer-reflexive candidates).
		ice.WithDisableActiveTCP(),
		// Those are the only remote candidates there are (tgcalls sends
		// none, GroupNetworkManager.cpp:398-421), so the wait of pion before
		// it nominates one (a second by default, for better kinds to turn
		// up) would only delay every join; renomination still moves to a
		// better pair later.
		ice.WithPrflxAcceptanceMinWait(0),
		ice.WithLoggerFactory(newLoggerFactory()),
	}
	if m.tcpMux != nil {
		networks = append(networks, ice.NetworkTypeTCP4, ice.NetworkTypeTCP6)
		opts = append(opts, ice.WithTCPMux(m.tcpMux))
	}
	opts = append(opts, ice.WithNetworkTypes(networks))
	if m.loopback {
		opts = append(opts, ice.WithIncludeLoopback())
	}
	if cfg.DisconnectedTimeout > 0 {
		opts = append(opts, ice.WithDisconnectedTimeout(cfg.DisconnectedTimeout))
	}
	if cfg.FailedTimeout > 0 {
		opts = append(opts, ice.WithFailedTimeout(cfg.FailedTimeout))
	}
	if cfg.KeepaliveInterval > 0 {
		opts = append(opts, ice.WithKeepaliveInterval(cfg.KeepaliveInterval))
	}
	if cfg.Renomination {
		opts = append(opts,
			ice.WithRenomination(ice.DefaultNominationValueGenerator()),
			ice.WithAutomaticRenomination(renominationInterval),
		)
	}
	agent, err := ice.NewAgentWithOptions(opts...)
	if err != nil {
		m.roles.remove(ufrag)
		cancel()
		return nil, fmt.Errorf("transport: ICE agent: %w", err)
	}
	e.agent = agent
	if err := agent.OnCandidate(func(ice.Candidate) {}); err != nil {
		e.fail(err)
		return nil, err
	}
	if err := agent.OnConnectionStateChange(func(s ice.ConnectionState) {
		if s == ice.ConnectionStateFailed {
			// Not on the agent's own goroutine: fail closes the agent.
			go e.fail(ErrICEFailed)
		}
	}); err != nil {
		e.fail(err)
		return nil, err
	}
	if err := agent.GatherCandidates(); err != nil {
		e.fail(err)
		return nil, fmt.Errorf("transport: gather: %w", err)
	}
	return e, nil
}

// LocalUfrag and LocalPwd are the SFU's ICE credentials for the answer.
func (e *Endpoint) LocalUfrag() string { return e.localUfrag }
func (e *Endpoint) LocalPwd() string   { return e.localPwd }

// Role is the ICE role the SFU took; RoleUnknown before the first check.
func (e *Endpoint) Role() Role { return Role(e.role.Load()) }

// Connected reports whether media can be sent.
func (e *Endpoint) Connected() bool { return e.connected.Load() }

// Done is closed when the endpoint is closed.
func (e *Endpoint) Done() <-chan struct{} { return e.done }

// Start connects in the background. The result comes to the handler.
func (e *Endpoint) Start() {
	go e.run()
}

// Close closes the endpoint; the handler gets OnClosed with ErrClosed if
// nothing else closed it first.
func (e *Endpoint) Close() { e.fail(ErrClosed) }

func (e *Endpoint) fail(reason error) {
	e.closeOnce.Do(func() {
		e.roles.remove(e.localUfrag)
		e.connected.Store(false)
		e.cancel()
		e.dcMu.Lock()
		if e.dc != nil {
			_ = e.dc.Close()
		}
		e.dcMu.Unlock()
		e.mu.Lock()
		if e.dtlsConn != nil {
			_ = e.dtlsConn.Close()
		}
		if e.dtlsIn != nil {
			_ = e.dtlsIn.Close()
		}
		e.mu.Unlock()
		if e.agent != nil {
			_ = e.agent.Close()
		}
		close(e.done)
		e.cfg.Handler.OnClosed(e, reason)
	})
}

func (e *Endpoint) run() {
	var role Role
	timer := time.NewTimer(e.cfg.StartTimeout)
	select {
	case role = <-e.roleCh:
		timer.Stop()
	case <-timer.C:
		e.fail(ErrStartTimeout)
		return
	case <-e.ctx.Done():
		timer.Stop()
		return
	}
	e.role.Store(int32(role))

	var err error
	if role == RoleControlled {
		e.iceConn, err = e.agent.StartAccept(e.cfg.RemoteUfrag, e.cfg.RemotePwd)
	} else {
		e.iceConn, err = e.agent.StartDial(e.cfg.RemoteUfrag, e.cfg.RemotePwd)
	}
	if err != nil {
		e.fail(fmt.Errorf("%w: %v", ErrICEFailed, err))
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.cfg.ConnectTimeout)
	defer cancel()
	if err := e.agent.AwaitConnect(ctx); err != nil {
		e.fail(fmt.Errorf("%w: %v", ErrICEFailed, err))
		return
	}

	dtlsIn := newDTLSPacketConn(e.iceConn, &e.writeMu)
	e.mu.Lock()
	e.dtlsIn = dtlsIn
	e.mu.Unlock()
	if e.ctx.Err() != nil {
		_ = dtlsIn.Close()
		return
	}
	go e.readLoop(dtlsIn)

	conn, err := e.handshake(ctx, dtlsIn)
	if err != nil {
		e.fail(fmt.Errorf("%w: %v", ErrDTLSFailed, err))
		return
	}
	e.connected.Store(true)
	e.cfg.Handler.OnConnected(e)
	go e.runDataChannel(conn)
}

// handshake runs DTLS as the client and sets SRTP up from it (RFC 5764).
func (e *Endpoint) handshake(ctx context.Context, dtlsIn *dtlsPacketConn) (*dtls.Conn, error) {
	conn, err := dtls.ClientWithOptions(dtlsIn, e.iceConn.RemoteAddr(),
		dtls.WithCertificates(e.cfg.Certificate.TLS),
		dtls.WithSRTPProtectionProfiles(
			// tgcalls offers the GCM profiles only (GroupNetworkManager.cpp:322-326).
			dtls.SRTP_AEAD_AES_128_GCM,
			dtls.SRTP_AEAD_AES_256_GCM,
			dtls.SRTP_AES128_CM_HMAC_SHA1_80,
		),
		dtls.WithExtendedMasterSecret(dtls.RequestExtendedMasterSecret),
		// The certificate is self-signed; it is checked against the
		// fingerprint from the join params instead.
		dtls.WithInsecureSkipVerify(true),
		dtls.WithVerifyPeerCertificate(e.verifyPeer),
		dtls.WithLoggerFactory(newLoggerFactory()),
	)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.dtlsConn = conn
	e.mu.Unlock()
	if e.ctx.Err() != nil {
		_ = conn.Close()
		return nil, ErrClosed
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	if err := e.setupSRTP(conn); err != nil {
		return nil, err
	}
	return conn, nil
}

// setupSRTP derives the SRTP keys from DTLS (RFC 5764 §4.2).
func (e *Endpoint) setupSRTP(conn *dtls.Conn) error {
	dtlsProfile, ok := conn.SelectedSRTPProtectionProfile()
	if !ok {
		return errors.New("no SRTP profile negotiated")
	}
	var profile srtp.ProtectionProfile
	switch dtlsProfile {
	case dtls.SRTP_AEAD_AES_128_GCM:
		profile = srtp.ProtectionProfileAeadAes128Gcm
	case dtls.SRTP_AEAD_AES_256_GCM:
		profile = srtp.ProtectionProfileAeadAes256Gcm
	case dtls.SRTP_AES128_CM_HMAC_SHA1_80:
		profile = srtp.ProtectionProfileAes128CmHmacSha1_80
	default:
		return fmt.Errorf("SRTP profile %d", dtlsProfile)
	}
	state, ok := conn.ConnectionState()
	if !ok {
		return errors.New("no DTLS state")
	}
	cfg := srtp.Config{Profile: profile}
	if err := cfg.ExtractSessionKeysFromDTLS(&state, true); err != nil {
		return err
	}
	local, err := srtp.CreateContext(cfg.Keys.LocalMasterKey, cfg.Keys.LocalMasterSalt, profile)
	if err != nil {
		return err
	}
	remote, err := srtp.CreateContext(cfg.Keys.RemoteMasterKey, cfg.Keys.RemoteMasterSalt, profile,
		srtp.SRTPReplayProtection(replayWindow), srtp.SRTCPReplayProtection(replayWindow))
	if err != nil {
		return err
	}
	e.srtpMu.Lock()
	e.local, e.remote = local, remote
	e.srtpMu.Unlock()
	return nil
}

func (e *Endpoint) verifyPeer(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("no certificate")
	}
	digest := sha256.Sum256(rawCerts[0])
	if subtle.ConstantTimeCompare(digest[:], e.cfg.RemoteFingerprint) != 1 {
		return errors.New("certificate does not match the fingerprint")
	}
	return nil
}

// readLoop sorts the packets of the ICE connection (RFC 7983 §7): DTLS goes
// to the DTLS connection, RTP and RTCP are decrypted and handed over.
func (e *Endpoint) readLoop(dtlsIn *dtlsPacketConn) {
	buf := make([]byte, receiveMTU)
	for {
		n, err := e.iceConn.Read(buf)
		if err != nil {
			e.fail(fmt.Errorf("%w: %v", ErrICEFailed, err))
			return
		}
		if n == 0 {
			continue
		}
		switch b := buf[0]; {
		case b >= 20 && b <= 63:
			dtlsIn.push(buf[:n])
		case b >= 128 && b <= 191:
			e.handleSRTP(buf[:n], time.Now())
		}
	}
}

func (e *Endpoint) handleSRTP(data []byte, arrival time.Time) {
	if len(data) < 12 {
		return
	}
	e.srtpMu.Lock()
	remote := e.remote
	e.srtpMu.Unlock()
	if remote == nil {
		return
	}
	// RTCP packet types are 192-223 in the second byte (RFC 5761 §4).
	if pt := data[1]; pt >= 192 && pt <= 223 {
		plain, err := remote.DecryptRTCP(nil, data, nil)
		if err != nil {
			return
		}
		pkts, err := rtcp.Unmarshal(plain)
		if err != nil || len(pkts) == 0 {
			return
		}
		e.cfg.Handler.OnRTCP(e, pkts, arrival)
		return
	}
	var hdr rtp.Header
	plain, err := remote.DecryptRTP(nil, data, &hdr)
	if err != nil {
		return
	}
	pkt := &rtp.Packet{}
	if err := pkt.Unmarshal(plain); err != nil {
		return
	}
	e.cfg.Handler.OnRTP(e, pkt, arrival)
}

// WriteRTP encrypts and sends a packet.
func (e *Endpoint) WriteRTP(pkt *rtp.Packet) error {
	if !e.connected.Load() {
		return ErrNotConnected
	}
	e.srtpMu.Lock()
	n, err := pkt.MarshalTo(growBuf(&e.plainBuf, pkt.MarshalSize()))
	if err != nil {
		e.srtpMu.Unlock()
		return err
	}
	out, err := e.local.EncryptRTP(e.encBuf[:0], e.plainBuf[:n], &pkt.Header)
	if err != nil {
		e.srtpMu.Unlock()
		return err
	}
	e.encBuf = out
	err = e.write(out)
	e.srtpMu.Unlock()
	return err
}

// WriteRTCP encrypts and sends a compound RTCP packet.
func (e *Endpoint) WriteRTCP(pkts []rtcp.Packet) error {
	if !e.connected.Load() {
		return ErrNotConnected
	}
	plain, err := rtcp.Marshal(pkts)
	if err != nil {
		return err
	}
	e.srtpMu.Lock()
	out, err := e.local.EncryptRTCP(e.encBuf[:0], plain, nil)
	if err != nil {
		e.srtpMu.Unlock()
		return err
	}
	e.encBuf = out
	err = e.write(out)
	e.srtpMu.Unlock()
	return err
}

func (e *Endpoint) write(p []byte) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_, err := e.iceConn.Write(p)
	return err
}

func growBuf(buf *[]byte, n int) []byte {
	if cap(*buf) < n {
		*buf = make([]byte, n, n+256)
	}
	*buf = (*buf)[:n]
	return *buf
}

// SendData sends a text message on the data channel.
func (e *Endpoint) SendData(msg []byte) error {
	e.dcMu.Lock()
	dc := e.dc
	e.dcMu.Unlock()
	if dc == nil {
		return ErrNotConnected
	}
	_, err := dc.WriteDataChannel(msg, true)
	return err
}

// runDataChannel runs SCTP over DTLS and accepts the client's data channel.
// tgcalls opens stream 0 labelled "data" and starts SCTP on ports
// 5000/5000 (SctpDataChannelProviderInterfaceImpl.cpp:30-41, 106); both
// sides send INIT, as pion/webrtc does for every role. When the client
// restarts SCTP, a new association takes the place of the old one.
func (e *Endpoint) runDataChannel(conn *dtls.Conn) {
	for e.ctx.Err() == nil {
		assoc, err := sctp.ClientContext(e.ctx,
			sctp.WithNetConn(lendSCTP(conn)),
			sctp.WithLoggerFactory(newLoggerFactory()),
			sctp.WithMaxReceiveBufferSize(1<<20),
		)
		if err != nil {
			// Media works without the data channel; the client only
			// loses video constraints.
			return
		}
		e.acceptDataChannels(assoc)
		_ = assoc.Close()
	}
}

// acceptDataChannels accepts the client's channels until the association
// ends; the last one opened is the one the SFU writes to.
func (e *Endpoint) acceptDataChannels(assoc *sctp.Association) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-e.ctx.Done():
			_ = assoc.Close()
		case <-stop:
		}
	}()
	for {
		dc, err := datachannel.Accept(assoc, &datachannel.Config{LoggerFactory: newLoggerFactory()})
		if err != nil {
			return
		}
		e.dcMu.Lock()
		if e.ctx.Err() != nil {
			e.dcMu.Unlock()
			_ = dc.Close()
			return
		}
		if e.dc != nil {
			_ = e.dc.Close()
		}
		e.dc = dc
		e.dcMu.Unlock()
		e.cfg.Handler.OnDataOpen(e)
		go e.readDataChannel(dc)
	}
}

func (e *Endpoint) readDataChannel(dc *datachannel.DataChannel) {
	buf := make([]byte, 64<<10)
	for {
		n, isString, err := dc.ReadDataChannel(buf)
		if err != nil {
			return
		}
		if isString && n > 0 {
			msg := append([]byte(nil), buf[:n]...)
			e.cfg.Handler.OnData(e, msg)
		}
	}
}

// PairNetwork names the transport of the selected pair: "udp4", "udp6",
// "tcp4" or "tcp6"; "" before one is selected.
func (e *Endpoint) PairNetwork() string {
	pair, err := e.agent.GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return ""
	}
	return pair.Local.NetworkType().String()
}

const iceChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/"

func randomICEString(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(iceChars)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = iceChars[v.Int64()]
	}
	return string(out), nil
}

// dtlsPacketConn feeds the DTLS records of the ICE connection to the DTLS
// library and writes its records back.
type dtlsPacketConn struct {
	conn    *ice.Conn
	writeMu *sync.Mutex
	in      chan []byte

	closeOnce sync.Once
	closed    chan struct{}
	deadline  atomic.Pointer[time.Time]
}

func newDTLSPacketConn(conn *ice.Conn, writeMu *sync.Mutex) *dtlsPacketConn {
	return &dtlsPacketConn{conn: conn, writeMu: writeMu, in: make(chan []byte, dtlsQueue), closed: make(chan struct{})}
}

func (c *dtlsPacketConn) push(p []byte) {
	select {
	case c.in <- append([]byte(nil), p...):
	case <-c.closed:
	default:
		// DTLS and SCTP retransmit what is dropped here.
	}
}

func (c *dtlsPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	var timeout <-chan time.Time
	if d := c.deadline.Load(); d != nil && !d.IsZero() {
		wait := time.Until(*d)
		if wait <= 0 {
			return 0, nil, errDeadline
		}
		t := time.NewTimer(wait)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case pkt := <-c.in:
		return copy(p, pkt), c.conn.RemoteAddr(), nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case <-timeout:
		return 0, nil, errDeadline
	}
}

func (c *dtlsPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(p)
}

func (c *dtlsPacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *dtlsPacketConn) LocalAddr() net.Addr { return c.conn.LocalAddr() }

func (c *dtlsPacketConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *dtlsPacketConn) SetReadDeadline(t time.Time) error {
	c.deadline.Store(&t)
	return nil
}

func (c *dtlsPacketConn) SetWriteDeadline(time.Time) error { return nil }

// errDeadline is a timeout error, as net.Conn deadlines return.
var errDeadline = timeoutError{}

type timeoutError struct{}

func (timeoutError) Error() string   { return "transport: deadline exceeded" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
