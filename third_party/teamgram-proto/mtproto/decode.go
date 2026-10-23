// Copyright 2024 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package mtproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"unsafe" // pluma: element sizes for the vector allocation budget
)

// Sentinel errors for hot-path decode operations to avoid per-call allocations.
var (
	errDecodeLong        = errors.New("DecodeLong")
	errDecodeDouble      = errors.New("DecodeDouble")
	errDecodeInt         = errors.New("DecodeInt")
	errDecodeUInt        = errors.New("DecodeUInt")
	errDecodeBytes       = errors.New("DecodeBytes")
	errDecodeStringBytes = errors.New("DecodeStringBytes")
	errDecodeVectorInt   = errors.New("DecodeVectorInt: Wrong size")
	errDecodeVectorLong  = errors.New("DecodeVectorLong: Wrong size")
	errDecodeVectorStr   = errors.New("DecodeVectorString: Wrong size")
	errDecodeVectorBytes = errors.New("DecodeVectorBytes: Wrong size")
	// pluma: errors for input that the limits below reject.
	errDecodeVectorLen   = errors.New("DecodeVector: length exceeds input")
	errDecodeVectorAlloc = errors.New("DecodeVector: allocation budget exceeded")
	errDecodeTooDeep     = errors.New("Decode: objects nested too deeply")
)

// pluma: limits that keep the decoding of untrusted input bounded in memory
// and stack, whatever the input claims.
const (
	// minVectorElemSize is the smallest TL encoding of a vector element (an
	// int or a constructor id): a vector never holds more elements than the
	// remaining input divided by it.
	minVectorElemSize = 4
	// maxVectorAlloc bounds the memory allocated for vector elements while
	// decoding one message (with its nested objects). Element structs are
	// much larger than their encoding (an Update is over 2 KB, its smallest
	// encoding 4 bytes), so the input size alone does not bound it.
	// Legitimate client requests stay far below.
	maxVectorAlloc = 16 << 20
	// maxDecodeDepth bounds the nesting of recursive TL types and of
	// objects decoded through Object() (containers, gzip_packed,
	// rpc_result): exhausting the goroutine stack is a fatal error that
	// recover cannot catch.
	maxDecodeDepth = 100
)

// pluma: decodeBudget is what one message may still consume; a buffer shares
// it with the buffers of the objects nested in it (newNestedDecodeBuf).
type decodeBudget struct {
	vectorAlloc int // bytes allocated for vector elements
	unpacked    int // bytes unpacked from gzip_packed
}

type DecodeBuf struct {
	buf  []byte
	off  int
	size int
	err  error
	// pluma: nesting depth (enterNested) and the shared budget of the
	// message; nil until first needed.
	depth  int
	budget *decodeBudget
}

var decodeBufPool = sync.Pool{
	New: func() interface{} { return &DecodeBuf{} },
}

// GetDecodeBuf returns a pooled DecodeBuf initialized with b. Call PutDecodeBuf when done.
func GetDecodeBuf(b []byte) *DecodeBuf {
	m := decodeBufPool.Get().(*DecodeBuf)
	m.buf = b
	m.off = 0
	m.size = len(b)
	m.err = nil
	m.depth = 0    // pluma: see enterNested
	m.budget = nil // pluma: see getBudget
	return m
}

// PutDecodeBuf returns a DecodeBuf to the pool.
func PutDecodeBuf(m *DecodeBuf) {
	m.buf = nil
	m.err = nil
	m.budget = nil // pluma: see getBudget
	decodeBufPool.Put(m)
}

func NewDecodeBuf(b []byte) *DecodeBuf {
	return &DecodeBuf{buf: b, size: len(b)} // pluma: keyed, DecodeBuf has more fields
}

// pluma: remaining returns the number of undecoded bytes.
func (m *DecodeBuf) remaining() int {
	return m.size - m.off
}

// pluma: getBudget returns the budget of the message m belongs to.
func (m *DecodeBuf) getBudget() *decodeBudget {
	if m.budget == nil {
		m.budget = &decodeBudget{}
	}
	return m.budget
}

// pluma: vectorTooLong must be called before allocating a vector of n
// elements of elemSize bytes (plus a pointer each), whose encoding takes at
// least minVectorElemSize bytes per element. It reports (and records as the
// decode error) a negative length, one the remaining input cannot hold, or
// one that would exceed the message's allocation budget; otherwise it charges
// the budget.
func (m *DecodeBuf) vectorTooLong(n int32, elemSize uintptr) bool {
	return m.vectorTooLongN(n, minVectorElemSize, int(elemSize)+8)
}

func (m *DecodeBuf) vectorTooLongN(n int32, minEncodedSize, allocSize int) bool {
	if m.err != nil {
		return true
	}
	if n < 0 || int64(n)*int64(minEncodedSize) > int64(m.remaining()) {
		m.err = errDecodeVectorLen
		return true
	}
	b := m.getBudget()
	alloc := int64(n) * int64(allocSize)
	if alloc > int64(maxVectorAlloc-b.vectorAlloc) {
		m.err = errDecodeVectorAlloc
		return true
	}
	b.vectorAlloc += int(alloc)
	return false
}

// pluma: enterNested must be called (and paired with leaveNested) before
// decoding a value of a recursive TL type or a nested object, so that the
// nesting depth stays below maxDecodeDepth. It returns false, with the decode
// error set, when the input is nested too deeply or already failed.
func (m *DecodeBuf) enterNested() bool {
	if m.err != nil {
		return false
	}
	if m.depth >= maxDecodeDepth {
		m.err = errDecodeTooDeep
		return false
	}
	m.depth++
	return true
}

func (m *DecodeBuf) leaveNested() {
	m.depth--
}

// pluma: newNestedDecodeBuf returns a buffer for bytes embedded in m (a
// message body, unpacked gzip data) that continues m's nesting depth and
// shares its budget.
func (m *DecodeBuf) newNestedDecodeBuf(b []byte) *DecodeBuf {
	d := NewDecodeBuf(b)
	d.depth = m.depth
	d.budget = m.getBudget()
	return d
}

// NewNestedDecodeBuf returns a buffer for a query embedded in an object
// decoded from m (the query of invoke* and initConnection, kept as raw bytes),
// one nesting level deeper than m and sharing its allocation budget.
// pluma: a caller that decodes such queries one level at a time (session)
// keeps the limits of the whole message instead of starting each level at
// depth 0 with a fresh budget.
func (m *DecodeBuf) NewNestedDecodeBuf(b []byte) *DecodeBuf {
	d := m.newNestedDecodeBuf(b)
	if d.depth >= maxDecodeDepth {
		d.err = errDecodeTooDeep
	} else {
		d.depth++
	}
	return d
}

// pluma: gzipBudget returns how many bytes gzip_packed objects may still
// unpack within the message m belongs to; useGzipBudget charges it.
func (m *DecodeBuf) gzipBudget() int {
	return maxGzipUnpackedSize - m.getBudget().unpacked
}

func (m *DecodeBuf) useGzipBudget(n int) {
	m.getBudget().unpacked += n
}

func (m *DecodeBuf) GetOffset() int {
	return m.off
}

func (m *DecodeBuf) GetSize() int {
	return m.size
}

func (m *DecodeBuf) GetError() error {
	return m.err
}

func (m *DecodeBuf) SetError(err error) {
	m.err = err
}

func (m *DecodeBuf) Long() int64 {
	if m.err != nil {
		return 0
	}
	if m.off+8 > m.size {
		m.err = errDecodeLong
		return 0
	}
	x := int64(binary.LittleEndian.Uint64(m.buf[m.off : m.off+8]))
	m.off += 8
	return x
}

func (m *DecodeBuf) Double() float64 {
	if m.err != nil {
		return 0
	}
	if m.off+8 > m.size {
		m.err = errDecodeDouble
		return 0
	}
	x := math.Float64frombits(binary.LittleEndian.Uint64(m.buf[m.off : m.off+8]))
	m.off += 8
	return x
}

func (m *DecodeBuf) Int() int32 {
	if m.err != nil {
		return 0
	}
	if m.off+4 > m.size {
		m.err = errDecodeInt
		return 0
	}
	x := binary.LittleEndian.Uint32(m.buf[m.off : m.off+4])
	m.off += 4
	return int32(x)
}

func (m *DecodeBuf) UInt() uint32 {
	if m.err != nil {
		return 0
	}
	if m.off+4 > m.size {
		m.err = errDecodeUInt
		return 0
	}
	x := binary.LittleEndian.Uint32(m.buf[m.off : m.off+4])
	m.off += 4
	return x
}

func (m *DecodeBuf) Bytes(size int) []byte {
	if m.err != nil {
		return nil
	}
	if size < 0 || size > m.remaining() { // pluma: a negative size made make() panic
		m.err = errDecodeBytes
		return nil
	}
	x := make([]byte, size)
	copy(x, m.buf[m.off:m.off+size])
	m.off += size
	return x
}

// BytesNoCopy returns a slice of the underlying buffer without allocation or copy.
// The returned slice is only valid while the DecodeBuf's buffer is not modified or freed.
// Use for synchronous processing paths where the data is consumed before the buffer is reused.
func (m *DecodeBuf) BytesNoCopy(size int) []byte {
	if m.err != nil {
		return nil
	}
	if size < 0 || size > m.remaining() { // pluma: a negative size made slicing panic
		m.err = errDecodeBytes
		return nil
	}
	x := m.buf[m.off : m.off+size]
	m.off += size
	return x
}

// StringBytes
/*
   public String readString(boolean exception) {
       int startReadPosition = getPosition();
       try {
           int sl = 1;
           int l = getIntFromByte(buffer.get());
           if (l >= 254) {
               l = getIntFromByte(buffer.get()) | (getIntFromByte(buffer.get()) << 8) | (getIntFromByte(buffer.get()) << 16);
               sl = 4;
           }
           byte[] b = new byte[l];
           buffer.get(b);
           int i = sl;
           while ((l + i) % 4 != 0) {
               buffer.get();
               i++;
           }
           return new String(b, "UTF-8");
       } catch (Exception e) {
           if (exception) {
               throw new RuntimeException("read string error", e);
           } else {
               if (BuildVars.LOGS_ENABLED) {
                   FileLog.e("read string error");
               }
           }
           position(startReadPosition);
       }
       return "";
   }
*/
func (m *DecodeBuf) StringBytes() []byte {
	if m.err != nil {
		return nil
	}
	var size, padding int

	if m.off+1 > m.size {
		m.err = errDecodeStringBytes
		return nil
	}
	size = int(m.buf[m.off])
	m.off++
	padding = (4 - ((size + 1) % 4)) & 3
	if size == 254 {
		if m.off+3 > m.size {
			m.err = errDecodeStringBytes
			return nil
		}
		size = int(m.buf[m.off]) | int(m.buf[m.off+1])<<8 | int(m.buf[m.off+2])<<16
		m.off += 3
		padding = (4 - size%4) & 3
	}

	if m.off+size > m.size {
		m.err = fmt.Errorf("DecodeStringBytes - Wrong size, (%d, %d, %d)", m.off, size, m.size)
		return nil
	}
	x := make([]byte, size)
	copy(x, m.buf[m.off:m.off+size])
	m.off += size

	if m.off+padding > m.size {
		m.err = errDecodeStringBytes
		return nil
	}
	m.off += padding

	return x
}

// StringBytesNoCopy returns a slice of the underlying buffer without allocation or copy.
// The returned slice is only valid while the DecodeBuf's buffer is not modified or freed.
// Use for synchronous processing paths where the data is consumed before the buffer is reused.
func (m *DecodeBuf) StringBytesNoCopy() []byte {
	if m.err != nil {
		return nil
	}
	var size, padding int

	if m.off+1 > m.size {
		m.err = errDecodeStringBytes
		return nil
	}
	size = int(m.buf[m.off])
	m.off++
	padding = (4 - ((size + 1) % 4)) & 3
	if size == 254 {
		if m.off+3 > m.size {
			m.err = errDecodeStringBytes
			return nil
		}
		size = int(m.buf[m.off]) | int(m.buf[m.off+1])<<8 | int(m.buf[m.off+2])<<16
		m.off += 3
		padding = (4 - size%4) & 3
	}

	if m.off+size > m.size {
		m.err = fmt.Errorf("DecodeStringBytes - Wrong size, (%d, %d, %d)", m.off, size, m.size)
		return nil
	}
	x := m.buf[m.off : m.off+size]
	m.off += size

	if m.off+padding > m.size {
		m.err = errDecodeStringBytes
		return nil
	}
	m.off += padding

	return x
}

func (m *DecodeBuf) String() string {
	b := m.StringBytes()
	if m.err != nil {
		return ""
	}
	x := string(b)
	return x
}

func (m *DecodeBuf) BigInt() *big.Int {
	b := m.StringBytes()
	if m.err != nil {
		return nil
	}
	y := make([]byte, len(b)+1)
	y[0] = 0
	copy(y[1:], b)
	x := new(big.Int).SetBytes(y)
	return x
}

func (m *DecodeBuf) VectorInt() []int32 {
	constructor := m.Int()
	if m.err != nil {
		return nil
	}
	if constructor != int32(CRC32_vector) {
		m.err = fmt.Errorf("DecodeVectorInt: Wrong constructor (0x%08x)", constructor)
		return nil
	}
	size := m.Int()
	if m.err != nil {
		return nil
	}
	if size < 0 {
		m.err = errDecodeVectorInt
		return nil
	}
	if m.vectorTooLongN(size, 4, int(unsafe.Sizeof(int32(0)))) { // pluma: bound by the input and the budget
		return nil
	}
	x := make([]int32, size)
	i := int32(0)
	for i < size {
		y := m.Int()
		if m.err != nil {
			return nil
		}
		x[i] = y
		i++
	}
	return x
}

func (m *DecodeBuf) VectorLong() []int64 {
	constructor := m.Int()
	if m.err != nil {
		return nil
	}
	if constructor != int32(CRC32_vector) {
		m.err = fmt.Errorf("DecodeVectorLong: Wrong constructor (0x%08x)", constructor)
		return nil
	}
	size := m.Int()
	if m.err != nil {
		return nil
	}
	if size < 0 {
		m.err = errDecodeVectorLong
		return nil
	}
	if m.vectorTooLongN(size, 8, int(unsafe.Sizeof(int64(0)))) { // pluma: bound by the input and the budget
		return nil
	}
	x := make([]int64, size)
	i := int32(0)
	for i < size {
		y := m.Long()
		if m.err != nil {
			return nil
		}
		x[i] = y
		i++
	}
	return x
}

func (m *DecodeBuf) VectorString() []string {
	constructor := m.Int()
	if m.err != nil {
		return nil
	}
	if constructor != int32(CRC32_vector) {
		m.err = fmt.Errorf("DecodeVectorString: Wrong constructor (0x%08x)", constructor)
		return nil
	}
	size := m.Int()
	if m.err != nil {
		return nil
	}
	if size < 0 {
		m.err = errDecodeVectorStr
		return nil
	}
	if m.vectorTooLongN(size, 4, int(unsafe.Sizeof(""))) { // pluma: bound by the input and the budget
		return nil
	}
	x := make([]string, size)
	i := int32(0)
	for i < size {
		y := m.String()
		if m.err != nil {
			return nil
		}
		x[i] = y
		i++
	}
	return x
}

func (m *DecodeBuf) VectorBytes() [][]byte {
	constructor := m.Int()
	if m.err != nil {
		return nil
	}
	if constructor != int32(CRC32_vector) {
		m.err = fmt.Errorf("DecodeVectorBytes: Wrong constructor (0x%08x)", constructor)
		return nil
	}
	size := m.Int()
	if m.err != nil {
		return nil
	}
	if size < 0 {
		m.err = errDecodeVectorBytes
		return nil
	}
	if m.vectorTooLongN(size, 4, int(unsafe.Sizeof([]byte(nil)))) { // pluma: bound by the input and the budget
		return nil
	}
	x := make([][]byte, size)
	i := int32(0)
	for i < size {
		y := m.StringBytes()
		if m.err != nil {
			return nil
		}
		x[i] = y
		i++
	}
	return x
}

func (m *DecodeBuf) Bool() bool {
	constructor := m.Int()
	if m.err != nil {
		return false
	}
	switch constructor {
	case int32(CRC32_boolTrue):
		return true
	case int32(CRC32_boolFalse):
		return false
	}
	return false
}

/*
func (m *DecodeBuf) Vector() []TLObject {
	constructor := m.Int()
	if m.err != nil {
		return nil
	}
	if constructor != int32(TLConstructor_CRC32_vector) {
		m.err = fmt.Errorf("DecodeVector: Wrong constructor (0x%08x)", constructor)
		return nil
	}
	size := m.Int()
	if m.err != nil {
		return nil
	}
	if size < 0 {
		m.err = errors.New("DecodeVector: Wrong size")
		return nil
	}
	x := make([]TLObject, size)
	i := int32(0)
	for i < size {
		y := m.Object()
		if m.err != nil {
			return nil
		}
		x[i] = y
		i++
	}
	return x
}
*/

func (m *DecodeBuf) Object() (r TLObject) {
	// pluma: objects decoded here may contain further objects (containers,
	// gzip_packed, rpc_result); bound the nesting.
	if !m.enterNested() {
		return nil
	}
	defer m.leaveNested()

	classID := m.Int()
	if m.err != nil {
		return nil
	}

	r = NewTLObjectByClassID(classID)
	if r == nil {
		m.err = fmt.Errorf("can't find registered classId: %x", uint32(classID))
		return nil
	}

	// log.Infof("newTLObjectByClassID, classID: %x", uint32(classID))
	err := r.Decode(m)
	if err != nil {
		// pluma: keep the cause a limit recorded (errors.Is works on it).
		if m.err != nil {
			err = m.err
		}
		m.err = fmt.Errorf("object(%x) decode error: %w", uint32(classID), err)
	}

	return
}

/*
func (d *DecodeBuf) dump() {
	fmt.Println(hex.Dump(d.buf[d.off:d.size]))
}

/*
func toBool(x TLObject) bool {
	_, ok := x.(TL_boolTrue)
	return ok
}
*/
