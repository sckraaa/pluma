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
	CRC32_UNKNOWN                    TLConstructor = 0
	CRC32_sender                     TLConstructor = 1513645242  // 0x5a3864ba
	CRC32_outboxMessage              TLConstructor = 1402283185  // 0x539524b1
	CRC32_contentMessage             TLConstructor = -1922780877 // 0x8d64b133
	CRC32_msg_pushUserMessage        TLConstructor = 902887962   // 0x35d0fa1a
	CRC32_msg_readMessageContents    TLConstructor = 673481940   // 0x282484d4
	CRC32_msg_sendMessageV2          TLConstructor = -188056380  // 0xf4ca7cc4
	CRC32_msg_editMessageV2          TLConstructor = 1778278369  // 0x69fe5fe1
	CRC32_msg_deleteMessages         TLConstructor = 568855069   // 0x21e80a1d
	CRC32_msg_deleteHistory          TLConstructor = 1975576778  // 0x75c0e8ca
	CRC32_msg_deletePhoneCallHistory TLConstructor = 649568574   // 0x26b7a13e
	CRC32_msg_deleteChatHistory      TLConstructor = -283155749  // 0xef1f62db
	CRC32_msg_readHistoryV2          TLConstructor = 263827974   // 0xfb9b206
	CRC32_msg_updatePinnedMessage    TLConstructor = -441560663  // 0xe5ae51a9
	CRC32_msg_unpinAllMessages       TLConstructor = -1199153371 // 0xb8865f25
	CRC32_msg_readMentions           TLConstructor = -2049897703 // 0x85d10b19
	CRC32_msg_deleteHistoryRange     TLConstructor = -2133492186 // 0x80d57e26

	// pluma: task 050
	CRC32_msg_editPeerFolders TLConstructor = -2047878569 // 0x85efda57
	// pluma: task 053
	CRC32_msg_sendReaction TLConstructor = 1297458671 // 0x4d55a5ef
	// pluma: task 053
	CRC32_msg_readReactions TLConstructor = -91564461 // 0xfa8ad653
	// pluma: task 053
	CRC32_msg_updateSavedReactionTag TLConstructor = -1806534101 // 0x94527a2b
	// pluma: task 047
	CRC32_msg_migrateChat TLConstructor = 1705671888 // 0x65aa7cd0
	// pluma: task 061
	CRC32_msg_getScheduledHistory TLConstructor = -1158782976 // 0xbaee6000
	// pluma: task 061
	CRC32_msg_getScheduledMessages TLConstructor = -557247872 // 0xdec91280
	// pluma: task 061
	CRC32_msg_sendScheduledMessages TLConstructor = -754764820 // 0xd30333ec
	// pluma: task 061
	CRC32_msg_deleteScheduledMessages TLConstructor = -698200857 // 0xd6624ce7
	// pluma: task 061
	CRC32_msg_hasScheduled TLConstructor = -1139649382 // 0xbc12549a
)
