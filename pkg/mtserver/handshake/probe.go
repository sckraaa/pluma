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

package handshake

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
)

// The path probe (task 113, ADR 0073): an unencrypted request a client sends
// on any connection, before or without a key, to see whether a path to the
// gateway carries much more than the ~16 KB after which filtered networks
// freeze a connection, and how fast. It is not part of the TL schema of the
// layer; both messages are defined here:
//
//	pluma.probe#5c316d30 nonce:int128 down:int pad:bytes = pluma.Probe;
//	pluma.probeData#6ab3780b nonce:int128 seq:int total:int server_ms:long pad:bytes = pluma.ProbeData;
//
// The constructors are the CRC32 of these lines without the constructor, as
// TL computes them. The gateway answers pluma.probe with total messages
// pluma.probeData, seq 0..total-1, whose pad add up to down bytes, at most
// ProbeChunkSize each. A probe leaves the key exchange state of the
// connection as it was.
const (
	CRC32_pluma_probe     uint32 = 0x5c316d30
	CRC32_pluma_probeData uint32 = 0x6ab3780b

	// ProbeChunkSize is the most pad bytes one pluma.probeData carries.
	ProbeChunkSize = 16 << 10
	// ProbeMinDown and ProbeMaxDown bound down: the gateway sends at least
	// ProbeMinDown and at most ProbeMaxDown bytes, whatever was asked.
	ProbeMinDown = 16 << 10
	ProbeMaxDown = 512 << 10
	// ProbeMaxPad bounds the pad of pluma.probe; a longer one is malformed.
	ProbeMaxPad = 64 << 10
)

var errProbeMessage = errors.New("handshake: malformed pluma.probe")

// Probe is a parsed pluma.probe.
type Probe struct {
	Nonce [16]byte
	// Down is the number of bytes asked, clamped to
	// ProbeMinDown..ProbeMaxDown.
	Down int
	// PadLen is the length of the pad the client sent.
	PadLen int
}

// Chunks returns how many pluma.probeData answer p.
func (p *Probe) Chunks() int {
	return (p.Down + ProbeChunkSize - 1) / ProbeChunkSize
}

// IsProbe reports whether the unencrypted message frame (auth_key_id 0)
// holds a pluma.probe, by its constructor alone.
func IsProbe(frame []byte) bool {
	return len(frame) >= unencryptedHeaderSize+4 &&
		binary.LittleEndian.Uint32(frame[unencryptedHeaderSize:]) == CRC32_pluma_probe
}

// ParseProbe parses an unencrypted message frame holding pluma.probe.
// message_data_length must be the length of the object exactly; bytes after
// the message (the padding of the padded intermediate transport) are
// ignored, as for the key exchange.
func ParseProbe(frame []byte) (*Probe, error) {
	if !IsProbe(frame) {
		return nil, errProbeMessage
	}
	n := binary.LittleEndian.Uint32(frame[16:unencryptedHeaderSize])
	if uint64(n) > uint64(len(frame)-unencryptedHeaderSize) {
		return nil, errProbeMessage
	}
	b := frame[unencryptedHeaderSize : unencryptedHeaderSize+int(n)]
	// constructor, nonce, down, then at least the length of pad.
	const fixed = 4 + 16 + 4
	if len(b) < fixed+4 {
		return nil, errProbeMessage
	}
	p := &Probe{}
	copy(p.Nonce[:], b[4:20])
	p.Down = clampProbeDown(int32(binary.LittleEndian.Uint32(b[20:24])))

	padLen, padStart, ok := tlBytesLen(b[fixed:])
	if !ok || padLen > ProbeMaxPad {
		return nil, errProbeMessage
	}
	if fixed+tlBytesSize(padStart, padLen) != len(b) {
		return nil, errProbeMessage
	}
	p.PadLen = padLen
	return p, nil
}

func clampProbeDown(down int32) int {
	switch {
	case down < ProbeMinDown:
		return ProbeMinDown
	case down > ProbeMaxDown:
		return ProbeMaxDown
	}
	return int(down)
}

// tlBytesLen reads the length prefix of a TL bytes value at the start of b:
// one byte below 254, else 254 and three bytes. It returns the length and
// the size of the prefix; false when b is too short for the value.
func tlBytesLen(b []byte) (n, prefix int, ok bool) {
	if len(b) < 1 {
		return 0, 0, false
	}
	if b[0] < 254 {
		n, prefix = int(b[0]), 1
	} else if b[0] == 254 && len(b) >= 4 {
		n, prefix = int(b[1])|int(b[2])<<8|int(b[3])<<16, 4
	} else {
		return 0, 0, false
	}
	if tlBytesSize(prefix, n) > len(b) {
		return 0, 0, false
	}
	return n, prefix, true
}

// tlBytesSize is the size of a TL bytes value: prefix, data and padding to
// a multiple of 4.
func tlBytesSize(prefix, n int) int {
	return (prefix + n + 3) &^ 3
}

// probeFill is the pad of every pluma.probeData: random bytes made once.
// The transports (obfuscated2, TLS) make each stream unique anyway; the pad
// only must not compress.
var probeFill = func() []byte {
	b := make([]byte, ProbeChunkSize)
	if _, err := rand.Read(b); err != nil {
		panic("handshake: no randomness for the probe pad: " + err.Error())
	}
	return b
}()

// ProbeData returns the unencrypted message pluma.probeData number seq of
// the answer to p, with the server time serverMs (unix milliseconds) and
// the message id msgId.
func ProbeData(p *Probe, seq int, serverMs, msgId int64) []byte {
	total := p.Chunks()
	padLen := ProbeChunkSize
	if seq == total-1 {
		padLen = p.Down - ProbeChunkSize*(total-1)
	}
	prefix := 1
	if padLen >= 254 {
		prefix = 4
	}
	dataLen := 4 + 16 + 4 + 4 + 8 + tlBytesSize(prefix, padLen)
	b := make([]byte, unencryptedHeaderSize+dataLen)
	binary.LittleEndian.PutUint64(b[8:], uint64(msgId))
	binary.LittleEndian.PutUint32(b[16:], uint32(dataLen))
	d := b[unencryptedHeaderSize:]
	binary.LittleEndian.PutUint32(d, CRC32_pluma_probeData)
	copy(d[4:20], p.Nonce[:])
	binary.LittleEndian.PutUint32(d[20:], uint32(int32(seq)))
	binary.LittleEndian.PutUint32(d[24:], uint32(int32(total)))
	binary.LittleEndian.PutUint64(d[28:], uint64(serverMs))
	pad := d[36:]
	if prefix == 1 {
		pad[0] = byte(padLen)
	} else {
		pad[0] = 254
		pad[1], pad[2], pad[3] = byte(padLen), byte(padLen>>8), byte(padLen>>16)
	}
	copy(pad[prefix:], probeFill[:padLen])
	return b
}
