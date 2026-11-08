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
	"github.com/teamgram/teamgram-server/app/bff/folders/internal/core"
)

// ChatlistsExportChatlistInvite
// chatlists.exportChatlistInvite#8472478e chatlist:InputChatlist title:string peers:Vector<InputPeer> = chatlists.ExportedChatlistInvite;
func (s *Service) ChatlistsExportChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsExportChatlistInvite) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.exportChatlistInvite - metadata: {%s}", c.MD)
	return c.ChatlistsExportChatlistInvite(request)
}

// ChatlistsDeleteExportedInvite
// chatlists.deleteExportedInvite#719c5c5e chatlist:InputChatlist slug:string = Bool;
func (s *Service) ChatlistsDeleteExportedInvite(ctx context.Context, request *mtproto.TLChatlistsDeleteExportedInvite) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.deleteExportedInvite - metadata: {%s}", c.MD)
	return c.ChatlistsDeleteExportedInvite(request)
}

// ChatlistsEditExportedInvite
// chatlists.editExportedInvite#653db63d flags:# chatlist:InputChatlist slug:string title:flags.1?string peers:flags.2?Vector<InputPeer> = ExportedChatlistInvite;
func (s *Service) ChatlistsEditExportedInvite(ctx context.Context, request *mtproto.TLChatlistsEditExportedInvite) (*mtproto.ExportedChatlistInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.editExportedInvite - metadata: {%s}", c.MD)
	return c.ChatlistsEditExportedInvite(request)
}

// ChatlistsGetExportedInvites
// chatlists.getExportedInvites#ce03da83 chatlist:InputChatlist = chatlists.ExportedInvites;
func (s *Service) ChatlistsGetExportedInvites(ctx context.Context, request *mtproto.TLChatlistsGetExportedInvites) (*mtproto.Chatlists_ExportedInvites, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.getExportedInvites - metadata: {%s}", c.MD)
	return c.ChatlistsGetExportedInvites(request)
}

// ChatlistsCheckChatlistInvite
// chatlists.checkChatlistInvite#41c10fff slug:string = chatlists.ChatlistInvite;
func (s *Service) ChatlistsCheckChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsCheckChatlistInvite) (*mtproto.Chatlists_ChatlistInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.checkChatlistInvite - metadata: {%s}", c.MD)
	return c.ChatlistsCheckChatlistInvite(request)
}

// ChatlistsJoinChatlistInvite
// chatlists.joinChatlistInvite#a6b1e39a slug:string peers:Vector<InputPeer> = Updates;
func (s *Service) ChatlistsJoinChatlistInvite(ctx context.Context, request *mtproto.TLChatlistsJoinChatlistInvite) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.joinChatlistInvite - metadata: {%s}", c.MD)
	return c.ChatlistsJoinChatlistInvite(request)
}

// ChatlistsGetChatlistUpdates
// chatlists.getChatlistUpdates#89419521 chatlist:InputChatlist = chatlists.ChatlistUpdates;
func (s *Service) ChatlistsGetChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsGetChatlistUpdates) (*mtproto.Chatlists_ChatlistUpdates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.getChatlistUpdates - metadata: {%s}", c.MD)
	return c.ChatlistsGetChatlistUpdates(request)
}

// ChatlistsJoinChatlistUpdates
// chatlists.joinChatlistUpdates#e089f8f5 chatlist:InputChatlist peers:Vector<InputPeer> = Updates;
func (s *Service) ChatlistsJoinChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsJoinChatlistUpdates) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.joinChatlistUpdates - metadata: {%s}", c.MD)
	return c.ChatlistsJoinChatlistUpdates(request)
}

// ChatlistsHideChatlistUpdates
// chatlists.hideChatlistUpdates#66e486fb chatlist:InputChatlist = Bool;
func (s *Service) ChatlistsHideChatlistUpdates(ctx context.Context, request *mtproto.TLChatlistsHideChatlistUpdates) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.hideChatlistUpdates - metadata: {%s}", c.MD)
	return c.ChatlistsHideChatlistUpdates(request)
}

// ChatlistsGetLeaveChatlistSuggestions
// chatlists.getLeaveChatlistSuggestions#fdbcd714 chatlist:InputChatlist = Vector<Peer>;
func (s *Service) ChatlistsGetLeaveChatlistSuggestions(ctx context.Context, request *mtproto.TLChatlistsGetLeaveChatlistSuggestions) (*mtproto.Vector_Peer, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.getLeaveChatlistSuggestions - metadata: {%s}", c.MD)
	return c.ChatlistsGetLeaveChatlistSuggestions(request)
}

// ChatlistsLeaveChatlist
// chatlists.leaveChatlist#74fae13a chatlist:InputChatlist peers:Vector<InputPeer> = Updates;
func (s *Service) ChatlistsLeaveChatlist(ctx context.Context, request *mtproto.TLChatlistsLeaveChatlist) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("chatlists.leaveChatlist - metadata: {%s}", c.MD)
	return c.ChatlistsLeaveChatlist(request)
}
