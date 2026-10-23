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

package gnet

import (
	"time"

	"github.com/teamgram/marmota/pkg/sync2"
	"github.com/teamgram/proto/mtproto"
)

var msgIdSeq = sync2.NewAtomicInt64(0)

func nextMessageId(isRpc bool) int64 {
	unixNano := time.Now().UnixNano()
	ts := unixNano / 1e9
	ms := (unixNano % 1e9) / 1e6
	sid := msgIdSeq.Add(1) & 0x1ffff
	msgIdSeq.CompareAndSwap(0x1ffff, 0)
	last := 1
	if !isRpc {
		last = 3
	}
	msgId := ts<<32 | int64(ms)<<21 | sid<<3 | int64(last)
	return msgId
}

func serializeToBuffer(x *mtproto.EncodeBuf, msgId int64, obj mtproto.TLObject) error {
	//obj.Encode(x, 0)
	// x := mtproto.NewEncodeBuf(8 + 4 + len(oBuf))
	x.Long(0)
	x.Long(msgId)
	offset := x.GetOffset()
	x.Int(0)
	err := obj.Encode(x, 0)
	if err != nil {
		return err
	}
	//x.Bytes(oBuf)
	x.IntOffset(offset, int32(x.GetOffset()-offset-4))
	return nil
}

func serializeToBuffer2(salt, sessionId int64, msg2 *mtproto.TLMessage2) []byte {
	x := mtproto.GetEncodeBuf()
	defer mtproto.PutEncodeBuf(x)

	x.Long(salt)
	x.Long(sessionId)
	msg2.Encode(x, 0)

	buf := append([]byte(nil), x.GetBuf()...)
	return buf
}

const (
	kMsgContainerBufLen = 8
)

var (
	kMsgContainerBuf = func() []byte {
		x := mtproto.GetEncodeBuf()
		x.Int(int32(mtproto.CRC32_msg_container))
		x.Int(0)
		buf := append([]byte(nil), x.GetBuf()...)
		mtproto.PutEncodeBuf(x)
		return buf
	}()
)

//func serializeToBuffer2(salt, sessionId int64, seqNo int32) []byte {
//	x := mtproto.NewEncodeBuf(512)
//
//	x.Long(salt)
//	x.Long(sessionId)
//	x.Long(mtproto.GenerateMessageId())
//	x.Int(seqNo)
//	x.Int(kMsgContainerBufLen)
//	x.Bytes(kMsgContainerBuf)
//
//	return x.GetBuf()
//}
