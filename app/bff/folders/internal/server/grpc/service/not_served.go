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
)

// The methods of mtproto.RPCFolders the server does not serve: the archive
// (folders.*) and shared folders (chatlists.*), upstream enterprise
// features. They answer ERR_ENTERPRISE_IS_BLOCKED (400), as before the
// folders were served here: a client gives up on a 400, while it retries
// an internal error.

func (s *Service) FoldersEditPeerFolders(ctx context.Context, request *mtproto.TLFoldersEditPeerFolders) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsExportChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsExportChatlistInvite) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsDeleteExportedInvite(ctx context.Context, request *mtproto.TLChatlistsDeleteExportedInvite) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsEditExportedInvite(ctx context.Context, request *mtproto.TLChatlistsEditExportedInvite) (*mtproto.ExportedChatlistInvite, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsGetExportedInvites(ctx context.Context, request *mtproto.TLChatlistsGetExportedInvites) (*mtproto.Chatlists_ExportedInvites, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsCheckChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsCheckChatlistInvite) (*mtproto.Chatlists_ChatlistInvite, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsJoinChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsJoinChatlistInvite) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsGetChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsGetChatlistUpdates) (*mtproto.Chatlists_ChatlistUpdates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsJoinChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsJoinChatlistUpdates) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsHideChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsHideChatlistUpdates) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsGetLeaveChatlistSuggestions(ctx context.Context, request *mtproto.TLChatlistsGetLeaveChatlistSuggestions) (*mtproto.Vector_Peer, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) ChatlistsLeaveChatlist(ctx context.Context, request *mtproto.TLChatlistsLeaveChatlist) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) FoldersDeleteFolder(ctx context.Context, request *mtproto.TLFoldersDeleteFolder) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
