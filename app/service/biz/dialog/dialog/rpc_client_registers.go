/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2024-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: Benqi (wubenqi@gmail.com)
 */

package dialog

import (
	"reflect"

	"github.com/teamgram/proto/mtproto"
)

var _ *mtproto.Bool

type newRPCReplyFunc func() interface{}

type RPCContextTuple struct {
	Method       string
	NewReplyFunc newRPCReplyFunc
}

var rpcContextRegisters = map[string]RPCContextTuple{
	"TLDialogSaveDraftMessage":                  {"/mtproto.RPCDialog/dialog_saveDraftMessage", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogClearDraftMessage":                 {"/mtproto.RPCDialog/dialog_clearDraftMessage", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetAllDrafts":                      {"/mtproto.RPCDialog/dialog_getAllDrafts", func() interface{} { return new(Vector_PeerWithDraftMessage) }},
	"TLDialogClearAllDrafts":                    {"/mtproto.RPCDialog/dialog_clearAllDrafts", func() interface{} { return new(Vector_PeerWithDraftMessage) }},
	"TLDialogMarkDialogUnread":                  {"/mtproto.RPCDialog/dialog_markDialogUnread", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogToggleDialogPin":                   {"/mtproto.RPCDialog/dialog_toggleDialogPin", func() interface{} { return new(mtproto.Int32) }},
	"TLDialogGetDialogUnreadMarkList":           {"/mtproto.RPCDialog/dialog_getDialogUnreadMarkList", func() interface{} { return new(Vector_DialogPeer) }},
	"TLDialogGetDialogsByOffsetDate":            {"/mtproto.RPCDialog/dialog_getDialogsByOffsetDate", func() interface{} { return new(Vector_DialogExt) }},
	"TLDialogGetDialogs":                        {"/mtproto.RPCDialog/dialog_getDialogs", func() interface{} { return new(Vector_DialogExt) }},
	"TLDialogGetDialogsByIdList":                {"/mtproto.RPCDialog/dialog_getDialogsByIdList", func() interface{} { return new(Vector_DialogExt) }},
	"TLDialogGetDialogsCount":                   {"/mtproto.RPCDialog/dialog_getDialogsCount", func() interface{} { return new(mtproto.Int32) }},
	"TLDialogGetPinnedDialogs":                  {"/mtproto.RPCDialog/dialog_getPinnedDialogs", func() interface{} { return new(Vector_DialogExt) }},
	"TLDialogReorderPinnedDialogs":              {"/mtproto.RPCDialog/dialog_reorderPinnedDialogs", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetDialogById":                     {"/mtproto.RPCDialog/dialog_getDialogById", func() interface{} { return new(DialogExt) }},
	"TLDialogGetTopMessage":                     {"/mtproto.RPCDialog/dialog_getTopMessage", func() interface{} { return new(mtproto.Int32) }},
	"TLDialogGetUserPinnedMessage":              {"/mtproto.RPCDialog/dialog_getUserPinnedMessage", func() interface{} { return new(mtproto.Int32) }},
	"TLDialogUpdateUserPinnedMessage":           {"/mtproto.RPCDialog/dialog_updateUserPinnedMessage", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogInsertOrUpdateDialogFilter":        {"/mtproto.RPCDialog/dialog_insertOrUpdateDialogFilter", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogDeleteDialogFilter":                {"/mtproto.RPCDialog/dialog_deleteDialogFilter", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogUpdateDialogFiltersOrder":          {"/mtproto.RPCDialog/dialog_updateDialogFiltersOrder", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetDialogFilters":                  {"/mtproto.RPCDialog/dialog_getDialogFilters", func() interface{} { return new(Vector_DialogFilterExt) }},
	"TLDialogGetDialogFolder":                   {"/mtproto.RPCDialog/dialog_getDialogFolder", func() interface{} { return new(Vector_DialogExt) }},
	"TLDialogEditPeerFolders":                   {"/mtproto.RPCDialog/dialog_editPeerFolders", func() interface{} { return new(Vector_DialogPinnedExt) }},
	"TLDialogGetChannelMessageReadParticipants": {"/mtproto.RPCDialog/dialog_getChannelMessageReadParticipants", func() interface{} { return new(Vector_Long) }},
	"TLDialogSetChatTheme":                      {"/mtproto.RPCDialog/dialog_setChatTheme", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogSetHistoryTTL":                     {"/mtproto.RPCDialog/dialog_setHistoryTTL", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetMyDialogsData":                  {"/mtproto.RPCDialog/dialog_getMyDialogsData", func() interface{} { return new(DialogsData) }},
	"TLDialogGetSavedDialogs":                   {"/mtproto.RPCDialog/dialog_getSavedDialogs", func() interface{} { return new(SavedDialogList) }},
	"TLDialogGetPinnedSavedDialogs":             {"/mtproto.RPCDialog/dialog_getPinnedSavedDialogs", func() interface{} { return new(SavedDialogList) }},
	"TLDialogToggleSavedDialogPin":              {"/mtproto.RPCDialog/dialog_toggleSavedDialogPin", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogReorderPinnedSavedDialogs":         {"/mtproto.RPCDialog/dialog_reorderPinnedSavedDialogs", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetDialogFilter":                   {"/mtproto.RPCDialog/dialog_getDialogFilter", func() interface{} { return new(DialogFilterExt) }},
	"TLDialogGetDialogFilterBySlug":             {"/mtproto.RPCDialog/dialog_getDialogFilterBySlug", func() interface{} { return new(DialogFilterExt) }},
	"TLDialogCreateDialogFilter":                {"/mtproto.RPCDialog/dialog_createDialogFilter", func() interface{} { return new(DialogFilterExt) }},
	"TLDialogToggleDialogFilterTags":            {"/mtproto.RPCDialog/dialog_toggleDialogFilterTags", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogGetDialogFilterTags":               {"/mtproto.RPCDialog/dialog_getDialogFilterTags", func() interface{} { return new(mtproto.Bool) }},
	"TLDialogSetChatWallpaper":                  {"/mtproto.RPCDialog/dialog_setChatWallpaper", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 063
	"TLDialogSaveThreadDraft": {"/mtproto.RPCDialog/dialog_saveThreadDraft", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 079
	"TLDialogGetSavedDialogsByIdList": {"/mtproto.RPCDialog/dialog_getSavedDialogsByIdList", func() interface{} { return new(SavedDialogList) }},
	// pluma: task 064
	"TLDialogExportChatlistInvite": {"/mtproto.RPCDialog/dialog_exportChatlistInvite", func() interface{} { return new(mtproto.Chatlists_ExportedChatlistInvite) }},
	// pluma: task 064
	"TLDialogEditChatlistInvite": {"/mtproto.RPCDialog/dialog_editChatlistInvite", func() interface{} { return new(mtproto.ExportedChatlistInvite) }},
	// pluma: task 064
	"TLDialogDeleteChatlistInvite": {"/mtproto.RPCDialog/dialog_deleteChatlistInvite", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 064
	"TLDialogGetChatlistInvites": {"/mtproto.RPCDialog/dialog_getChatlistInvites", func() interface{} { return new(mtproto.Chatlists_ExportedInvites) }},
	// pluma: task 064
	"TLDialogGetChatlistInvite": {"/mtproto.RPCDialog/dialog_getChatlistInvite", func() interface{} { return new(ChatlistInviteExt) }},
	// pluma: task 064
	"TLDialogJoinChatlist": {"/mtproto.RPCDialog/dialog_joinChatlist", func() interface{} { return new(DialogFilterExt) }},
	// pluma: task 064
	"TLDialogHideChatlistUpdates": {"/mtproto.RPCDialog/dialog_hideChatlistUpdates", func() interface{} { return new(mtproto.Bool) }},
}

func FindRPCContextTuple(t interface{}) *RPCContextTuple {
	rt := reflect.TypeOf(t)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	m, ok := rpcContextRegisters[rt.Name()]
	if !ok {
		// log.Errorf("Can't find name: %s", rt.Name())
		return nil
	}
	return &m
}

func GetRPCContextRegisters() map[string]RPCContextTuple {
	return rpcContextRegisters
}
