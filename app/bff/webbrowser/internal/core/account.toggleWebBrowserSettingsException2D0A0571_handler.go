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
	"github.com/teamgram/proto/mtproto"
)

// AccountToggleWebBrowserSettingsException2D0A0571
// account.toggleWebBrowserSettingsException#2d0a0571 flags:# delete:flags.1?true open_external_browser:flags.0?Bool url:string = Bool;
//
// The constructor before #60ed4229, with the same meaning; none of the
// clients we build sends it. The answer has no updates, so every device of
// the account, this one too, gets updateWebBrowserException by push. An
// exception refused for a full list changes nothing: boolFalse and no push
// (toggle).
func (c *WebBrowserCore) AccountToggleWebBrowserSettingsException2D0A0571(in *mtproto.TLAccountToggleWebBrowserSettingsException2D0A0571) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.toggleWebBrowserSettingsException#2d0a0571 - error: %v", err)
		return nil, err
	}
	update, err := c.toggle(in.GetDelete(), in.GetOpenExternalBrowser(), in.GetUrl())
	if err != nil {
		c.Logger.Errorf("account.toggleWebBrowserSettingsException#2d0a0571 - error: %v", err)
		return nil, err
	}
	if update == nil {
		return mtproto.BoolFalse, nil
	}
	c.pushAll("account.toggleWebBrowserSettingsException#2d0a0571", mtproto.MakeUpdatesByUpdates(update))
	return mtproto.BoolTrue, nil
}
