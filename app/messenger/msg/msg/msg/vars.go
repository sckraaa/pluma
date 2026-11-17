// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package msg

const (
	EditTypeNormal   = 0
	EditTypePoll     = 1
	EditTypeReaction = 2
	// EditTypeViaBot is a bot's edit of a message a user sent via it
	// (messages.editInlineBotMessage, task 106): user_id is the sender, in
	// whose name the message changes, and new_message keeps the message's
	// via_bot_id, the bot. No session of the sender asked for it, so
	// auth_key_id is 0 and every one of them gets the edit.
	EditTypeViaBot = 3
)
