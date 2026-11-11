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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"

	"github.com/zeromicro/go-zero/core/logx"
)

// recommendingChannels answers the two calls of the method; any other
// method of the client would panic on the nil interface.
type recommendingChannels struct {
	channel_client.ChannelClient
	mc  *channelpb.MutableChannel
	got *channelpb.TLChannelGetChannelRecommendations
}

func (r *recommendingChannels) ChannelGetMutableChannel(context.Context, *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	return r.mc, nil
}

func (r *recommendingChannels) ChannelGetChannelRecommendations(_ context.Context, in *channelpb.TLChannelGetChannelRecommendations) (*mtproto.Messages_Chats, error) {
	r.got = in
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{}).To_Messages_Chats(), nil
}

// The channels like a channel are asked about a channel the account may
// read, by its id; without a channel, for the account alone. A private
// channel the account is not in, or a wrong access_hash, is not asked
// about.
func TestChannelRecommendationsAsks(t *testing.T) {
	const self, id, hash = int64(10), int64(1_000_000_005), int64(77)
	channel := func(public bool, members ...int64) *channelpb.MutableChannel {
		c := &channelpb.ImmutableChannel{Id: id, AccessHash: hash, Broadcast: true}
		if public {
			c.Username = "news"
		}
		mc := &channelpb.MutableChannel{Channel: c}
		for _, m := range members {
			mc.Participants = append(mc.Participants, &channelpb.ImmutableChannelParticipant{UserId: m, State: channelpb.StateIn})
		}
		return mc
	}
	input := func(hash int64) *mtproto.InputChannel {
		return mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: id, AccessHash: hash}).To_InputChannel()
	}
	ask := func(mc *channelpb.MutableChannel, in *mtproto.InputChannel) (*recommendingChannels, error) {
		client := &recommendingChannels{mc: mc}
		c := &ChannelsCore{
			ctx:    context.Background(),
			svcCtx: &svc.ServiceContext{Channel: client},
			Logger: logx.WithContext(context.Background()),
			MD:     &metadata.RpcMetadata{UserId: self},
		}
		_, err := c.ChannelsGetChannelRecommendations(&mtproto.TLChannelsGetChannelRecommendations{Channel: in})
		return client, err
	}

	for _, tc := range []struct {
		name string
		mc   *channelpb.MutableChannel
	}{
		{"a public channel", channel(true)},
		{"a private channel of the account", channel(false, self)},
	} {
		client, err := ask(tc.mc, input(hash))
		if err != nil || client.got.GetChannelId() != id || client.got.GetSelfId() != self {
			t.Errorf("%s: asked %v, %v", tc.name, client.got, err)
		}
	}

	client, err := ask(nil, nil)
	if err != nil || client.got.GetChannelId() != 0 || client.got.GetSelfId() != self {
		t.Errorf("for the account: asked %v, %v", client.got, err)
	}

	for _, tc := range []struct {
		name string
		mc   *channelpb.MutableChannel
		in   *mtproto.InputChannel
		want error
	}{
		{"a private channel of others", channel(false, 11), input(hash), mtproto.ErrChannelPrivate},
		{"a wrong access_hash", channel(true), input(hash + 1), mtproto.ErrChannelInvalid},
	} {
		client, err := ask(tc.mc, tc.in)
		if !errors.Is(err, tc.want) || client.got != nil {
			t.Errorf("%s: %v, asked %v; want %v", tc.name, err, client.got, tc.want)
		}
	}
}
