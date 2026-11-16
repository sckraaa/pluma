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

package cdn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// bufReader is a codec.CodecReader over the bytes received so far.
type bufReader struct{ buf []byte }

func (r *bufReader) Peek(n int) ([]byte, error) {
	if n < 0 || n > len(r.buf) {
		return r.buf, nil
	}
	return r.buf[:n], nil
}

// String keeps the bytes out of the codec's log of the connection.
func (r *bufReader) String() string { return "test connection" }

func (r *bufReader) Discard(n int) (int, error) {
	n = min(n, len(r.buf))
	r.buf = r.buf[n:]
	return n, nil
}

// startResPQ serves the start of a key exchange on a loopback port with
// the server side of the transport (pkg/mtserver/codec), as the node does:
// a connection made with secret gets resPQ with the fingerprint of *fp; any
// other is closed.
func startResPQ(t *testing.T, secret codec.Secret, fp *atomic.Int64) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go serveResPQ(conn, secret, fp.Load())
		}
	}()
	return l.Addr().String()
}

func serveResPQ(conn net.Conn, secret codec.Secret, fp int64) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := &bufReader{}
	var c codec.Codec
	read := func() bool {
		b := make([]byte, 4096)
		n, err := conn.Read(b)
		r.buf = append(r.buf, b[:n]...)
		return err == nil
	}
	for c == nil {
		var err error
		if c, err = codec.CreateSecretCodec(r, secret); errors.Is(err, codec.ErrUnexpectedEOF) {
			if !read() {
				return
			}
		} else if err != nil {
			return
		}
	}
	var msg []byte
	for msg == nil {
		_, p, err := c.Decode(r)
		switch {
		case errors.Is(err, codec.ErrUnexpectedEOF):
			if !read() {
				return
			}
		case err != nil:
			return
		default:
			msg = p
		}
	}
	n := binary.LittleEndian.Uint32(msg[16:])
	d := mtproto.NewDecodeBuf(msg[20 : 20+n])
	req, ok := d.Object().(*mtproto.TLReqPqMulti)
	if !ok {
		return
	}
	x := mtproto.NewEncodeBuf(128)
	res := mtproto.MakeTLResPQ(&mtproto.ResPQ{
		Nonce:                       req.GetNonce(),
		ServerNonce:                 bytes.Repeat([]byte{7}, 16),
		Pq:                          string([]byte{0x17, 0xED, 0x48, 0x94, 0x1A, 0x08, 0xF9, 0x81}),
		ServerPublicKeyFingerprints: []int64{fp},
	})
	if err := res.Encode(x, 0); err != nil {
		return
	}
	body := x.GetBuf()
	out := make([]byte, 20+len(body))
	binary.LittleEndian.PutUint64(out[8:], uint64(time.Now().Unix()<<32|1))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(body)))
	copy(out[20:], body)
	frame, err := c.Encode(nil, &mtproto.MTPRawMessage{Payload: out})
	if err != nil {
		return
	}
	_, _ = conn.Write(frame)
}

// proberEnv is a Prober with a node in memory whose public port answers
// the key exchange.
type proberEnv struct {
	*homeEnv
	prober *Prober
	fp     *atomic.Int64
}

func newProberEnv(t *testing.T) *proberEnv {
	t.Helper()
	e := &proberEnv{homeEnv: newHomeEnv(t), fp: &atomic.Int64{}}
	// No row yet: the prober makes it.
	e.store = NewMemStore()
	e.home = NewHome(e.store, e.set, e.files)
	e.home.now = e.clock.now
	e.push = NewPusher(e.store, e.node, e.set, PusherDeps{Reader: e.files, Healthy: func() bool { return true }, OnReady: e.home.Forget})
	e.push.now = e.clock.now
	e.fp.Store(e.node.status.Fingerprint)
	e.set.DcAddress = startResPQ(t, e.set.DcSecret, e.fp)
	e.prober = NewProber(e.store, e.node, e.set)
	e.prober.now = e.clock.now
	return e
}

func (e *proberEnv) row() *Node {
	e.t.Helper()
	n, err := e.store.Node(e.ctx, testDc)
	if err != nil || n == nil {
		e.t.Fatalf("node row: %v, %v", n, err)
	}
	return n
}

func (e *proberEnv) probeOnce() {
	e.t.Helper()
	if err := e.prober.probe(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

// TestProbeHealthy: a node that answers on both sides is healthy for 30 s,
// gets the request_token key, and its key is given out.
func TestProbeHealthy(t *testing.T) {
	e := newProberEnv(t)
	e.probeOnce()
	n := e.row()
	if !e.prober.Healthy() || n.HealthyUntil != e.clock.now().Add(healthyFor).UnixMilli() || n.UnhealthyReason != "" {
		t.Fatalf("row %+v, healthy %v", n, e.prober.Healthy())
	}
	if n.PublicKey != e.node.status.PublicKeyPem || n.Fingerprint != e.node.status.Fingerprint || len(n.RtKey) != RtKeySize {
		t.Fatalf("row %+v", n)
	}
	if e.node.keys == nil || !bytes.Equal(e.node.keys[0], n.RtKey) {
		t.Fatal("the node did not get the request_token key")
	}
	// The key is handed once, then again after the node's restart.
	e.probeOnce()
	if e.node.keyCalls != 1 {
		t.Fatalf("%d calls of SetRequestTokenKeys", e.node.keyCalls)
	}
	e.node.restart()
	e.probeOnce()
	if e.node.keyCalls != 2 || !bytes.Equal(e.node.keys[0], e.row().RtKey) {
		t.Fatalf("%d calls of SetRequestTokenKeys after the restart", e.node.keyCalls)
	}
	// A request_token the node signs passes at home.
	if !VerifyRequestToken(e.row().RtKeys(), []byte("t"), SignRequestToken(e.node.keys[0], []byte("t"), e.clock.now()), e.clock.now()) {
		t.Fatal("a token signed with the key handed to the node does not pass")
	}
}

// TestProbeRotation: after 30 days the key is replaced, the old one kept as
// the previous, and the node gets both.
func TestProbeRotation(t *testing.T) {
	e := newProberEnv(t)
	e.probeOnce()
	first := e.row().RtKey
	e.clock.add(rtKeyEvery)
	e.probeOnce()
	n := e.row()
	if bytes.Equal(n.RtKey, first) || !bytes.Equal(n.RtKeyPrev, first) {
		t.Fatal("the key was not rotated")
	}
	if !bytes.Equal(e.node.keys[0], n.RtKey) || !bytes.Equal(e.node.keys[1], first) {
		t.Fatal("the node did not get the rotated keys")
	}
}

// TestProbeUnhealthy: each way a node fails takes it out of the redirects,
// with its reason; its key stays given out.
func TestProbeUnhealthy(t *testing.T) {
	cases := []struct {
		name      string
		breakNode func(e *proberEnv)
		reason    string
	}{
		{"unreachable", func(e *proberEnv) { e.node.statusErr = errors.New("connection refused") }, unhealthyUnreachable},
		{"another dc", func(e *proberEnv) { e.node.status.DcId = 202 }, unhealthyDcMismatch},
		{"disk low", func(e *proberEnv) { e.node.status.DiskLow = true }, unhealthyDiskLow},
		{"egress", func(e *proberEnv) { e.node.status.EgressBytes = e.set.MonthlyEgressLimit }, unhealthyEgress},
		{"another key at the address", func(e *proberEnv) { e.fp.Store(42) }, unhealthyFingerprint},
		{"mtproto port closed", func(e *proberEnv) { e.set.DcAddress = "127.0.0.1:1" }, unhealthyMTProto},
		{"wrong secret", func(e *proberEnv) { e.set.DcSecret[0] ^= 1 }, unhealthyMTProto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newProberEnv(t)
			e.probeOnce()
			if !e.prober.Healthy() {
				t.Fatalf("not healthy before: %+v", e.row())
			}
			c.breakNode(e)
			e.probeOnce()
			n := e.row()
			if e.prober.Healthy() || n.HealthyUntil != 0 || n.UnhealthyReason != c.reason {
				t.Fatalf("row %+v, want out for %s", n, c.reason)
			}
			if n.PublicKey == "" {
				t.Fatal("the key of a node that is out was dropped")
			}
		})
	}
}

// TestProbeEgressLastMonth: the traffic of a month that is over does not
// count.
func TestProbeEgressLastMonth(t *testing.T) {
	e := newProberEnv(t)
	e.node.status.EgressMonth = "2026-10"
	e.node.status.EgressBytes = e.set.MonthlyEgressLimit * 2
	e.probeOnce()
	if !e.prober.Healthy() {
		t.Fatalf("out for last month's traffic: %+v", e.row())
	}
}

// TestInventory: a ready copy the node lost is made absent and queued by
// the next request; a file on the node no copy accounts for is deleted;
// one being pushed is kept.
func TestInventory(t *testing.T) {
	e := newProberEnv(t)
	e.probeOnce()
	kept := e.readyFile(50, 2<<20)
	lost := e.readyFile(51, 2<<20)
	if _, err := e.node.Delete(e.ctx, lost.Secrets.Token); err != nil {
		t.Fatal(err)
	}
	orphan := randomBytes(t, TokenSize)
	e.node.files[string(orphan)] = []byte("cipher")
	// A copy pushed and stored, not yet made ready: not an orphan.
	e.queue(52, 2<<20)
	c, err := e.store.Claim(e.ctx, testDc, pushLease, e.clock.now().UnixMilli())
	if err != nil || c == nil {
		t.Fatalf("claim: %v, %v", c, err)
	}
	pushing, err := e.store.SetKey(e.ctx, c.File.RowId, mustSecrets(t))
	if err != nil {
		t.Fatal(err)
	}
	e.node.files[string(pushing.Secrets.Token)] = []byte("cipher")
	e.node.deleted = nil

	e.clock.add(time.Second)
	if err = e.prober.inventory(e.ctx); err != nil {
		t.Fatal(err)
	}
	if r := e.replica(51); r.State != StateAbsent {
		t.Fatalf("lost copy %+v, want absent", r)
	}
	if r := e.replica(50); r.State != StateReady {
		t.Fatalf("kept copy %+v, want ready", r)
	}
	if len(e.node.deleted) != 1 || !bytes.Equal(e.node.deleted[0], orphan) {
		t.Fatalf("deleted %d files, want the orphan only", len(e.node.deleted))
	}
	if e.node.file(kept.Secrets.Token) == nil || e.node.file(pushing.Secrets.Token) == nil {
		t.Fatal("a file with a copy at home was deleted")
	}
	// The next request of the lost file queues it again.
	e.home.Forget(lost.Key)
	if _, result := e.home.Redirect(e.ctx, docLoc(51), 0); result != ResultNotReady {
		t.Fatalf("lost file: %q", result)
	}
	if r := e.replica(51); r.State != StateQueued {
		t.Fatalf("lost copy %+v, want queued", r)
	}
}
