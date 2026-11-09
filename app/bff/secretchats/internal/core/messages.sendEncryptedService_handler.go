// Copyright 2026 The Pluma Authors
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

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// MessagesSendEncryptedService
// messages.sendEncryptedService#32d439a4 peer:InputEncryptedChat random_id:long data:bytes = messages.SentEncryptedMessage;
func (c *SecretChatsCore) MessagesSendEncryptedService(in *mtproto.TLMessagesSendEncryptedService) (*mtproto.Messages_SentEncryptedMessage, error) {
	sent, err := c.send(&sendInput{name: "messages.sendEncryptedService", peer: in.GetPeer(), randomId: in.GetRandomId(), data: in.GetData(), service: true})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesSentEncryptedMessage(&mtproto.Messages_SentEncryptedMessage{Date: sent.Date}).To_Messages_SentEncryptedMessage(), nil
}
