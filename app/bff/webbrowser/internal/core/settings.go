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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/webbrowser"
)

// settingsOf is s as account.webBrowserSettings, with its hash.
func settingsOf(s webbrowser.Settings) *mtproto.Account_WebBrowserSettings {
	return mtproto.MakeTLAccountWebBrowserSettings(&mtproto.Account_WebBrowserSettings{
		OpenExternalBrowser: s.OpenExternal,
		DisplayCloseButton:  s.DisplayCloseButton,
		ExternalExceptions:  exceptionsOf(s.External),
		InappExceptions:     exceptionsOf(s.InApp),
		Hash:                webbrowser.Hash(s),
	}).To_Account_WebBrowserSettings()
}

func exceptionsOf(list []webbrowser.Exception) []*mtproto.WebDomainException {
	out := make([]*mtproto.WebDomainException, 0, len(list))
	for _, e := range list {
		out = append(out, exceptionOf(e))
	}
	return out
}

// exceptionOf is e as webDomainException. Clients show title large and
// the domain under it (iOS SettingsUI/Sources/Data and
// Storage/WebBrowserSettingsController.swift:292, 308; Android
// ui/web/WebBrowserSettings.java:309-311); the server does not load the
// page for its title and icon, so the title is the domain and there is no
// favicon.
func exceptionOf(e webbrowser.Exception) *mtproto.WebDomainException {
	return mtproto.MakeTLWebDomainException(&mtproto.WebDomainException{
		Domain: e.Domain,
		Url:    e.URL,
		Title:  e.Domain,
	}).To_WebDomainException()
}

// toggle adds the exception of url to the list openExternal names, or,
// with del, removes its domain from that list or, without one, from both
// (account.toggleWebBrowserSettingsException). It returns the update that
// tells a client the change: iOS changes its copy only by the
// updateWebBrowserException of the answer
// (TelegramCore/Sources/Settings/AccountWebBrowserSettings.swift:180-218),
// Android and iOS apply it with the same rules, the domain into one list
// and out of the other (Android MessagesController.java:25672-25700, iOS
// AccountWebBrowserSettings.swift:62-95). A domain that is in no list is
// removed all the same: the answer is the same, so a repeated request does
// no harm.
//
// A new exception for a list that already has webbrowser.MaxExceptions is
// not kept, and toggle returns no update and no error. Android never asks
// for it: it checks the size itself and then just does not add the domain
// (MessagesController.java:25378-25383, ui/ArticleViewer.java:4880). iOS
// has no limit of its own and repeats a request that fails with any error
// but FROZEN_METHOD_INVALID with no bound (TelegramCore/Sources/Network/
// Network.swift:1513-1525), so an error would come back every 5 s until
// the app restarts; an answer with no update leaves its list as it is
// (AccountWebBrowserSettings.swift:174-218).
func (c *WebBrowserCore) toggle(del bool, openExternal *mtproto.Bool, url string) (*mtproto.Update, error) {
	if !del && openExternal == nil {
		// Clients always send one of the two (Android
		// MessagesController.java:25421-25488, iOS
		// AccountWebBrowserSettings.swift:162-172).
		return nil, mtproto.ErrInputRequestInvalid
	}
	e, err := webbrowser.NormalizeURL(url)
	if err != nil {
		return nil, mtproto.ErrUrlInvalid
	}
	if del {
		var list *bool
		if openExternal != nil {
			external := mtproto.FromBool(openExternal)
			list = &external
		}
		if _, err = c.svcCtx.Store.DeleteException(c.ctx, c.MD.UserId, e.Domain, list); err != nil {
			return nil, err
		}
	} else if err = c.svcCtx.Store.PutException(c.ctx, c.MD.UserId, e, mtproto.FromBool(openExternal)); err != nil {
		if errors.Is(err, webbrowser.ErrLimit) {
			return nil, nil
		}
		return nil, err
	}
	return exceptionUpdate(del, openExternal, e), nil
}

func exceptionUpdate(del bool, openExternal *mtproto.Bool, e webbrowser.Exception) *mtproto.Update {
	return mtproto.MakeTLUpdateWebBrowserException(&mtproto.Update{
		Delete:                       del,
		OpenExternalBrowser_FLAGBOOL: openExternal,
		Exception_WEBDOMAINEXCEPTION: exceptionOf(e),
	}).To_Update()
}

// pushOthers sends updates to the account's other devices, pushAll to all
// of them, without seq, as the drafts do (messages.saveDraft): a lost push
// is made good by the hourly read with the hash of both clients that use
// these settings (Android MessagesController.java:25408-25419, iOS
// TelegramCore/Sources/State/AccountTaskManager.swift:146). The change is
// already kept, so a push that fails is logged and the request succeeds.
func (c *WebBrowserCore) pushOthers(method string, updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       updates,
	}); err != nil {
		c.Logger.Errorf("%s - push to the other devices: %v", method, err)
	}
}

func (c *WebBrowserCore) pushAll(method string, updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncPushUpdates(c.ctx, &sync.TLSyncPushUpdates{
		UserId:  c.MD.UserId,
		Updates: updates,
	}); err != nil {
		c.Logger.Errorf("%s - push to the devices: %v", method, err)
	}
}
