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

package channel_client

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/zeromicro/go-zero/zrpc"
)

type ChannelClient interface {
	ChannelGetMutableChannel(ctx context.Context, in *channel.TLChannelGetMutableChannel) (*channel.MutableChannel, error)
	ChannelGetChannelListByIdList(ctx context.Context, in *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error)
	ChannelCreateChannel(ctx context.Context, in *channel.TLChannelCreateChannel) (*mtproto.Updates, error)
	ChannelGetChannelDialogs(ctx context.Context, in *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error)
	ChannelGetTopMessages(ctx context.Context, in *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error)
}

type defaultChannelClient struct {
	cli zrpc.Client
}

func NewChannelClient(cli zrpc.Client) ChannelClient {
	return &defaultChannelClient{
		cli: cli,
	}
}

// ChannelGetMutableChannel
// channel.getMutableChannel channel_id:long id:Vector<long> = MutableChannel;
func (m *defaultChannelClient) ChannelGetMutableChannel(ctx context.Context, in *channel.TLChannelGetMutableChannel) (*channel.MutableChannel, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetMutableChannel(ctx, in)
}

// ChannelGetChannelListByIdList
// channel.getChannelListByIdList self_id:long id_list:Vector<long> = Vector<MutableChannel>;
func (m *defaultChannelClient) ChannelGetChannelListByIdList(ctx context.Context, in *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChannelListByIdList(ctx, in)
}

// ChannelCreateChannel
// channel.createChannel creator_id:long title:string about:string broadcast:Bool megagroup:Bool = Updates;
func (m *defaultChannelClient) ChannelCreateChannel(ctx context.Context, in *channel.TLChannelCreateChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelCreateChannel(ctx, in)
}

// ChannelGetChannelDialogs
// channel.getChannelDialogs self_id:long id_list:Vector<long> = Vector<Dialog>;
func (m *defaultChannelClient) ChannelGetChannelDialogs(ctx context.Context, in *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChannelDialogs(ctx, in)
}

// ChannelGetTopMessages
// channel.getTopMessages self_id:long id_list:Vector<long> = Vector<Message>;
func (m *defaultChannelClient) ChannelGetTopMessages(ctx context.Context, in *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetTopMessages(ctx, in)
}
