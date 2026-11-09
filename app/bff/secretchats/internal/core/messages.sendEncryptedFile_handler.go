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

// MessagesSendEncryptedFile
// messages.sendEncryptedFile#5559481d flags:# silent:flags.0?true peer:InputEncryptedChat random_id:long data:bytes file:InputEncryptedFile = messages.SentEncryptedMessage;
func (c *SecretChatsCore) MessagesSendEncryptedFile(in *mtproto.TLMessagesSendEncryptedFile) (*mtproto.Messages_SentEncryptedMessage, error) {
	if in.GetFile() == nil {
		return nil, mtproto.ErrFileEmtpy
	}
	sent, err := c.send(&sendInput{name: "messages.sendEncryptedFile", peer: in.GetPeer(), randomId: in.GetRandomId(), data: in.GetData(), file: in.GetFile()})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesSentEncryptedFile(&mtproto.Messages_SentEncryptedMessage{
		Date: sent.Date,
		File: sent.File.Encrypted(),
	}).To_Messages_SentEncryptedMessage(), nil
}
