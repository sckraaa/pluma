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

package tde2e

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Constructor ids of e2e_api.tl. The block and the broadcasts carry theirs
// in the schema (e2e_api.tl:67-68, 81); the others are the ids tdlib's
// generator computed, as in the generated td/telegram/e2e_api.h of the same
// commit (the header Telegram-iOS ships in third-party/td).
const (
	idBlock               uint32 = 0x639a3db6
	idBroadcastCommit     uint32 = 0xd1512ae7
	idBroadcastReveal     uint32 = 0x83f4f9d8
	idGroupParticipant    uint32 = 0x18f3971f
	idGroupState          uint32 = 0x1ddc7584
	idSharedKey           uint32 = 0x8a847e7f
	idChangeNoop          uint32 = 0xdeb4a41b
	idChangeSetValue      uint32 = 0xfe0139cc
	idChangeSetGroupState uint32 = 0x2cf17146
	idChangeSetSharedKey  uint32 = 0x987a2158
	idStateProof          uint32 = 0xd6b679e6
)

var (
	errShort    = errors.New("unexpected end of data")
	errTrailing = errors.New("trailing data")
)

// reader reads TL as td::TlParser does: little-endian integers, int256 and
// int512 as raw bytes, vectors as a bare count (e2e_api.tl uses the bare
// vector), objects of a boxed type with their constructor id. The first
// error sticks; later reads return zero values.
type reader struct {
	buf []byte
	off int
	err error
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.buf)-r.off < n {
		r.fail(errShort)
		return nil
	}
	b := r.buf[r.off : r.off+n]
	r.off += n
	return b
}

func (r *reader) uint32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) int32() int32 {
	return int32(r.uint32())
}

// flags reads a field of type #. The parser tdlib generates refuses a
// negative one ("Variable of type # can't be negative",
// td/generate/tl_writer_cpp.cpp:266-287), so a block with bit 31 set in any
// of its flags fields does not parse in tde2e.
func (r *reader) flags() int32 {
	v := r.int32()
	if r.err == nil && v < 0 {
		r.fail(fmt.Errorf("negative flags %d", v))
	}
	return v
}

func (r *reader) int64() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b))
}

func (r *reader) int256() (v [32]byte) {
	copy(v[:], r.take(32))
	return v
}

func (r *reader) int512() (v [64]byte) {
	copy(v[:], r.take(64))
	return v
}

// expect reads a constructor id and checks it.
func (r *reader) expect(id uint32) bool {
	got := r.uint32()
	if r.err != nil {
		return false
	}
	if got != id {
		r.fail(fmt.Errorf("constructor %08x, want %08x", got, id))
		return false
	}
	return true
}

// count reads a vector length. Every element takes at least minSize bytes,
// so a count the rest of the data cannot hold is refused before anything is
// allocated (td::TlFetchVector, td/tl/tl_object_parse.h:120-136, checks it
// against one byte per element).
func (r *reader) count(minSize int) int {
	n := r.int32()
	if r.err != nil {
		return 0
	}
	if n < 0 || int(n) > (len(r.buf)-r.off)/minSize {
		r.fail(fmt.Errorf("vector of %d elements", n))
		return 0
	}
	return int(n)
}

// bytes reads a TL string: a length byte below 254, or 254 and a 3-byte
// length, then the data padded to a multiple of 4 (td::TlParser::
// fetch_string, tdutils/td/utils/tl_parsers.h:127-162). td also reads the
// length byte 255 with a 7-byte length, which its storer writes only for
// 2^24 bytes and more; a block is at most MaxBlockSize, so it is refused
// here. The result does not alias the input.
func (r *reader) bytes() []byte {
	h := r.take(1)
	if h == nil {
		return nil
	}
	n, head := int(h[0]), 1
	switch n {
	case 254:
		l := r.take(3)
		if l == nil {
			return nil
		}
		n, head = int(l[0])|int(l[1])<<8|int(l[2])<<16, 4
	case 255:
		r.fail(errors.New("string length byte 255"))
		return nil
	}
	data := r.take(n)
	r.take(pad(head + n))
	if r.err != nil {
		return nil
	}
	return append([]byte{}, data...)
}

// end reports the first error, or trailing data: like td's fetch_end, an
// object is always the whole buffer.
func (r *reader) end() error {
	if r.err != nil {
		return r.err
	}
	if r.off != len(r.buf) {
		return errTrailing
	}
	return nil
}

func pad(n int) int {
	return (4 - n%4) % 4
}

// writer writes TL as td::TlStorerUnsafe does.
type writer struct {
	buf []byte
}

func (w *writer) uint32(v uint32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
}

func (w *writer) int32(v int32) {
	w.uint32(uint32(v))
}

func (w *writer) int64(v int64) {
	w.buf = binary.LittleEndian.AppendUint64(w.buf, uint64(v))
}

func (w *writer) raw(b []byte) {
	w.buf = append(w.buf, b...)
}

// bytes writes a TL string. Lengths of 2^24 and more, which td writes with
// the length byte 255, never occur: a block is at most MaxBlockSize.
func (w *writer) bytes(b []byte) {
	head := 1
	if len(b) < 254 {
		w.buf = append(w.buf, byte(len(b)))
	} else {
		w.buf = append(w.buf, 254, byte(len(b)), byte(len(b)>>8), byte(len(b)>>16))
		head = 4
	}
	w.buf = append(w.buf, b...)
	w.buf = append(w.buf, make([]byte, pad(head+len(b)))...)
}
