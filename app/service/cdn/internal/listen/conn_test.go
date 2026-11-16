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

package listen

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// plainCodec writes payloads as they are.
type plainCodec struct{}

func (plainCodec) Encode(_ codec.CodecWriter, msg interface{}) ([]byte, error) {
	return msg.(*mtproto.MTPRawMessage).Payload, nil
}

func (plainCodec) Decode(codec.CodecReader) (bool, []byte, error) { return false, nil, nil }

func (plainCodec) EncodeQuickAck(uint32) []byte { return nil }

// TestSendWaitsForWrite: Send returns only once the client has taken the
// packet, so that a client that stops reading cannot pile up answers on the
// node beyond its requests in flight.
func TestSendWaitsForWrite(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: Config{Egress: func(int) {}}}
	c := &conn{s: s, nc: server, ctx: ctx, cancel: cancel, out: make(chan outItem, outQueue), codec: plainCodec{}}
	s.wg.Add(1)
	go c.writeLoop()
	defer func() {
		c.close()
		s.wg.Wait()
	}()

	sent := make(chan error, 1)
	go func() { sent <- c.Send([]byte("answer")) }()
	select {
	case err := <-sent:
		t.Fatalf("Send returned (%v) before the client read anything", err)
	case <-time.After(200 * time.Millisecond):
	}

	got := make([]byte, len("answer"))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after the packet was read")
	}
	if string(got) != "answer" {
		t.Fatalf("written %q", got)
	}

	// A connection that closes releases a sender waiting on it.
	go func() { sent <- c.Send([]byte("lost")) }()
	time.Sleep(50 * time.Millisecond)
	c.close()
	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("Send on a closed connection reported no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after the connection closed")
	}
}
