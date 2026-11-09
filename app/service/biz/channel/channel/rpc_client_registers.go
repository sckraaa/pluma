/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 */

package channel

import (
	"reflect"

	"github.com/teamgram/proto/mtproto"
)

type newRPCReplyFunc func() interface{}

type RPCContextTuple struct {
	Method       string
	NewReplyFunc newRPCReplyFunc
}

var rpcContextRegisters = map[string]RPCContextTuple{
	"TLChannelGetMutableChannel":      {"/mtproto.RPCChannel/channel_getMutableChannel", func() interface{} { return new(MutableChannel) }},
	"TLChannelGetChannelListByIdList": {"/mtproto.RPCChannel/channel_getChannelListByIdList", func() interface{} { return new(Vector_MutableChannel) }},
	// pluma: task 038
	"TLChannelCreateChannel": {"/mtproto.RPCChannel/channel_createChannel", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 038
	"TLChannelGetChannelDialogs": {"/mtproto.RPCChannel/channel_getChannelDialogs", func() interface{} { return new(Vector_Dialog) }},
	// pluma: task 038
	"TLChannelGetTopMessages": {"/mtproto.RPCChannel/channel_getTopMessages", func() interface{} { return new(Vector_Message) }},
	// pluma: task 039
	"TLChannelGetMemberIdList": {"/mtproto.RPCChannel/channel_getMemberIdList", func() interface{} { return new(Vector_Long) }},
	// pluma: task 039
	"TLChannelGetHistory": {"/mtproto.RPCChannel/channel_getHistory", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 039
	"TLChannelGetMessages": {"/mtproto.RPCChannel/channel_getMessages", func() interface{} { return new(Vector_Message) }},
	// pluma: task 040
	"TLChannelGetChannelDifference": {"/mtproto.RPCChannel/channel_getChannelDifference", func() interface{} { return new(mtproto.Updates_ChannelDifference) }},
	// pluma: task 041
	"TLChannelJoinChannel": {"/mtproto.RPCChannel/channel_joinChannel", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 041
	"TLChannelLeaveChannel": {"/mtproto.RPCChannel/channel_leaveChannel", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 041
	"TLChannelCheckChatInvite": {"/mtproto.RPCChannel/channel_checkChatInvite", func() interface{} { return new(mtproto.ChatInvite) }},
	// pluma: task 041
	"TLChannelGetParticipants": {"/mtproto.RPCChannel/channel_getParticipants", func() interface{} { return new(mtproto.Channels_ChannelParticipants) }},
	// pluma: task 043
	"TLChannelReadHistory": {"/mtproto.RPCChannel/channel_readHistory", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 043
	"TLChannelDeleteMessages": {"/mtproto.RPCChannel/channel_deleteMessages", func() interface{} { return new(mtproto.Messages_AffectedMessages) }},
	// pluma: task 041
	"TLChannelInviteToChannel": {"/mtproto.RPCChannel/channel_inviteToChannel", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 044
	"TLChannelEditTitle": {"/mtproto.RPCChannel/channel_editTitle", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 044
	"TLChannelEditAbout": {"/mtproto.RPCChannel/channel_editAbout", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 044
	"TLChannelCheckUsername": {"/mtproto.RPCChannel/channel_checkUsername", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 044
	"TLChannelUpdateUsername": {"/mtproto.RPCChannel/channel_updateUsername", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 044
	"TLChannelGetAdminedPublicChannels": {"/mtproto.RPCChannel/channel_getAdminedPublicChannels", func() interface{} { return new(Vector_Long) }},
	// pluma: task 055
	"TLChannelDeleteChannel": {"/mtproto.RPCChannel/channel_deleteChannel", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 046
	"TLChannelIncrementMessageViews": {"/mtproto.RPCChannel/channel_incrementMessageViews", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 046
	"TLChannelAddMessageForwards": {"/mtproto.RPCChannel/channel_addMessageForwards", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 043
	"TLChannelDeleteHistory": {"/mtproto.RPCChannel/channel_deleteHistory", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 043
	"TLChannelDeleteParticipantHistory": {"/mtproto.RPCChannel/channel_deleteParticipantHistory", func() interface{} { return new(mtproto.Messages_AffectedHistory) }},
	// pluma: task 045
	"TLChannelToggleSignatures": {"/mtproto.RPCChannel/channel_toggleSignatures", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 045
	"TLChannelToggleSlowMode": {"/mtproto.RPCChannel/channel_toggleSlowMode", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 041
	"TLChannelToggleJoinRequest": {"/mtproto.RPCChannel/channel_toggleJoinRequest", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 041
	"TLChannelExportChatInvite": {"/mtproto.RPCChannel/channel_exportChatInvite", func() interface{} { return new(mtproto.ExportedChatInvite) }},
	// pluma: task 041
	"TLChannelGetExportedChatInvite": {"/mtproto.RPCChannel/channel_getExportedChatInvite", func() interface{} { return new(mtproto.ExportedChatInvite) }},
	// pluma: task 041
	"TLChannelGetExportedChatInvites": {"/mtproto.RPCChannel/channel_getExportedChatInvites", func() interface{} { return new(mtproto.Messages_ExportedChatInvites) }},
	// pluma: task 041
	"TLChannelEditExportedChatInvite": {"/mtproto.RPCChannel/channel_editExportedChatInvite", func() interface{} { return new(mtproto.Messages_ExportedChatInvite) }},
	// pluma: task 041
	"TLChannelDeleteExportedChatInvite": {"/mtproto.RPCChannel/channel_deleteExportedChatInvite", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 041
	"TLChannelDeleteRevokedExportedChatInvites": {"/mtproto.RPCChannel/channel_deleteRevokedExportedChatInvites", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 041
	"TLChannelGetAdminsWithInvites": {"/mtproto.RPCChannel/channel_getAdminsWithInvites", func() interface{} { return new(mtproto.Messages_ChatAdminsWithInvites) }},
	// pluma: task 041
	"TLChannelGetChatInviteImporters": {"/mtproto.RPCChannel/channel_getChatInviteImporters", func() interface{} { return new(mtproto.Messages_ChatInviteImporters) }},
	// pluma: task 041
	"TLChannelHideChatJoinRequests": {"/mtproto.RPCChannel/channel_hideChatJoinRequests", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 041
	"TLChannelGetPendingJoinRequests": {"/mtproto.RPCChannel/channel_getPendingJoinRequests", func() interface{} { return new(mtproto.Update) }},
	// pluma: task 041
	"TLChannelGetLeftChannels": {"/mtproto.RPCChannel/channel_getLeftChannels", func() interface{} { return new(mtproto.Messages_Chats) }},
	// pluma: task 060
	"TLChannelToggleForum": {"/mtproto.RPCChannel/channel_toggleForum", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelToggleViewForumAsMessages": {"/mtproto.RPCChannel/channel_toggleViewForumAsMessages", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelCreateForumTopic": {"/mtproto.RPCChannel/channel_createForumTopic", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelEditForumTopic": {"/mtproto.RPCChannel/channel_editForumTopic", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelUpdatePinnedForumTopic": {"/mtproto.RPCChannel/channel_updatePinnedForumTopic", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelReorderPinnedForumTopics": {"/mtproto.RPCChannel/channel_reorderPinnedForumTopics", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 060
	"TLChannelDeleteTopicHistory": {"/mtproto.RPCChannel/channel_deleteTopicHistory", func() interface{} { return new(mtproto.Messages_AffectedHistory) }},
	// pluma: task 060
	"TLChannelGetForumTopics": {"/mtproto.RPCChannel/channel_getForumTopics", func() interface{} { return new(mtproto.Messages_ForumTopics) }},
	// pluma: task 060
	"TLChannelGetForumTopicsByID": {"/mtproto.RPCChannel/channel_getForumTopicsByID", func() interface{} { return new(mtproto.Messages_ForumTopics) }},
	// pluma: task 060
	"TLChannelSearchTopic": {"/mtproto.RPCChannel/channel_searchTopic", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 066
	"TLChannelSearchCount": {"/mtproto.RPCChannel/channel_searchCount", func() interface{} { return new(mtproto.Int32) }},
	// pluma: task 066
	"TLChannelSearchCalendar": {"/mtproto.RPCChannel/channel_searchCalendar", func() interface{} { return new(mtproto.Messages_SearchResultsCalendar) }},
	// pluma: task 066
	"TLChannelSearchPositions": {"/mtproto.RPCChannel/channel_searchPositions", func() interface{} { return new(mtproto.Messages_SearchResultsPositions) }},
	// pluma: task 066
	"TLChannelSearchAcross": {"/mtproto.RPCChannel/channel_searchAcross", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 060
	"TLChannelReadTopicMentions": {"/mtproto.RPCChannel/channel_readTopicMentions", func() interface{} { return new(mtproto.Messages_AffectedHistory) }},
	// pluma: task 060
	"TLChannelUnpinAllTopicMessages": {"/mtproto.RPCChannel/channel_unpinAllTopicMessages", func() interface{} { return new(mtproto.Messages_AffectedHistory) }},
	// pluma: task 063
	"TLChannelSetHistoryTTL": {"/mtproto.RPCChannel/channel_setHistoryTTL", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 044
	"TLChannelToggleSetting": {"/mtproto.RPCChannel/channel_toggleSetting", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 044
	"TLChannelSetAvailableReactions": {"/mtproto.RPCChannel/channel_setAvailableReactions", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 048
	"TLChannelSetDiscussionGroup": {"/mtproto.RPCChannel/channel_setDiscussionGroup", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 048
	"TLChannelGetGroupsForDiscussion": {"/mtproto.RPCChannel/channel_getGroupsForDiscussion", func() interface{} { return new(Vector_Long) }},
	// pluma: task 048
	"TLChannelGetDiscussionMessage": {"/mtproto.RPCChannel/channel_getDiscussionMessage", func() interface{} { return new(mtproto.Messages_DiscussionMessage) }},
	// pluma: task 048
	"TLChannelGetReplies": {"/mtproto.RPCChannel/channel_getReplies", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 048
	"TLChannelReadDiscussion": {"/mtproto.RPCChannel/channel_readDiscussion", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 044
	"TLChannelEditPhoto": {"/mtproto.RPCChannel/channel_editPhoto", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 043
	"TLChannelReadMentions": {"/mtproto.RPCChannel/channel_readMentions", func() interface{} { return new(mtproto.Messages_AffectedHistory) }},
	// pluma: task 043
	"TLChannelGetUnreadMentions": {"/mtproto.RPCChannel/channel_getUnreadMentions", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 043
	"TLChannelReadMessageContents": {"/mtproto.RPCChannel/channel_readMessageContents", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 043
	"TLChannelSearch": {"/mtproto.RPCChannel/channel_search", func() interface{} { return new(mtproto.Messages_Messages) }},
	// pluma: task 042
	"TLChannelEditCreator": {"/mtproto.RPCChannel/channel_editCreator", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 042
	"TLChannelEditAdmin": {"/mtproto.RPCChannel/channel_editAdmin", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 042
	"TLChannelEditBanned": {"/mtproto.RPCChannel/channel_editBanned", func() interface{} { return new(mtproto.Updates) }},
	// pluma: task 042
	"TLChannelEditDefaultBannedRights": {"/mtproto.RPCChannel/channel_editDefaultBannedRights", func() interface{} { return new(mtproto.Updates) }},
}

func FindRPCContextTuple(t interface{}) *RPCContextTuple {
	rt := reflect.TypeOf(t)
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}

	m, ok := rpcContextRegisters[rt.Name()]
	if !ok {
		return nil
	}
	return &m
}

func GetRPCContextRegisters() map[string]RPCContextTuple {
	return rpcContextRegisters
}
