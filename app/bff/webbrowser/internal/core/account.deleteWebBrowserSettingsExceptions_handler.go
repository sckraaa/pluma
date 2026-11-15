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

// AccountDeleteWebBrowserSettingsExceptions
// account.deleteWebBrowserSettingsExceptions#86a0765d = account.WebBrowserSettings;
//
// Both lists are emptied, the flags stay. Android takes the answer as its
// copy (MessagesController.java:25491-25506); the other devices get an
// updateWebBrowserException{delete} for each domain removed, in one push.
func (c *WebBrowserCore) AccountDeleteWebBrowserSettingsExceptions(in *mtproto.TLAccountDeleteWebBrowserSettingsExceptions) (*mtproto.Account_WebBrowserSettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("account.deleteWebBrowserSettingsExceptions - error: %v", err)
		return nil, err
	}
	removed, err := c.svcCtx.Store.DeleteAllExceptions(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.deleteWebBrowserSettingsExceptions - error: %v", err)
		return nil, err
	}
	s, err := c.svcCtx.Store.Get(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.deleteWebBrowserSettingsExceptions - error: %v", err)
		return nil, err
	}
	var updates []*mtproto.Update
	for _, list := range []struct {
		exceptions []webbrowser.Exception
		external   *mtproto.Bool
	}{{removed.External, mtproto.BoolTrue}, {removed.InApp, mtproto.BoolFalse}} {
		for _, e := range list.exceptions {
			updates = append(updates, exceptionUpdate(true, list.external, e))
		}
	}
	if len(updates) > 0 {
		c.pushOthers("account.deleteWebBrowserSettingsExceptions", mtproto.MakeUpdatesByUpdates(updates...))
	}
	return settingsOf(s), nil
}
