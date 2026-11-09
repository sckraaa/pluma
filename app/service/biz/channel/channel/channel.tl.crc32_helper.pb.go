/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 */

package channel

const (
	CRC32_UNKNOWN                        TLConstructor = 0
	CRC32_immutableChannel               TLConstructor = 1726695119  // 0x66eb46cf
	CRC32_immutableChannelParticipant    TLConstructor = 885437367   // 0x34c6b3b7
	CRC32_mutableChannel                 TLConstructor = -662027966  // 0xd88a4142
	CRC32_channel_getMutableChannel      TLConstructor = -437245607  // 0xe5f02959
	CRC32_channel_getChannelListByIdList TLConstructor = -1986537988 // 0x8997d5fc
	// pluma: task 038
	CRC32_channel_createChannel TLConstructor = 1532314294 // 0x5b5542b6
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
	// pluma: task 043
	CRC32_channel_deleteMessages TLConstructor = 1972359002 // 0x758fcf5a
	// pluma: task 041
	CRC32_channel_inviteToChannel TLConstructor = 293053083 // 0x1177a29b
	// pluma: task 044
	CRC32_channel_editTitle TLConstructor = 325646357 // 0x1368f815
	// pluma: task 044
	CRC32_channel_editAbout TLConstructor = -564196321 // 0xde5f0c1f
	// pluma: task 044
	CRC32_channel_checkUsername TLConstructor = 986177068 // 0x3ac7de2c
	// pluma: task 044
	CRC32_channel_updateUsername TLConstructor = 418987336 // 0x18f93d48
	// pluma: task 044
	CRC32_channel_getAdminedPublicChannels TLConstructor = -2083237595 // 0x83d45125
	// pluma: task 055
	CRC32_channel_deleteChannel TLConstructor = -979916526 // 0xc597a912
	// pluma: task 046
	CRC32_channel_incrementMessageViews TLConstructor = 1668730682 // 0x6376cf3a
	// pluma: task 046
	CRC32_channel_addMessageForwards TLConstructor = -1892777199 // 0x8f2e8311
	// pluma: task 043
	CRC32_channel_deleteHistory TLConstructor = -1441587995 // 0xaa131ce5
	// pluma: task 043
	CRC32_channel_deleteParticipantHistory TLConstructor = -1046826439 // 0xc19ab239
	// pluma: task 045
	CRC32_channel_toggleSignatures TLConstructor = 1137012701 // 0x43c56fdd
	// pluma: task 045
	CRC32_channel_toggleSlowMode TLConstructor = -1991690360 // 0x89493788
	// pluma: task 041
	CRC32_channel_toggleJoinRequest TLConstructor = 295422623 // 0x119bca9f
	// pluma: task 041
	CRC32_channel_exportChatInvite TLConstructor = 1117566284 // 0x429cb54c
	// pluma: task 041
	CRC32_channel_getExportedChatInvite TLConstructor = 1706919575 // 0x65bd8697
	// pluma: task 041
	CRC32_channel_getExportedChatInvites TLConstructor = -1412847419 // 0xabc9a8c5
	// pluma: task 041
	CRC32_channel_editExportedChatInvite TLConstructor = 1438538611 // 0x55be5b73
	// pluma: task 041
	CRC32_channel_deleteExportedChatInvite TLConstructor = 714654860 // 0x2a98c48c
	// pluma: task 041
	CRC32_channel_deleteRevokedExportedChatInvites TLConstructor = -692237588 // 0xd6bd4aec
	// pluma: task 041
	CRC32_channel_getAdminsWithInvites TLConstructor = 1965843750 // 0x752c6526
	// pluma: task 041
	CRC32_channel_getChatInviteImporters TLConstructor = 1917986431 // 0x7252267f
	// pluma: task 041
	CRC32_channel_hideChatJoinRequests TLConstructor = -1624124263 // 0x9f31d499
	// pluma: task 041
	CRC32_channel_getPendingJoinRequests TLConstructor = 2040559942 // 0x79a07946
	// pluma: task 041
	CRC32_channel_getLeftChannels TLConstructor = -2063367371 // 0x85038335
	// pluma: task 060
	CRC32_channel_toggleForum TLConstructor = 1073186003 // 0x3ff784d3
	// pluma: task 060
	CRC32_channel_toggleViewForumAsMessages TLConstructor = -1875771675 // 0x9031fee5
	// pluma: task 060
	CRC32_channel_createForumTopic TLConstructor = -610599565 // 0xdb9afd73
	// pluma: task 060
	CRC32_channel_editForumTopic TLConstructor = 1853047093 // 0x6e734135
	// pluma: task 060
	CRC32_channel_updatePinnedForumTopic TLConstructor = -110001806 // 0xf9718172
	// pluma: task 060
	CRC32_channel_reorderPinnedForumTopics TLConstructor = -1269591301 // 0xb45392fb
	// pluma: task 060
	CRC32_channel_deleteTopicHistory TLConstructor = 62210273 // 0x03b540e1
	// pluma: task 060
	CRC32_channel_getForumTopics TLConstructor = -553446363 // 0xdf031425
	// pluma: task 060
	CRC32_channel_getForumTopicsByID TLConstructor = 1892057379 // 0x70c68123
	// pluma: task 060
	CRC32_channel_searchTopic TLConstructor = -2126242873 // 0x81441bc7
	// pluma: task 066
	CRC32_channel_searchCount TLConstructor = 622787338 // 0x251efb0a
	// pluma: task 066
	CRC32_channel_searchCalendar TLConstructor = -16614681 // 0xff027ae7
	// pluma: task 066
	CRC32_channel_searchPositions TLConstructor = -1144250748 // 0xbbcc1e84
	// pluma: task 066
	CRC32_channel_searchAcross TLConstructor = -1564360787 // 0xa2c1bfad
	// pluma: task 060
	CRC32_channel_readTopicMentions TLConstructor = -701570663 // 0xd62ee199
	// pluma: task 060
	CRC32_channel_unpinAllTopicMessages TLConstructor = -1405087645 // 0xac401063
	// pluma: task 063
	CRC32_channel_setHistoryTTL TLConstructor = -1811095406 // 0x940ce092
	// pluma: task 065
	CRC32_channel_setChatWallpaper TLConstructor = -1144851290 // 0xbbc2f4a6
	// pluma: task 065
	CRC32_channel_setChatTheme TLConstructor = -653877279 // 0xd9069fe1
	// pluma: task 044
	CRC32_channel_toggleSetting TLConstructor = -922563101 // 0xc902cde3
	// pluma: task 044
	CRC32_channel_setAvailableReactions TLConstructor = 994913470 // 0x3b4d2cbe
	// pluma: task 048
	CRC32_channel_setDiscussionGroup TLConstructor = 407453767 // 0x18494047
	// pluma: task 048
	CRC32_channel_getGroupsForDiscussion TLConstructor = -1820701585 // 0x937a4c6f
	// pluma: task 048
	CRC32_channel_getDiscussionMessage TLConstructor = 941034930 // 0x38170db2
	// pluma: task 048
	CRC32_channel_getReplies TLConstructor = 973790184 // 0x3a0adbe8
	// pluma: task 048
	CRC32_channel_readDiscussion TLConstructor = -325315711 // 0xec9c1381
	// pluma: task 044
	CRC32_channel_editPhoto TLConstructor = -1984581655 // 0x89b5afe9
	// pluma: task 043
	CRC32_channel_readMentions TLConstructor = -521821973 // 0xe0e5a0eb
	// pluma: task 043
	CRC32_channel_getUnreadMentions TLConstructor = 1865304106 // 0x6f2e482a
	// pluma: task 043
	CRC32_channel_readMessageContents TLConstructor = 2055458774 // 0x7a83cfd6
	// pluma: task 043
	CRC32_channel_search TLConstructor = 471620827 // 0x1c1c5cdb
	// pluma: task 042
	CRC32_channel_editCreator TLConstructor = 860909233 // 0x33506eb1
	// pluma: task 042
	CRC32_channel_editAdmin TLConstructor = 1274612374 // 0x4bf90a96
	// pluma: task 042
	CRC32_channel_editBanned TLConstructor = -1457424068 // 0xa921793c
	// pluma: task 042
	CRC32_channel_editDefaultBannedRights TLConstructor = 1263700059 // 0x4b52885b
)
