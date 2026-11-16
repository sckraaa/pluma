// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
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

package codec

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	rand2 "math/rand"

	"github.com/teamgram/proto/mtproto"
)

// PaddedIntermediateCodec implements the MTProto padded intermediate transport.
// Compared to intermediate, it allows random padding bytes after the payload
// to make traffic analysis more difficult.
type PaddedIntermediateCodec struct {
	*AesCTR128Crypto
	state     int
	packetLen uint32
	// arbitraryLength accepts packets whose length is not a multiple of 4:
	// the length counts the random padding, which the official clients do
	// not round (tdesktop VersionD::finalizePacket adds 0-15 bytes,
	// Telegram/SourceFiles/mtproto/connection_tcp.cpp:195-208; Android adds
	// 0-256 bytes to unencrypted and 0-15 to encrypted packets and reads
	// any length back, TMessagesProj/jni/tgnet/Connection.cpp:216, 485-491).
	// The packet then ends with the padding: an unencrypted message is
	// followed by up to 256 bytes, an encrypted one by up to 15. It is set
	// for connections made with a secret (secret.go), where every client
	// speaks this transport; the gateway keeps the stricter check.
	arbitraryLength bool
}

func newMTProtoPaddedIntermediateCodec(crypto *AesCTR128Crypto) *PaddedIntermediateCodec {
	return &PaddedIntermediateCodec{
		AesCTR128Crypto: crypto,
		state:           WAIT_PACKET_LENGTH,
	}
}

func generatePadding(size int) []byte {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return b
}

// Encode encodes frames upon server responses into TCP stream.
func (c *PaddedIntermediateCodec) Encode(conn CodecWriter, msg interface{}) ([]byte, error) {
	rawMsg, ok := msg.(*mtproto.MTPRawMessage)
	if !ok {
		err := fmt.Errorf("msg type error, only MTPRawMessage, msg: %T", msg)
		return nil, err
	}

	// The length counts the padding: the clients read that many bytes as
	// the packet (tdesktop VersionD::readPacket, connection_tcp.cpp:210-229;
	// Android Connection::onReceivedData). A transport error or quick ack
	// (4 bytes) goes without padding: tdesktop takes a packet of fewer than
	// 12 bytes for one (TcpConnection::parsePacket, connection_tcp.cpp:394-410),
	// Android one of at most 56 (ConnectionsManager.cpp:834).
	paddingLen := 0
	if len(rawMsg.Payload) > 4 {
		paddingLen = rand2.Int() % 16 //nolint:gosec // the length of transport padding, not a secret
	}
	buf := make([]byte, 4+len(rawMsg.Payload)+paddingLen)
	binary.LittleEndian.PutUint32(buf, uint32(len(rawMsg.Payload)+paddingLen))
	copy(buf[4:], rawMsg.Payload)
	if paddingLen > 0 {
		_, _ = rand.Read(buf[4+len(rawMsg.Payload):])
	}

	return c.Encrypt(buf), nil
}

// EncodeQuickAck encodes the Quick ACK token for the padded intermediate transport.
func (c *PaddedIntermediateCodec) EncodeQuickAck(token uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], token)
	return c.Encrypt(buf[:])
}

// Decode decodes frames from TCP stream via specific implementation.
func (c *PaddedIntermediateCodec) Decode(conn CodecReader) (bool, []byte, error) {
	var (
		buf     []byte
		n       int
		in      innerBuffer
		err     error
		needAck bool
	)

	in, _ = conn.Peek(-1)

	if c.state == WAIT_PACKET_LENGTH {
		if buf, err = in.readN(4); err != nil {
			return false, nil, ErrUnexpectedEOF
		}
		_, _ = conn.Discard(4)
		buf = c.Decrypt(buf)
		c.packetLen = binary.LittleEndian.Uint32(buf)
		c.state = WAIT_PACKET
	}

	needAck = c.packetLen>>31 == 1
	n = int(c.packetLen & 0x7fffffff)
	if n <= 0 || (!c.arbitraryLength && n%4 != 0) {
		return false, nil, ErrProtoBadLength
	}
	if n > MAX_MTPRORO_FRAME_SIZE {
		return false, nil, fmt.Errorf("%w: too large data(%d)", ErrProtoBadLength, n)
	}

	if buf, err = in.readN(n); err != nil {
		return false, nil, ErrUnexpectedEOF
	}
	buf = c.Decrypt(buf)
	_, _ = conn.Discard(n)
	c.state = WAIT_PACKET_LENGTH

	return needAck, buf, nil
}
