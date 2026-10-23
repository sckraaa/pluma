// pluma: tests for the limits that keep decoding of untrusted input bounded
// (vector lengths, allocation budget, nesting depth, gzip_packed size).

package mtproto

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"testing"
)

// tlBuf builds raw TL input for tests.
type tlBuf struct{ b []byte }

func (t *tlBuf) i32(v int32) *tlBuf {
	t.b = binary.LittleEndian.AppendUint32(t.b, uint32(v))
	return t
}

func (t *tlBuf) c(v TLConstructor) *tlBuf { return t.i32(int32(v)) }

func (t *tlBuf) i64(v int64) *tlBuf {
	t.b = binary.LittleEndian.AppendUint64(t.b, uint64(v))
	return t
}

func (t *tlBuf) raw(b []byte) *tlBuf {
	t.b = append(t.b, b...)
	return t
}

// str appends a TL string/bytes value.
func (t *tlBuf) str(b []byte) *tlBuf {
	x := NewEncodeBuf(len(b) + 8)
	x.StringBytes(b)
	return t.raw(x.GetBuf())
}

// message2 appends msg_id seqno bytes body.
func (t *tlBuf) message2(msgId int64, body []byte) *tlBuf {
	return t.i64(msgId).i32(0).i32(int32(len(body))).raw(body)
}

func gzipBytes(t testing.TB, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gzipPacked(t testing.TB, obj []byte) []byte {
	return (&tlBuf{}).c(CRC32_gzip_packed).str(gzipBytes(t, obj)).b
}

func helpGetConfig() []byte { return (&tlBuf{}).c(CRC32_help_getConfig).b }

// msgsAck is msgs_ack with the given vector length and ids actually present.
func msgsAck(length int32, ids int) []byte {
	t := (&tlBuf{}).c(CRC32_msgs_ack).c(CRC32_vector).i32(length)
	for i := 0; i < ids; i++ {
		t.i64(int64(i))
	}
	return t.b
}

// updatesWithConfigs is updates{updates: n × updateConfig, users: [], chats: []}.
func updatesWithConfigs(n int) []byte {
	t := (&tlBuf{}).c(CRC32_updates).c(CRC32_vector).i32(int32(n))
	for i := 0; i < n; i++ {
		t.c(CRC32_updateConfig)
	}
	return t.c(CRC32_vector).i32(0).c(CRC32_vector).i32(0).i32(0).i32(0).b
}

// nestedJSONArrays is jsonArray[jsonArray[...[jsonNull]...]] depth levels deep,
// as the params of help.saveAppLog-like input would carry it.
func nestedJSONArrays(depth int) []byte {
	t := &tlBuf{}
	for i := 0; i < depth; i++ {
		t.c(CRC32_jsonArray).c(CRC32_vector).i32(1)
	}
	return t.c(CRC32_jsonNull).b
}

// nestedContainers is msg_container[msg_container[...[help.getConfig]...]].
func nestedContainers(depth int) []byte {
	body := helpGetConfig()
	for i := 0; i < depth; i++ {
		body = (&tlBuf{}).c(CRC32_msg_container).i32(1).message2(int64(i+1)<<2, body).b
	}
	return body
}

func decodeObject(b []byte) (TLObject, error) {
	d := NewDecodeBuf(b)
	o := d.Object()
	return o, d.GetError()
}

// allocDuring returns the bytes allocated while f runs.
func allocDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// allocLimit scales an allocation bound to the build: with -race the compiler
// keeps temporary allocations it otherwise elides (bytes.Buffer growth then
// allocates twice), so the same decoding allocates up to twice as much. The
// bounds still catch unbounded allocation in both builds.
func allocLimit(n uint64) uint64 {
	if raceEnabled {
		return 2 * n
	}
	return n
}

func TestDecodeLimits(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		wantErr error // nil: must decode; errAny: any error
		// maxAlloc bounds what decoding may allocate.
		maxAlloc uint64
	}{
		{"msgs_ack ok", msgsAck(3, 3), nil, 4 << 10},
		{"msgs_ack huge length", msgsAck(0x7fffffff, 1), errDecodeVectorLen, 4 << 10},
		{"msgs_ack negative length", msgsAck(-1, 1), errAny, 4 << 10},
		{"msgs_ack length beyond input", msgsAck(4, 3), errDecodeVectorLen, 4 << 10},
		{"updates ok", updatesWithConfigs(1000), nil, 4 << 20},
		{"updates over the allocation budget", updatesWithConfigs(10000), errDecodeVectorAlloc, 1 << 20},
		{"updates huge length", (&tlBuf{}).c(CRC32_updates).c(CRC32_vector).i32(0x7fffffff).c(CRC32_updateConfig).b, errDecodeVectorLen, 4 << 10},
		{"msg_container huge length", (&tlBuf{}).c(CRC32_msg_container).i32(0x7fffffff).b, errDecodeVectorLen, 4 << 10},
		{"msg_container negative length", (&tlBuf{}).c(CRC32_msg_container).i32(-5).b, errDecodeVectorLen, 4 << 10},
		{"message2 negative bytes", (&tlBuf{}).c(CRC32_msg_container).i32(1).i64(4).i32(0).i32(-8).raw(helpGetConfig()).b, errAny, 4 << 10},
		{"message2 bytes beyond input", (&tlBuf{}).c(CRC32_msg_container).i32(1).i64(4).i32(0).i32(1 << 30).raw(helpGetConfig()).b, errAny, 4 << 10},
		{"containers nested 50 deep", nestedContainers(50), nil, 1 << 20},
		{"containers nested 200 deep", nestedContainers(200), errDecodeTooDeep, 1 << 20},
		{"json nested 50 deep", (&tlBuf{}).c(CRC32_jsonArray).c(CRC32_vector).i32(1).raw(nestedJSONArrays(49)).b, nil, 1 << 20},
		{"json nested 100k deep", nestedJSONArrays(100_000), errDecodeTooDeep, 1 << 20},
		{"gzip_packed ok", gzipPacked(t, helpGetConfig()), nil, 1 << 20},
		{"future_salts", (&tlBuf{}).c(CRC32_future_salts).i64(1).i32(2).i32(1).i32(3).i32(4).i64(5).b, nil, 4 << 10},
		{"future_salts huge length", (&tlBuf{}).c(CRC32_future_salts).i64(1).i32(2).i32(0x7fffffff).b, errDecodeVectorLen, 4 << 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				o   TLObject
				err error
			)
			alloc := allocDuring(func() { o, err = decodeObject(tc.in) })
			switch {
			case tc.wantErr == nil && (err != nil || o == nil):
				t.Fatalf("decode failed: %v", err)
			case tc.wantErr == errAny && err == nil:
				t.Fatalf("decode succeeded, want an error")
			case tc.wantErr != nil && tc.wantErr != errAny && !errors.Is(err, tc.wantErr):
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if alloc > allocLimit(tc.maxAlloc) {
				t.Fatalf("decoding %d bytes allocated %d bytes, want <= %d", len(tc.in), alloc, allocLimit(tc.maxAlloc))
			}
		})
	}
}

var errAny = errors.New("any error")

func TestDecodeFutureSaltsBareElements(t *testing.T) {
	in := (&tlBuf{}).c(CRC32_future_salts).i64(1).i32(2).i32(1).i32(3).i32(4).i64(5).b
	o, err := decodeObject(in)
	if err != nil {
		t.Fatal(err)
	}
	salts := o.(*TLFutureSalts).GetSalts()
	if len(salts) != 1 || salts[0].GetValidSince() != 3 || salts[0].GetValidUntil() != 4 || salts[0].GetSalt() != 5 {
		t.Fatalf("salts = %v", salts)
	}
}

func TestDecodeBufBounds(t *testing.T) {
	d := NewDecodeBuf(make([]byte, 8))
	if b := d.Bytes(-1); b != nil || d.GetError() == nil {
		t.Fatalf("Bytes(-1) = %v, %v", b, d.GetError())
	}
	d = NewDecodeBuf(make([]byte, 8))
	if b := d.BytesNoCopy(-1); b != nil || d.GetError() == nil {
		t.Fatalf("BytesNoCopy(-1) = %v, %v", b, d.GetError())
	}

	vec := func(n int32, payload int) []byte {
		return (&tlBuf{}).c(CRC32_vector).i32(n).raw(make([]byte, payload)).b
	}
	for _, tc := range []struct {
		name string
		f    func(d *DecodeBuf) int
	}{
		{"VectorInt", func(d *DecodeBuf) int { return len(d.VectorInt()) }},
		{"VectorLong", func(d *DecodeBuf) int { return len(d.VectorLong()) }},
		{"VectorString", func(d *DecodeBuf) int { return len(d.VectorString()) }},
		{"VectorBytes", func(d *DecodeBuf) int { return len(d.VectorBytes()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDecodeBuf(vec(0x7fffffff, 16))
			var n int
			alloc := allocDuring(func() { n = tc.f(d) })
			if n != 0 || !errors.Is(d.GetError(), errDecodeVectorLen) {
				t.Fatalf("got %d elements, err %v", n, d.GetError())
			}
			if alloc > allocLimit(4<<10) {
				t.Fatalf("allocated %d bytes", alloc)
			}

			d = NewDecodeBuf(vec(2, 16))
			if n := tc.f(d); n != 2 || d.GetError() != nil {
				t.Fatalf("valid vector: got %d elements, err %v", n, d.GetError())
			}
		})
	}
}

// gzipZeros packs n zero bytes without holding them in memory.
func gzipZeros(t testing.TB, n int) []byte {
	t.Helper()
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	chunk := make([]byte, 1<<20)
	for n > 0 {
		k := min(n, len(chunk))
		if _, err := w.Write(chunk[:k]); err != nil {
			t.Fatal(err)
		}
		n -= k
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecodeGzipBomb(t *testing.T) {
	// 256 MiB of zeros packs into about 256 KB.
	bomb := (&tlBuf{}).c(CRC32_gzip_packed).str(gzipZeros(t, 256<<20)).b
	var err error
	alloc := allocDuring(func() { _, err = decodeObject(bomb) })
	if !errors.Is(err, errGzipTooLarge) {
		t.Fatalf("err = %v, want %v", err, errGzipTooLarge)
	}
	// Unpacking stops at the limit: what is allocated is the limit and the
	// buffer growth around it, not the 256 MiB.
	if alloc > allocLimit(5*maxGzipUnpackedSize) {
		t.Fatalf("allocated %d bytes", alloc)
	}

	// The budget is per message: many small bombs in one container add up.
	body := helpGetConfig()
	body = append(body, make([]byte, 6<<20)...) // padding after the object is ignored
	t6 := gzipPacked(t, body)
	c := (&tlBuf{}).c(CRC32_msg_container).i32(3)
	for i := 0; i < 3; i++ {
		c.message2(int64(i+1)<<2, t6)
	}
	if _, err := decodeObject(c.b); !errors.Is(err, errGzipTooLarge) {
		t.Fatalf("container of three 6 MiB packs: err = %v, want %v", err, errGzipTooLarge)
	}

	// Two of them fit.
	c = (&tlBuf{}).c(CRC32_msg_container).i32(2)
	for i := 0; i < 2; i++ {
		c.message2(int64(i+1)<<2, t6)
	}
	if _, err := decodeObject(c.b); err != nil {
		t.Fatalf("container of two 6 MiB packs: %v", err)
	}
}

func TestDecodeBudgetIsPerMessage(t *testing.T) {
	// A pooled buffer starts every message with a fresh budget.
	in := updatesWithConfigs(5000) // about 10 MiB of the 16 MiB budget
	for i := 0; i < 3; i++ {
		d := GetDecodeBuf(in)
		o := d.Object()
		err := d.GetError()
		PutDecodeBuf(d)
		if o == nil || err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
}

// FuzzDecodeObject decodes arbitrary input as a TL object: it must never
// panic and never allocate more than a bounded multiple of its input.
func FuzzDecodeObject(f *testing.F) {
	seeds := [][]byte{
		msgsAck(3, 3),
		msgsAck(0x7fffffff, 1),
		updatesWithConfigs(3),
		nestedContainers(3),
		nestedJSONArrays(5),
		gzipPacked(f, helpGetConfig()),
		gzipPacked(f, nestedContainers(2)),
		(&tlBuf{}).c(CRC32_future_salts).i64(1).i32(2).i32(1).i32(3).i32(4).i64(5).b,
		(&tlBuf{}).c(CRC32_rpc_result).i64(1).raw(helpGetConfig()).b,
		(&tlBuf{}).c(CRC32_msg_copy).c(CRC32_msg_container).i32(0).b,
		(&tlBuf{}).c(CRC32_invokeWithLayer).i32(228).raw(helpGetConfig()).b,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		alloc := allocDuring(func() {
			d := GetDecodeBuf(in)
			_ = d.Object()
			PutDecodeBuf(d)
		})
		// Vector elements may be up to ~2 KB per 4 input bytes and gzip
		// data may unpack about 1000:1, both within fixed per-message caps.
		limit := uint64(1<<20) + 2048*uint64(len(in))
		if limit > 3*maxVectorAlloc+3*maxGzipUnpackedSize {
			limit = 3*maxVectorAlloc + 3*maxGzipUnpackedSize
		}
		if limit = allocLimit(limit); alloc > limit {
			t.Fatalf("decoding %d bytes allocated %d bytes (limit %d)", len(in), alloc, limit)
		}
	})
}

// registeredClassIds lists every registered constructor id in a fixed order.
func registeredClassIds() []int32 {
	ids := make([]int32, 0, len(clazzIdRegisters2))
	for id := range clazzIdRegisters2 {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// FuzzDecodeEachConstructor decodes arbitrary data as the body of every
// registered constructor (picked by index), so that each generated decoder is
// reached: none may panic or allocate unboundedly.
func FuzzDecodeEachConstructor(f *testing.F) {
	ids := registeredClassIds()
	for i := 0; i < len(ids); i += 7 {
		f.Add(uint16(i), []byte{})
		f.Add(uint16(i), (&tlBuf{}).i32(-1).c(CRC32_vector).i32(0x7fffffff).b)
		f.Add(uint16(i), (&tlBuf{}).i32(0x7fffffff).c(CRC32_vector).i32(3).i64(1).i64(2).i64(3).b)
	}
	f.Fuzz(func(t *testing.T, idx uint16, body []byte) {
		in := (&tlBuf{}).i32(ids[int(idx)%len(ids)]).raw(body).b
		alloc := allocDuring(func() {
			d := GetDecodeBuf(in)
			_ = d.Object()
			PutDecodeBuf(d)
		})
		limit := uint64(1<<20) + 2048*uint64(len(in))
		if limit = allocLimit(limit); alloc > limit {
			t.Fatalf("decoding %d bytes allocated %d bytes (limit %d)", len(in), alloc, limit)
		}
	})
}

// TestDecodeEachConstructorShortInput decodes every registered constructor
// from truncated and hostile bodies.
func TestDecodeEachConstructorShortInput(t *testing.T) {
	bodies := [][]byte{
		nil,
		{0xff},
		(&tlBuf{}).i32(-1).i32(-1).i32(-1).i32(-1).b,
		(&tlBuf{}).i32(-1).c(CRC32_vector).i32(0x7fffffff).b,
		(&tlBuf{}).i32(0x7fffffff).c(CRC32_vector).i32(0x7fffffff).c(CRC32_vector).i32(0x7fffffff).b,
		bytes.Repeat([]byte{0xfe, 0xff, 0xff, 0x7f}, 64),
	}
	for _, id := range registeredClassIds() {
		for _, body := range bodies {
			in := (&tlBuf{}).i32(id).raw(body).b
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("constructor %#x, body %x: panic: %v", uint32(id), body, r)
					}
				}()
				d := NewDecodeBuf(in)
				_ = d.Object()
			}()
		}
	}
}

// pluma: invoke* wrappers keep their query as bytes that point into the
// message (no copy per level), and a query decoded through
// NewNestedDecodeBuf is one level deeper and shares the budget.
func TestNestedQueryBuffer(t *testing.T) {
	inner := (&tlBuf{}).c(CRC32_help_getConfig).b
	msg := (&tlBuf{}).c(CRC32_invokeWithoutUpdates).raw(inner).b

	d := NewDecodeBuf(msg)
	w, ok := d.Object().(*TLInvokeWithoutUpdates)
	if !ok || d.GetError() != nil {
		t.Fatalf("decode: %v", d.GetError())
	}
	if len(w.Query) != len(inner) || &w.Query[0] != &msg[4] {
		t.Fatal("query copied")
	}

	n := d.NewNestedDecodeBuf(w.Query)
	if n.depth != d.depth+1 || n.getBudget() != d.getBudget() {
		t.Fatalf("depth %d (parent %d), shared budget %v", n.depth, d.depth, n.getBudget() == d.getBudget())
	}
	if _, ok := n.Object().(*TLHelpGetConfig); !ok || n.GetError() != nil {
		t.Fatalf("nested decode: %v", n.GetError())
	}

	// A chain of nested buffers ends at maxDecodeDepth.
	b := NewDecodeBuf(inner)
	for i := 0; i < maxDecodeDepth; i++ {
		b = b.NewNestedDecodeBuf(inner)
	}
	if b.Object() != nil || !errors.Is(b.GetError(), errDecodeTooDeep) {
		t.Fatalf("depth %d: %v", b.depth, b.GetError())
	}
}
