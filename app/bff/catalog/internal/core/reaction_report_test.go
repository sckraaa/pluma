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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

const (
	reportChannel int64 = 70
	reportHash    int64 = 71
	reportMe      int64 = 1
)

// reportChannels is biz channel for one supergroup as reportMe sees it.
type reportChannels struct {
	channel_client.ChannelClient
	channel  *channelpb.ImmutableChannel
	me       *channelpb.ImmutableChannelParticipant
	messages []int32 // the ids that are messages; others are messageEmpty
	asked    bool    // the messages were read
}

func (f *reportChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if in.GetChannelId() != reportChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	mc := &channelpb.MutableChannel{Channel: f.channel}
	if f.me != nil {
		mc.Participants = []*channelpb.ImmutableChannelParticipant{f.me}
	}
	return mc, nil
}

func (f *reportChannels) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	f.asked = true
	out := &channelpb.Vector_Message{}
	for _, id := range in.GetId() {
		if slices.Contains(f.messages, id) {
			out.Datas = append(out.Datas, mtproto.MakeTLMessage(&mtproto.Message{Id: id}).To_Message())
		} else {
			out.Datas = append(out.Datas, mtproto.MakeTLMessageEmpty(&mtproto.Message{Id: id}).To_Message())
		}
	}
	return out, nil
}

// noReports fails the test when a report is stored.
type noReports struct{ t *testing.T }

func (r noReports) ReportsSince(context.Context, int64, int64) (int, error) { return 0, nil }

func (r noReports) AddReport(_ context.Context, rep *moderation.Report, _ []int32, _ *moderation.Snapshot) error {
	r.t.Errorf("report stored: %+v", rep)
	return nil
}

func supergroup(username string) *channelpb.ImmutableChannel {
	return &channelpb.ImmutableChannel{Id: reportChannel, AccessHash: reportHash, Megagroup: true, Username: username, Title: "group"}
}

func member(role int32, rights int32) *channelpb.ImmutableChannelParticipant {
	return &channelpb.ImmutableChannelParticipant{UserId: reportMe, Role: role, State: channelpb.StateIn, AdminRights: rights, Date: 1}
}

func reportRequest(peer *mtproto.InputPeer, id int32, reactionUser int64) *mtproto.TLMessagesReportReaction {
	return &mtproto.TLMessagesReportReaction{
		Peer:         peer,
		Id:           id,
		ReactionPeer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: reactionUser, AccessHash: 1}).To_InputPeer(),
	}
}

func channelPeer(hash int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: reportChannel, AccessHash: hash}).To_InputPeer()
}

// The checks before anything is read of the reactions, each with the error
// the clients get; none of them files a report.
func TestReportReactionRefusals(t *testing.T) {
	cases := []struct {
		name     string
		bot      bool
		channel  *channelpb.ImmutableChannel
		me       *channelpb.ImmutableChannelParticipant
		req      *mtproto.TLMessagesReportReaction
		want     error
		messages bool // the messages were read
	}{
		{name: "a bot", bot: true, channel: supergroup("pub"), me: member(channelpb.RoleMember, 0),
			req: reportRequest(channelPeer(reportHash), 5, 2), want: mtproto.ErrBotMethodInvalid},
		{name: "a private chat", channel: supergroup("pub"),
			req: reportRequest(mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 2}).To_InputPeer(), 5, 2), want: mtproto.ErrPeerIdInvalid},
		{name: "a basic group", channel: supergroup("pub"),
			req: reportRequest(mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 3}).To_InputPeer(), 5, 2), want: mtproto.ErrPeerIdInvalid},
		{name: "no message id", channel: supergroup("pub"), me: member(channelpb.RoleMember, 0),
			req: reportRequest(channelPeer(reportHash), 0, 2), want: mtproto.ErrMsgIdInvalid},
		{name: "a wrong access hash", channel: supergroup("pub"), me: member(channelpb.RoleMember, 0),
			req: reportRequest(channelPeer(reportHash+1), 5, 2), want: mtproto.ErrPeerIdInvalid},
		{name: "a broadcast channel", channel: &channelpb.ImmutableChannel{Id: reportChannel, AccessHash: reportHash, Broadcast: true, Username: "pub"},
			me: member(channelpb.RoleCreator, 0), req: reportRequest(channelPeer(reportHash), 5, 2), want: mtproto.ErrPeerIdInvalid},
		{name: "out of a private group", channel: supergroup(""),
			req: reportRequest(channelPeer(reportHash), 5, 2), want: mtproto.ErrChannelPrivate},
		{name: "a member of a private group", channel: supergroup(""), me: member(channelpb.RoleMember, 0),
			req: reportRequest(channelPeer(reportHash), 5, 2), want: mtproto.ErrChatAdminRequired},
		{name: "an admin who may not ban, in a private group", channel: supergroup(""), me: member(channelpb.RoleAdmin, mtproto.ADMIN_DELETE_MESSAGES),
			req: reportRequest(channelPeer(reportHash), 5, 2), want: mtproto.ErrChatAdminRequired},
		{name: "a message that is not there", channel: supergroup("pub"), me: member(channelpb.RoleMember, 0),
			req: reportRequest(channelPeer(reportHash), 6, 2), want: mtproto.ErrMsgIdInvalid, messages: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &reportChannels{channel: tc.channel, me: tc.me, messages: []int32{5}}
			c := New(context.Background(), &svc.ServiceContext{Channel: ch, Reports: noReports{t}})
			c.MD = &metadata.RpcMetadata{UserId: reportMe, IsBot: tc.bot}
			_, err := c.MessagesReportReaction(tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v, want %v", err, tc.want)
			}
			if ch.asked != tc.messages {
				t.Errorf("the messages read: %v", ch.asked)
			}
		})
	}
}

// Who may report: the creator and an admin who may ban anywhere, anyone in
// a public supergroup, also one who has not joined it.
func TestCanReportReaction(t *testing.T) {
	now := int32(time.Now().Unix())
	cases := []struct {
		name    string
		channel *channelpb.ImmutableChannel
		me      *channelpb.ImmutableChannelParticipant
		want    error
	}{
		{"the creator of a private group", supergroup(""), member(channelpb.RoleCreator, 0), nil},
		{"an admin who may ban, in a private group", supergroup(""), member(channelpb.RoleAdmin, mtproto.ADMIN_BAN_USERS), nil},
		{"an admin who may not ban, in a private group", supergroup(""), member(channelpb.RoleAdmin, mtproto.ADMIN_DELETE_MESSAGES), mtproto.ErrChatAdminRequired},
		{"a member of a private group", supergroup(""), member(channelpb.RoleMember, 0), mtproto.ErrChatAdminRequired},
		{"a member of a public group", supergroup("pub"), member(channelpb.RoleMember, 0), nil},
		{"one who never joined a public group", supergroup("pub"), nil, nil},
	}
	for _, tc := range cases {
		if err := canReportReaction(tc.channel, tc.me, now); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// The user reported is someone else who reacted to the message; the keys
// go in the order chosen.
func TestReactionOf(t *testing.T) {
	rows := []reactions.Row{
		{UserId: 2, Reaction: "e🔥", ChosenOrder: 1, Date: 10},
		{UserId: 3, Reaction: "e👍", ChosenOrder: 0, Date: 11},
		{UserId: 2, Reaction: "c123", ChosenOrder: 0, Date: 12},
		{UserId: reportMe, Reaction: "e👎", ChosenOrder: 0, Date: 13},
	}
	user := func(id int64) *mtproto.InputPeer {
		return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
	}
	id, keys, err := reactionOf(user(2), reportMe, rows)
	if err != nil || id != 2 || !slices.Equal(keys, []string{"c123", "e🔥"}) {
		t.Errorf("user 2: %d %v %v", id, keys, err)
	}
	for name, p := range map[string]*mtproto.InputPeer{
		"oneself":             mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(),
		"oneself by id":       user(reportMe),
		"one who did not":     user(4),
		"a channel":           mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 9}).To_InputPeer(),
		"an empty peer":       mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
		"no peer (Android's)": nil,
	} {
		if _, _, err := reactionOf(p, reportMe, rows); !errors.Is(err, mtproto.ErrUserIdInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
