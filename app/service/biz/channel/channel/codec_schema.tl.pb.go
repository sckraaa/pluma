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
	1726695119: func() mtproto.TLObject { // 0x66eb46cf
		o := MakeTLImmutableChannel(nil)
		o.Data2.Constructor = 1726695119
		return o
	},
	885437367: func() mtproto.TLObject { // 0x34c6b3b7
		o := MakeTLImmutableChannelParticipant(nil)
		o.Data2.Constructor = 885437367
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
	1532314294: func() mtproto.TLObject { // 0x5b5542b6 pluma: task 038
		return &TLChannelCreateChannel{
			Constructor: 1532314294,
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
	634004096: func() mtproto.TLObject { // 0x25ca2280 pluma: task 041
		return &TLChannelJoinChannel{
			Constructor: 634004096,
		}
	},
	1331881771: func() mtproto.TLObject { // 0x4f62e72b pluma: task 041
		return &TLChannelLeaveChannel{
			Constructor: 1331881771,
		}
	},
	-1311035397: func() mtproto.TLObject { // 0xb1db2ffb pluma: task 041
		return &TLChannelCheckChatInvite{
			Constructor: -1311035397,
		}
	},
	-212979149: func() mtproto.TLObject { // 0xf34e3233 pluma: task 041
		return &TLChannelGetParticipants{
			Constructor: -212979149,
		}
	},
	-585404897: func() mtproto.TLObject { // 0xdd1b6e1f pluma: task 043
		return &TLChannelReadHistory{
			Constructor: -585404897,
		}
	},
	1972359002: func() mtproto.TLObject { // 0x758fcf5a pluma: task 043
		return &TLChannelDeleteMessages{
			Constructor: 1972359002,
		}
	},
	293053083: func() mtproto.TLObject { // 0x1177a29b pluma: task 041
		return &TLChannelInviteToChannel{
			Constructor: 293053083,
		}
	},
	325646357: func() mtproto.TLObject { // 0x1368f815 pluma: task 044
		return &TLChannelEditTitle{
			Constructor: 325646357,
		}
	},
	-564196321: func() mtproto.TLObject { // 0xde5f0c1f pluma: task 044
		return &TLChannelEditAbout{
			Constructor: -564196321,
		}
	},
	986177068: func() mtproto.TLObject { // 0x3ac7de2c pluma: task 044
		return &TLChannelCheckUsername{
			Constructor: 986177068,
		}
	},
	418987336: func() mtproto.TLObject { // 0x18f93d48 pluma: task 044
		return &TLChannelUpdateUsername{
			Constructor: 418987336,
		}
	},
	-2083237595: func() mtproto.TLObject { // 0x83d45125 pluma: task 044
		return &TLChannelGetAdminedPublicChannels{
			Constructor: -2083237595,
		}
	},
	-979916526: func() mtproto.TLObject { // 0xc597a912 pluma: task 055
		return &TLChannelDeleteChannel{
			Constructor: -979916526,
		}
	},
	1139340211: func() mtproto.TLObject { // 0x43e8f3b3 pluma: task 078
		return &TLChannelGetAdminLog{
			Constructor: 1139340211,
		}
	},
	210006620: func() mtproto.TLObject { // 0xc84725c pluma: task 078
		return &TLChannelGetReadParticipants{
			Constructor: 210006620,
		}
	},
	-86486467: func() mtproto.TLObject { // 0xfad8523d pluma: task 078
		return &TLChannelEditParticipantRank{
			Constructor: -86486467,
		}
	},
	-63031132: func() mtproto.TLObject { // 0xfc3e38a4 pluma: task 072
		return &TLChannelSetBlocked{
			Constructor: -63031132,
		}
	},
	1668730682: func() mtproto.TLObject { // 0x6376cf3a pluma: task 046
		return &TLChannelIncrementMessageViews{
			Constructor: 1668730682,
		}
	},
	-1892777199: func() mtproto.TLObject { // 0x8f2e8311 pluma: task 046
		return &TLChannelAddMessageForwards{
			Constructor: -1892777199,
		}
	},
	-1441587995: func() mtproto.TLObject { // 0xaa131ce5 pluma: task 043
		return &TLChannelDeleteHistory{
			Constructor: -1441587995,
		}
	},
	-1046826439: func() mtproto.TLObject { // 0xc19ab239 pluma: task 043
		return &TLChannelDeleteParticipantHistory{
			Constructor: -1046826439,
		}
	},
	1137012701: func() mtproto.TLObject { // 0x43c56fdd pluma: task 045
		return &TLChannelToggleSignatures{
			Constructor: 1137012701,
		}
	},
	-1991690360: func() mtproto.TLObject { // 0x89493788 pluma: task 045
		return &TLChannelToggleSlowMode{
			Constructor: -1991690360,
		}
	},
	295422623: func() mtproto.TLObject { // 0x119bca9f pluma: task 041
		return &TLChannelToggleJoinRequest{
			Constructor: 295422623,
		}
	},
	1117566284: func() mtproto.TLObject { // 0x429cb54c pluma: task 041
		return &TLChannelExportChatInvite{
			Constructor: 1117566284,
		}
	},
	1706919575: func() mtproto.TLObject { // 0x65bd8697 pluma: task 041
		return &TLChannelGetExportedChatInvite{
			Constructor: 1706919575,
		}
	},
	-1412847419: func() mtproto.TLObject { // 0xabc9a8c5 pluma: task 041
		return &TLChannelGetExportedChatInvites{
			Constructor: -1412847419,
		}
	},
	1438538611: func() mtproto.TLObject { // 0x55be5b73 pluma: task 041
		return &TLChannelEditExportedChatInvite{
			Constructor: 1438538611,
		}
	},
	714654860: func() mtproto.TLObject { // 0x2a98c48c pluma: task 041
		return &TLChannelDeleteExportedChatInvite{
			Constructor: 714654860,
		}
	},
	-692237588: func() mtproto.TLObject { // 0xd6bd4aec pluma: task 041
		return &TLChannelDeleteRevokedExportedChatInvites{
			Constructor: -692237588,
		}
	},
	1965843750: func() mtproto.TLObject { // 0x752c6526 pluma: task 041
		return &TLChannelGetAdminsWithInvites{
			Constructor: 1965843750,
		}
	},
	1917986431: func() mtproto.TLObject { // 0x7252267f pluma: task 041
		return &TLChannelGetChatInviteImporters{
			Constructor: 1917986431,
		}
	},
	-1624124263: func() mtproto.TLObject { // 0x9f31d499 pluma: task 041
		return &TLChannelHideChatJoinRequests{
			Constructor: -1624124263,
		}
	},
	2040559942: func() mtproto.TLObject { // 0x79a07946 pluma: task 041
		return &TLChannelGetPendingJoinRequests{
			Constructor: 2040559942,
		}
	},
	-2063367371: func() mtproto.TLObject { // 0x85038335 pluma: task 041
		return &TLChannelGetLeftChannels{
			Constructor: -2063367371,
		}
	},
	1073186003: func() mtproto.TLObject { // 0x3ff784d3 pluma: task 060
		return &TLChannelToggleForum{
			Constructor: 1073186003,
		}
	},
	-1875771675: func() mtproto.TLObject { // 0x9031fee5 pluma: task 060
		return &TLChannelToggleViewForumAsMessages{
			Constructor: -1875771675,
		}
	},
	-610599565: func() mtproto.TLObject { // 0xdb9afd73 pluma: task 060
		return &TLChannelCreateForumTopic{
			Constructor: -610599565,
		}
	},
	1853047093: func() mtproto.TLObject { // 0x6e734135 pluma: task 060
		return &TLChannelEditForumTopic{
			Constructor: 1853047093,
		}
	},
	-110001806: func() mtproto.TLObject { // 0xf9718172 pluma: task 060
		return &TLChannelUpdatePinnedForumTopic{
			Constructor: -110001806,
		}
	},
	-1269591301: func() mtproto.TLObject { // 0xb45392fb pluma: task 060
		return &TLChannelReorderPinnedForumTopics{
			Constructor: -1269591301,
		}
	},
	62210273: func() mtproto.TLObject { // 0x03b540e1 pluma: task 060
		return &TLChannelDeleteTopicHistory{
			Constructor: 62210273,
		}
	},
	-553446363: func() mtproto.TLObject { // 0xdf031425 pluma: task 060
		return &TLChannelGetForumTopics{
			Constructor: -553446363,
		}
	},
	1892057379: func() mtproto.TLObject { // 0x70c68123 pluma: task 060
		return &TLChannelGetForumTopicsByID{
			Constructor: 1892057379,
		}
	},
	-2126242873: func() mtproto.TLObject { // 0x81441bc7 pluma: task 060
		return &TLChannelSearchTopic{
			Constructor: -2126242873,
		}
	},
	622787338: func() mtproto.TLObject { // 0x251efb0a pluma: task 066
		return &TLChannelSearchCount{
			Constructor: 622787338,
		}
	},
	-16614681: func() mtproto.TLObject { // 0xff027ae7 pluma: task 066
		return &TLChannelSearchCalendar{
			Constructor: -16614681,
		}
	},
	-1144250748: func() mtproto.TLObject { // 0xbbcc1e84 pluma: task 066
		return &TLChannelSearchPositions{
			Constructor: -1144250748,
		}
	},
	-1564360787: func() mtproto.TLObject { // 0xa2c1bfad pluma: task 066
		return &TLChannelSearchAcross{
			Constructor: -1564360787,
		}
	},
	-701570663: func() mtproto.TLObject { // 0xd62ee199 pluma: task 060
		return &TLChannelReadTopicMentions{
			Constructor: -701570663,
		}
	},
	-1405087645: func() mtproto.TLObject { // 0xac401063 pluma: task 060
		return &TLChannelUnpinAllTopicMessages{
			Constructor: -1405087645,
		}
	},
	-1811095406: func() mtproto.TLObject { // 0x940ce092 pluma: task 063
		return &TLChannelSetHistoryTTL{
			Constructor: -1811095406,
		}
	},
	-1144851290: func() mtproto.TLObject { // 0xbbc2f4a6 pluma: task 065
		return &TLChannelSetChatWallpaper{
			Constructor: -1144851290,
		}
	},
	-653877279: func() mtproto.TLObject { // 0xd9069fe1 pluma: task 065
		return &TLChannelSetChatTheme{
			Constructor: -653877279,
		}
	},
	-922563101: func() mtproto.TLObject { // 0xc902cde3 pluma: task 044
		return &TLChannelToggleSetting{
			Constructor: -922563101,
		}
	},
	994913470: func() mtproto.TLObject { // 0x3b4d2cbe pluma: task 044
		return &TLChannelSetAvailableReactions{
			Constructor: 994913470,
		}
	},
	407453767: func() mtproto.TLObject { // 0x18494047 pluma: task 048
		return &TLChannelSetDiscussionGroup{
			Constructor: 407453767,
		}
	},
	-1820701585: func() mtproto.TLObject { // 0x937a4c6f pluma: task 048
		return &TLChannelGetGroupsForDiscussion{
			Constructor: -1820701585,
		}
	},
	941034930: func() mtproto.TLObject { // 0x38170db2 pluma: task 048
		return &TLChannelGetDiscussionMessage{
			Constructor: 941034930,
		}
	},
	973790184: func() mtproto.TLObject { // 0x3a0adbe8 pluma: task 048
		return &TLChannelGetReplies{
			Constructor: 973790184,
		}
	},
	-325315711: func() mtproto.TLObject { // 0xec9c1381 pluma: task 048
		return &TLChannelReadDiscussion{
			Constructor: -325315711,
		}
	},
	-1984581655: func() mtproto.TLObject { // 0x89b5afe9 pluma: task 044
		return &TLChannelEditPhoto{
			Constructor: -1984581655,
		}
	},
	-521821973: func() mtproto.TLObject { // 0xe0e5a0eb pluma: task 043
		return &TLChannelReadMentions{
			Constructor: -521821973,
		}
	},
	1865304106: func() mtproto.TLObject { // 0x6f2e482a pluma: task 043
		return &TLChannelGetUnreadMentions{
			Constructor: 1865304106,
		}
	},
	2055458774: func() mtproto.TLObject { // 0x7a83cfd6 pluma: task 043
		return &TLChannelReadMessageContents{
			Constructor: 2055458774,
		}
	},
	471620827: func() mtproto.TLObject { // 0x1c1c5cdb pluma: task 043
		return &TLChannelSearch{
			Constructor: 471620827,
		}
	},
	860909233: func() mtproto.TLObject { // 0x33506eb1 pluma: task 042
		return &TLChannelEditCreator{
			Constructor: 860909233,
		}
	},
	1274612374: func() mtproto.TLObject { // 0x4bf90a96 pluma: task 042
		return &TLChannelEditAdmin{
			Constructor: 1274612374,
		}
	},
	-1457424068: func() mtproto.TLObject { // 0xa921793c pluma: task 042
		return &TLChannelEditBanned{
			Constructor: -1457424068,
		}
	},
	1263700059: func() mtproto.TLObject { // 0x4b52885b pluma: task 042
		return &TLChannelEditDefaultBannedRights{
			Constructor: 1263700059,
		}
	},
	1530267177: func() mtproto.TLObject { // 0x5b360629 pluma: task 067
		return &TLChannelExpireMessages{
			Constructor: 1530267177,
		}
	},
	-793086366: func() mtproto.TLObject { // 0xd0ba7662 pluma: task 067
		return &TLChannelGetUnreadReactions{
			Constructor: -793086366,
		}
	},
	2058932457: func() mtproto.TLObject { // 0x7ab8d0e9 pluma: task 067
		return &TLChannelReadReactions{
			Constructor: 2058932457,
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
	case 0x66eb46cf:
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
		0x66eb46cf: func() error {
			x.UInt(0x66eb46cf)

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
				if d.GetForumTabs() {
					flags |= 1 << 15
				}
				if d.GetBlocked() {
					flags |= 1 << 16
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
				if d.GetSignatureProfiles() {
					flags |= 1 << 10
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
			x.Int(d.GetAvailableReactionsType())
			x.VectorString(d.GetAvailableReactions())

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
		0x66eb46cf: func() error {
			d := m.Data2

			var flags = dBuf.UInt()
			d.Broadcast = (flags & (1 << 0)) != 0
			d.Megagroup = (flags & (1 << 1)) != 0
			d.Forum = (flags & (1 << 2)) != 0
			d.ForumTabs = (flags & (1 << 15)) != 0
			d.Blocked = (flags & (1 << 16)) != 0
			d.Signatures = (flags & (1 << 3)) != 0
			d.Noforwards = (flags & (1 << 4)) != 0
			d.JoinToSend = (flags & (1 << 5)) != 0
			d.JoinRequest = (flags & (1 << 6)) != 0
			d.ParticipantsHidden = (flags & (1 << 7)) != 0
			d.HiddenPrehistory = (flags & (1 << 8)) != 0
			d.Deleted = (flags & (1 << 9)) != 0
			d.SignatureProfiles = (flags & (1 << 10)) != 0
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
			d.AvailableReactionsType = dBuf.Int()
			d.AvailableReactions = dBuf.VectorString()
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
	case 0x34c6b3b7:
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
		0x34c6b3b7: func() error {
			x.UInt(0x34c6b3b7)

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
			x.Long(d.GetLastSendDate())

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
		0x34c6b3b7: func() error {
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
			d.LastSendDate = dBuf.Long()
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
	case 0x5b5542b6:
		x.UInt(0x5b5542b6)

		// no flags

		x.Long(m.GetCreatorId())
		x.String(m.GetTitle())
		x.String(m.GetAbout())
		m.GetBroadcast().Encode(x, layer)
		m.GetMegagroup().Encode(x, layer)
		m.GetForum().Encode(x, layer)

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
	case 0x5b5542b6:

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
		m5 := &mtproto.Bool{}
		m5.Decode(dBuf)
		m.Forum = m5
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

// TLChannelJoinChannel (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelJoinChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x25ca2280:
		x.UInt(0x25ca2280)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetLink())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelJoinChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelJoinChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x25ca2280:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Link = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelLeaveChannel (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelLeaveChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x4f62e72b:
		x.UInt(0x4f62e72b)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelLeaveChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelLeaveChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x4f62e72b:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelCheckChatInvite (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelCheckChatInvite) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xb1db2ffb:
		x.UInt(0xb1db2ffb)

		// no flags

		x.Long(m.GetSelfId())
		x.String(m.GetLink())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelCheckChatInvite) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelCheckChatInvite) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xb1db2ffb:

		// not has flags

		m.SelfId = dBuf.Long()
		m.Link = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetParticipants (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetParticipants) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xf34e3233:
		x.UInt(0xf34e3233)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetFilter())
		x.String(m.GetQ())
		x.Int(m.GetOffset())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetParticipants) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetParticipants) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xf34e3233:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Filter = dBuf.String()
		m.Q = dBuf.String()
		m.Offset = dBuf.Int()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelReadHistory (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadHistory) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xdd1b6e1f:
		x.UInt(0xdd1b6e1f)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMaxId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReadHistory) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadHistory) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xdd1b6e1f:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MaxId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteMessages (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x758fcf5a:
		x.UInt(0x758fcf5a)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
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

func (m *TLChannelDeleteMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x758fcf5a:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		c3 := dBuf.Int()
		if c3 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 3, c3)
		}
		l3 := dBuf.Int()
		if vectorTooLong(dBuf, l3) {
			return fmt.Errorf("invalid vector length: %d", l3)
		}
		v3 := make([]int32, l3)
		for i := int32(0); i < l3; i++ {
			v3[i] = dBuf.Int()
		}
		m.Id = v3
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelInviteToChannel (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelInviteToChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x1177a29b:
		x.UInt(0x1177a29b)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetId())))
		for _, v := range m.GetId() {
			x.Long(v)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelInviteToChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelInviteToChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x1177a29b:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		c3 := dBuf.Int()
		if c3 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 3, c3)
		}
		l3 := dBuf.Int()
		if vectorTooLong(dBuf, l3) {
			return fmt.Errorf("invalid vector length: %d", l3)
		}
		v3 := make([]int64, l3)
		for i := int32(0); i < l3; i++ {
			v3[i] = dBuf.Long()
		}
		m.Id = v3
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditTitle (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditTitle) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x1368f815:
		x.UInt(0x1368f815)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetTitle())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditTitle) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditTitle) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x1368f815:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Title = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditAbout (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditAbout) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xde5f0c1f:
		x.UInt(0xde5f0c1f)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetAbout())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditAbout) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditAbout) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xde5f0c1f:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.About = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelCheckUsername (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelCheckUsername) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x3ac7de2c:
		x.UInt(0x3ac7de2c)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetUsername())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelCheckUsername) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelCheckUsername) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x3ac7de2c:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Username = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelUpdateUsername (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelUpdateUsername) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x18f93d48:
		x.UInt(0x18f93d48)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetUsername())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelUpdateUsername) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelUpdateUsername) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x18f93d48:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Username = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetAdminedPublicChannels (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetAdminedPublicChannels) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x83d45125:
		x.UInt(0x83d45125)

		// no flags

		x.Long(m.GetSelfId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetAdminedPublicChannels) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetAdminedPublicChannels) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x83d45125:

		// not has flags

		m.SelfId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteChannel (pluma: task 055)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteChannel) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xc597a912:
		x.UInt(0xc597a912)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteChannel) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteChannel) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xc597a912:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditAdmin (pluma: task 042)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditAdmin) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x4bf90a96:
		x.UInt(0x4bf90a96)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetUserId())
		x.Int(m.GetAdminRights())
		x.String(m.GetRank())
		m.GetKeepRank().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditAdmin) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditAdmin) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x4bf90a96:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.UserId = dBuf.Long()
		m.AdminRights = dBuf.Int()
		m.Rank = dBuf.String()
		m6 := &mtproto.Bool{}
		m6.Decode(dBuf)
		m.KeepRank = m6
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditBanned (pluma: task 042)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditBanned) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xa921793c:
		x.UInt(0xa921793c)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetUserId())
		x.Int(m.GetBannedRights())
		x.Int(m.GetUntilDate())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditBanned) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditBanned) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xa921793c:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.UserId = dBuf.Long()
		m.BannedRights = dBuf.Int()
		m.UntilDate = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditDefaultBannedRights (pluma: task 042)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditDefaultBannedRights) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x4b52885b:
		x.UInt(0x4b52885b)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetBannedRights())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditDefaultBannedRights) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditDefaultBannedRights) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x4b52885b:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.BannedRights = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditCreator (pluma: task 042)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditCreator) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x33506eb1:
		x.UInt(0x33506eb1)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetUserId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditCreator) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditCreator) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x33506eb1:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.UserId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSearch (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearch) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x1c1c5cdb:
		x.UInt(0x1c1c5cdb)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetQ())
		x.Long(m.GetFromId())
		x.Int(m.GetMediaType())
		x.Int(m.GetMinDate())
		x.Int(m.GetMaxDate())
		x.Int(m.GetOffsetId())
		x.Int(m.GetAddOffset())
		x.Int(m.GetLimit())
		x.Int(m.GetMaxId())
		x.Int(m.GetMinId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSearch) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearch) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x1c1c5cdb:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Q = dBuf.String()
		m.FromId = dBuf.Long()
		m.MediaType = dBuf.Int()
		m.MinDate = dBuf.Int()
		m.MaxDate = dBuf.Int()
		m.OffsetId = dBuf.Int()
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

// TLChannelReadMessageContents (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadMessageContents) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x7a83cfd6:
		x.UInt(0x7a83cfd6)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
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

func (m *TLChannelReadMessageContents) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadMessageContents) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x7a83cfd6:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		c3 := dBuf.Int()
		if c3 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 3, c3)
		}
		l3 := dBuf.Int()
		if vectorTooLong(dBuf, l3) {
			return fmt.Errorf("invalid vector length: %d", l3)
		}
		v3 := make([]int32, l3)
		for i := int32(0); i < l3; i++ {
			v3[i] = dBuf.Int()
		}
		m.Id = v3
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelReadMentions (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadMentions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xe0e5a0eb:
		x.UInt(0xe0e5a0eb)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReadMentions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadMentions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xe0e5a0eb:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetUnreadMentions (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetUnreadMentions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x6f2e482a:
		x.UInt(0x6f2e482a)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetOffsetId())
		x.Int(m.GetAddOffset())
		x.Int(m.GetLimit())
		x.Int(m.GetMaxId())
		x.Int(m.GetMinId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetUnreadMentions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetUnreadMentions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x6f2e482a:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.OffsetId = dBuf.Int()
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

// TLChannelEditPhoto (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditPhoto) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x89b5afe9:
		x.UInt(0x89b5afe9)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetPhotoId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditPhoto) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditPhoto) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x89b5afe9:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.PhotoId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetDiscussionGroup (pluma: task 048)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetDiscussionGroup) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x18494047:
		x.UInt(0x18494047)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetGroupId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetDiscussionGroup) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetDiscussionGroup) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x18494047:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.GroupId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetGroupsForDiscussion (pluma: task 048)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetGroupsForDiscussion) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x937a4c6f:
		x.UInt(0x937a4c6f)

		// no flags

		x.Long(m.GetSelfId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetGroupsForDiscussion) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetGroupsForDiscussion) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x937a4c6f:

		// not has flags

		m.SelfId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetDiscussionMessage (pluma: task 048)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetDiscussionMessage) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x38170db2:
		x.UInt(0x38170db2)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetDiscussionMessage) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetDiscussionMessage) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x38170db2:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetReplies (pluma: task 048)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetReplies) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x3a0adbe8:
		x.UInt(0x3a0adbe8)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMsgId())
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

func (m *TLChannelGetReplies) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetReplies) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x3a0adbe8:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MsgId = dBuf.Int()
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

// TLChannelReadDiscussion (pluma: task 048)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadDiscussion) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xec9c1381:
		x.UInt(0xec9c1381)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMsgId())
		x.Int(m.GetReadMaxId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReadDiscussion) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadDiscussion) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xec9c1381:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MsgId = dBuf.Int()
		m.ReadMaxId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleSetting (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleSetting) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xc902cde3:
		x.UInt(0xc902cde3)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetSetting())
		m.GetEnabled().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleSetting) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleSetting) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xc902cde3:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Setting = dBuf.Int()
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.Enabled = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetAvailableReactions (pluma: task 044)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetAvailableReactions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x3b4d2cbe:
		x.UInt(0x3b4d2cbe)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetReactionsType())
		x.VectorString(m.GetReactions())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetAvailableReactions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetAvailableReactions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x3b4d2cbe:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.ReactionsType = dBuf.Int()
		m.Reactions = dBuf.VectorString()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelIncrementMessageViews (pluma: task 046)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelIncrementMessageViews) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x6376cf3a:
		x.UInt(0x6376cf3a)

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

func (m *TLChannelIncrementMessageViews) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelIncrementMessageViews) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x6376cf3a:

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

// TLChannelAddMessageForwards (pluma: task 046)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelAddMessageForwards) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x8f2e8311:
		x.UInt(0x8f2e8311)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetId())))
		for _, v := range m.GetId() {
			x.Int(v)
		}

		x.Int(int32(mtproto.CRC32_vector))
		x.Int(int32(len(m.GetRandomId())))
		for _, v := range m.GetRandomId() {
			x.Long(v)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelAddMessageForwards) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelAddMessageForwards) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x8f2e8311:

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
		c3 := dBuf.Int()
		if c3 != int32(mtproto.CRC32_vector) {
			return fmt.Errorf("invalid mtproto.CRC32_vector, c%d: %d", 3, c3)
		}
		l3 := dBuf.Int()
		if vectorTooLong(dBuf, l3) {
			return fmt.Errorf("invalid vector length: %d", l3)
		}
		v3 := make([]int64, l3)
		for i := int32(0); i < l3; i++ {
			v3[i] = dBuf.Long()
		}
		m.RandomId = v3
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteHistory (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteHistory) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xaa131ce5:
		x.UInt(0xaa131ce5)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetForEveryone().Encode(x, layer)
		x.Int(m.GetMaxId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteHistory) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteHistory) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xaa131ce5:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.ForEveryone = m3
		m.MaxId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteParticipantHistory (pluma: task 043)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteParticipantHistory) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xc19ab239:
		x.UInt(0xc19ab239)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetParticipantId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteParticipantHistory) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteParticipantHistory) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xc19ab239:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.ParticipantId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleSignatures (pluma: task 045)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleSignatures) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x43c56fdd:
		x.UInt(0x43c56fdd)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetEnabled().Encode(x, layer)
		m.GetProfiles().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleSignatures) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleSignatures) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x43c56fdd:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Enabled = m3
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.Profiles = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleSlowMode (pluma: task 045)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleSlowMode) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x89493788:
		x.UInt(0x89493788)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetSeconds())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleSlowMode) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleSlowMode) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x89493788:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Seconds = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleJoinRequest (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleJoinRequest) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x119bca9f:
		x.UInt(0x119bca9f)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetEnabled().Encode(x, layer)
		m.GetApplyToInvites().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleJoinRequest) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleJoinRequest) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x119bca9f:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Enabled = m3
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.ApplyToInvites = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelExportChatInvite (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelExportChatInvite) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x429cb54c:
		x.UInt(0x429cb54c)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		m.GetLegacyRevokePermanent().Encode(x, layer)
		m.GetRequestNeeded().Encode(x, layer)
		x.Int(m.GetExpireDate())
		x.Int(m.GetUsageLimit())
		x.String(m.GetTitle())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelExportChatInvite) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelExportChatInvite) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x429cb54c:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m2 := &mtproto.Bool{}
		m2.Decode(dBuf)
		m.LegacyRevokePermanent = m2
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.RequestNeeded = m3
		m.ExpireDate = dBuf.Int()
		m.UsageLimit = dBuf.Int()
		m.Title = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetExportedChatInvite (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetExportedChatInvite) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x65bd8697:
		x.UInt(0x65bd8697)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetLink())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetExportedChatInvite) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetExportedChatInvite) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x65bd8697:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Link = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetExportedChatInvites (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetExportedChatInvites) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xabc9a8c5:
		x.UInt(0xabc9a8c5)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Long(m.GetAdminId())
		m.GetRevoked().Encode(x, layer)
		x.Int(m.GetOffsetDate())
		x.String(m.GetOffsetLink())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetExportedChatInvites) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetExportedChatInvites) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xabc9a8c5:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.AdminId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Revoked = m3
		m.OffsetDate = dBuf.Int()
		m.OffsetLink = dBuf.String()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditExportedChatInvite (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditExportedChatInvite) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x55be5b73:
		x.UInt(0x55be5b73)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetLink())
		m.GetRevoked().Encode(x, layer)
		x.Int(m.GetFields())
		x.Int(m.GetExpireDate())
		x.Int(m.GetUsageLimit())
		m.GetRequestNeeded().Encode(x, layer)
		x.String(m.GetTitle())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditExportedChatInvite) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditExportedChatInvite) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x55be5b73:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Link = dBuf.String()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Revoked = m3
		m.Fields = dBuf.Int()
		m.ExpireDate = dBuf.Int()
		m.UsageLimit = dBuf.Int()
		m7 := &mtproto.Bool{}
		m7.Decode(dBuf)
		m.RequestNeeded = m7
		m.Title = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteExportedChatInvite (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteExportedChatInvite) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x2a98c48c:
		x.UInt(0x2a98c48c)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetLink())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteExportedChatInvite) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteExportedChatInvite) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x2a98c48c:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Link = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteRevokedExportedChatInvites (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteRevokedExportedChatInvites) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xd6bd4aec:
		x.UInt(0xd6bd4aec)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Long(m.GetAdminId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteRevokedExportedChatInvites) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteRevokedExportedChatInvites) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xd6bd4aec:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.AdminId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetAdminsWithInvites (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetAdminsWithInvites) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x752c6526:
		x.UInt(0x752c6526)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetAdminsWithInvites) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetAdminsWithInvites) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x752c6526:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetChatInviteImporters (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetChatInviteImporters) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x7252267f:
		x.UInt(0x7252267f)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		m.GetRequested().Encode(x, layer)
		x.String(m.GetLink())
		x.String(m.GetQ())
		x.Int(m.GetOffsetDate())
		x.Long(m.GetOffsetUser())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetChatInviteImporters) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetChatInviteImporters) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x7252267f:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m2 := &mtproto.Bool{}
		m2.Decode(dBuf)
		m.Requested = m2
		m.Link = dBuf.String()
		m.Q = dBuf.String()
		m.OffsetDate = dBuf.Int()
		m.OffsetUser = dBuf.Long()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelHideChatJoinRequests (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelHideChatJoinRequests) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x9f31d499:
		x.UInt(0x9f31d499)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetApproved().Encode(x, layer)
		m.GetAll().Encode(x, layer)
		x.String(m.GetLink())
		x.Long(m.GetUserId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelHideChatJoinRequests) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelHideChatJoinRequests) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x9f31d499:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Approved = m3
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.All = m4
		m.Link = dBuf.String()
		m.UserId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetPendingJoinRequests (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetPendingJoinRequests) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x79a07946:
		x.UInt(0x79a07946)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetPendingJoinRequests) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetPendingJoinRequests) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x79a07946:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetLeftChannels (pluma: task 041)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetLeftChannels) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x85038335:
		x.UInt(0x85038335)

		// no flags

		x.Long(m.GetSelfId())
		x.Int(m.GetOffset())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetLeftChannels) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetLeftChannels) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x85038335:

		// not has flags

		m.SelfId = dBuf.Long()
		m.Offset = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleForum (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleForum) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x3ff784d3:
		x.UInt(0x3ff784d3)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetEnabled().Encode(x, layer)
		m.GetTabs().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleForum) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleForum) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x3ff784d3:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Enabled = m3
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.Tabs = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelToggleViewForumAsMessages (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelToggleViewForumAsMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x9031fee5:
		x.UInt(0x9031fee5)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetEnabled().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelToggleViewForumAsMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelToggleViewForumAsMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x9031fee5:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Enabled = m3
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelCreateForumTopic (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelCreateForumTopic) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xdb9afd73:
		x.UInt(0xdb9afd73)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetTitle())
		x.Int(m.GetIconColor())
		x.Long(m.GetIconEmojiId())
		m.GetTitleMissing().Encode(x, layer)
		x.Long(m.GetRandomId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelCreateForumTopic) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelCreateForumTopic) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xdb9afd73:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Title = dBuf.String()
		m.IconColor = dBuf.Int()
		m.IconEmojiId = dBuf.Long()
		m6 := &mtproto.Bool{}
		m6.Decode(dBuf)
		m.TitleMissing = m6
		m.RandomId = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditForumTopic (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditForumTopic) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x6e734135:
		x.UInt(0x6e734135)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopicId())
		x.Int(m.GetFields())
		x.String(m.GetTitle())
		x.Long(m.GetIconEmojiId())
		m.GetClosed().Encode(x, layer)
		m.GetHidden().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditForumTopic) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditForumTopic) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x6e734135:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopicId = dBuf.Int()
		m.Fields = dBuf.Int()
		m.Title = dBuf.String()
		m.IconEmojiId = dBuf.Long()
		m7 := &mtproto.Bool{}
		m7.Decode(dBuf)
		m.Closed = m7
		m8 := &mtproto.Bool{}
		m8.Decode(dBuf)
		m.Hidden = m8
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelUpdatePinnedForumTopic (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelUpdatePinnedForumTopic) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xf9718172:
		x.UInt(0xf9718172)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopicId())
		m.GetPinned().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelUpdatePinnedForumTopic) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelUpdatePinnedForumTopic) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xf9718172:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopicId = dBuf.Int()
		m4 := &mtproto.Bool{}
		m4.Decode(dBuf)
		m.Pinned = m4
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelReorderPinnedForumTopics (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReorderPinnedForumTopics) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xb45392fb:
		x.UInt(0xb45392fb)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		m.GetForce().Encode(x, layer)
		x.VectorInt(m.GetOrder())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReorderPinnedForumTopics) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReorderPinnedForumTopics) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xb45392fb:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.Force = m3
		m.Order = dBuf.VectorInt()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelDeleteTopicHistory (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelDeleteTopicHistory) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x03b540e1:
		x.UInt(0x03b540e1)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelDeleteTopicHistory) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelDeleteTopicHistory) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x03b540e1:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetForumTopics (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetForumTopics) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xdf031425:
		x.UInt(0xdf031425)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetQ())
		x.Int(m.GetOffsetDate())
		x.Int(m.GetOffsetId())
		x.Int(m.GetOffsetTopic())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetForumTopics) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetForumTopics) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xdf031425:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Q = dBuf.String()
		m.OffsetDate = dBuf.Int()
		m.OffsetId = dBuf.Int()
		m.OffsetTopic = dBuf.Int()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetForumTopicsByID (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetForumTopicsByID) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x70c68123:
		x.UInt(0x70c68123)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.VectorInt(m.GetTopics())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetForumTopicsByID) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetForumTopicsByID) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x70c68123:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Topics = dBuf.VectorInt()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSearchTopic (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearchTopic) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x81441bc7:
		x.UInt(0x81441bc7)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())
		x.String(m.GetQ())
		x.Long(m.GetFromId())
		x.Int(m.GetMediaType())
		m.GetUnreadMentions().Encode(x, layer)
		x.Int(m.GetMinDate())
		x.Int(m.GetMaxDate())
		x.Int(m.GetOffsetId())
		x.Int(m.GetAddOffset())
		x.Int(m.GetLimit())
		x.Int(m.GetMaxId())
		x.Int(m.GetMinId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSearchTopic) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearchTopic) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x81441bc7:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		m.Q = dBuf.String()
		m.FromId = dBuf.Long()
		m.MediaType = dBuf.Int()
		m6 := &mtproto.Bool{}
		m6.Decode(dBuf)
		m.UnreadMentions = m6
		m.MinDate = dBuf.Int()
		m.MaxDate = dBuf.Int()
		m.OffsetId = dBuf.Int()
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

// TLChannelReadTopicMentions (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadTopicMentions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xd62ee199:
		x.UInt(0xd62ee199)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReadTopicMentions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadTopicMentions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xd62ee199:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelUnpinAllTopicMessages (pluma: task 060)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelUnpinAllTopicMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xac401063:
		x.UInt(0xac401063)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelUnpinAllTopicMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelUnpinAllTopicMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xac401063:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetHistoryTTL (pluma: task 063)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetHistoryTTL) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x940ce092:
		x.UInt(0x940ce092)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTtlPeriod())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetHistoryTTL) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetHistoryTTL) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x940ce092:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TtlPeriod = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetChatWallpaper (pluma: task 065)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetChatWallpaper) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xbbc2f4a6:
		x.UInt(0xbbc2f4a6)

		// set flags
		var flags uint32 = 0
		if m.GetWallpaper() != nil {
			flags |= 1 << 0
		}
		x.UInt(flags)

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		if m.GetWallpaper() != nil {
			m.GetWallpaper().Encode(x, layer)
		}

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetChatWallpaper) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetChatWallpaper) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xbbc2f4a6:

		flags := dBuf.UInt()
		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		if (flags & (1 << 0)) != 0 {
			m.Wallpaper = &mtproto.WallPaper{}
			m.Wallpaper.Decode(dBuf)
		}
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetChatTheme (pluma: task 065)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetChatTheme) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xd9069fe1:
		x.UInt(0xd9069fe1)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.String(m.GetThemeEmoticon())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetChatTheme) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetChatTheme) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xd9069fe1:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.ThemeEmoticon = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSearchCount (pluma: task 066)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearchCount) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x251efb0a:
		x.UInt(0x251efb0a)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())
		x.Int(m.GetMediaType())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelSearchCount) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearchCount) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x251efb0a:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		m.MediaType = dBuf.Int()
		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// TLChannelSearchCalendar (pluma: task 066)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearchCalendar) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xff027ae7:
		x.UInt(0xff027ae7)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMediaType())
		x.Int(m.GetOffsetId())
		x.Int(m.GetOffsetDate())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelSearchCalendar) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearchCalendar) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xff027ae7:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MediaType = dBuf.Int()
		m.OffsetId = dBuf.Int()
		m.OffsetDate = dBuf.Int()
		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// TLChannelSearchPositions (pluma: task 066)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearchPositions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xbbcc1e84:
		x.UInt(0xbbcc1e84)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMediaType())
		x.Int(m.GetOffsetId())
		x.Int(m.GetLimit())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelSearchPositions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearchPositions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xbbcc1e84:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MediaType = dBuf.Int()
		m.OffsetId = dBuf.Int()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// TLChannelSearchAcross (pluma: task 066)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSearchAcross) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xa2c1bfad:
		x.UInt(0xa2c1bfad)

		// no flags

		x.Long(m.GetSelfId())
		m.GetPublicOnly().Encode(x, layer)
		m.GetBroadcastsOnly().Encode(x, layer)
		m.GetGroupsOnly().Encode(x, layer)
		x.Long(m.GetCommunityId())
		x.Int(m.GetFolderMode())
		x.VectorLong(m.GetFolderChannels())
		x.String(m.GetQ())
		x.Int(m.GetMediaType())
		x.Int(m.GetMinDate())
		x.Int(m.GetMaxDate())
		x.Int(m.GetOffsetRate())
		x.Long(m.GetOffsetChannelId())
		x.Int(m.GetOffsetId())
		x.Int(m.GetLimit())

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}

	return nil
}

func (m *TLChannelSearchAcross) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSearchAcross) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xa2c1bfad:

		// not has flags

		m.SelfId = dBuf.Long()
		m1 := &mtproto.Bool{}
		m1.Decode(dBuf)
		m.PublicOnly = m1
		m2 := &mtproto.Bool{}
		m2.Decode(dBuf)
		m.BroadcastsOnly = m2
		m3 := &mtproto.Bool{}
		m3.Decode(dBuf)
		m.GroupsOnly = m3
		m.CommunityId = dBuf.Long()
		m.FolderMode = dBuf.Int()
		m.FolderChannels = dBuf.VectorLong()
		m.Q = dBuf.String()
		m.MediaType = dBuf.Int()
		m.MinDate = dBuf.Int()
		m.MaxDate = dBuf.Int()
		m.OffsetRate = dBuf.Int()
		m.OffsetChannelId = dBuf.Long()
		m.OffsetId = dBuf.Int()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		return fmt.Errorf("invalid constructor: %x", uint32(m.Constructor))
	}
}

// TLChannelExpireMessages (pluma: task 067)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelExpireMessages) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x5b360629:
		x.UInt(0x5b360629)

		// no flags

		x.Long(m.GetChannelId())
		x.Long(m.GetNow())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelExpireMessages) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelExpireMessages) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x5b360629:

		// not has flags

		m.ChannelId = dBuf.Long()
		m.Now = dBuf.Long()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetUnreadReactions (pluma: task 067)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetUnreadReactions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xd0ba7662:
		x.UInt(0xd0ba7662)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())
		x.Int(m.GetOffsetId())
		x.Int(m.GetAddOffset())
		x.Int(m.GetLimit())
		x.Int(m.GetMaxId())
		x.Int(m.GetMinId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetUnreadReactions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetUnreadReactions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xd0ba7662:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		m.OffsetId = dBuf.Int()
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

// TLChannelReadReactions (pluma: task 067)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelReadReactions) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x7ab8d0e9:
		x.UInt(0x7ab8d0e9)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Int(m.GetTopMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelReadReactions) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelReadReactions) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x7ab8d0e9:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.TopMsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelSetBlocked (pluma: task 072)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelSetBlocked) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xfc3e38a4:
		x.UInt(0xfc3e38a4)

		// no flags

		x.Long(m.GetChannelId())
		m.GetBlocked().Encode(x, layer)

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelSetBlocked) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelSetBlocked) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xfc3e38a4:

		// not has flags

		m.ChannelId = dBuf.Long()
		m1 := &mtproto.Bool{}
		m1.Decode(dBuf)
		m.Blocked = m1
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelEditParticipantRank (pluma: task 078)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelEditParticipantRank) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xfad8523d:
		x.UInt(0xfad8523d)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetPermAuthKeyId())
		x.Long(m.GetChannelId())
		x.Long(m.GetUserId())
		x.String(m.GetRank())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelEditParticipantRank) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelEditParticipantRank) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xfad8523d:

		// not has flags

		m.SelfId = dBuf.Long()
		m.PermAuthKeyId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.UserId = dBuf.Long()
		m.Rank = dBuf.String()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// TLChannelGetReadParticipants (pluma: task 078)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetReadParticipants) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0xc84725c:
		x.UInt(0xc84725c)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.Int(m.GetMsgId())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetReadParticipants) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetReadParticipants) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0xc84725c:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.MsgId = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// Vector_ReadParticipantDate (pluma: task 078)
///////////////////////////////////////////////////////////////////////////////

func (m *Vector_ReadParticipantDate) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		if err := v.Encode(x, layer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Vector_ReadParticipantDate) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]*mtproto.ReadParticipantDate, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = new(mtproto.ReadParticipantDate)
		if err := m.Datas[i].Decode(dBuf); err != nil {
			return err
		}
	}

	return dBuf.GetError()
}

func (m *Vector_ReadParticipantDate) CalcByteSize(layer int32) int {
	return 0
}

// TLChannelGetAdminLog (pluma: task 078)
///////////////////////////////////////////////////////////////////////////////

func (m *TLChannelGetAdminLog) Encode(x *mtproto.EncodeBuf, layer int32) error {
	switch uint32(m.Constructor) {
	case 0x43e8f3b3:
		x.UInt(0x43e8f3b3)

		// no flags

		x.Long(m.GetSelfId())
		x.Long(m.GetChannelId())
		x.String(m.GetQ())
		x.Long(m.GetFilter())
		x.VectorLong(m.GetAdmins())
		x.Long(m.GetMaxId())
		x.Long(m.GetMinId())
		x.Int(m.GetLimit())

	default:
		// log.Errorf("")
	}

	return nil
}

func (m *TLChannelGetAdminLog) CalcByteSize(layer int32) int {
	return 0
}

func (m *TLChannelGetAdminLog) Decode(dBuf *mtproto.DecodeBuf) error {
	switch uint32(m.Constructor) {
	case 0x43e8f3b3:

		// not has flags

		m.SelfId = dBuf.Long()
		m.ChannelId = dBuf.Long()
		m.Q = dBuf.String()
		m.Filter = dBuf.Long()
		m.Admins = dBuf.VectorLong()
		m.MaxId = dBuf.Long()
		m.MinId = dBuf.Long()
		m.Limit = dBuf.Int()
		return dBuf.GetError()

	default:
		// log.Errorf("")
	}
	return dBuf.GetError()
}

// Vector_ChannelAdminLogEvent (pluma: task 078)
///////////////////////////////////////////////////////////////////////////////

func (m *Vector_ChannelAdminLogEvent) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.Int(int32(mtproto.CRC32_vector))
	x.Int(int32(len(m.Datas)))
	for _, v := range m.Datas {
		if err := v.Encode(x, layer); err != nil {
			return err
		}
	}

	return nil
}

func (m *Vector_ChannelAdminLogEvent) Decode(dBuf *mtproto.DecodeBuf) error {
	if c := dBuf.Int(); c != int32(mtproto.CRC32_vector) {
		return fmt.Errorf("invalid CRC32_vector: %d", c)
	}
	l1 := dBuf.Int()
	if vectorTooLong(dBuf, l1) {
		return fmt.Errorf("invalid vector length: %d", l1)
	}
	m.Datas = make([]*mtproto.ChannelAdminLogEvent, l1)
	for i := int32(0); i < l1; i++ {
		m.Datas[i] = new(mtproto.ChannelAdminLogEvent)
		if err := m.Datas[i].Decode(dBuf); err != nil {
			return err
		}
	}

	return dBuf.GetError()
}

func (m *Vector_ChannelAdminLogEvent) CalcByteSize(layer int32) int {
	return 0
}
