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
	// pluma: task 043
	Predicate_channel_readHistory = "channel_readHistory"
	// pluma: task 043
	Predicate_channel_deleteMessages = "channel_deleteMessages"
	// pluma: task 041
	Predicate_channel_inviteToChannel = "channel_inviteToChannel"
	// pluma: task 044
	Predicate_channel_editTitle = "channel_editTitle"
	// pluma: task 044
	Predicate_channel_editAbout = "channel_editAbout"
	// pluma: task 044
	Predicate_channel_checkUsername = "channel_checkUsername"
	// pluma: task 044
	Predicate_channel_updateUsername = "channel_updateUsername"
	// pluma: task 044
	Predicate_channel_getAdminedPublicChannels = "channel_getAdminedPublicChannels"
	// pluma: task 055
	Predicate_channel_deleteChannel = "channel_deleteChannel"
	// pluma: task 046
	Predicate_channel_incrementMessageViews = "channel_incrementMessageViews"
	// pluma: task 046
	Predicate_channel_addMessageForwards = "channel_addMessageForwards"
	// pluma: task 043
	Predicate_channel_deleteHistory = "channel_deleteHistory"
	// pluma: task 043
	Predicate_channel_deleteParticipantHistory = "channel_deleteParticipantHistory"
	// pluma: task 045
	Predicate_channel_toggleSignatures = "channel_toggleSignatures"
	// pluma: task 045
	Predicate_channel_toggleSlowMode = "channel_toggleSlowMode"
	// pluma: task 041
	Predicate_channel_toggleJoinRequest = "channel_toggleJoinRequest"
	// pluma: task 041
	Predicate_channel_exportChatInvite = "channel_exportChatInvite"
	// pluma: task 041
	Predicate_channel_getExportedChatInvite = "channel_getExportedChatInvite"
	// pluma: task 041
	Predicate_channel_getExportedChatInvites = "channel_getExportedChatInvites"
	// pluma: task 041
	Predicate_channel_editExportedChatInvite = "channel_editExportedChatInvite"
	// pluma: task 041
	Predicate_channel_deleteExportedChatInvite = "channel_deleteExportedChatInvite"
	// pluma: task 041
	Predicate_channel_deleteRevokedExportedChatInvites = "channel_deleteRevokedExportedChatInvites"
	// pluma: task 041
	Predicate_channel_getAdminsWithInvites = "channel_getAdminsWithInvites"
	// pluma: task 041
	Predicate_channel_getChatInviteImporters = "channel_getChatInviteImporters"
	// pluma: task 041
	Predicate_channel_hideChatJoinRequests = "channel_hideChatJoinRequests"
	// pluma: task 041
	Predicate_channel_getPendingJoinRequests = "channel_getPendingJoinRequests"
	// pluma: task 041
	Predicate_channel_getLeftChannels = "channel_getLeftChannels"
	// pluma: task 060
	Predicate_channel_toggleForum = "channel_toggleForum"
	// pluma: task 060
	Predicate_channel_toggleViewForumAsMessages = "channel_toggleViewForumAsMessages"
	// pluma: task 060
	Predicate_channel_createForumTopic = "channel_createForumTopic"
	// pluma: task 060
	Predicate_channel_editForumTopic = "channel_editForumTopic"
	// pluma: task 060
	Predicate_channel_updatePinnedForumTopic = "channel_updatePinnedForumTopic"
	// pluma: task 060
	Predicate_channel_reorderPinnedForumTopics = "channel_reorderPinnedForumTopics"
	// pluma: task 060
	Predicate_channel_deleteTopicHistory = "channel_deleteTopicHistory"
	// pluma: task 060
	Predicate_channel_getForumTopics = "channel_getForumTopics"
	// pluma: task 060
	Predicate_channel_getForumTopicsByID = "channel_getForumTopicsByID"
	// pluma: task 060
	Predicate_channel_searchTopic = "channel_searchTopic"
	// pluma: task 066
	Predicate_channel_searchCount = "channel_searchCount"
	// pluma: task 066
	Predicate_channel_searchCalendar = "channel_searchCalendar"
	// pluma: task 066
	Predicate_channel_searchPositions = "channel_searchPositions"
	// pluma: task 066
	Predicate_channel_searchAcross = "channel_searchAcross"
	// pluma: task 060
	Predicate_channel_readTopicMentions = "channel_readTopicMentions"
	// pluma: task 060
	Predicate_channel_unpinAllTopicMessages = "channel_unpinAllTopicMessages"
	// pluma: task 063
	Predicate_channel_setHistoryTTL = "channel_setHistoryTTL"
	// pluma: task 044
	Predicate_channel_toggleSetting = "channel_toggleSetting"
	// pluma: task 044
	Predicate_channel_setAvailableReactions = "channel_setAvailableReactions"
	// pluma: task 048
	Predicate_channel_setDiscussionGroup = "channel_setDiscussionGroup"
	// pluma: task 048
	Predicate_channel_getGroupsForDiscussion = "channel_getGroupsForDiscussion"
	// pluma: task 048
	Predicate_channel_getDiscussionMessage = "channel_getDiscussionMessage"
	// pluma: task 048
	Predicate_channel_getReplies = "channel_getReplies"
	// pluma: task 048
	Predicate_channel_readDiscussion = "channel_readDiscussion"
	// pluma: task 044
	Predicate_channel_editPhoto = "channel_editPhoto"
	// pluma: task 043
	Predicate_channel_readMentions = "channel_readMentions"
	// pluma: task 043
	Predicate_channel_getUnreadMentions = "channel_getUnreadMentions"
	// pluma: task 043
	Predicate_channel_readMessageContents = "channel_readMessageContents"
	// pluma: task 043
	Predicate_channel_search = "channel_search"
	// pluma: task 042
	Predicate_channel_editCreator = "channel_editCreator"
	// pluma: task 042
	Predicate_channel_editAdmin = "channel_editAdmin"
	// pluma: task 042
	Predicate_channel_editBanned = "channel_editBanned"
	// pluma: task 042
	Predicate_channel_editDefaultBannedRights = "channel_editDefaultBannedRights"
)

var clazzNameRegisters2 = map[string]map[int]int32{
	Predicate_immutableChannel: {
		0: 1726695119, // 0x66eb46cf
	},
	Predicate_immutableChannelParticipant: {
		0: 885437367, // 0x34c6b3b7
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
		0: 1532314294, // 0x5b5542b6

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
	Predicate_channel_readHistory: { // pluma: task 043
		0: -585404897, // 0xdd1b6e1f

	},
	Predicate_channel_deleteMessages: { // pluma: task 043
		0: 1972359002, // 0x758fcf5a

	},
	Predicate_channel_inviteToChannel: { // pluma: task 041
		0: 293053083, // 0x1177a29b

	},
	Predicate_channel_editTitle: { // pluma: task 044
		0: 325646357, // 0x1368f815

	},
	Predicate_channel_editAbout: { // pluma: task 044
		0: -564196321, // 0xde5f0c1f

	},
	Predicate_channel_checkUsername: { // pluma: task 044
		0: 986177068, // 0x3ac7de2c

	},
	Predicate_channel_updateUsername: { // pluma: task 044
		0: 418987336, // 0x18f93d48

	},
	Predicate_channel_getAdminedPublicChannels: { // pluma: task 044
		0: -2083237595, // 0x83d45125

	},
	Predicate_channel_deleteChannel: { // pluma: task 055
		0: -979916526, // 0xc597a912

	},
	Predicate_channel_incrementMessageViews: { // pluma: task 046
		0: 1668730682, // 0x6376cf3a

	},
	Predicate_channel_addMessageForwards: { // pluma: task 046
		0: -1892777199, // 0x8f2e8311

	},
	Predicate_channel_deleteHistory: { // pluma: task 043
		0: -1441587995, // 0xaa131ce5

	},
	Predicate_channel_deleteParticipantHistory: { // pluma: task 043
		0: -1046826439, // 0xc19ab239

	},
	Predicate_channel_toggleSignatures: { // pluma: task 045
		0: 1137012701, // 0x43c56fdd

	},
	Predicate_channel_toggleSlowMode: { // pluma: task 045
		0: -1991690360, // 0x89493788

	},
	Predicate_channel_toggleJoinRequest: { // pluma: task 041
		0: 295422623, // 0x119bca9f

	},
	Predicate_channel_exportChatInvite: { // pluma: task 041
		0: 1117566284, // 0x429cb54c

	},
	Predicate_channel_getExportedChatInvite: { // pluma: task 041
		0: 1706919575, // 0x65bd8697

	},
	Predicate_channel_getExportedChatInvites: { // pluma: task 041
		0: -1412847419, // 0xabc9a8c5

	},
	Predicate_channel_editExportedChatInvite: { // pluma: task 041
		0: 1438538611, // 0x55be5b73

	},
	Predicate_channel_deleteExportedChatInvite: { // pluma: task 041
		0: 714654860, // 0x2a98c48c

	},
	Predicate_channel_deleteRevokedExportedChatInvites: { // pluma: task 041
		0: -692237588, // 0xd6bd4aec

	},
	Predicate_channel_getAdminsWithInvites: { // pluma: task 041
		0: 1965843750, // 0x752c6526

	},
	Predicate_channel_getChatInviteImporters: { // pluma: task 041
		0: 1917986431, // 0x7252267f

	},
	Predicate_channel_hideChatJoinRequests: { // pluma: task 041
		0: -1624124263, // 0x9f31d499

	},
	Predicate_channel_getPendingJoinRequests: { // pluma: task 041
		0: 2040559942, // 0x79a07946

	},
	Predicate_channel_getLeftChannels: { // pluma: task 041
		0: -2063367371, // 0x85038335

	},
	Predicate_channel_toggleForum: { // pluma: task 060
		0: 1073186003, // 0x3ff784d3

	},
	Predicate_channel_toggleViewForumAsMessages: { // pluma: task 060
		0: -1875771675, // 0x9031fee5

	},
	Predicate_channel_createForumTopic: { // pluma: task 060
		0: -610599565, // 0xdb9afd73

	},
	Predicate_channel_editForumTopic: { // pluma: task 060
		0: 1853047093, // 0x6e734135

	},
	Predicate_channel_updatePinnedForumTopic: { // pluma: task 060
		0: -110001806, // 0xf9718172

	},
	Predicate_channel_reorderPinnedForumTopics: { // pluma: task 060
		0: -1269591301, // 0xb45392fb

	},
	Predicate_channel_deleteTopicHistory: { // pluma: task 060
		0: 62210273, // 0x03b540e1

	},
	Predicate_channel_getForumTopics: { // pluma: task 060
		0: -553446363, // 0xdf031425

	},
	Predicate_channel_getForumTopicsByID: { // pluma: task 060
		0: 1892057379, // 0x70c68123

	},
	Predicate_channel_searchTopic: { // pluma: task 060
		0: -2126242873, // 0x81441bc7

	},
	Predicate_channel_searchCount: { // pluma: task 066
		0: 622787338, // 0x251efb0a

	},
	Predicate_channel_searchCalendar: { // pluma: task 066
		0: -16614681, // 0xff027ae7

	},
	Predicate_channel_searchPositions: { // pluma: task 066
		0: -1144250748, // 0xbbcc1e84

	},
	Predicate_channel_searchAcross: { // pluma: task 066
		0: -1564360787, // 0xa2c1bfad

	},
	Predicate_channel_readTopicMentions: { // pluma: task 060
		0: -701570663, // 0xd62ee199

	},
	Predicate_channel_unpinAllTopicMessages: { // pluma: task 060
		0: -1405087645, // 0xac401063

	},
	Predicate_channel_setHistoryTTL: { // pluma: task 063
		0: -1811095406, // 0x940ce092

	},
	Predicate_channel_toggleSetting: { // pluma: task 044
		0: -922563101, // 0xc902cde3

	},
	Predicate_channel_setAvailableReactions: { // pluma: task 044
		0: 994913470, // 0x3b4d2cbe

	},
	Predicate_channel_setDiscussionGroup: { // pluma: task 048
		0: 407453767, // 0x18494047

	},
	Predicate_channel_getGroupsForDiscussion: { // pluma: task 048
		0: -1820701585, // 0x937a4c6f

	},
	Predicate_channel_getDiscussionMessage: { // pluma: task 048
		0: 941034930, // 0x38170db2

	},
	Predicate_channel_getReplies: { // pluma: task 048
		0: 973790184, // 0x3a0adbe8

	},
	Predicate_channel_readDiscussion: { // pluma: task 048
		0: -325315711, // 0xec9c1381

	},
	Predicate_channel_editPhoto: { // pluma: task 044
		0: -1984581655, // 0x89b5afe9

	},
	Predicate_channel_readMentions: { // pluma: task 043
		0: -521821973, // 0xe0e5a0eb

	},
	Predicate_channel_getUnreadMentions: { // pluma: task 043
		0: 1865304106, // 0x6f2e482a

	},
	Predicate_channel_readMessageContents: { // pluma: task 043
		0: 2055458774, // 0x7a83cfd6

	},
	Predicate_channel_search: { // pluma: task 043
		0: 471620827, // 0x1c1c5cdb

	},
	Predicate_channel_editCreator: { // pluma: task 042
		0: 860909233, // 0x33506eb1

	},
	Predicate_channel_editAdmin: { // pluma: task 042
		0: 1274612374, // 0x4bf90a96

	},
	Predicate_channel_editBanned: { // pluma: task 042
		0: -1457424068, // 0xa921793c

	},
	Predicate_channel_editDefaultBannedRights: { // pluma: task 042
		0: 1263700059, // 0x4b52885b

	},
}

var clazzIdNameRegisters2 = map[int32]string{
	1726695119:  Predicate_immutableChannel,                         // 0x66eb46cf
	885437367:   Predicate_immutableChannelParticipant,              // 0x34c6b3b7
	-662027966:  Predicate_mutableChannel,                           // 0xd88a4142
	-437245607:  Predicate_channel_getMutableChannel,                // 0xe5f02959
	-1986537988: Predicate_channel_getChannelListByIdList,           // 0x8997d5fc
	-641600475:  Predicate_channel_createChannel,                    // 0xd9c1f425 pluma: task 038
	-1725659745: Predicate_channel_getChannelDialogs,                // 0x9924859f pluma: task 038
	687937944:   Predicate_channel_getTopMessages,                   // 0x29011998 pluma: task 038
	771323905:   Predicate_channel_getMemberIdList,                  // 0x2df97801 pluma: task 039
	653475021:   Predicate_channel_getHistory,                       // 0x26f33ccd pluma: task 039
	-1395419135: Predicate_channel_getMessages,                      // 0xacd39801 pluma: task 039
	2140231388:  Predicate_channel_getChannelDifference,             // 0x7f9156dc pluma: task 040
	634004096:   Predicate_channel_joinChannel,                      // 0x25ca2280 pluma: task 041
	1331881771:  Predicate_channel_leaveChannel,                     // 0x4f62e72b pluma: task 041
	-1311035397: Predicate_channel_checkChatInvite,                  // 0xb1db2ffb pluma: task 041
	-212979149:  Predicate_channel_getParticipants,                  // 0xf34e3233 pluma: task 041
	-585404897:  Predicate_channel_readHistory,                      // 0xdd1b6e1f pluma: task 043
	1972359002:  Predicate_channel_deleteMessages,                   // 0x758fcf5a pluma: task 043
	293053083:   Predicate_channel_inviteToChannel,                  // 0x1177a29b pluma: task 041
	325646357:   Predicate_channel_editTitle,                        // 0x1368f815 pluma: task 044
	-564196321:  Predicate_channel_editAbout,                        // 0xde5f0c1f pluma: task 044
	986177068:   Predicate_channel_checkUsername,                    // 0x3ac7de2c pluma: task 044
	418987336:   Predicate_channel_updateUsername,                   // 0x18f93d48 pluma: task 044
	-2083237595: Predicate_channel_getAdminedPublicChannels,         // 0x83d45125 pluma: task 044
	-979916526:  Predicate_channel_deleteChannel,                    // 0xc597a912 pluma: task 055
	1668730682:  Predicate_channel_incrementMessageViews,            // 0x6376cf3a pluma: task 046
	-1892777199: Predicate_channel_addMessageForwards,               // 0x8f2e8311 pluma: task 046
	-1441587995: Predicate_channel_deleteHistory,                    // 0xaa131ce5 pluma: task 043
	-1046826439: Predicate_channel_deleteParticipantHistory,         // 0xc19ab239 pluma: task 043
	1137012701:  Predicate_channel_toggleSignatures,                 // 0x43c56fdd pluma: task 045
	-1991690360: Predicate_channel_toggleSlowMode,                   // 0x89493788 pluma: task 045
	-1811095406: Predicate_channel_setHistoryTTL,                    // 0x940ce092 pluma: task 063
	-922563101:  Predicate_channel_toggleSetting,                    // 0xc902cde3 pluma: task 044
	994913470:   Predicate_channel_setAvailableReactions,            // 0x3b4d2cbe pluma: task 044
	407453767:   Predicate_channel_setDiscussionGroup,               // 0x18494047 pluma: task 048
	-1820701585: Predicate_channel_getGroupsForDiscussion,           // 0x937a4c6f pluma: task 048
	941034930:   Predicate_channel_getDiscussionMessage,             // 0x38170db2 pluma: task 048
	973790184:   Predicate_channel_getReplies,                       // 0x3a0adbe8 pluma: task 048
	-325315711:  Predicate_channel_readDiscussion,                   // 0xec9c1381 pluma: task 048
	-1984581655: Predicate_channel_editPhoto,                        // 0x89b5afe9 pluma: task 044
	-521821973:  Predicate_channel_readMentions,                     // 0xe0e5a0eb pluma: task 043
	1865304106:  Predicate_channel_getUnreadMentions,                // 0x6f2e482a pluma: task 043
	2055458774:  Predicate_channel_readMessageContents,              // 0x7a83cfd6 pluma: task 043
	471620827:   Predicate_channel_search,                           // 0x1c1c5cdb pluma: task 043
	860909233:   Predicate_channel_editCreator,                      // 0x33506eb1 pluma: task 042
	1274612374:  Predicate_channel_editAdmin,                        // 0x4bf90a96 pluma: task 042
	-1457424068: Predicate_channel_editBanned,                       // 0xa921793c pluma: task 042
	1263700059:  Predicate_channel_editDefaultBannedRights,          // 0x4b52885b pluma: task 042
	-1502604851: Predicate_immutableChannel,                         // 0xa67011cd
	675582471:   Predicate_immutableChannelParticipant,              // 0x28449207
	295422623:   Predicate_channel_toggleJoinRequest,                // 0x119bca9f pluma: task 041
	1117566284:  Predicate_channel_exportChatInvite,                 // 0x429cb54c pluma: task 041
	1706919575:  Predicate_channel_getExportedChatInvite,            // 0x65bd8697 pluma: task 041
	-1412847419: Predicate_channel_getExportedChatInvites,           // 0xabc9a8c5 pluma: task 041
	1438538611:  Predicate_channel_editExportedChatInvite,           // 0x55be5b73 pluma: task 041
	714654860:   Predicate_channel_deleteExportedChatInvite,         // 0x2a98c48c pluma: task 041
	-692237588:  Predicate_channel_deleteRevokedExportedChatInvites, // 0xd6bd4aec pluma: task 041
	1965843750:  Predicate_channel_getAdminsWithInvites,             // 0x752c6526 pluma: task 041
	1917986431:  Predicate_channel_getChatInviteImporters,           // 0x7252267f pluma: task 041
	-1624124263: Predicate_channel_hideChatJoinRequests,             // 0x9f31d499 pluma: task 041
	2040559942:  Predicate_channel_getPendingJoinRequests,           // 0x79a07946 pluma: task 041
	-2063367371: Predicate_channel_getLeftChannels,                  // 0x85038335 pluma: task 041
	1532314294:  Predicate_channel_createChannel,                    // 0x5b5542b6 pluma: task 038
	1073186003:  Predicate_channel_toggleForum,                      // 0x3ff784d3 pluma: task 060
	-1875771675: Predicate_channel_toggleViewForumAsMessages,        // 0x9031fee5 pluma: task 060
	-610599565:  Predicate_channel_createForumTopic,                 // 0xdb9afd73 pluma: task 060
	1853047093:  Predicate_channel_editForumTopic,                   // 0x6e734135 pluma: task 060
	-110001806:  Predicate_channel_updatePinnedForumTopic,           // 0xf9718172 pluma: task 060
	-1269591301: Predicate_channel_reorderPinnedForumTopics,         // 0xb45392fb pluma: task 060
	62210273:    Predicate_channel_deleteTopicHistory,               // 0x03b540e1 pluma: task 060
	-553446363:  Predicate_channel_getForumTopics,                   // 0xdf031425 pluma: task 060
	1892057379:  Predicate_channel_getForumTopicsByID,               // 0x70c68123 pluma: task 060
	-2126242873: Predicate_channel_searchTopic,                      // 0x81441bc7 pluma: task 060
	622787338: Predicate_channel_searchCount, // 0x251efb0a pluma: task 066
	-16614681: Predicate_channel_searchCalendar, // 0xff027ae7 pluma: task 066
	-1144250748: Predicate_channel_searchPositions, // 0xbbcc1e84 pluma: task 066
	-1564360787: Predicate_channel_searchAcross, // 0xa2c1bfad pluma: task 066
	-701570663:  Predicate_channel_readTopicMentions,                // 0xd62ee199 pluma: task 060
	-1405087645: Predicate_channel_unpinAllTopicMessages,            // 0xac401063 pluma: task 060
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
