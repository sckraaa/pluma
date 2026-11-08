/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2025 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/core"
)

// DialogSaveDraftMessage
// dialog.saveDraftMessage user_id:long peer_type:int peer_id:long message:DraftMessage = Bool;
func (s *Service) DialogSaveDraftMessage(ctx context.Context, request *dialog.TLDialogSaveDraftMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.saveDraftMessage - metadata: {%s}", c.MD)

	r, err := c.DialogSaveDraftMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogClearDraftMessage
// dialog.clearDraftMessage user_id:long peer_type:int peer_id:long = Bool;
func (s *Service) DialogClearDraftMessage(ctx context.Context, request *dialog.TLDialogClearDraftMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.clearDraftMessage - metadata: {%s}", c.MD)

	r, err := c.DialogClearDraftMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetAllDrafts
// dialog.getAllDrafts user_id:long = Vector<PeerWithDraftMessage>;
func (s *Service) DialogGetAllDrafts(ctx context.Context, request *dialog.TLDialogGetAllDrafts) (*dialog.Vector_PeerWithDraftMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getAllDrafts - metadata: {%s}", c.MD)

	r, err := c.DialogGetAllDrafts(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogClearAllDrafts
// dialog.clearAllDrafts user_id:long = Vector<PeerWithDraftMessage>;
func (s *Service) DialogClearAllDrafts(ctx context.Context, request *dialog.TLDialogClearAllDrafts) (*dialog.Vector_PeerWithDraftMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.clearAllDrafts - metadata: {%s}", c.MD)

	r, err := c.DialogClearAllDrafts(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogMarkDialogUnread
// dialog.markDialogUnread user_id:long peer_type:int peer_id:long unread_mark:Bool = Bool;
func (s *Service) DialogMarkDialogUnread(ctx context.Context, request *dialog.TLDialogMarkDialogUnread) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.markDialogUnread - metadata: {%s}", c.MD)

	r, err := c.DialogMarkDialogUnread(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogToggleDialogPin
// dialog.toggleDialogPin user_id:long peer_type:int peer_id:long pinned:Bool = Int32;
func (s *Service) DialogToggleDialogPin(ctx context.Context, request *dialog.TLDialogToggleDialogPin) (*mtproto.Int32, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.toggleDialogPin - metadata: {%s}", c.MD)

	r, err := c.DialogToggleDialogPin(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogUnreadMarkList
// dialog.getDialogUnreadMarkList user_id:long = Vector<DialogPeer>;
func (s *Service) DialogGetDialogUnreadMarkList(ctx context.Context, request *dialog.TLDialogGetDialogUnreadMarkList) (*dialog.Vector_DialogPeer, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogUnreadMarkList - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogUnreadMarkList(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogsByOffsetDate
// dialog.getDialogsByOffsetDate user_id:long exclude_pinned:Bool offset_date:int limit:int = Vector<DialogExt>;
func (s *Service) DialogGetDialogsByOffsetDate(ctx context.Context, request *dialog.TLDialogGetDialogsByOffsetDate) (*dialog.Vector_DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogsByOffsetDate - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogsByOffsetDate(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogs
// dialog.getDialogs user_id:long exclude_pinned:Bool folder_id:int = Vector<DialogExt>;
func (s *Service) DialogGetDialogs(ctx context.Context, request *dialog.TLDialogGetDialogs) (*dialog.Vector_DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogsByIdList
// dialog.getDialogsByIdList user_id:long id_list:Vector<long> = Vector<DialogExt>;
func (s *Service) DialogGetDialogsByIdList(ctx context.Context, request *dialog.TLDialogGetDialogsByIdList) (*dialog.Vector_DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogsByIdList - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogsByIdList(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogsCount
// dialog.getDialogsCount user_id:long exclude_pinned:Bool folder_id:int = Int32;
func (s *Service) DialogGetDialogsCount(ctx context.Context, request *dialog.TLDialogGetDialogsCount) (*mtproto.Int32, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogsCount - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogsCount(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetPinnedDialogs
// dialog.getPinnedDialogs  user_id:long folder_id:int = Vector<DialogExt>;
func (s *Service) DialogGetPinnedDialogs(ctx context.Context, request *dialog.TLDialogGetPinnedDialogs) (*dialog.Vector_DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getPinnedDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogGetPinnedDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogReorderPinnedDialogs
// dialog.reorderPinnedDialogs user_id:long force:Bool folder_id:int id_list:Vector<long> = Bool;
func (s *Service) DialogReorderPinnedDialogs(ctx context.Context, request *dialog.TLDialogReorderPinnedDialogs) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.reorderPinnedDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogReorderPinnedDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogById
// dialog.getDialogById user_id:long peer_type:int peer_id:long = DialogExt;
func (s *Service) DialogGetDialogById(ctx context.Context, request *dialog.TLDialogGetDialogById) (*dialog.DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogById - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogById(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetTopMessage
// dialog.getTopMessage user_id:long peer_type:int peer_id:long = Int32;
func (s *Service) DialogGetTopMessage(ctx context.Context, request *dialog.TLDialogGetTopMessage) (*mtproto.Int32, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getTopMessage - metadata: {%s}", c.MD)

	r, err := c.DialogGetTopMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetUserPinnedMessage
// dialog.getUserPinnedMessage user_id:long peer_type:int peer_id:long = Int32;
func (s *Service) DialogGetUserPinnedMessage(ctx context.Context, request *dialog.TLDialogGetUserPinnedMessage) (*mtproto.Int32, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getUserPinnedMessage - metadata: {%s}", c.MD)

	r, err := c.DialogGetUserPinnedMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogUpdateUserPinnedMessage
// dialog.updateUserPinnedMessage user_id:long peer_type:int peer_id:long pinned_msg_id:int = Bool;
func (s *Service) DialogUpdateUserPinnedMessage(ctx context.Context, request *dialog.TLDialogUpdateUserPinnedMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.updateUserPinnedMessage - metadata: {%s}", c.MD)

	r, err := c.DialogUpdateUserPinnedMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogInsertOrUpdateDialogFilter
// dialog.insertOrUpdateDialogFilter user_id:long id:int dialog_filter:DialogFilter = Bool;
func (s *Service) DialogInsertOrUpdateDialogFilter(ctx context.Context, request *dialog.TLDialogInsertOrUpdateDialogFilter) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.insertOrUpdateDialogFilter - metadata: {%s}", c.MD)

	r, err := c.DialogInsertOrUpdateDialogFilter(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogDeleteDialogFilter
// dialog.deleteDialogFilter user_id:long id:int = Bool;
func (s *Service) DialogDeleteDialogFilter(ctx context.Context, request *dialog.TLDialogDeleteDialogFilter) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.deleteDialogFilter - metadata: {%s}", c.MD)

	r, err := c.DialogDeleteDialogFilter(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogUpdateDialogFiltersOrder
// dialog.updateDialogFiltersOrder user_id:long order:Vector<int> = Bool;
func (s *Service) DialogUpdateDialogFiltersOrder(ctx context.Context, request *dialog.TLDialogUpdateDialogFiltersOrder) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.updateDialogFiltersOrder - metadata: {%s}", c.MD)

	r, err := c.DialogUpdateDialogFiltersOrder(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogFilters
// dialog.getDialogFilters user_id:long = Vector<DialogFilterExt>;
func (s *Service) DialogGetDialogFilters(ctx context.Context, request *dialog.TLDialogGetDialogFilters) (*dialog.Vector_DialogFilterExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogFilters - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogFilters(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogFolder
// dialog.getDialogFolder user_id:long folder_id:int = Vector<DialogExt>;
func (s *Service) DialogGetDialogFolder(ctx context.Context, request *dialog.TLDialogGetDialogFolder) (*dialog.Vector_DialogExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogFolder - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogFolder(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogEditPeerFolders
// dialog.editPeerFolders user_id:long peer_dialog_list:Vector<long> folder_id:int = Vector<DialogPinnedExt>;
func (s *Service) DialogEditPeerFolders(ctx context.Context, request *dialog.TLDialogEditPeerFolders) (*dialog.Vector_DialogPinnedExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.editPeerFolders - metadata: {%s}", c.MD)

	r, err := c.DialogEditPeerFolders(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetChannelMessageReadParticipants
// dialog.getChannelMessageReadParticipants user_id:long channel_id:long msg_id:int = Vector<long>;
func (s *Service) DialogGetChannelMessageReadParticipants(ctx context.Context, request *dialog.TLDialogGetChannelMessageReadParticipants) (*dialog.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getChannelMessageReadParticipants - metadata: {%s}", c.MD)

	r, err := c.DialogGetChannelMessageReadParticipants(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogSetChatTheme
// dialog.setChatTheme user_id:long peer_type:int peer_id:long theme_emoticon:string = Bool;
func (s *Service) DialogSetChatTheme(ctx context.Context, request *dialog.TLDialogSetChatTheme) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.setChatTheme - metadata: {%s}", c.MD)

	r, err := c.DialogSetChatTheme(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogSetHistoryTTL
// dialog.setHistoryTTL user_id:long peer_type:int peer_id:long ttl_period:int = Bool;
func (s *Service) DialogSetHistoryTTL(ctx context.Context, request *dialog.TLDialogSetHistoryTTL) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.setHistoryTTL - metadata: {%s}", c.MD)

	r, err := c.DialogSetHistoryTTL(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetMyDialogsData
// dialog.getMyDialogsData flags:# user_id:long user:flags.0?true chat:flags.1?true channel:flags.2?true = DialogsData;
func (s *Service) DialogGetMyDialogsData(ctx context.Context, request *dialog.TLDialogGetMyDialogsData) (*dialog.DialogsData, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getMyDialogsData - metadata: {%s}", c.MD)

	r, err := c.DialogGetMyDialogsData(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetSavedDialogs
// dialog.getSavedDialogs user_id:long exclude_pinned:Bool offset_date:int offset_id:int offset_peer:PeerUtil limit:int = SavedDialogList;
func (s *Service) DialogGetSavedDialogs(ctx context.Context, request *dialog.TLDialogGetSavedDialogs) (*dialog.SavedDialogList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getSavedDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogGetSavedDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetPinnedSavedDialogs
// dialog.getPinnedSavedDialogs user_id:long = SavedDialogList;
func (s *Service) DialogGetPinnedSavedDialogs(ctx context.Context, request *dialog.TLDialogGetPinnedSavedDialogs) (*dialog.SavedDialogList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getPinnedSavedDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogGetPinnedSavedDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogToggleSavedDialogPin
// dialog.toggleSavedDialogPin user_id:long peer:PeerUtil pinned:Bool = Bool;
func (s *Service) DialogToggleSavedDialogPin(ctx context.Context, request *dialog.TLDialogToggleSavedDialogPin) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.toggleSavedDialogPin - metadata: {%s}", c.MD)

	r, err := c.DialogToggleSavedDialogPin(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogReorderPinnedSavedDialogs
// dialog.reorderPinnedSavedDialogs user_id:long force:Bool order:Vector<PeerUtil> = Bool;
func (s *Service) DialogReorderPinnedSavedDialogs(ctx context.Context, request *dialog.TLDialogReorderPinnedSavedDialogs) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.reorderPinnedSavedDialogs - metadata: {%s}", c.MD)

	r, err := c.DialogReorderPinnedSavedDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogFilter
// dialog.getDialogFilter user_id:long id:int = DialogFilterExt;
func (s *Service) DialogGetDialogFilter(ctx context.Context, request *dialog.TLDialogGetDialogFilter) (*dialog.DialogFilterExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogFilter - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogFilter(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogFilterBySlug
// dialog.getDialogFilterBySlug user_id:long slug:string = DialogFilterExt;
func (s *Service) DialogGetDialogFilterBySlug(ctx context.Context, request *dialog.TLDialogGetDialogFilterBySlug) (*dialog.DialogFilterExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogFilterBySlug - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogFilterBySlug(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogCreateDialogFilter
// dialog.createDialogFilter user_id:long dialog_filter:DialogFilterExt = DialogFilterExt;
func (s *Service) DialogCreateDialogFilter(ctx context.Context, request *dialog.TLDialogCreateDialogFilter) (*dialog.DialogFilterExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.createDialogFilter - metadata: {%s}", c.MD)

	r, err := c.DialogCreateDialogFilter(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogToggleDialogFilterTags
// dialog.toggleDialogFilterTags user_id:long enabled:Bool = Bool;
func (s *Service) DialogToggleDialogFilterTags(ctx context.Context, request *dialog.TLDialogToggleDialogFilterTags) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.toggleDialogFilterTags - metadata: {%s}", c.MD)

	r, err := c.DialogToggleDialogFilterTags(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetDialogFilterTags
// dialog.getDialogFilterTags user_id:long = Bool;
func (s *Service) DialogGetDialogFilterTags(ctx context.Context, request *dialog.TLDialogGetDialogFilterTags) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getDialogFilterTags - metadata: {%s}", c.MD)

	r, err := c.DialogGetDialogFilterTags(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogSetChatWallpaper
// dialog.setChatWallpaper user_id:long peer_type:int peer_id:long wallpaper_id:long = Bool;
func (s *Service) DialogSetChatWallpaper(ctx context.Context, request *dialog.TLDialogSetChatWallpaper) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.setChatWallpaper - metadata: {%s}", c.MD)

	r, err := c.DialogSetChatWallpaper(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogExportChatlistInvite
// dialog.exportChatlistInvite user_id:long filter_id:int title:string peers:Vector<InputPeer> migrated_chats:Vector<long> migrated_channels:Vector<long> chats:Vector<Chat> check_only:Bool = chatlists.ExportedChatlistInvite;
func (s *Service) DialogExportChatlistInvite(ctx context.Context, request *dialog.TLDialogExportChatlistInvite) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.exportChatlistInvite - metadata: {%s}", c.MD)

	r, err := c.DialogExportChatlistInvite(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogEditChatlistInvite
// dialog.editChatlistInvite user_id:long filter_id:int slug:string set_title:Bool title:string set_peers:Bool peers:Vector<InputPeer> migrated_chats:Vector<long> migrated_channels:Vector<long> chats:Vector<Chat> check_only:Bool = ExportedChatlistInvite;
func (s *Service) DialogEditChatlistInvite(ctx context.Context, request *dialog.TLDialogEditChatlistInvite) (*mtproto.ExportedChatlistInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.editChatlistInvite - metadata: {%s}", c.MD)

	r, err := c.DialogEditChatlistInvite(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogDeleteChatlistInvite
// dialog.deleteChatlistInvite user_id:long filter_id:int slug:string = Bool;
func (s *Service) DialogDeleteChatlistInvite(ctx context.Context, request *dialog.TLDialogDeleteChatlistInvite) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.deleteChatlistInvite - metadata: {%s}", c.MD)

	r, err := c.DialogDeleteChatlistInvite(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetChatlistInvites
// dialog.getChatlistInvites user_id:long filter_id:int = chatlists.ExportedInvites;
func (s *Service) DialogGetChatlistInvites(ctx context.Context, request *dialog.TLDialogGetChatlistInvites) (*mtproto.Chatlists_ExportedInvites, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getChatlistInvites - metadata: {%s}", c.MD)

	r, err := c.DialogGetChatlistInvites(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogGetChatlistInvite
// dialog.getChatlistInvite user_id:long slug:string filter_id:int = ChatlistInviteExt;
func (s *Service) DialogGetChatlistInvite(ctx context.Context, request *dialog.TLDialogGetChatlistInvite) (*dialog.ChatlistInviteExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.getChatlistInvite - metadata: {%s}", c.MD)

	r, err := c.DialogGetChatlistInvite(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogJoinChatlist
// dialog.joinChatlist user_id:long slug:string filter_id:int peers:Vector<InputPeer> chats:Vector<Chat> check_only:Bool = DialogFilterExt;
func (s *Service) DialogJoinChatlist(ctx context.Context, request *dialog.TLDialogJoinChatlist) (*dialog.DialogFilterExt, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.joinChatlist - metadata: {%s}", c.MD)

	r, err := c.DialogJoinChatlist(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// DialogHideChatlistUpdates
// dialog.hideChatlistUpdates user_id:long filter_id:int = Bool;
func (s *Service) DialogHideChatlistUpdates(ctx context.Context, request *dialog.TLDialogHideChatlistUpdates) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("dialog.hideChatlistUpdates - metadata: {%s}", c.MD)

	r, err := c.DialogHideChatlistUpdates(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
