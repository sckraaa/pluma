/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2024-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: Benqi (wubenqi@gmail.com)
 */

// ConstructorList
// RequestList

package idgen

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

//////////////////////////////////////////////////////////////////////////////////////////

var _ *wrapperspb.Int32Value
var _ *mtproto.Bool
var _ fmt.Stringer

var clazzIdRegisters2 = map[int32]func() mtproto.TLObject{
	// Constructor

	// Method
	-1099886560: func() mtproto.TLObject { // 0xbe711020
		return &TLIdgenNextId{
			Constructor: -1099886560,
		}
	},
	1204121518: func() mtproto.TLObject { // 0x47c56fae
		return &TLIdgenNextIds{
			Constructor: 1204121518,
		}
	},
}

func NewTLObjectByClassID(classId int32) mtproto.TLObject {
	f, ok := clazzIdRegisters2[classId]
	if !ok {
		return nil
	}
	return f()
}

func CheckClassID(classId int32) (ok bool) {
	_, ok = clazzIdRegisters2[classId]
	return
}

//----------------------------------------------------------------------------------------------------------------
// TLIdgenNextId
///////////////////////////////////////////////////////////////////////////////

func (m *TLIdgenNextId) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xbe711020:
		x.UInt(0xbe711020)

		// no flags

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLIdgenNextId) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLIdgenNextId) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xbe711020:

		// not has flags

		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLIdgenNextIds
///////////////////////////////////////////////////////////////////////////////

func (m *TLIdgenNextIds) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x47c56fae:
		x.UInt(0x47c56fae)

		// no flags

		x.Int(m.GetNum())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLIdgenNextIds) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLIdgenNextIds) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x47c56fae:

		// not has flags

		m.Num = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// Vector_Long
// /////////////////////////////////////////////////////////////////////////////
func (m *Vector_Long) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.VectorLong(m.Datas)

	return nil
}

func (m *Vector_Long) Decode(dBuf *mtproto.DecodeBuf) error {
	m.Datas = dBuf.VectorLong()

	return dBuf.GetError()
}

func (m *Vector_Long) CalcByteSize(layer int32) int {
	return 0
}
