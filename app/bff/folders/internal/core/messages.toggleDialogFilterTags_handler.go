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

// MessagesToggleDialogFilterTags
// messages.toggleDialogFilterTags#fd2dda49 enabled:Bool = Bool;
//
// Folder tags are a Premium feature: turning them on answers
// PREMIUM_ACCOUNT_REQUIRED, as Telegram does for a user without Premium;
// turning them off changes nothing, they are off.
func (c *FoldersCore) MessagesToggleDialogFilterTags(in *mtproto.TLMessagesToggleDialogFilterTags) (*mtproto.Bool, error) {
	if mtproto.FromBool(in.GetEnabled()) {
		err := mtproto.ErrPremiumAccountRequired
		c.Logger.Errorf("messages.toggleDialogFilterTags - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
