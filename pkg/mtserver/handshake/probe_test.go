package handshake

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

// probeRequest serializes pluma.probe as an unencrypted message the way a
// client does, independently of the code under test.
func probeRequest(nonce [16]byte, down int32, pad []byte) []byte {
	var d []byte
	d = binary.LittleEndian.AppendUint32(d, 0x5c316d30)
	d = append(d, nonce[:]...)
	d = binary.LittleEndian.AppendUint32(d, uint32(down))
	d = appendTLBytes(d, pad)
	var m []byte
	m = binary.LittleEndian.AppendUint64(m, 0)
	m = binary.LittleEndian.AppendUint64(m, 0x6000000000000004)
	m = binary.LittleEndian.AppendUint32(m, uint32(len(d)))
	return append(m, d...)
}

func appendTLBytes(b, v []byte) []byte {
	start := len(b)
	if len(v) < 254 {
		b = append(b, byte(len(v)))
	} else {
		b = append(b, 254, byte(len(v)), byte(len(v)>>8), byte(len(v)>>16))
	}
	b = append(b, v...)
	for (len(b)-start)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// The constructors are the CRC32 of the TL lines, as TL computes them (the
// same way gives req_pq_multi its known 0xbe7e8ef1).
func TestProbeConstructors(t *testing.T) {
	for _, c := range []struct {
		line string
		want uint32
	}{
		{"req_pq_multi nonce:int128 = ResPQ", 0xbe7e8ef1},
		{"pluma.probe nonce:int128 down:int pad:bytes = pluma.Probe", CRC32_pluma_probe},
		{"pluma.probeData nonce:int128 seq:int total:int server_ms:long pad:bytes = pluma.ProbeData", CRC32_pluma_probeData},
	} {
		if got := crc32.ChecksumIEEE([]byte(c.line)); got != c.want {
			t.Errorf("crc32(%q) = %#08x, want %#08x", c.line, got, c.want)
		}
	}
}

func TestParseProbe(t *testing.T) {
	nonce := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	for _, c := range []struct {
		name     string
		down     int32
		pad      int
		wantDown int
	}{
		{"gate", 64 << 10, 20 << 10, 64 << 10},
		{"rank", 192 << 10, 20 << 10, 192 << 10},
		{"no pad", 64 << 10, 0, 64 << 10},
		{"short pad", 64 << 10, 253, 64 << 10},
		{"long pad", 64 << 10, 254, 64 << 10},
		{"largest pad", 64 << 10, ProbeMaxPad, 64 << 10},
		{"too little asked", 1000, 0, ProbeMinDown},
		{"negative", -1, 0, ProbeMinDown},
		{"too much asked", 1 << 30, 0, ProbeMaxDown},
		{"odd", 100_001, 0, 100_001},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame := probeRequest(nonce, c.down, bytes.Repeat([]byte{0xa5}, c.pad))
			if !IsProbe(frame) {
				t.Fatal("not taken for a probe")
			}
			p, err := ParseProbe(frame)
			if err != nil {
				t.Fatal(err)
			}
			if p.Nonce != nonce || p.Down != c.wantDown || p.PadLen != c.pad {
				t.Fatalf("parsed %x down %d pad %d, want down %d pad %d", p.Nonce, p.Down, p.PadLen, c.wantDown, c.pad)
			}
			// The padding of the padded intermediate transport after the
			// message is ignored.
			if _, err := ParseProbe(append(frame, 1, 2, 3)); err != nil {
				t.Fatalf("with transport padding: %v", err)
			}
		})
	}
}

func TestParseProbeMalformed(t *testing.T) {
	var nonce [16]byte
	good := probeRequest(nonce, 64<<10, make([]byte, 300))
	setLen := func(frame []byte, n int) []byte {
		frame = append([]byte(nil), frame...)
		binary.LittleEndian.PutUint32(frame[16:], uint32(n))
		return frame
	}
	for name, frame := range map[string][]byte{
		"pad too long":              probeRequest(nonce, 64<<10, make([]byte, ProbeMaxPad+1)),
		"length past the frame":     setLen(good, len(good)-unencryptedHeaderSize+1),
		"length inside the object":  setLen(good, len(good)-unencryptedHeaderSize-4),
		"bytes after the object":    setLen(append(append([]byte(nil), good...), 0, 0, 0, 0), len(good)-unencryptedHeaderSize+4),
		"pad cut":                   setLen(good[:len(good)-8], len(good)-unencryptedHeaderSize-8),
		"no pad":                    setLen(good[:unencryptedHeaderSize+24], 24),
		"bad bytes prefix":          setLen(append(append([]byte(nil), good[:unencryptedHeaderSize+24]...), 255, 0, 0, 0), 28),
		"another constructor":       append(append([]byte(nil), good[:unencryptedHeaderSize]...), 0xf1, 0x8e, 0x7e, 0xbe),
		"header only":               good[:unencryptedHeaderSize],
		"constructor without nonce": setLen(good[:unencryptedHeaderSize+4], 4),
	} {
		if _, err := ParseProbe(frame); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// parseProbeData decodes pluma.probeData independently of ProbeData.
func parseProbeData(t *testing.T, b []byte) (nonce [16]byte, seq, total int32, serverMs int64, pad []byte) {
	t.Helper()
	if len(b) < unencryptedHeaderSize+40 || binary.LittleEndian.Uint64(b) != 0 {
		t.Fatalf("not an unencrypted message: %d bytes", len(b))
	}
	n := int(binary.LittleEndian.Uint32(b[16:]))
	if n != len(b)-unencryptedHeaderSize || n%4 != 0 {
		t.Fatalf("message_data_length %d of %d bytes", n, len(b)-unencryptedHeaderSize)
	}
	d := b[unencryptedHeaderSize:]
	if c := binary.LittleEndian.Uint32(d); c != 0x6ab3780b {
		t.Fatalf("constructor %#08x", c)
	}
	copy(nonce[:], d[4:20])
	seq = int32(binary.LittleEndian.Uint32(d[20:]))
	total = int32(binary.LittleEndian.Uint32(d[24:]))
	serverMs = int64(binary.LittleEndian.Uint64(d[28:]))
	l, prefix, ok := tlBytesLen(d[36:])
	if !ok || 36+tlBytesSize(prefix, l) != len(d) {
		t.Fatalf("pad: length %d, prefix %d, ok %v, in %d bytes", l, prefix, ok, len(d)-36)
	}
	return nonce, seq, total, serverMs, d[36+prefix : 36+prefix+l]
}

func TestProbeData(t *testing.T) {
	nonce := [16]byte{9, 8, 7}
	for _, c := range []struct {
		down       int
		wantChunks int
		lastPad    int
	}{
		{ProbeMinDown, 1, ProbeChunkSize},
		{64 << 10, 4, ProbeChunkSize},
		{192 << 10, 12, ProbeChunkSize},
		{ProbeMaxDown, 32, ProbeChunkSize},
		{ProbeChunkSize + 10, 2, 10},
		{ProbeChunkSize + 254, 2, 254},
	} {
		p := &Probe{Nonce: nonce, Down: c.down}
		if p.Chunks() != c.wantChunks {
			t.Fatalf("down %d: %d chunks, want %d", c.down, p.Chunks(), c.wantChunks)
		}
		sum := 0
		for seq := 0; seq < p.Chunks(); seq++ {
			msg := ProbeData(p, seq, 1_800_000_000_123, int64(0x6000000000000001+seq*4))
			if id := binary.LittleEndian.Uint64(msg[8:]); id != uint64(0x6000000000000001+seq*4) {
				t.Fatalf("msg_id %#x", id)
			}
			gotNonce, gotSeq, gotTotal, ms, pad := parseProbeData(t, msg)
			if gotNonce != nonce || int(gotSeq) != seq || int(gotTotal) != c.wantChunks || ms != 1_800_000_000_123 {
				t.Fatalf("down %d seq %d: nonce %x seq %d total %d server_ms %d", c.down, seq, gotNonce, gotSeq, gotTotal, ms)
			}
			want := ProbeChunkSize
			if seq == c.wantChunks-1 {
				want = c.lastPad
			}
			if len(pad) != want {
				t.Fatalf("down %d seq %d: pad %d, want %d", c.down, seq, len(pad), want)
			}
			sum += len(pad)
		}
		if sum != c.down {
			t.Fatalf("down %d: the pads add up to %d", c.down, sum)
		}
	}
}
