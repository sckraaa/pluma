package sess

import (
	"bytes"
	"context"
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// nestedInvokes returns query wrapped n times in invokeWithoutUpdates (and,
// with mixed, alternately in invokeWithLayer), as one encoded object.
func nestedInvokes(t testing.TB, n int, mixed bool, query mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(64)
	if err := query.Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	inner := x.GetBuf()
	var b bytes.Buffer
	b.Grow(n*8 + len(inner))
	withLayer := int32(mtproto.CRC32_invokeWithLayer)
	withoutUpdates := int32(mtproto.CRC32_invokeWithoutUpdates)
	for i := 0; i < n; i++ {
		if mixed && i%2 == 1 {
			b.Write(binary.LittleEndian.AppendUint32(nil, uint32(withLayer)))
			b.Write(binary.LittleEndian.AppendUint32(nil, 228))
		} else {
			b.Write(binary.LittleEndian.AppendUint32(nil, uint32(withoutUpdates)))
		}
	}
	b.Write(inner)
	return b.Bytes()
}

// processEncoded decodes one message body as session does and handles it.
func processEncoded(t testing.TB, sess *session, msgId int64, body []byte) {
	t.Helper()
	d := mtproto.NewDecodeBuf(body)
	obj := d.Object()
	if err := d.GetError(); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sess.processMsg(context.Background(), testGatewayId, testClientIp, &inboxMsg{msgId: msgId}, obj, invokeChain{})
}

// TestInvokeNestingBounded: a message of deeply nested invoke* wrappers is
// refused with an rpc_error at maxInvokeNesting levels, at a cost linear in
// its size (it used to copy the rest of the message at every level and
// recurse once per level: gigabytes and a fatal stack overflow).
func TestInvokeNestingBounded(t *testing.T) {
	getConfig := &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig}

	for _, tc := range []struct {
		name   string
		levels int
		mixed  bool
		wantOk bool
	}{
		{"one wrapper", 1, false, true},
		{"at the limit", maxInvokeNesting, true, true},
		{"one level too many", maxInvokeNesting + 1, false, false},
		{"65k levels (256 KiB)", 1 << 16, false, false},
		{"mixed 256k levels (1.5 MiB)", 1 << 18, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, sess := newTestUnboundWrapper(t, &fakeAuthsessionClient{})
			body := nestedInvokes(t, tc.levels, tc.mixed, getConfig)
			msgId := testBindMsgId + 8

			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			processEncoded(t, sess, msgId, body)
			runtime.ReadMemStats(&after)

			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(4*len(body))+1<<20 {
				t.Fatalf("%d levels (%d bytes) allocated %d bytes", tc.levels, len(body), alloc)
			}
			if tc.wantOk {
				if len(w.tmpRpcApiMessageList) != 1 {
					t.Fatalf("%d requests queued, want 1", len(w.tmpRpcApiMessageList))
				}
				if _, ok := w.tmpRpcApiMessageList[0].reqMsg.(*mtproto.TLHelpGetConfig); !ok {
					t.Fatalf("queued %T", w.tmpRpcApiMessageList[0].reqMsg)
				}
				return
			}
			if len(w.tmpRpcApiMessageList) != 0 {
				t.Fatalf("%d requests queued", len(w.tmpRpcApiMessageList))
			}
			rpcErr := rpcErrorInOutQueue(t, sess, msgId)
			if rpcErr == nil || rpcErr.Code() != 400 || rpcErr.Message() != "INPUT_REQUEST_INVALID" {
				t.Fatalf("reply = %v, want 400 INPUT_REQUEST_INVALID", rpcErr)
			}
		})
	}
}

// TestOfficialClientWrappers: the nesting official clients send is accepted.
func TestOfficialClientWrappers(t *testing.T) {
	w, sess := newTestUnboundWrapper(t, &fakeAuthsessionClient{})
	encode := func(o mtproto.TLObject) []byte {
		x := mtproto.NewEncodeBuf(256)
		if err := o.Encode(x, 228); err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), x.GetBuf()...)
	}
	query := encode(&mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	query = encode(&mtproto.TLInvokeWithoutUpdates{Constructor: mtproto.CRC32_invokeWithoutUpdates, Query: query})
	query = encode(&mtproto.TLInitConnection{
		Constructor:    mtproto.CRC32_initConnection_c1cd5ea9,
		ApiId:          4,
		DeviceModel:    "Pixel",
		SystemVersion:  "Android 15",
		AppVersion:     "12.0",
		SystemLangCode: "en",
		LangPack:       "android",
		LangCode:       "en",
		Query:          query,
	})
	query = encode(&mtproto.TLInvokeWithLayer{Constructor: mtproto.CRC32_invokeWithLayer, Layer: 228, Query: query})
	query = encode(&mtproto.TLInvokeAfterMsg{Constructor: mtproto.CRC32_invokeAfterMsg, MsgId: testBindMsgId, Query: query})

	processEncoded(t, sess, testBindMsgId+12, query)
	if len(w.tmpRpcApiMessageList) != 1 {
		t.Fatalf("%d requests queued, want 1", len(w.tmpRpcApiMessageList))
	}
	if w.Layer() != 228 {
		t.Fatalf("layer %d", w.Layer())
	}
}

// TestNestedQueryKeepsDecodeBudget: the levels share one nesting depth, so a
// deep object at the bottom of the chain cannot use a fresh depth per level.
func TestNestedQueryKeepsDecodeBudget(t *testing.T) {
	root := invokeChain{}
	q := nestedInvokes(t, 1, false, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	_, next, err := root.decodeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if next.level != 1 || next.parent == nil {
		t.Fatalf("chain %+v", next)
	}
	if _, _, err := (invokeChain{level: maxInvokeNesting}).decodeQuery(q); err != errInvokeTooDeep {
		t.Fatalf("too deep: %v", err)
	}
	if _, _, err := root.decodeQuery(nil); err != errInvokeNoQuery {
		t.Fatalf("empty query: %v", err)
	}
}
