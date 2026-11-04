/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 */

package channel

const (
	CRC32_UNKNOWN                        TLConstructor = 0
	CRC32_immutableChannel               TLConstructor = -684293246  // 0xd7368382
	CRC32_immutableChannelParticipant    TLConstructor = 675582471   // 0x28449207
	CRC32_mutableChannel                 TLConstructor = -662027966  // 0xd88a4142
	CRC32_channel_getMutableChannel      TLConstructor = -437245607  // 0xe5f02959
	CRC32_channel_getChannelListByIdList TLConstructor = -1986537988 // 0x8997d5fc
	// pluma: task 038
	CRC32_channel_createChannel TLConstructor = -641600475 // 0xd9c1f425
	// pluma: task 038
	CRC32_channel_getChannelDialogs TLConstructor = -1725659745 // 0x9924859f
	// pluma: task 038
	CRC32_channel_getTopMessages TLConstructor = 687937944 // 0x29011998
	// pluma: task 039
	CRC32_channel_getMemberIdList TLConstructor = 771323905 // 0x2df97801
	// pluma: task 039
	CRC32_channel_getHistory TLConstructor = 653475021 // 0x26f33ccd
	// pluma: task 039
	CRC32_channel_getMessages TLConstructor = -1395419135 // 0xacd39801
	// pluma: task 040
	CRC32_channel_getChannelDifference TLConstructor = 2140231388 // 0x7f9156dc
	// pluma: task 041
	CRC32_channel_joinChannel TLConstructor = 634004096 // 0x25ca2280
	// pluma: task 041
	CRC32_channel_leaveChannel TLConstructor = 1331881771 // 0x4f62e72b
	// pluma: task 041
	CRC32_channel_checkChatInvite TLConstructor = -1311035397 // 0xb1db2ffb
	// pluma: task 041
	CRC32_channel_getParticipants TLConstructor = -212979149 // 0xf34e3233
	// pluma: task 043
	CRC32_channel_readHistory TLConstructor = -585404897 // 0xdd1b6e1f
)
