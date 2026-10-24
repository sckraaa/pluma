// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

// MessagesEditChatCreator
// messages.editChatCreator#f743b857 peer:InputPeer user_id:InputUser password:InputCheckPasswordSRP = Updates;
//
// The transfer of a chat's ownership is not served (an upstream enterprise
// feature, ERR_ENTERPRISE_IS_BLOCKED). The 2FA password it takes is checked
// first with the shared code, as Telegram does, so clients go through their
// usual steps: PASSWORD_MISSING without a password (the user is asked to set
// one), PASSWORD_HASH_INVALID for inputCheckPasswordEmpty or a wrong one
// (the user is asked for it), FLOOD_WAIT_X past the limits.
func (c *ChatsCore) MessagesEditChatCreator(in *mtproto.TLMessagesEditChatCreator) (*mtproto.Updates, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if err := c.svcCtx.Twofa.Check(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId, in.GetPassword()); err != nil {
		if errors.Is(err, twofa.ErrNoPassword) {
			err = mtproto.ErrPasswordMissing
		}
		c.Logger.Errorf("messages.editChatCreator - user %d: %v", c.MD.UserId, err)
		return nil, err
	}

	return nil, mtproto.ErrEnterpriseIsBlocked
}
