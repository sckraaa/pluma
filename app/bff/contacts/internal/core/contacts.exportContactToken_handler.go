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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// ContactsExportContactToken
// contacts.exportContactToken#f8654027 = ExportedContactToken;
//
// A temporary link to the profile of the account, which the clients show
// as a QR code and as a link, with the time it expires: they count down to
// it and ask for the next one (tdesktop info_profile_qr_link.cpp:197-265,
// iOS ChatQrCodeScreen.swift:1109-1136, Android
// MessagesController.java:23598-23622). The link is of the link host, which
// opens it in the application (app/interface/links, "contact/<token>").
func (c *ContactsCore) ContactsExportContactToken(in *mtproto.TLContactsExportContactToken) (*mtproto.ExportedContactToken, error) {
	token, expires, err := c.svcCtx.Dao.Tokens.Export(c.ctx, c.MD.UserId, time.Now().Unix())
	if err != nil {
		c.Logger.Errorf("contacts.exportContactToken - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLExportedContactToken(&mtproto.ExportedContactToken{
		Url:     "https://" + env2.TDotMe + "/contact/" + token,
		Expires: int32(expires),
	}).To_ExportedContactToken(), nil
}
