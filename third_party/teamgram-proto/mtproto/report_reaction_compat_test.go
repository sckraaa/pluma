// pluma: messages.reportReaction#61422a48, the request Android still sends
// (tgnet/TLRPC.java, TL_messages_reportReaction), decodes into the current
// TLMessagesReportReaction.

package mtproto

import (
	"testing"
)

func reportReactionAndroid(user func(*tlBuf) *tlBuf) []byte {
	b := (&tlBuf{}).
		c(CRC32_messages_reportReaction_61422a48).
		c(CRC32_inputPeerChannel).i64(1000000007).i64(-77).
		i32(42)
	return user(b).b
}

func decodeReportReaction(t *testing.T, raw []byte) *TLMessagesReportReaction {
	t.Helper()
	d := NewDecodeBuf(raw)
	obj := d.Object()
	if err := d.GetError(); err != nil {
		t.Fatalf("decode: %v", err)
	}
	r, ok := obj.(*TLMessagesReportReaction)
	if !ok {
		t.Fatalf("decoded %T, want *TLMessagesReportReaction", obj)
	}
	return r
}

func TestReportReactionAndroidConstructor(t *testing.T) {
	t.Run("inputUser", func(t *testing.T) {
		r := decodeReportReaction(t, reportReactionAndroid(func(b *tlBuf) *tlBuf {
			return b.c(CRC32_inputUser).i64(1234).i64(5678)
		}))
		if got := r.GetPeer(); got.GetPredicateName() != Predicate_inputPeerChannel || got.GetChannelId() != 1000000007 || got.GetAccessHash() != -77 {
			t.Fatalf("peer = %v", got)
		}
		if r.GetId() != 42 {
			t.Fatalf("id = %d", r.GetId())
		}
		p := r.GetReactionPeer()
		if p.GetPredicateName() != Predicate_inputPeerUser || p.GetUserId() != 1234 || p.GetAccessHash() != 5678 || p.GetConstructor() != CRC32_inputPeerUser {
			t.Fatalf("reaction_peer = %v", p)
		}
		if u := FromInputPeer2(1, p); !u.IsUser() || u.PeerId != 1234 {
			t.Fatalf("reaction_peer as a peer = %v", u)
		}
	})
	t.Run("inputUserSelf", func(t *testing.T) {
		r := decodeReportReaction(t, reportReactionAndroid(func(b *tlBuf) *tlBuf {
			return b.c(CRC32_inputUserSelf)
		}))
		if p := r.GetReactionPeer(); p.GetPredicateName() != Predicate_inputPeerSelf {
			t.Fatalf("reaction_peer = %v", p)
		}
	})
	t.Run("inputUserEmpty", func(t *testing.T) {
		r := decodeReportReaction(t, reportReactionAndroid(func(b *tlBuf) *tlBuf {
			return b.c(CRC32_inputUserEmpty)
		}))
		if p := r.GetReactionPeer(); p.GetPredicateName() != Predicate_inputPeerEmpty {
			t.Fatalf("reaction_peer = %v", p)
		}
	})
	t.Run("in a message", func(t *testing.T) {
		// The way session reads a request (TLMessage2); before the
		// constructor was known the whole message failed to decode.
		body := reportReactionAndroid(func(b *tlBuf) *tlBuf { return b.c(CRC32_inputUser).i64(1).i64(2) })
		m := &TLMessage2{}
		if err := m.Decode(NewDecodeBuf((&tlBuf{}).message2(1<<32, body).b)); err != nil {
			t.Fatalf("message2: %v", err)
		}
		if _, ok := m.Object.(*TLMessagesReportReaction); !ok {
			t.Fatalf("message2 object %T", m.Object)
		}
	})
	t.Run("unknown user constructor", func(t *testing.T) {
		d := NewDecodeBuf(reportReactionAndroid(func(b *tlBuf) *tlBuf { return b.i32(0x0badf00d) }))
		d.Object()
		if d.GetError() == nil {
			t.Fatal("decoded an unknown InputUser")
		}
	})
	t.Run("truncated", func(t *testing.T) {
		raw := reportReactionAndroid(func(b *tlBuf) *tlBuf { return b.c(CRC32_inputUser).i64(1) })
		d := NewDecodeBuf(raw)
		d.Object()
		if d.GetError() == nil {
			t.Fatal("decoded a truncated request")
		}
	})
}

func TestReportReactionCurrentConstructor(t *testing.T) {
	raw := (&tlBuf{}).
		c(CRC32_messages_reportReaction).
		c(CRC32_inputPeerChannel).i64(9).i64(8).
		i32(7).
		c(CRC32_inputPeerUser).i64(6).i64(5).b
	r := decodeReportReaction(t, raw)
	if p := r.GetReactionPeer(); p.GetPredicateName() != Predicate_inputPeerUser || p.GetUserId() != 6 || r.GetId() != 7 {
		t.Fatalf("decoded %v", r)
	}
}
