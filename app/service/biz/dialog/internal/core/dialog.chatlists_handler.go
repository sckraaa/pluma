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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dao"
)

// The shared folders (task 064): the handlers are dao.ExportChatlistInvite
// and the rest, one box transaction each (dao/chatlist.go).

// logChatlist logs an error other than an answer of the method.
func (c *DialogCore) logChatlist(method string, err error) error {
	var rpcErr *mtproto.TLRpcError
	if !errors.As(err, &rpcErr) {
		c.Logger.Errorf("%s: %v", method, err)
	}
	return err
}

// DialogExportChatlistInvite
// dialog.exportChatlistInvite user_id:long filter_id:int title:string peers:Vector<InputPeer> migrated_chats:Vector<long> migrated_channels:Vector<long> chats:Vector<Chat> check_only:Bool = chatlists.ExportedChatlistInvite;
func (c *DialogCore) DialogExportChatlistInvite(in *dialog.TLDialogExportChatlistInvite) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	r, err := c.svcCtx.Dao.ExportChatlistInvite(c.ctx, in.UserId, in.FilterId, dao.ChatlistShare{
		Title:            in.Title,
		Peers:            in.Peers,
		MigratedChats:    in.MigratedChats,
		MigratedChannels: in.MigratedChannels,
		Chats:            in.Chats,
		CheckOnly:        mtproto.FromBool(in.CheckOnly),
	})
	if err != nil {
		return nil, c.logChatlist("dialog.exportChatlistInvite", err)
	}
	if r == nil {
		// A check: the answer is not read.
		return mtproto.MakeTLChatlistsExportedChatlistInvite(nil).To_Chatlists_ExportedChatlistInvite(), nil
	}
	return r, nil
}

// DialogEditChatlistInvite
// dialog.editChatlistInvite user_id:long filter_id:int slug:string set_title:Bool title:string set_peers:Bool peers:Vector<InputPeer> migrated_chats:Vector<long> migrated_channels:Vector<long> chats:Vector<Chat> check_only:Bool = ExportedChatlistInvite;
func (c *DialogCore) DialogEditChatlistInvite(in *dialog.TLDialogEditChatlistInvite) (*mtproto.ExportedChatlistInvite, error) {
	r, err := c.svcCtx.Dao.EditChatlistInvite(c.ctx, in.UserId, in.FilterId, in.Slug, mtproto.FromBool(in.SetTitle), in.Title, mtproto.FromBool(in.SetPeers), dao.ChatlistShare{
		Peers:            in.Peers,
		MigratedChats:    in.MigratedChats,
		MigratedChannels: in.MigratedChannels,
		Chats:            in.Chats,
		CheckOnly:        mtproto.FromBool(in.CheckOnly),
	})
	if err != nil {
		return nil, c.logChatlist("dialog.editChatlistInvite", err)
	}
	if r == nil {
		return mtproto.MakeTLExportedChatlistInvite(nil).To_ExportedChatlistInvite(), nil
	}
	return r, nil
}

// DialogDeleteChatlistInvite
// dialog.deleteChatlistInvite user_id:long filter_id:int slug:string = Bool;
func (c *DialogCore) DialogDeleteChatlistInvite(in *dialog.TLDialogDeleteChatlistInvite) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.DeleteChatlistInvite(c.ctx, in.UserId, in.FilterId, in.Slug); err != nil {
		return nil, c.logChatlist("dialog.deleteChatlistInvite", err)
	}
	return mtproto.BoolTrue, nil
}

// DialogGetChatlistInvites
// dialog.getChatlistInvites user_id:long filter_id:int = chatlists.ExportedInvites;
func (c *DialogCore) DialogGetChatlistInvites(in *dialog.TLDialogGetChatlistInvites) (*mtproto.Chatlists_ExportedInvites, error) {
	r, err := c.svcCtx.Dao.GetChatlistInvites(c.ctx, in.UserId, in.FilterId)
	if err != nil {
		return nil, c.logChatlist("dialog.getChatlistInvites", err)
	}
	return r, nil
}

// DialogGetChatlistInvite
// dialog.getChatlistInvite user_id:long slug:string filter_id:int = ChatlistInviteExt;
func (c *DialogCore) DialogGetChatlistInvite(in *dialog.TLDialogGetChatlistInvite) (*dialog.ChatlistInviteExt, error) {
	r, err := c.svcCtx.Dao.GetChatlistInvite(c.ctx, in.UserId, in.Slug, in.FilterId)
	if err != nil {
		return nil, c.logChatlist("dialog.getChatlistInvite", err)
	}
	return r, nil
}

// DialogJoinChatlist
// dialog.joinChatlist user_id:long slug:string filter_id:int peers:Vector<InputPeer> chats:Vector<Chat> check_only:Bool = DialogFilterExt;
func (c *DialogCore) DialogJoinChatlist(in *dialog.TLDialogJoinChatlist) (*dialog.DialogFilterExt, error) {
	r, err := c.svcCtx.Dao.JoinChatlistFolder(c.ctx, in.UserId, in.Slug, in.FilterId, in.Peers, in.Chats, mtproto.FromBool(in.CheckOnly))
	if err != nil {
		return nil, c.logChatlist("dialog.joinChatlist", err)
	}
	if r == nil {
		return dialog.MakeTLDialogFilterExt(nil).To_DialogFilterExt(), nil
	}
	return r, nil
}

// DialogHideChatlistUpdates
// dialog.hideChatlistUpdates user_id:long filter_id:int = Bool;
func (c *DialogCore) DialogHideChatlistUpdates(in *dialog.TLDialogHideChatlistUpdates) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.HideChatlistUpdates(c.ctx, in.UserId, in.FilterId); err != nil {
		return nil, c.logChatlist("dialog.hideChatlistUpdates", err)
	}
	return mtproto.BoolTrue, nil
}
