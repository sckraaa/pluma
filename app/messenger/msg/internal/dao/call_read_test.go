package dao

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The message of a call the callee answered or declined is read on
// arrival; a missed call, one without a reason and any other message are
// not.
func TestCallTakenUp(t *testing.T) {
	call := func(reason *mtproto.PhoneCallDiscardReason, duration int32) *mtproto.Message {
		return mtproto.MakeTLMessageService(&mtproto.Message{
			Action: mtproto.MakeMessageActionPhoneCall(false, 1, reason, duration),
		}).To_Message()
	}
	for _, c := range []struct {
		name string
		m    *mtproto.Message
		want bool
	}{
		{"answered", call(mtproto.MakeTLPhoneCallDiscardReasonHangup(nil).To_PhoneCallDiscardReason(), 13), true},
		{"answered, connection lost", call(mtproto.MakeTLPhoneCallDiscardReasonDisconnect(nil).To_PhoneCallDiscardReason(), 0), true},
		{"declined", call(mtproto.MakeTLPhoneCallDiscardReasonBusy(nil).To_PhoneCallDiscardReason(), 0), true},
		{"declined by hangup", call(mtproto.MakeTLPhoneCallDiscardReasonHangup(nil).To_PhoneCallDiscardReason(), 0), true},
		{"missed", call(mtproto.MakeTLPhoneCallDiscardReasonMissed(nil).To_PhoneCallDiscardReason(), 0), false},
		{"no reason", call(nil, 0), false},
		{"other action", mtproto.MakeTLMessageService(&mtproto.Message{
			Action: mtproto.MakeMessageActionScreenshotTaken(),
		}).To_Message(), false},
		{"text", mtproto.MakeTLMessage(&mtproto.Message{Message: "x"}).To_Message(), false},
	} {
		if got := callTakenUp(c.m); got != c.want {
			t.Errorf("%s: callTakenUp = %v, want %v", c.name, got, c.want)
		}
	}
}
