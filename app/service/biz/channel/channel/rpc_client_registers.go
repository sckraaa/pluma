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
