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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/core"
)

// The temporary links to a profile (RPCProfileLinks, task 084).

var _ mtproto.RPCProfileLinksServer = (*Service)(nil)

// ContactsExportContactToken
// contacts.exportContactToken#f8654027 = ExportedContactToken;
func (s *Service) ContactsExportContactToken(ctx context.Context, request *mtproto.TLContactsExportContactToken) (*mtproto.ExportedContactToken, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("contacts.exportContactToken - metadata: {%s}", c.MD)
	return c.ContactsExportContactToken(request)
}

// ContactsImportContactToken
// contacts.importContactToken#13005788 token:string = User;
//
// The token is not logged: whoever reads it within its time opens the
// profile.
func (s *Service) ContactsImportContactToken(ctx context.Context, request *mtproto.TLContactsImportContactToken) (*mtproto.User, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("contacts.importContactToken - metadata: {%s}", c.MD)
	return c.ContactsImportContactToken(request)
}
