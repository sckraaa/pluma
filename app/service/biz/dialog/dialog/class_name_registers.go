/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2025-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: Benqi (wubenqi@gmail.com)
 */

package dialog

const (
	Predicate_updateDraftMessage                       = "updateDraftMessage"
	Predicate_dialogExt                                = "dialogExt"
	Predicate_dialogPinnedExt                          = "dialogPinnedExt"
	Predicate_dialogFilterExt                          = "dialogFilterExt"
	Predicate_simpleDialogsData                        = "simpleDialogsData"
	Predicate_savedDialogList                          = "savedDialogList"
	Predicate_dialog_saveDraftMessage                  = "dialog_saveDraftMessage"
	Predicate_dialog_clearDraftMessage                 = "dialog_clearDraftMessage"
	Predicate_dialog_getAllDrafts                      = "dialog_getAllDrafts"
	Predicate_dialog_clearAllDrafts                    = "dialog_clearAllDrafts"
	Predicate_dialog_markDialogUnread                  = "dialog_markDialogUnread"
	Predicate_dialog_toggleDialogPin                   = "dialog_toggleDialogPin"
	Predicate_dialog_getDialogUnreadMarkList           = "dialog_getDialogUnreadMarkList"
	Predicate_dialog_getDialogsByOffsetDate            = "dialog_getDialogsByOffsetDate"
	Predicate_dialog_getDialogs                        = "dialog_getDialogs"
	Predicate_dialog_getDialogsByIdList                = "dialog_getDialogsByIdList"
	Predicate_dialog_getDialogsCount                   = "dialog_getDialogsCount"
	Predicate_dialog_getPinnedDialogs                  = "dialog_getPinnedDialogs"
	Predicate_dialog_reorderPinnedDialogs              = "dialog_reorderPinnedDialogs"
	Predicate_dialog_getDialogById                     = "dialog_getDialogById"
	Predicate_dialog_getTopMessage                     = "dialog_getTopMessage"
	Predicate_dialog_getUserPinnedMessage              = "dialog_getUserPinnedMessage"
	Predicate_dialog_updateUserPinnedMessage           = "dialog_updateUserPinnedMessage"
	Predicate_dialog_insertOrUpdateDialogFilter        = "dialog_insertOrUpdateDialogFilter"
	Predicate_dialog_deleteDialogFilter                = "dialog_deleteDialogFilter"
	Predicate_dialog_updateDialogFiltersOrder          = "dialog_updateDialogFiltersOrder"
	Predicate_dialog_getDialogFilters                  = "dialog_getDialogFilters"
	Predicate_dialog_getDialogFolder                   = "dialog_getDialogFolder"
	Predicate_dialog_editPeerFolders                   = "dialog_editPeerFolders"
	Predicate_dialog_getChannelMessageReadParticipants = "dialog_getChannelMessageReadParticipants"
	Predicate_dialog_setChatTheme                      = "dialog_setChatTheme"
	Predicate_dialog_setHistoryTTL                     = "dialog_setHistoryTTL"
	Predicate_dialog_getMyDialogsData                  = "dialog_getMyDialogsData"
	Predicate_dialog_getSavedDialogs                   = "dialog_getSavedDialogs"
	Predicate_dialog_getPinnedSavedDialogs             = "dialog_getPinnedSavedDialogs"
	Predicate_dialog_toggleSavedDialogPin              = "dialog_toggleSavedDialogPin"
	Predicate_dialog_reorderPinnedSavedDialogs         = "dialog_reorderPinnedSavedDialogs"
	Predicate_dialog_getDialogFilter                   = "dialog_getDialogFilter"
	Predicate_dialog_getDialogFilterBySlug             = "dialog_getDialogFilterBySlug"
	Predicate_dialog_createDialogFilter                = "dialog_createDialogFilter"
	Predicate_dialog_toggleDialogFilterTags            = "dialog_toggleDialogFilterTags"
	Predicate_dialog_getDialogFilterTags               = "dialog_getDialogFilterTags"
	Predicate_dialog_setChatWallpaper                  = "dialog_setChatWallpaper"
	// pluma: task 063
	Predicate_dialog_saveThreadDraft = "dialog_saveThreadDraft"
	// pluma: task 064
	Predicate_chatlistInviteExt           = "chatlistInviteExt"
	Predicate_dialog_exportChatlistInvite = "dialog_exportChatlistInvite"
	Predicate_dialog_editChatlistInvite   = "dialog_editChatlistInvite"
	Predicate_dialog_deleteChatlistInvite = "dialog_deleteChatlistInvite"
	Predicate_dialog_getChatlistInvites   = "dialog_getChatlistInvites"
	Predicate_dialog_getChatlistInvite    = "dialog_getChatlistInvite"
	Predicate_dialog_joinChatlist         = "dialog_joinChatlist"
	Predicate_dialog_hideChatlistUpdates  = "dialog_hideChatlistUpdates"
	Predicate_dialog_setChatWallpaper     = "dialog_setChatWallpaper"
)

var clazzNameRegisters2 = map[string]map[int]int32{
	Predicate_updateDraftMessage: {
		0: -155335502, // 0xf6bdc4b2

	},
	Predicate_dialogExt: {
		0: 1930144063, // 0x730ba93f

	},
	Predicate_dialogPinnedExt: {
		0: 245834284, // 0xea7222c

	},
	Predicate_dialogFilterExt: {
		0: -1496016642, // 0xa6d498fe

	},
	Predicate_simpleDialogsData: {
		0: 492418141, // 0x1d59b45d

	},
	Predicate_savedDialogList: {
		0: 2005919834, // 0x778fe85a

	},
	Predicate_dialog_saveDraftMessage: {
		0: 1321916826, // 0x4ecad99a

	},
	Predicate_dialog_clearDraftMessage: {
		0: -76500326, // 0xfb70b29a

	},
	Predicate_dialog_getAllDrafts: {
		0: -1394716698, // 0xacde4fe6

	},
	Predicate_dialog_clearAllDrafts: {
		0: 1102614780, // 0x41b890fc

	},
	Predicate_dialog_markDialogUnread: {
		0: 1160941838, // 0x4532910e

	},
	Predicate_dialog_toggleDialogPin: {
		0: -2038504145, // 0x867ee52f

	},
	Predicate_dialog_getDialogUnreadMarkList: {
		0: -893634316, // 0xcabc38f4

	},
	Predicate_dialog_getDialogsByOffsetDate: {
		0: -1652652540, // 0x9d7e8604

	},
	Predicate_dialog_getDialogs: {
		0: -2046091754, // 0x860b1e16

	},
	Predicate_dialog_getDialogsByIdList: {
		0: -1390049167, // 0xad258871

	},
	Predicate_dialog_getDialogsCount: {
		0: -533089179, // 0xe039b465

	},
	Predicate_dialog_getPinnedDialogs: {
		0: -1463673931, // 0xa8c21bb5

	},
	Predicate_dialog_reorderPinnedDialogs: {
		0: -18664089, // 0xfee33567

	},
	Predicate_dialog_getDialogById: {
		0: -1587594251, // 0xa15f3bf5

	},
	Predicate_dialog_getTopMessage: {
		0: -92425614, // 0xfa7db272

	},
	Predicate_dialog_getUserPinnedMessage: {
		0: -1885617487, // 0x8f9bc2b1

	},
	Predicate_dialog_updateUserPinnedMessage: {
		0: 371388970, // 0x1622f22a

	},
	Predicate_dialog_insertOrUpdateDialogFilter: {
		0: 178824068, // 0xaa8a384

	},
	Predicate_dialog_deleteDialogFilter: {
		0: 31276695, // 0x1dd3e97

	},
	Predicate_dialog_updateDialogFiltersOrder: {
		0: -1321465025, // 0xb13c0b3f

	},
	Predicate_dialog_getDialogFilters: {
		0: 1818717244, // 0x6c676c3c

	},
	Predicate_dialog_getDialogFolder: {
		0: 1092325045, // 0x411b8eb5

	},
	Predicate_dialog_editPeerFolders: {
		0: 608601754, // 0x2446869a

	},
	Predicate_dialog_getChannelMessageReadParticipants: {
		0: 683494715, // 0x28bd4d3b

	},
	Predicate_dialog_setChatTheme: {
		0: -374431190, // 0xe9aea22a

	},
	Predicate_dialog_setHistoryTTL: {
		0: 165263532, // 0x9d9b8ac

	},
	Predicate_dialog_getMyDialogsData: {
		0: 2128645891, // 0x7ee08f03

	},
	Predicate_dialog_getSavedDialogs: {
		0: 952227432, // 0x38c1d668

	},
	Predicate_dialog_getPinnedSavedDialogs: {
		0: 1084471271, // 0x40a3b7e7

	},
	Predicate_dialog_toggleSavedDialogPin: {
		0: 1156782041, // 0x44f317d9

	},
	Predicate_dialog_reorderPinnedSavedDialogs: {
		0: -665007150, // 0xd85ccbd2

	},
	Predicate_dialog_getDialogFilter: {
		0: -209189348, // 0xf388061c

	},
	Predicate_dialog_getDialogFilterBySlug: {
		0: 1313177583, // 0x4e457fef

	},
	Predicate_dialog_createDialogFilter: {
		0: -959749265, // 0xc6cb636f

	},
	Predicate_dialog_toggleDialogFilterTags: {
		0: -1597149815, // 0xa0cd6d89

	},
	Predicate_dialog_getDialogFilterTags: {
		0: -84870505, // 0xfaf0fa97

	},
	Predicate_chatlistInviteExt: { // pluma: task 064
		0: 845724500, // 0x3268bb54

	},
	Predicate_dialog_exportChatlistInvite: { // pluma: task 064
		0: -342736050, // 0xeb92434e

	},
	Predicate_dialog_editChatlistInvite: { // pluma: task 064
		0: 167718874, // 0x09ff2fda

	},
	Predicate_dialog_deleteChatlistInvite: { // pluma: task 064
		0: -1495566841, // 0xa6db7607

	},
	Predicate_dialog_getChatlistInvites: { // pluma: task 064
		0: -1799858035, // 0x94b8588d

	},
	Predicate_dialog_getChatlistInvite: { // pluma: task 064
		0: -992271428, // 0xc4db23bc

	},
	Predicate_dialog_joinChatlist: { // pluma: task 064
		0: -209386231, // 0xf3850509

	},
	Predicate_dialog_hideChatlistUpdates: { // pluma: task 064
		0: -2122372205, // 0x817f2b93

	},
	Predicate_dialog_setChatWallpaper: {
		0: -1252926702, // 0xb551db12

	},
	Predicate_dialog_saveThreadDraft: { // pluma: task 063
		0: -416704029, // 0xe72999e3

	},
}

var clazzIdNameRegisters2 = map[int32]string{
	-155335502:  Predicate_updateDraftMessage,                       // 0xf6bdc4b2
	1930144063:  Predicate_dialogExt,                                // 0x730ba93f
	245834284:   Predicate_dialogPinnedExt,                          // 0xea7222c
	-1496016642: Predicate_dialogFilterExt,                          // 0xa6d498fe
	492418141:   Predicate_simpleDialogsData,                        // 0x1d59b45d
	2005919834:  Predicate_savedDialogList,                          // 0x778fe85a
	1321916826:  Predicate_dialog_saveDraftMessage,                  // 0x4ecad99a
	-76500326:   Predicate_dialog_clearDraftMessage,                 // 0xfb70b29a
	-1394716698: Predicate_dialog_getAllDrafts,                      // 0xacde4fe6
	1102614780:  Predicate_dialog_clearAllDrafts,                    // 0x41b890fc
	1160941838:  Predicate_dialog_markDialogUnread,                  // 0x4532910e
	-2038504145: Predicate_dialog_toggleDialogPin,                   // 0x867ee52f
	-893634316:  Predicate_dialog_getDialogUnreadMarkList,           // 0xcabc38f4
	-1652652540: Predicate_dialog_getDialogsByOffsetDate,            // 0x9d7e8604
	-2046091754: Predicate_dialog_getDialogs,                        // 0x860b1e16
	-1390049167: Predicate_dialog_getDialogsByIdList,                // 0xad258871
	-533089179:  Predicate_dialog_getDialogsCount,                   // 0xe039b465
	-1463673931: Predicate_dialog_getPinnedDialogs,                  // 0xa8c21bb5
	-18664089:   Predicate_dialog_reorderPinnedDialogs,              // 0xfee33567
	-1587594251: Predicate_dialog_getDialogById,                     // 0xa15f3bf5
	-92425614:   Predicate_dialog_getTopMessage,                     // 0xfa7db272
	-1885617487: Predicate_dialog_getUserPinnedMessage,              // 0x8f9bc2b1
	371388970:   Predicate_dialog_updateUserPinnedMessage,           // 0x1622f22a
	178824068:   Predicate_dialog_insertOrUpdateDialogFilter,        // 0xaa8a384
	31276695:    Predicate_dialog_deleteDialogFilter,                // 0x1dd3e97
	-1321465025: Predicate_dialog_updateDialogFiltersOrder,          // 0xb13c0b3f
	1818717244:  Predicate_dialog_getDialogFilters,                  // 0x6c676c3c
	1092325045:  Predicate_dialog_getDialogFolder,                   // 0x411b8eb5
	608601754:   Predicate_dialog_editPeerFolders,                   // 0x2446869a
	683494715:   Predicate_dialog_getChannelMessageReadParticipants, // 0x28bd4d3b
	-374431190:  Predicate_dialog_setChatTheme,                      // 0xe9aea22a
	165263532:   Predicate_dialog_setHistoryTTL,                     // 0x9d9b8ac
	2128645891:  Predicate_dialog_getMyDialogsData,                  // 0x7ee08f03
	952227432:   Predicate_dialog_getSavedDialogs,                   // 0x38c1d668
	1084471271:  Predicate_dialog_getPinnedSavedDialogs,             // 0x40a3b7e7
	1156782041:  Predicate_dialog_toggleSavedDialogPin,              // 0x44f317d9
	-665007150:  Predicate_dialog_reorderPinnedSavedDialogs,         // 0xd85ccbd2
	-209189348:  Predicate_dialog_getDialogFilter,                   // 0xf388061c
	1313177583:  Predicate_dialog_getDialogFilterBySlug,             // 0x4e457fef
	-959749265:  Predicate_dialog_createDialogFilter,                // 0xc6cb636f
	-1597149815: Predicate_dialog_toggleDialogFilterTags,            // 0xa0cd6d89
	-84870505:   Predicate_dialog_getDialogFilterTags,               // 0xfaf0fa97
	845724500:   Predicate_chatlistInviteExt,                        // 0x3268bb54 pluma: task 064
	-342736050:  Predicate_dialog_exportChatlistInvite,              // 0xeb92434e pluma: task 064
	167718874:   Predicate_dialog_editChatlistInvite,                // 0x09ff2fda pluma: task 064
	-1495566841: Predicate_dialog_deleteChatlistInvite,              // 0xa6db7607 pluma: task 064
	-1799858035: Predicate_dialog_getChatlistInvites,                // 0x94b8588d pluma: task 064
	-992271428:  Predicate_dialog_getChatlistInvite,                 // 0xc4db23bc pluma: task 064
	-209386231:  Predicate_dialog_joinChatlist,                      // 0xf3850509 pluma: task 064
	-2122372205: Predicate_dialog_hideChatlistUpdates,               // 0x817f2b93 pluma: task 064
	-1252926702: Predicate_dialog_setChatWallpaper,                  // 0xb551db12
	-416704029:  Predicate_dialog_saveThreadDraft,                   // 0xe72999e3 pluma: task 063

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
