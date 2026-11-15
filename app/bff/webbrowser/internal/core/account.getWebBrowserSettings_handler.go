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
	"github.com/teamgram/teamgram-server/pkg/webbrowser"
)

// AccountGetWebBrowserSettings
// account.getWebBrowserSettings#56655768 hash:long = account.WebBrowserSettings;
//
// iOS asks on start and every hour with the hash of its copy
// (TelegramCore/Sources/State/ManagedWebBrowserSettingsUpdates.swift:7-15,
// Settings/AccountWebBrowserSettings.swift:133-140), and repeats the
// request after any error without end
// (TelegramCore/Sources/Network/Network.swift:1513-1525); Android keeps a
// copy it reads again every hour (MessagesController.java:25392-25419).
// The same hash: account.webBrowserSettingsNotModified.
func (c *WebBrowserCore) AccountGetWebBrowserSettings(in *mtproto.TLAccountGetWebBrowserSettings) (*mtproto.Account_WebBrowserSettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.getWebBrowserSettings - error: %v", err)
		return nil, err
	}
	s, err := c.svcCtx.Store.Get(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getWebBrowserSettings - error: %v", err)
		return nil, err
	}
	if in.GetHash() == webbrowser.Hash(s) {
		return mtproto.MakeTLAccountWebBrowserSettingsNotModified(nil).To_Account_WebBrowserSettings(), nil
	}
	return settingsOf(s), nil
}
