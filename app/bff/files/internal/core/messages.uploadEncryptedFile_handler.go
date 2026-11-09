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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/secretchat"

	"google.golang.org/grpc/status"
)

// MessagesUploadEncryptedFile
// messages.uploadEncryptedFile#5057c497 peer:InputEncryptedChat file:InputEncryptedFile = EncryptedFile;
//
// The file is stored for the chat without a message: the clients reference
// it later with inputEncryptedFile.
func (c *FilesCore) MessagesUploadEncryptedFile(in *mtproto.TLMessagesUploadEncryptedFile) (*mtproto.EncryptedFile, error) {
	store := c.svcCtx.Dao.SecretChats
	_, err := store.Check(c.ctx, secretchat.Device{UserId: c.MD.UserId, AuthKey: c.MD.PermAuthKeyId}, in.GetPeer().GetChatId(), in.GetPeer().GetAccessHash())
	if err != nil {
		switch {
		case errors.Is(err, secretchat.ErrChatNotFound):
			return nil, mtproto.ErrChatIdInvalid
		case errors.Is(err, secretchat.ErrWrongDevice):
			return nil, mtproto.ErrEncryptionIdInvalid
		}
		c.Logger.Errorf("messages.uploadEncryptedFile - %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	file, err := store.ResolveFile(c.ctx, c.svcCtx.Dao.DfsClient, c.MD.PermAuthKeyId, in.GetFile())
	if err != nil {
		c.Logger.Errorf("messages.uploadEncryptedFile - %v", err)
		if _, ok := status.FromError(err); ok {
			return nil, err
		}
		return nil, mtproto.ErrInternalServerError
	}
	return file.Encrypted(), nil
}
