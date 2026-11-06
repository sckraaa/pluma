package sess

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// TestMsgIdOnWholeSecond: Android makes msg_id from milliseconds, so a
// request sent on a whole second has zero lower 32 bits; it ignores code 18,
// and such a request would never get an answer (ConnectionsManager.cpp
// generateMessageId and the bad_msg_notification switch at aac1efcb). Only a
// msg_id not divisible by 4 is refused.
func TestMsgIdOnWholeSecond(t *testing.T) {
	_, sess := newTestPermWrapper(t)
	now := time.Now().Unix()
	cases := []struct {
		name  string
		msgId int64
		want  bool
	}{
		{"whole second", now << 32, true},
		{"fraction", msgIdAt(now, 0x40000000), true},
		{"not divisible by 4", now<<32 | 0x40000002, false},
	}
	for _, c := range cases {
		msg := &mtproto.TLMessage2{MsgId: c.msgId, Seqno: 1, Object: &mtproto.TLPing{PingId: 1}}
		if got := sess.checkBadMsgNotification(context.Background(), "gateway", false, msg); got != c.want {
			t.Errorf("%s: msg_id %#x accepted %v, want %v", c.name, c.msgId, got, c.want)
		}
	}
}
