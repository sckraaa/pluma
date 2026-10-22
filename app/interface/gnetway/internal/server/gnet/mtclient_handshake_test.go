package gnet

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"github.com/panjf2000/gnet/v2"
)

// harnessTransport carries pkg/mtclient packets over a harness connection:
// each packet is one transport frame, as the TCP codecs hand them over.
type harnessTransport struct {
	h    *testHarness
	c    *fakeConn
	read int
}

func (t *harnessTransport) WritePacket(p []byte) error {
	if a := t.h.sendFrame(t.c, p); a != gnet.None {
		return fmt.Errorf("gateway action %v", a)
	}
	return nil
}

func (t *harnessTransport) ReadPacket() ([]byte, error) {
	t.h.waitWrites(t.c, t.read+1)
	var out []byte
	t.h.do(func() { out = t.c.out[t.read] })
	t.read++
	return out, nil
}

func (t *harnessTransport) Close() error { return nil }

// TestMTClientKeyExchange runs the key exchange of pkg/mtclient (the client
// of the end-to-end and conformance tests) against the gateway: both sides
// must derive the same key, of the requested type.
func TestMTClientKeyExchange(t *testing.T) {
	for _, temp := range []bool{false, true} {
		t.Run(fmt.Sprintf("temp=%v", temp), func(t *testing.T) {
			h := newTestHarness(t)
			c := h.newConn(1, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			opts := mtclient.KeyOptions{}
			if temp {
				opts = mtclient.KeyOptions{Temp: true, ExpiresIn: 3600}
			}
			key, err := mtclient.CreateAuthKey(ctx, &harnessTransport{h: h, c: c}, &h.key.PublicKey, opts)
			if err != nil {
				t.Fatal(err)
			}

			h.do(func() {
				if len(h.saved) != 1 {
					t.Fatalf("saved %d keys", len(h.saved))
				}
				k := h.saved[0]
				if k.AuthKeyId != key.ID || !bytes.Equal(k.AuthKey, key.Key) {
					t.Fatal("the server saved another key than the client computed")
				}
				wantType := int32(mtproto.AuthKeyTypePerm)
				if temp {
					wantType = mtproto.AuthKeyTypeTemp
				}
				if k.AuthKeyType != wantType || key.Temp != temp {
					t.Fatalf("key type %d (client temp %v), want %d", k.AuthKeyType, key.Temp, wantType)
				}
			})
			if key.ServerSalt == 0 {
				t.Error("no initial server salt")
			}
			if temp && key.ExpiresAt <= time.Now().Unix() {
				t.Errorf("temporary key expires at %d", key.ExpiresAt)
			}
		})
	}
}
