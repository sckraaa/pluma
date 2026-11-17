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

// AccountUpdateWebBrowserSettings
// account.updateWebBrowserSettings#9adf82fe flags:# open_external_browser:flags.0?true display_close_button:flags.1?true = account.WebBrowserSettings;
//
// Both clients send both flags and take the answer as their copy (Android
// MessagesController.java:25645-25660, iOS
// AccountWebBrowserSettings.swift:142-160); the other devices get
// updateWebBrowserSettings.
func (c *WebBrowserCore) AccountUpdateWebBrowserSettings(in *mtproto.TLAccountUpdateWebBrowserSettings) (*mtproto.Account_WebBrowserSettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.updateWebBrowserSettings - error: %v", err)
		return nil, err
	}
	if err := c.svcCtx.Store.SetFlags(c.ctx, c.MD.UserId, in.GetOpenExternalBrowser(), in.GetDisplayCloseButton()); err != nil {
		c.Logger.Errorf("account.updateWebBrowserSettings - error: %v", err)
		return nil, err
	}
	s, err := c.svcCtx.Store.Get(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.updateWebBrowserSettings - error: %v", err)
		return nil, err
	}
	c.pushOthers("account.updateWebBrowserSettings", mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateWebBrowserSettings(&mtproto.Update{
		OpenExternalBrowser_FLAGBOOLEAN: s.OpenExternal,
		DisplayCloseButton:              s.DisplayCloseButton,
	}).To_Update()))
	return settingsOf(s), nil
}
