/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 *
 * The TL codec of the service's schema (channel.tl.proto), in the layout of
 * the codecs mtprotoc gives the other services.
 */

// ConstructorList
// RequestList

package channel

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"
)

//////////////////////////////////////////////////////////////////////////////////////////

var _ *mtproto.Bool
var _ fmt.Stringer

var clazzIdRegisters2 = map[int32]func() mtproto.TLObject{
	// Constructor
	-684293246: func() mtproto.TLObject { // 0xd7368382
		o := MakeTLImmutableChannel(nil)
		o.Data2.Constructor = -684293246
		return o
	},
	675582471: func() mtproto.TLObject { // 0x28449207
		o := MakeTLImmutableChannelParticipant(nil)
		o.Data2.Constructor = 675582471
		return o
	},
	-662027966: func() mtproto.TLObject { // 0xd88a4142
		o := MakeTLMutableChannel(nil)
		o.Data2.Constructor = -662027966
		return o
	},

	// Method
	-437245607: func() mtproto.TLObject { // 0xe5f02959
		return &TLChannelGetMutableChannel{
			Constructor: -437245607,
		}
	},
	-1986537988: func() mtproto.TLObject { // 0x8997d5fc
		return &TLChannelGetChannelListByIdList{
			Constructor: -1986537988,
		}
	},
	-641600475: func() mtproto.TLObject { // 0xd9c1f425 pluma: task 038
		return &TLChannelCreateChannel{
			Constructor: -641600475,
		}
	},
	-1725659745: func() mtproto.TLObject { // 0x9924859f pluma: task 038
		return &TLChannelGetChannelDialogs{
			Constructor: -1725659745,
		}
	},
	687937944: func() mtproto.TLObject { // 0x29011998 pluma: task 038
		return &TLChannelGetTopMessages{
			Constructor: 687937944,
		}
	},
	771323905: func() mtproto.TLObject { // 0x2df97801 pluma: task 039
		return &TLChannelGetMemberIdList{
			Constructor: 771323905,
		}
	},
	653475021: func() mtproto.TLObject { // 0x26f33ccd pluma: task 039
		return &TLChannelGetHistory{
			Constructor: 653475021,
		}
	},
	-1395419135: func() mtproto.TLObject { // 0xacd39801 pluma: task 039
		return &TLChannelGetMessages{
			Constructor: -1395419135,
		}
	},
	2140231388: func() mtproto.TLObject { // 0x7f9156dc pluma: task 040
		return &TLChannelGetChannelDifference{
			Constructor: 2140231388,
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

// vectorTooLong reports a vector length the rest of dBuf cannot hold: every
// element takes at least its 4-byte constructor.
func vectorTooLong(dBuf *mtproto.DecodeBuf, n int32) bool {
	return n < 0 || int(n) > (dBuf.GetSize()-dBuf.GetOffset())/4
}

//----------------------------------------------------------------------------------------------------------------

///////////////////////////////////////////////////////////////////////////////
// ImmutableChannel <--
//  + TL_ImmutableChannel
//

func (m *ImmutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	predicateName := m.PredicateName
	if predicateName == "" {
		if n, ok := clazzIdNameRegisters2[int32(m.Constructor)]; ok {
			predicateName = n
		}
	}

	switch predicateName {
	case Predicate_immutableChannel:
		return m.To_ImmutableChannel().Encode(x, layer)

	default:
		return fmt.Errorf("invalid predicate: %q", predicateName)
	}
}

func (m *ImmutableChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *ImmutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	m.Constructor = TLConstructor(dBuf.Int())
	switch uint32(m.Constructor) {
	case 0xd7368382:
		m2 := MakeTLImmutableChannel(m)
		_ = m2.Decode(dBuf)

	default:
		return fmt.Errorf("invalid constructorId: 0x%x", uint32(m.Constructor))
	}
	return dBuf.GetError()
}

// To_ImmutableChannel
func (m *ImmutableChannel) To_ImmutableChannel() *TLImmutableChannel {
	m.PredicateName = Predicate_immutableChannel
	return &TLImmutableChannel{
		Data2: m,
	}
}

// MakeTLImmutableChannel
func MakeTLImmutableChannel(data2 *ImmutableChannel) *TLImmutableChannel {
	if data2 == nil {
		return &TLImmutableChannel{Data2: &ImmutableChannel{
			PredicateName: Predicate_immutableChannel,
		}}
	} else {
		data2.PredicateName = Predicate_immutableChannel
		return &TLImmutableChannel{Data2: data2}
	}
}

func (m *TLImmutableChannel) To_ImmutableChannel() *ImmutableChannel {
	m.Data2.PredicateName = Predicate_immutableChannel
	return m.Data2
}

func (m *TLImmutableChannel) GetPredicateName() string {
	return Predicate_immutableChannel
}

func (m *TLImmutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	var encodeF = map[uint32]func() error{
		0xd7368382: func() error {
			x.UInt(0xd7368382)

			d := m.Data2

			// set flags
			var getFlags = func() uint32 {
				var flags uint32 = 0

				if d.GetBroadcast() {
					flags |= 1 << 0
				}
				if d.GetMegagroup() {
					flags |= 1 << 1
				}
				if d.GetForum() {
					flags |= 1 << 2
				}
				if d.GetSignatures() {
					flags |= 1 << 3
				}
				if d.GetNoforwards() {
					flags |= 1 << 4
				}
				if d.GetJoinToSend() {
					flags |= 1 << 5
				}
				if d.GetJoinRequest() {
					flags |= 1 << 6
				}
				if d.GetParticipantsHidden() {
					flags |= 1 << 7
				}
				if d.GetHiddenPrehistory() {
					flags |= 1 << 8
				}
				if d.GetDeleted() {
					flags |= 1 << 9
				}

				return flags
			}

			// set flags
			var flags = getFlags()
			x.UInt(flags)
			x.Long(d.GetId())
			x.Long(d.GetAccessHash())
			x.Long(d.GetCreatorUserId())
			x.String(d.GetTitle())
			x.String(d.GetAbout())
			photo := d.GetPhoto()
			if photo == nil {
				photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
			}
			photo.Encode(x, layer)
			x.String(d.GetUsername())
			x.Int(d.GetParticipantsCount())
			x.Int(d.GetAdminsCount())
			x.Int(d.GetKickedCount())
			x.Int(d.GetBannedCount())
			x.Int(d.GetDefaultBannedRights())
			x.Int(d.GetSlowmodeSeconds())
			x.Long(d.GetLinkedChatId())
			x.Long(d.GetMigratedFromChatId())
			x.Int(d.GetMigratedFromMaxId())
			x.Int(d.GetPts())
			x.Int(d.GetTopMessage())
			x.Int(d.GetVersion())
			x.Long(d.GetDate())

			return nil
		},
	}

	clazzId := GetClazzID(Predicate_immutableChannel, int(layer))
	if f, ok := encodeF[uint32(clazzId)]; ok {
		return f()
	}
	return fmt.Errorf("not found clazzId by (%s, %d)", Predicate_immutableChannel, layer)
}

func (m *TLImmutableChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLImmutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	var decodeF = map[uint32]func() error{
		0xd7368382: func() error {
			d := m.Data2

			var flags = dBuf.UInt()
			d.Broadcast = (flags & (1 << 0)) != 0
			d.Megagroup = (flags & (1 << 1)) != 0
			d.Forum = (flags & (1 << 2)) != 0
			d.Signatures = (flags & (1 << 3)) != 0
			d.Noforwards = (flags & (1 << 4)) != 0
			d.JoinToSend = (flags & (1 << 5)) != 0
			d.JoinRequest = (flags & (1 << 6)) != 0
			d.ParticipantsHidden = (flags & (1 << 7)) != 0
			d.HiddenPrehistory = (flags & (1 << 8)) != 0
			d.Deleted = (flags & (1 << 9)) != 0
			d.Id = dBuf.Long()
			d.AccessHash = dBuf.Long()
			d.CreatorUserId = dBuf.Long()
			d.Title = dBuf.String()
			d.About = dBuf.String()

			m5 := &mtproto.Photo{}
			_ = m5.Decode(dBuf)
			d.Photo = m5

			d.Username = dBuf.String()
			d.ParticipantsCount = dBuf.Int()
			d.AdminsCount = dBuf.Int()
			d.KickedCount = dBuf.Int()
			d.BannedCount = dBuf.Int()
			d.DefaultBannedRights = dBuf.Int()
			d.SlowmodeSeconds = dBuf.Int()
			d.LinkedChatId = dBuf.Long()
			d.MigratedFromChatId = dBuf.Long()
			d.MigratedFromMaxId = dBuf.Int()
			d.Pts = dBuf.Int()
			d.TopMessage = dBuf.Int()
			d.Version = dBuf.Int()
			d.Date = dBuf.Long()
			return dBuf.GetError()
		},
	}

	if f, ok := decodeF[uint32(m.Data2.Constructor)]; ok {
		return f()
	}
	return fmt.Errorf("invalid constructor: %x", uint32(m.Data2.Constructor))
}

///////////////////////////////////////////////////////////////////////////////
// ImmutableChannelParticipant <--
//  + TL_ImmutableChannelParticipant
//

func (m *ImmutableChannelParticipant) Encode(x *mtproto.EncodeBuf, layer int32) error {
	predicateName := m.PredicateName
	if predicateName == "" {
		if n, ok := clazzIdNameRegisters2[int32(m.Constructor)]; ok {
			predicateName = n
		}
	}

	switch predicateName {
	case Predicate_immutableChannelParticipant:
		return m.To_ImmutableChannelParticipant().Encode(x, layer)

	default:
		return fmt.Errorf("invalid predicate: %q", predicateName)
	}
}

func (m *ImmutableChannelParticipant) CalcByteSize(layer int32) int {
	return 0
}

func (m *ImmutableChannelParticipant) Decode(dBuf *mtproto.DecodeBuf) error {
	m.Constructor = TLConstructor(dBuf.Int())
	switch uint32(m.Constructor) {
	case 0x28449207:
		m2 := MakeTLImmutableChannelParticipant(m)
		_ = m2.Decode(dBuf)

	default:
		return fmt.Errorf("invalid constructorId: 0x%x", uint32(m.Constructor))
	}
	return dBuf.GetError()
}

// To_ImmutableChannelParticipant
func (m *ImmutableChannelParticipant) To_ImmutableChannelParticipant() *TLImmutableChannelParticipant {
	m.PredicateName = Predicate_immutableChannelParticipant
	return &TLImmutableChannelParticipant{
		Data2: m,
	}
}

// MakeTLImmutableChannelParticipant
func MakeTLImmutableChannelParticipant(data2 *ImmutableChannelParticipant) *TLImmutableChannelParticipant {
	if data2 == nil {
		return &TLImmutableChannelParticipant{Data2: &ImmutableChannelParticipant{
			PredicateName: Predicate_immutableChannelParticipant,
		}}
	} else {
		data2.PredicateName = Predicate_immutableChannelParticipant
		return &TLImmutableChannelParticipant{Data2: data2}
	}
}

func (m *TLImmutableChannelParticipant) To_ImmutableChannelParticipant() *ImmutableChannelParticipant {
	m.Data2.PredicateName = Predicate_immutableChannelParticipant
	return m.Data2
}

func (m *TLImmutableChannelParticipant) GetPredicateName() string {
	return Predicate_immutableChannelParticipant
}

func (m *TLImmutableChannelParticipant) Encode(x *mtproto.EncodeBuf, layer int32) error {
	var encodeF = map[uint32]func() error{
		0x28449207: func() error {
			x.UInt(0x28449207)

			d := m.Data2
			x.Long(d.GetChannelId())
			x.Long(d.GetUserId())
			x.Int(d.GetRole())
			x.Int(d.GetState())
			x.Int(d.GetAdminRights())
			x.String(d.GetRank())
			x.Int(d.GetBannedRights())
			x.Int(d.GetUntilDate())
			x.Long(d.GetInviterUserId())
			x.Long(d.GetPromotedBy())
			x.Long(d.GetKickedBy())
			x.Int(d.GetAvailableMinId())
			x.Long(d.GetDate())

			return nil
		},
	}

	clazzId := GetClazzID(Predicate_immutableChannelParticipant, int(layer))
	if f, ok := encodeF[uint32(clazzId)]; ok {
		return f()
	}
	return fmt.Errorf("not found clazzId by (%s, %d)", Predicate_immutableChannelParticipant, layer)
}

func (m *TLImmutableChannelParticipant) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLImmutableChannelParticipant) Decode(dBuf *mtproto.DecodeBuf) error {
	var decodeF = map[uint32]func() error{
		0x28449207: func() error {
			d := m.Data2
			d.ChannelId = dBuf.Long()
			d.UserId = dBuf.Long()
			d.Role = dBuf.Int()
			d.State = dBuf.Int()
			d.AdminRights = dBuf.Int()
			d.Rank = dBuf.String()
			d.BannedRights = dBuf.Int()
			d.UntilDate = dBuf.Int()
			d.InviterUserId = dBuf.Long()
			d.PromotedBy = dBuf.Long()
			d.KickedBy = dBuf.Long()
			d.AvailableMinId = dBuf.Int()
			d.Date = dBuf.Long()
			return dBuf.GetError()
		},
	}

	if f, ok := decodeF[uint32(m.Data2.Constructor)]; ok {
		return f()
	}
	return fmt.Errorf("invalid constructor: %x", uint32(m.Data2.Constructor))
}

///////////////////////////////////////////////////////////////////////////////
// MutableChannel <--
//  + TL_MutableChannel
//

func (m *MutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	predicateName := m.PredicateName
	if predicateName == "" {
		if n, ok := clazzIdNameRegisters2[int32(m.Constructor)]; ok {
			predicateName = n
		}
	}

	switch predicateName {
	case Predicate_mutableChannel:
		return m.To_MutableChannel().Encode(x, layer)

	default:
		return fmt.Errorf("invalid predicate: %q", predicateName)
	}
}

func (m *MutableChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *MutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	m.Constructor = TLConstructor(dBuf.Int())
	switch uint32(m.Constructor) {
	case 0xd88a4142:
		m2 := MakeTLMutableChannel(m)
		_ = m2.Decode(dBuf)

	default:
		return fmt.Errorf("invalid constructorId: 0x%x", uint32(m.Constructor))
	}
	return dBuf.GetError()
}

// To_MutableChannel
func (m *MutableChannel) To_MutableChannel() *TLMutableChannel {
	m.PredicateName = Predicate_mutableChannel
	return &TLMutableChannel{
		Data2: m,
	}
}

// MakeTLMutableChannel
func MakeTLMutableChannel(data2 *MutableChannel) *TLMutableChannel {
	if data2 == nil {
		return &TLMutableChannel{Data2: &MutableChannel{
			PredicateName: Predicate_mutableChannel,
		}}
	} else {
		data2.PredicateName = Predicate_mutableChannel
		return &TLMutableChannel{Data2: data2}
	}
}

func (m *TLMutableChannel) To_MutableChannel() *MutableChannel {
	m.Data2.PredicateName = Predicate_mutableChannel
	return m.Data2
}

func (m *TLMutableChannel) GetPredicateName() string {
	return Predicate_mutableChannel
}

func (m *TLMutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	var encodeF = map[uint32]func() error{
		0xd88a4142: func() error {
			x.UInt(0xd88a4142)

			c := m.Data2.GetChannel()
			if c == nil {
				return fmt.Errorf("%s: no channel", Predicate_mutableChannel)
			}
			if err := c.Encode(x, layer); err != nil {
				return err
			}

			x.Int(int32(mtproto.CRC32_vector))
			list1 := m.Data2.GetParticipants()
			x.Int(int32(len(list1)))
			for _, v := range list1 {
				if err := v.Encode(x, layer); err != nil {
					return err
				}
			}

			return nil
		},
	}

	clazzId := GetClazzID(Predicate_mutableChannel, int(layer))
	if f, ok := encodeF[uint32(clazzId)]; ok {
		return f()
	}
	return fmt.Errorf("not found clazzId by (%s, %d)", Predicate_mutableChannel, layer)
}

func (m *TLMutableChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLMutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	var decodeF = map[uint32]func() error{
		0xd88a4142: func() error {
			m0 := &ImmutableChannel{}
			if err := m0.Decode(dBuf); err != nil {
				return err
			}
			m.Data2.Channel = m0

			c1 := dBuf.Int()
			if c1 != int32(mtproto.CRC32_vector) {
				return fmt.Errorf("invalid CRC32_vector, c%d: %d", 1, c1)
			}
			l1 := dBuf.Int()
			if vectorTooLong(dBuf, l1) {
				return fmt.Errorf("invalid vector length: %d", l1)
			}
			v1 := make([]*ImmutableChannelParticipant, l1)
			for i := int32(0); i < l1; i++ {
				v1[i] = &ImmutableChannelParticipant{}
				if err := v1[i].Decode(dBuf); err != nil {
					return err
				}
			}
			m.Data2.Participants = v1

			return dBuf.GetError()
		},
	}

	if f, ok := decodeF[uint32(m.Data2.Constructor)]; ok {
		return f()
	}
	return fmt.Errorf("invalid constructor: %x", uint32(m.Data2.Constructor))
}

//----------------------------------------------------------------------------------------------------------------
// TLChannelGetMutableChannel
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetMutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xe5f02959:
		x.UInt(0xe5f02959)

		// no flags

		x.Long(m.GetChannelId())

		x.VectorLong(m.GetId())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelGetMutableChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetMutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xe5f02959:

		// not has flags

		m.ChannelId = dBuf.Long()

		m.Id = dBuf.VectorLong()

		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// TLChannelGetChannelListByIdList
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetChannelListByIdList) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x8997d5fc:
		x.UInt(0x8997d5fc)

		// no flags

		x.Long(m.GetSelfId())

		x.VectorLong(m.GetIdList())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelGetChannelListByIdList) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetChannelListByIdList) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x8997d5fc:

		// not has flags

		m.SelfId = dBuf.Long()

		m.IdList = dBuf.VectorLong()

		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// ----------------------------------------------------------------------------------------------------------------
// Vector_MutableChannel
// /////////////////////////////////////////////////////////////////////////////
func (m *Vector_MutableChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		if err := v.Encode(x, layer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Vector_MutableChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]*MutableChannel, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = new(MutableChannel)
		if err := m.Datas[i].Decode(dBuf); err != nil {
			return err
		}
	}

	return dBuf.GetError()
}

func (m *Vector_MutableChannel) CalcByteSize(layer int32) int {
	return 0
}

// TLChannelCreateChannel (pluma: task 038)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelCreateChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xd9c1f425:
		x.UInt(0xd9c1f425)

		// no flags

		x.Long(m.GetCreatorId())
		x.String(m.GetTitle())
		x.String(m.GetAbout())
		m.GetBroadcast().Encode(x, layer)
		m.GetMegagroup().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelCreateChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelCreateChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xd9c1f425:

		// not has flags

		m.CreatorId = dBuf.Long()
		m.Title = dBuf.String()
		m.About = dBuf.String()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Broadcast = m3
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.Megagroup = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetChannelDialogs (pluma: task 038)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetChannelDialogs) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x9924859f:
		x.UInt(0x9924859f)

		// no flags

		x.Long(m.GetSelfId())

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetIdList())))
		for _, v := range m.GetIdList() {
			x.Long(v)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetChannelDialogs) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetChannelDialogs) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x9924859f:

		// not has flags

		m.SelfId = dBuf.Long()
		c1 := dBuf.Int()
		if c1 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 1, c1)
		}
		l1 := dBuf.Int()
		if vectorTooLong(dBuf, l1) {
			return fmt.Errorf("invalid vector length: %d", l1)
		}
		v1 := make([]int64, l1)
		for i := int32(0); i < l1; i++ {
			v1[i] = dBuf.Long()
		}
		m.IdList = v1
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetTopMessages (pluma: task 038)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetTopMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x29011998:
		x.UInt(0x29011998)

		// no flags

		x.Long(m.GetSelfId())

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetIdList())))
		for _, v := range m.GetIdList() {
			x.Long(v)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetTopMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetTopMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x29011998:

		// not has flags

		m.SelfId = dBuf.Long()
		c1 := dBuf.Int()
		if c1 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 1, c1)
		}
		l1 := dBuf.Int()
		if vectorTooLong(dBuf, l1) {
			return fmt.Errorf("invalid vector length: %d", l1)
		}
		v1 := make([]int64, l1)
		for i := int32(0); i < l1; i++ {
			v1[i] = dBuf.Long()
		}
		m.IdList = v1
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// Vector_Dialog (pluma: task 038)
///////////////////////////////////////////////////////////////////////////////

func (m *Vector_Dialog) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		if err := v.Encode(x, layer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Vector_Dialog) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]*mtproto.Dialog, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = new(mtproto.Dialog)
		if err := m.Datas[i].Decode(dBuf); err != nil {
			return err
		}
	}

	return dBuf.GetError()
}

func (m *Vector_Dialog) CalcByteSize(layer int32) int {
	return 0
}

// Vector_Message (pluma: task 038)
///////////////////////////////////////////////////////////////////////////////

func (m *Vector_Message) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		if err := v.Encode(x, layer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Vector_Message) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]*mtproto.Message, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = new(mtproto.Message)
		if err := m.Datas[i].Decode(dBuf); err != nil {
			return err
		}
	}

	return dBuf.GetError()
}

func (m *Vector_Message) CalcByteSize(layer int32) int {
	return 0
}

// TLChannelGetMemberIdList (pluma: task 039)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetMemberIdList) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x2df97801:
		x.UInt(0x2df97801)

		// no flags

		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetMemberIdList) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetMemberIdList) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x2df97801:

		// not has flags

		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetHistory (pluma: task 039)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetHistory) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x26f33ccd:
		x.UInt(0x26f33ccd)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetOffsetId())
		x.Int(m.GetOffsetDate())
		x.Int(m.GetAddOffset())
		x.Int(m.GetLimit())
		x.Int(m.GetMaxId())
		x.Int(m.GetMinId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetHistory) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetHistory) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x26f33ccd:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.OffsetId = dBuf.Int()
		m.OffsetDate = dBuf.Int()
		m.AddOffset = dBuf.Int()
		m.Limit = dBuf.Int()
		m.MaxId = dBuf.Int()
		m.MinId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetMessages (pluma: task 039)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xacd39801:
		x.UInt(0xacd39801)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetId())))
		for _, v := range m.GetId() {
			x.Int(v)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xacd39801:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		c2 := dBuf.Int()
		if c2 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 2, c2)
		}
		l2 := dBuf.Int()
		if vectorTooLong(dBuf, l2) {
			return fmt.Errorf("invalid vector length: %d", l2)
		}
		v2 := make([]int32, l2)
		for i := int32(0); i < l2; i++ {
			v2[i] = dBuf.Int()
		}
		m.Id = v2
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// Vector_Long (pluma: task 039)
///////////////////////////////////////////////////////////////////////////////

func (m *Vector_Long) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		x.Long(v)
	}

	return nil
}

func (m *Vector_Long) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]int64, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = dBuf.Long()
	}

	return dBuf.GetError()
}

func (m *Vector_Long) CalcByteSize(layer int32) int {
	return 0
}

// TLChannelGetChannelDifference (pluma: task 040)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetChannelDifference) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x7f9156dc:
		x.UInt(0x7f9156dc)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetPts())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetChannelDifference) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetChannelDifference) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x7f9156dc:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Pts = dBuf.Int()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}
