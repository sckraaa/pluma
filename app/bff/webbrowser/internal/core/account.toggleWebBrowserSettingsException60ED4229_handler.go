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

// AccountToggleWebBrowserSettingsException60ED4229
// account.toggleWebBrowserSettingsException#60ed4229 flags:# delete:flags.1?true open_external_browser:flags.0?Bool url:string = Updates;
//
// The answer carries updateWebBrowserException, the other devices get the
// same update.
func (c *WebBrowserCore) AccountToggleWebBrowserSettingsException60ED4229(in *mtproto.TLAccountToggleWebBrowserSettingsException60ED4229) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.toggleWebBrowserSettingsException - error: %v", err)
		return nil, err
	}
	update, err := c.toggle(in.GetDelete(), in.GetOpenExternalBrowser(), in.GetUrl())
	if err != nil {
		c.Logger.Errorf("account.toggleWebBrowserSettingsException - error: %v", err)
		return nil, err
	}
	c.pushOthers("account.toggleWebBrowserSettingsException", mtproto.MakeUpdatesByUpdates(update))
	return mtproto.MakeUpdatesByUpdates(update), nil
}
