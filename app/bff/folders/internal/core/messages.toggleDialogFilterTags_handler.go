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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// MessagesToggleDialogFilterTags
// messages.toggleDialogFilterTags#fd2dda49 enabled:Bool = Bool;
//
// Turns the folder tags on or off. Tags are a Premium feature: turning
// them on without Premium answers PREMIUM_ACCOUNT_REQUIRED, as Telegram
// does; turning them off is always allowed. The choice stays when Premium
// ends, and tags show again when it comes back. The user's other devices
// get updateDialogFilters and ask for the folders again.
func (c *FoldersCore) MessagesToggleDialogFilterTags(in *mtproto.TLMessagesToggleDialogFilterTags) (*mtproto.Bool, error) {
	enabled := mtproto.FromBool(in.GetEnabled())
	if enabled {
		premium, err := c.premium()
		if err != nil {
			c.Logger.Errorf("messages.toggleDialogFilterTags - user %d: %v", c.MD.UserId, err)
			return nil, err
		}
		if !premium {
			err = mtproto.ErrPremiumAccountRequired
			c.Logger.Errorf("messages.toggleDialogFilterTags - user %d: %v", c.MD.UserId, err)
			return nil, err
		}
	}
	if _, err := c.svcCtx.Dao.DialogClient.DialogToggleDialogFilterTags(c.ctx, &dialog.TLDialogToggleDialogFilterTags{
		UserId:  c.MD.UserId,
		Enabled: mtproto.ToBool(enabled),
	}); err != nil {
		c.Logger.Errorf("messages.toggleDialogFilterTags - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	c.pushToOtherDevices(mtproto.MakeTLUpdateDialogFilters(nil).To_Update())
	return mtproto.BoolTrue, nil
}
