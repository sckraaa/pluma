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
