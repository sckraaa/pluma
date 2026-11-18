package mtclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/freezeproxy"
)

// probeGateway answers req_pq_multi and pluma.probe over the intermediate
// transport as the gateway does, with its own encoder; onProbe, when set,
// answers the probe instead.
type probeGateway struct {
	onProbe func(c net.Conn, nonce []byte, down int)
}

func startProbeGateway(t *testing.T, onProbe func(c net.Conn, nonce []byte, down int)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &probeGateway{onProbe: onProbe}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go g.serve(c)
		}
	}()
	return ln.Addr().String()
}

func writeIntermediate(c net.Conn, p []byte) error {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(p)))
	_, err := c.Write(append(b, p...))
	return err
}

func unencryptedMessage(body []byte) []byte {
	b := make([]byte, 20, 20+len(body))
	binary.LittleEndian.PutUint64(b[8:], uint64(time.Now().UnixNano())|1)
	binary.LittleEndian.PutUint32(b[16:], uint32(len(body)))
	return append(b, body...)
}

// probeDataMessage is pluma.probeData seq of total with a pad of padLen.
func probeDataMessage(nonce []byte, seq, total, padLen int) []byte {
	var d []byte
	d = binary.LittleEndian.AppendUint32(d, 0x6ab3780b)
	d = append(d, nonce...)
	d = binary.LittleEndian.AppendUint32(d, uint32(seq))
	d = binary.LittleEndian.AppendUint32(d, uint32(total))
	d = binary.LittleEndian.AppendUint64(d, uint64(time.Now().UnixMilli()))
	return unencryptedMessage(appendTLBytes(d, make([]byte, padLen)))
}

// answerProbe sends down bytes in chunks of 16 KiB, as the gateway does.
func answerProbe(c net.Conn, nonce []byte, down int) {
	total := (down + 16<<10 - 1) / (16 << 10)
	for seq := 0; seq < total; seq++ {
		n := min(16<<10, down-seq*(16<<10))
		if writeIntermediate(c, probeDataMessage(nonce, seq, total, n)) != nil {
			return
		}
	}
}

func (g *probeGateway) serve(c net.Conn) {
	defer c.Close()
	var tag [4]byte
	if _, err := io.ReadFull(c, tag[:]); err != nil || binary.LittleEndian.Uint32(tag[:]) != intermediateTag {
		return
	}
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(c, hdr[:]); err != nil {
			return
		}
		p := make([]byte, binary.LittleEndian.Uint32(hdr[:]))
		if _, err := io.ReadFull(c, p); err != nil {
			return
		}
		switch binary.LittleEndian.Uint32(p[20:]) {
		case 0xbe7e8ef1: // req_pq_multi
			x := mtproto.NewEncodeBuf(128)
			_ = mtproto.MakeTLResPQ(&mtproto.ResPQ{
				Nonce:                       p[24:40],
				ServerNonce:                 bytes.Repeat([]byte{1}, 16),
				Pq:                          string([]byte{0x17, 0xed, 0x48, 0x94, 0x1a, 0x08, 0xf9, 0x81}),
				ServerPublicKeyFingerprints: []int64{1},
			}).Encode(x, 0)
			if writeIntermediate(c, unencryptedMessage(x.GetBuf())) != nil {
				return
			}
		case 0x5c316d30:
			nonce := append([]byte(nil), p[24:40]...)
			// The gateway's bounds.
			down := min(max(int(int32(binary.LittleEndian.Uint32(p[40:]))), 16<<10), 512<<10)
			if g.onProbe != nil {
				g.onProbe(c, nonce, down)
			} else {
				answerProbe(c, nonce, down)
			}
		default:
			return
		}
	}
}

func TestProbeOK(t *testing.T) {
	addr := startProbeGateway(t, nil)
	for _, resPQ := range []bool{false, true} {
		res := ProbeTCP(context.Background(), addr, ProbeOptions{ResPQ: resPQ})
		if res.Outcome != ProbeOK || res.Received != DefaultProbeDown || res.Total != 4 {
			t.Fatalf("resPQ %v: %s", resPQ, res)
		}
		if res.RTT <= 0 || res.BPS <= 0 || res.Score() <= res.RTT {
			t.Fatalf("resPQ %v: rtt %s, bps %.0f, score %s", resPQ, res.RTT, res.BPS, res.Score())
		}
	}
	// Asked below the bound, the gateway sends 16 KiB.
	if res := ProbeTCP(context.Background(), addr, ProbeOptions{Down: 1000, Up: -1}); res.Outcome != ProbeOK || res.Received != 16<<10 {
		t.Fatalf("1000 bytes asked: %s", res)
	}
}

// A path that freezes after 16 KB is found frozen within the stall time of
// the last byte; through the same proxy without a limit it is ok.
func TestProbeFrozen(t *testing.T) {
	addr := startProbeGateway(t, nil)
	for _, resPQ := range []bool{false, true} {
		p, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: addr, Down: 16 << 10})
		if err != nil {
			t.Fatal(err)
		}
		res := ProbeTCP(context.Background(), p.Addr(), ProbeOptions{ResPQ: resPQ})
		_ = p.Close()
		if res.Outcome != ProbeFrozen {
			t.Fatalf("resPQ %v: %s, want frozen", resPQ, res)
		}
		if res.Elapsed < DefaultProbeStall || res.Elapsed > DefaultProbeStall+500*time.Millisecond {
			t.Fatalf("resPQ %v: frozen after %s, want %s and a little", resPQ, res.Elapsed, DefaultProbeStall)
		}
	}
	p, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if res := ProbeTCP(context.Background(), p.Addr(), ProbeOptions{ResPQ: true}); res.Outcome != ProbeOK {
		t.Fatalf("a clean path: %s", res)
	}
}

// A path that freezes on the way up (the request never arrives whole)
// answers resPQ and then nothing: frozen after the stall time counted from
// the request plus the RTT of resPQ, not failed at the deadline.
func TestProbeFrozenUp(t *testing.T) {
	addr := startProbeGateway(t, nil)
	p, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: addr, Up: 4 << 10})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	res := ProbeTCP(context.Background(), p.Addr(), ProbeOptions{ResPQ: true})
	if res.Outcome != ProbeFrozen || res.Elapsed > DefaultProbeStall+500*time.Millisecond {
		t.Fatalf("%s, want frozen within %s", res, DefaultProbeStall)
	}
}

// A gateway without the probe closes the connection on it: unsupported
// after resPQ, failed without.
func TestProbeUnsupported(t *testing.T) {
	addr := startProbeGateway(t, func(c net.Conn, _ []byte, _ int) { _ = c.Close() })
	if res := ProbeTCP(context.Background(), addr, ProbeOptions{ResPQ: true}); res.Outcome != ProbeUnsupported {
		t.Fatalf("after resPQ: %s, want unsupported", res)
	}
	if res := ProbeTCP(context.Background(), addr, ProbeOptions{}); res.Outcome != ProbeFailed {
		t.Fatalf("without resPQ: %s, want failed", res)
	}
}

// Bytes that keep coming but not all by the deadline: slow, with a speed.
func TestProbeSlow(t *testing.T) {
	addr := startProbeGateway(t, func(c net.Conn, nonce []byte, down int) {
		total := (down + 16<<10 - 1) / (16 << 10)
		var b []byte
		for seq := 0; seq < total; seq++ {
			msg := probeDataMessage(nonce, seq, total, 16<<10)
			b = append(append(b, binary.LittleEndian.AppendUint32(nil, uint32(len(msg)))...), msg...)
		}
		for len(b) > 0 {
			n := min(2<<10, len(b))
			if _, err := c.Write(b[:n]); err != nil {
				return
			}
			b = b[n:]
			time.Sleep(50 * time.Millisecond)
		}
	})
	res := ProbeTCP(context.Background(), addr, ProbeOptions{Deadline: time.Second})
	if res.Outcome != ProbeSlow || res.BPS <= 0 || res.Received >= DefaultProbeDown {
		t.Fatalf("%s, want slow", res)
	}
}

// No connection, or a connection that never says a byte: failed.
func TestProbeFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	silent := ln.Addr().String()
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	start := time.Now()
	if res := ProbeTCP(context.Background(), silent, ProbeOptions{ResPQ: true, Deadline: 500 * time.Millisecond}); res.Outcome != ProbeFailed {
		t.Fatalf("a silent server: %s, want failed", res)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("failed after %s, want the deadline", d)
	}

	refused, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := refused.Addr().String()
	_ = refused.Close()
	if res := ProbeTCP(context.Background(), addr, ProbeOptions{}); res.Outcome != ProbeFailed || res.Err == nil {
		t.Fatalf("no server: %s, want failed", res)
	}
}
