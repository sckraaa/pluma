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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// requestChannels answers the look-up of channels a user chose and counts
// the promotions of the bot; any other method would panic on the nil
// interface.
type requestChannels struct {
	channel_client.ChannelClient
	channels map[int64]*channelpb.MutableChannel
	promoted []int64
}

func (r *requestChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if ch, ok := r.channels[in.GetChannelId()]; ok {
		return ch, nil
	}
	return nil, mtproto.ErrChannelInvalid
}

func (r *requestChannels) ChannelEditAdmin(_ context.Context, in *channelpb.TLChannelEditAdmin) (*mtproto.Updates, error) {
	r.promoted = append(r.promoted, in.GetChannelId())
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

// A chat that does not match the button leaves the bot out of every chat
// chosen with it: all are checked before the bot is promoted in any.
func TestRequestedChatsCheckBeforeBringingTheBot(t *testing.T) {
	const user, bot = int64(10), int64(20)
	channel := func(id int64, members ...*channelpb.ImmutableChannelParticipant) *channelpb.MutableChannel {
		return &channelpb.MutableChannel{
			Channel:      &channelpb.ImmutableChannel{Id: id, Broadcast: true, Title: "c"},
			Participants: members,
		}
	}
	creator := &channelpb.ImmutableChannelParticipant{UserId: user, Role: channelperm.RoleCreator, State: channelperm.StateIn}
	fake := &requestChannels{channels: map[int64]*channelpb.MutableChannel{
		1: channel(1, creator),
		2: channel(2), // the user is not in it
	}}
	c := testCore(&dao.Dao{Channel: fake}, user)
	pt := mtproto.MakeTLRequestPeerTypeBroadcast(&mtproto.RequestPeerType{
		BotAdminRights: mtproto.MakeTLChatAdminRights(&mtproto.ChatAdminRights{PostMessages: true}).To_ChatAdminRights(),
	}).To_RequestPeerType()
	button := mtproto.MakeTLKeyboardButtonRequestPeer(&mtproto.KeyboardButton{Text: "Pick", ButtonId: 1, MaxQuantity: 2, PeerType: pt}).To_KeyboardButton()
	botUser := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(&mtproto.UserData{Id: bot, Bot: mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()}).To_UserData()}).To_ImmutableUser()
	peers := []*mtproto.PeerUtil{mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, 1), mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, 2)}

	if _, _, err := c.requestedChats(peers, pt, button, botUser); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("err = %v", err)
	}
	if len(fake.promoted) != 0 {
		t.Fatalf("the bot was promoted in %v", fake.promoted)
	}

	// Both chats match: the bot is promoted in each, its updates returned.
	fake.channels[2] = channel(2, &channelpb.ImmutableChannelParticipant{UserId: user, Role: channelperm.RoleCreator, State: channelperm.StateIn})
	shared, updates, err := c.requestedChats(peers, pt, button, botUser)
	if err != nil || len(shared) != 2 || len(updates) != 2 || len(fake.promoted) != 2 {
		t.Fatalf("shared %d, updates %d, promoted %v, err %v", len(shared), len(updates), fake.promoted, err)
	}
}
