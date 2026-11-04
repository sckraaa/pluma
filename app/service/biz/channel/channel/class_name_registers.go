/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 */

package channel

const (
	Predicate_immutableChannel               = "immutableChannel"
	Predicate_immutableChannelParticipant    = "immutableChannelParticipant"
	Predicate_mutableChannel                 = "mutableChannel"
	Predicate_channel_getMutableChannel      = "channel_getMutableChannel"
	Predicate_channel_getChannelListByIdList = "channel_getChannelListByIdList"
	// pluma: task 038
	Predicate_channel_createChannel = "channel_createChannel"
	// pluma: task 038
	Predicate_channel_getChannelDialogs = "channel_getChannelDialogs"
	// pluma: task 038
	Predicate_channel_getTopMessages = "channel_getTopMessages"
	// pluma: task 039
	Predicate_channel_getMemberIdList = "channel_getMemberIdList"
	// pluma: task 039
	Predicate_channel_getHistory = "channel_getHistory"
	// pluma: task 039
	Predicate_channel_getMessages = "channel_getMessages"
	// pluma: task 040
	Predicate_channel_getChannelDifference = "channel_getChannelDifference"
	// pluma: task 041
	Predicate_channel_joinChannel = "channel_joinChannel"
	// pluma: task 041
	Predicate_channel_leaveChannel = "channel_leaveChannel"
	// pluma: task 041
	Predicate_channel_checkChatInvite = "channel_checkChatInvite"
	// pluma: task 041
	Predicate_channel_getParticipants = "channel_getParticipants"
)

var clazzNameRegisters2 = map[string]map[int]int32{
	Predicate_immutableChannel: {
		0: -684293246, // 0xd7368382
	},
	Predicate_immutableChannelParticipant: {
		0: 675582471, // 0x28449207
	},
	Predicate_mutableChannel: {
		0: -662027966, // 0xd88a4142
	},
	Predicate_channel_getMutableChannel: {
		0: -437245607, // 0xe5f02959
	},
	Predicate_channel_getChannelListByIdList: {
		0: -1986537988, // 0x8997d5fc
	},
	Predicate_channel_createChannel: { // pluma: task 038
		0: -641600475, // 0xd9c1f425

	},
	Predicate_channel_getChannelDialogs: { // pluma: task 038
		0: -1725659745, // 0x9924859f

	},
	Predicate_channel_getTopMessages: { // pluma: task 038
		0: 687937944, // 0x29011998

	},
	Predicate_channel_getMemberIdList: { // pluma: task 039
		0: 771323905, // 0x2df97801

	},
	Predicate_channel_getHistory: { // pluma: task 039
		0: 653475021, // 0x26f33ccd

	},
	Predicate_channel_getMessages: { // pluma: task 039
		0: -1395419135, // 0xacd39801

	},
	Predicate_channel_getChannelDifference: { // pluma: task 040
		0: 2140231388, // 0x7f9156dc

	},
	Predicate_channel_joinChannel: { // pluma: task 041
		0: 634004096, // 0x25ca2280

	},
	Predicate_channel_leaveChannel: { // pluma: task 041
		0: 1331881771, // 0x4f62e72b

	},
	Predicate_channel_checkChatInvite: { // pluma: task 041
		0: -1311035397, // 0xb1db2ffb

	},
	Predicate_channel_getParticipants: { // pluma: task 041
		0: -212979149, // 0xf34e3233

	},
}

var clazzIdNameRegisters2 = map[int32]string{
	-684293246:  Predicate_immutableChannel,               // 0xd7368382
	675582471:   Predicate_immutableChannelParticipant,    // 0x28449207
	-662027966:  Predicate_mutableChannel,                 // 0xd88a4142
	-437245607:  Predicate_channel_getMutableChannel,      // 0xe5f02959
	-1986537988: Predicate_channel_getChannelListByIdList, // 0x8997d5fc
	-641600475:  Predicate_channel_createChannel,          // 0xd9c1f425 pluma: task 038
	-1725659745: Predicate_channel_getChannelDialogs,      // 0x9924859f pluma: task 038
	687937944:   Predicate_channel_getTopMessages,         // 0x29011998 pluma: task 038
	771323905:   Predicate_channel_getMemberIdList,        // 0x2df97801 pluma: task 039
	653475021:   Predicate_channel_getHistory,             // 0x26f33ccd pluma: task 039
	-1395419135: Predicate_channel_getMessages,            // 0xacd39801 pluma: task 039
	2140231388:  Predicate_channel_getChannelDifference,   // 0x7f9156dc pluma: task 040
	634004096:   Predicate_channel_joinChannel,            // 0x25ca2280 pluma: task 041
	1331881771:  Predicate_channel_leaveChannel,           // 0x4f62e72b pluma: task 041
	-1311035397: Predicate_channel_checkChatInvite,        // 0xb1db2ffb pluma: task 041
	-212979149:  Predicate_channel_getParticipants,        // 0xf34e3233 pluma: task 041
}

func GetClazzID(clazzName string, layer int) int32 {
	if m, ok := clazzNameRegisters2[clazzName]; ok {
		m2, ok2 := m[layer]
		if ok2 {
			return m2
		}
		m2, ok2 = m[0]
		if ok2 {
			return m2
		}
	}
	return 0
}
