/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2025-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: Benqi (wubenqi@gmail.com)
 */

package msg

const (
	Predicate_sender                     = "sender"
	Predicate_outboxMessage              = "outboxMessage"
	Predicate_contentMessage             = "contentMessage"
	Predicate_msg_pushUserMessage        = "msg_pushUserMessage"
	Predicate_msg_readMessageContents    = "msg_readMessageContents"
	Predicate_msg_sendMessageV2          = "msg_sendMessageV2"
	Predicate_msg_editMessageV2          = "msg_editMessageV2"
	Predicate_msg_deleteMessages         = "msg_deleteMessages"
	Predicate_msg_deleteHistory          = "msg_deleteHistory"
	Predicate_msg_deletePhoneCallHistory = "msg_deletePhoneCallHistory"
	Predicate_msg_deleteChatHistory      = "msg_deleteChatHistory"
	Predicate_msg_readHistoryV2          = "msg_readHistoryV2"
	Predicate_msg_updatePinnedMessage    = "msg_updatePinnedMessage"
	Predicate_msg_unpinAllMessages       = "msg_unpinAllMessages"
	Predicate_msg_readMentions           = "msg_readMentions"
	Predicate_msg_deleteHistoryRange     = "msg_deleteHistoryRange"

	// pluma: task 050
	Predicate_msg_editPeerFolders = "msg_editPeerFolders"
	// pluma: task 053
	Predicate_msg_sendReaction = "msg_sendReaction"
	// pluma: task 053
	Predicate_msg_readReactions = "msg_readReactions"
	// pluma: task 053
	Predicate_msg_updateSavedReactionTag = "msg_updateSavedReactionTag"
	// pluma: task 047
	Predicate_msg_migrateChat = "msg_migrateChat"
	// pluma: task 062
	Predicate_msg_sendVote = "msg_sendVote"
	// pluma: task 062
	Predicate_msg_closePoll = "msg_closePoll"
	// pluma: task 062
	Predicate_msg_addPollAnswer = "msg_addPollAnswer"
	// pluma: task 062
	Predicate_msg_deletePollAnswer = "msg_deletePollAnswer"
	// pluma: task 062
	Predicate_msg_readPollVotes = "msg_readPollVotes"
	// pluma: task 062
	Predicate_msg_toggleTodoCompleted = "msg_toggleTodoCompleted"
	// pluma: task 062
	Predicate_msg_appendTodoList = "msg_appendTodoList"
	// pluma: task 062
	Predicate_msg_editTodoList = "msg_editTodoList"
)

var clazzNameRegisters2 = map[string]map[int]int32{
	Predicate_sender: {
		0: 1513645242, // 0x5a3864ba

	},
	Predicate_outboxMessage: {
		0: 1402283185, // 0x539524b1

	},
	Predicate_contentMessage: {
		0: -1922780877, // 0x8d64b133

	},
	Predicate_msg_pushUserMessage: {
		0: 902887962, // 0x35d0fa1a

	},
	Predicate_msg_readMessageContents: {
		0: 673481940, // 0x282484d4

	},
	Predicate_msg_sendMessageV2: {
		0: -188056380, // 0xf4ca7cc4

	},
	Predicate_msg_editMessageV2: {
		0: 1778278369, // 0x69fe5fe1

	},
	Predicate_msg_deleteMessages: {
		0: 568855069, // 0x21e80a1d

	},
	Predicate_msg_deleteHistory: {
		0: 1975576778, // 0x75c0e8ca

	},
	Predicate_msg_deletePhoneCallHistory: {
		0: 649568574, // 0x26b7a13e

	},
	Predicate_msg_deleteChatHistory: {
		0: -283155749, // 0xef1f62db

	},
	Predicate_msg_readHistoryV2: {
		0: 263827974, // 0xfb9b206

	},
	Predicate_msg_updatePinnedMessage: {
		0: -441560663, // 0xe5ae51a9

	},
	Predicate_msg_unpinAllMessages: {
		0: -1199153371, // 0xb8865f25

	},
	Predicate_msg_readMentions: {
		0: -2049897703, // 0x85d10b19

	},
	Predicate_msg_deleteHistoryRange: {
		0: -2133492186, // 0x80d57e26

	},
	Predicate_msg_editPeerFolders: { // pluma: task 050
		0: -2047878569, // 0x85efda57

	},
	Predicate_msg_sendReaction: { // pluma: task 053
		0: 1297458671, // 0x4d55a5ef

	},
	Predicate_msg_readReactions: { // pluma: task 053
		0: -91564461, // 0xfa8ad653

	},
	Predicate_msg_updateSavedReactionTag: { // pluma: task 053
		0: -1806534101, // 0x94527a2b

	},
	Predicate_msg_migrateChat: { // pluma: task 047
		0: 1705671888, // 0x65aa7cd0

	},
	Predicate_msg_sendVote: { // pluma: task 062
		0: 764721422, // 0x2d94b90e

	},
	Predicate_msg_closePoll: { // pluma: task 062
		0: 512196319, // 0x1e877edf

	},
	Predicate_msg_addPollAnswer: { // pluma: task 062
		0: 660503693, // 0x275e7c8d

	},
	Predicate_msg_deletePollAnswer: { // pluma: task 062
		0: 871616521, // 0x33f3d009

	},
	Predicate_msg_readPollVotes: { // pluma: task 062
		0: 574587703, // 0x223f8337

	},
	Predicate_msg_toggleTodoCompleted: { // pluma: task 062
		0: 2077104412, // 0x7bce191c

	},
	Predicate_msg_appendTodoList: { // pluma: task 062
		0: 1736696679, // 0x6783e367

	},
	Predicate_msg_editTodoList: { // pluma: task 062
		0: 1650536534, // 0x62613056

	},
}

var clazzIdNameRegisters2 = map[int32]string{
	1513645242:  Predicate_sender,                     // 0x5a3864ba
	1402283185:  Predicate_outboxMessage,              // 0x539524b1
	-1922780877: Predicate_contentMessage,             // 0x8d64b133
	902887962:   Predicate_msg_pushUserMessage,        // 0x35d0fa1a
	673481940:   Predicate_msg_readMessageContents,    // 0x282484d4
	-188056380:  Predicate_msg_sendMessageV2,          // 0xf4ca7cc4
	1778278369:  Predicate_msg_editMessageV2,          // 0x69fe5fe1
	568855069:   Predicate_msg_deleteMessages,         // 0x21e80a1d
	1975576778:  Predicate_msg_deleteHistory,          // 0x75c0e8ca
	649568574:   Predicate_msg_deletePhoneCallHistory, // 0x26b7a13e
	-283155749:  Predicate_msg_deleteChatHistory,      // 0xef1f62db
	263827974:   Predicate_msg_readHistoryV2,          // 0xfb9b206
	-441560663:  Predicate_msg_updatePinnedMessage,    // 0xe5ae51a9
	-1199153371: Predicate_msg_unpinAllMessages,       // 0xb8865f25
	-2049897703: Predicate_msg_readMentions,           // 0x85d10b19
	-2133492186: Predicate_msg_deleteHistoryRange,     // 0x80d57e26
	-2047878569: Predicate_msg_editPeerFolders,        // 0x85efda57 pluma: task 050

	1297458671:  Predicate_msg_sendReaction,           // 0x4d55a5ef pluma: task 053
	-91564461:   Predicate_msg_readReactions,          // 0xfa8ad653 pluma: task 053
	-1806534101: Predicate_msg_updateSavedReactionTag, // 0x94527a2b pluma: task 053
	1705671888:  Predicate_msg_migrateChat,            // 0x65aa7cd0 pluma: task 047
	764721422:   Predicate_msg_sendVote,               // 0x2d94b90e pluma: task 062
	512196319:   Predicate_msg_closePoll,              // 0x1e877edf pluma: task 062
	660503693:   Predicate_msg_addPollAnswer,          // 0x275e7c8d pluma: task 062
	871616521:   Predicate_msg_deletePollAnswer,       // 0x33f3d009 pluma: task 062
	574587703:   Predicate_msg_readPollVotes,          // 0x223f8337 pluma: task 062
	2077104412:  Predicate_msg_toggleTodoCompleted,    // 0x7bce191c pluma: task 062
	1736696679:  Predicate_msg_appendTodoList,         // 0x6783e367 pluma: task 062
	1650536534:  Predicate_msg_editTodoList,           // 0x62613056 pluma: task 062
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
