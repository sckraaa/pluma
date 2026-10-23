package sess

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// TestDetailedInfoFromClient: msg_detailed_info and msg_new_detailed_info are
// server-to-client messages, but a client can send them. Both are only logged;
// msg_new_detailed_info used to be type-asserted to msg_detailed_info and
// panicked.
func TestDetailedInfoFromClient(t *testing.T) {
	encode := func(o mtproto.TLObject) []byte {
		x := mtproto.NewEncodeBuf(64)
		if err := o.Encode(x, 228); err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), x.GetBuf()...)
	}

	for _, tc := range []struct {
		name string
		obj  mtproto.TLObject
	}{
		{"msg_detailed_info", mtproto.MakeTLMsgDetailedInfo(&mtproto.MsgDetailedInfo{MsgId: 1, AnswerMsgId: 2, Bytes: 3})},
		{"msg_new_detailed_info", mtproto.MakeTLMsgNewDetailedInfo(&mtproto.MsgDetailedInfo{AnswerMsgId: 2, Bytes: 3})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := encode(tc.obj)
			// The decoder must hand processMsg the same type, or the case
			// under test is never reached.
			if got, want := fmt.Sprintf("%T", mtproto.NewDecodeBuf(body).Object()), fmt.Sprintf("%T", tc.obj); got != want {
				t.Fatalf("decoded %s, want %s", got, want)
			}
			w, sess := newTestUnboundWrapper(t, &fakeAuthsessionClient{})
			processEncoded(t, sess, testBindMsgId+8, body)
			if len(w.tmpRpcApiMessageList) != 0 {
				t.Fatalf("%d requests queued", len(w.tmpRpcApiMessageList))
			}
		})
	}
}
