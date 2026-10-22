package sess

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// sentMessages decodes what the session handed to the gateway on an HTTP
// connection since the last call (one payload per response channel, which is
// closed after it: salt, session_id, then one message) and returns the
// messages, a container as its elements.
func sentMessages(t *testing.T, chans []chan interface{}) []*mtproto.TLMessage2 {
	t.Helper()
	var out []*mtproto.TLMessage2
	for _, ch := range chans {
		var payload []byte
		select {
		case v, ok := <-ch:
			if !ok {
				continue // read by an earlier call
			}
			payload = v.([]byte)
		default:
			continue
		}
		if len(payload) < 32 {
			t.Fatalf("payload of %d bytes", len(payload))
		}
		msgID := int64(binary.LittleEndian.Uint64(payload[16:]))
		d := mtproto.NewDecodeBuf(payload[32:])
		obj := d.Object()
		if err := d.GetError(); err != nil {
			t.Fatalf("decode message %d: %v", msgID, err)
		}
		if c, ok := obj.(*mtproto.TLMsgContainer); ok {
			out = append(out, c.Messages...)
			continue
		}
		out = append(out, &mtproto.TLMessage2{MsgId: msgID, Object: obj})
	}
	return out
}

// TestNewSessionCreatedSentOnce: the first message of a new session gets
// new_session_created once, followed by the reply. It used to be sent
// directly and then again, with the same msg_id, from the outgoing queue
// together with the reply.
func TestNewSessionCreatedSentOnce(t *testing.T) {
	ctx := context.Background()
	_, sess := newTestUnboundWrapper(t, &fakeAuthsessionClient{})
	const salt = int64(0x5a17)
	now := int32(time.Now().Unix())
	sess.sessList.cacheSalt = mtproto.MakeTLFutureSalt(&mtproto.FutureSalt{
		ValidSince: now - 60,
		ValidUntil: now + 3600,
		Salt:       salt,
	})

	// An HTTP connection: every payload for the client takes one waiting
	// request's response channel, so the channels record each send.
	sess.isHttp = true
	chans := make([]chan interface{}, 4)
	for i := range chans {
		chans[i] = make(chan interface{}, 1)
		sess.httpQueue.Push(chans[i])
	}
	sess.onSessionConnNew(ctx, testGatewayId)

	ping := &mtproto.TLMessage2{
		MsgId:  time.Now().Unix()<<32 | 4,
		Seqno:  1,
		Object: &mtproto.TLPing{Constructor: mtproto.CRC32_ping, PingId: 77},
	}
	sess.onSessionMessageData(ctx, testGatewayId, testClientIp, salt, ping)

	var created, pongs int
	seen := map[int64]bool{}
	for _, m := range sentMessages(t, chans) {
		if seen[m.MsgId] {
			t.Errorf("msg_id %d (%T) sent twice", m.MsgId, m.Object)
		}
		seen[m.MsgId] = true
		switch o := m.Object.(type) {
		case *mtproto.TLNewSessionCreated:
			created++
			if o.GetFirstMsgId() != ping.MsgId || o.GetServerSalt() != salt {
				t.Errorf("new_session_created: first_msg_id %d, salt %d", o.GetFirstMsgId(), o.GetServerSalt())
			}
		case *mtproto.TLPong:
			pongs++
		default:
			t.Errorf("unexpected %T", o)
		}
	}
	if created != 1 || pongs != 1 {
		t.Fatalf("sent new_session_created %d times and pong %d times, want once each", created, pongs)
	}

	// It is still kept for a re-send until the client acknowledges it, but
	// not before waitMsgAcksTimeout.
	var queued *outboxMsg
	for e := sess.outQueue.oMsgs.Front(); e != nil; e = e.Next() {
		if o := e.Value.(*outboxMsg); o.msg != nil && mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(o.msg.Body))) == mtproto.CRC32_new_session_created {
			queued = o
		}
	}
	if queued == nil || queued.sent == 0 {
		t.Fatalf("new_session_created in the outgoing queue: %+v, want kept as sent", queued)
	}
	sess.sendQueueToGateway(ctx, testGatewayId)
	if again := sentMessages(t, chans); len(again) != 0 {
		t.Fatalf("sent %d more messages before waitMsgAcksTimeout", len(again))
	}
}
