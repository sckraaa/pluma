// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// MessagesReceivedMessages
// messages.receivedMessages#5a954c0 max_id:int = Vector<ReceivedNotifyMessage>;
//
// "Confirms receipt of messages by a client, cancels PUSH-notification
// sending"; the answer is the messages "for which PUSH-notifications were
// cancelled" (core.telegram.org/method/messages.receivedMessages,
// constructor/receivedNotifyMessage). The server holds no push back to
// cancel: a push goes out at once to the devices without a live session
// and to none with one (app/messenger/push/internal/core/core.go:233-248),
// so the list of cancelled ones is always empty, and that is the answer.
// Only Web K calls it, on each new highest message id, and does not read
// the answer (lib/appManagers/appMessagesManager.ts:13421-13435).
func (c *MessagesCore) MessagesReceivedMessages(in *mtproto.TLMessagesReceivedMessages) (*mtproto.Vector_ReceivedNotifyMessage, error) {
	if c.MD.GetIsBot() {
		return nil, mtproto.ErrBotMethodInvalid
	}
	if in.GetMaxId() < 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	return &mtproto.Vector_ReceivedNotifyMessage{
		Datas: []*mtproto.ReceivedNotifyMessage{},
	}, nil
}
