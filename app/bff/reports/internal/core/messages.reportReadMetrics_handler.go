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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/readmetrics"
)

// MessagesReportReadMetrics
// messages.reportReadMetrics#4067c5e6 peer:InputPeer metrics:Vector<InputMessageReadMetric> = Bool;
//
// Keeps how long and how far the messages of a chat were on the screen
// (task 098, part D), for the statistics of the chat (task 103,
// readmetrics.Store.Aggregate). Android sends it for basic groups,
// supergroups and channels (ui/ChatActivity.java:8778-8780); Desktop for
// the messages with views, that is channel posts, every 5 s
// (history/view/history_view_read_metrics_tracker.cpp:342-344,
// api/api_read_metrics.cpp:16, 34-72); iOS for broadcast channels
// (TelegramUI/Sources/ChatHistoryListNode.swift:3752-3768) and Web K for
// channel posts (helpers/readMetricsTracker.ts:5-8). None reads the
// answer. A private chat is accepted as well, though no client sends one.
// Only users may use the method
// (core.telegram.org/method/messages.reportReadMetrics). The chat must be
// one the user sees; a metric of a message newer than the chat's newest is
// not kept. The values are never logged.
func (c *ReportsCore) MessagesReportReadMetrics(in *mtproto.TLMessagesReportReadMetrics) (*mtproto.Bool, error) {
	if c.MD.GetIsBot() {
		return nil, mtproto.ErrBotMethodInvalid
	}
	peer, err := peerOf(c.MD.UserId, in.GetPeer())
	if err != nil {
		return nil, err
	}
	metrics := readmetrics.FromInput(in.GetMetrics())
	if err = readmetrics.Validate(metrics); err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		return mtproto.BoolTrue, nil
	}
	chat := metricsChat(c.MD.UserId, peer)
	top, err := c.svcCtx.Dao.Access.Top(c.ctx, chat, readmetrics.MaxMsgId(metrics), func(ctx context.Context) (int32, error) {
		return c.chatTop(ctx, peer, chat)
	})
	if err != nil {
		if !errors.Is(err, mtproto.ErrPeerIdInvalid) {
			c.Logger.Errorf("messages.reportReadMetrics - user %d, chat %d/%d: %v", c.MD.UserId, chat.PeerType, chat.PeerId, err)
		}
		return nil, err
	}
	if _, err = c.svcCtx.Dao.ReadMetrics.AddMetrics(c.ctx, c.MD.UserId, chat.PeerType, chat.PeerId, readmetrics.UpTo(metrics, top)); err != nil {
		c.Logger.Errorf("messages.reportReadMetrics - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// metricsChat is the chat of a report of metrics as it is stored: Saved
// Messages is the private chat with oneself.
func metricsChat(self int64, peer *mtproto.PeerUtil) readmetrics.Chat {
	if peer.PeerType == mtproto.PEER_SELF {
		return readmetrics.Chat{UserId: self, PeerType: mtproto.PEER_USER, PeerId: self}
	}
	return readmetrics.Chat{UserId: self, PeerType: peer.PeerType, PeerId: peer.PeerId}
}

// chatTop reads whether the user may see the chat, PEER_ID_INVALID if not,
// and the id of its newest message the user sees.
//
// A channel or a supergroup is read as the channel methods read it: a
// member, or anyone not kicked from a public one or reading a discussion
// group through its channel (bff channels readable, ADR 0034, p. 4); its
// ids are the channel's. A basic group needs its participant, and a
// private chat a history: their ids are of the user's box, the newest of
// which is the top.
func (c *ReportsCore) chatTop(ctx context.Context, peer *mtproto.PeerUtil, chat readmetrics.Chat) (int32, error) {
	switch chat.PeerType {
	case mtproto.PEER_CHANNEL:
		mc, err := c.svcCtx.Dao.ChannelGetMutableChannel(ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: chat.PeerId, Id: []int64{chat.UserId}})
		if err != nil {
			return 0, err
		}
		ch := mc.GetChannel()
		if ch.GetAccessHash() != peer.AccessHash || ch.GetDeleted() || !channelReadable(mc, chat.UserId) {
			return 0, mtproto.ErrPeerIdInvalid
		}
		return ch.GetTopMessage(), nil
	case mtproto.PEER_CHAT:
		mc, err := c.svcCtx.Dao.Chat.ChatGetMutableChat(ctx, &chatpb.TLChatGetMutableChat{ChatId: chat.PeerId})
		if err != nil {
			return 0, err
		}
		if p, ok := mc.GetImmutableChatParticipant(chat.UserId); !ok || !p.IsChatMemberStateNormal() {
			return 0, mtproto.ErrPeerIdInvalid
		}
	}
	last, err := c.svcCtx.Dao.MessageGetHistoryMessages(ctx, &message.TLMessageGetHistoryMessages{
		UserId:   chat.UserId,
		PeerType: chat.PeerType,
		PeerId:   chat.PeerId,
		Limit:    1,
	})
	if err != nil {
		return 0, err
	}
	var top int32
	for _, b := range last.GetDatas() {
		top = max(top, b.GetMessageId(), b.GetMessage().GetId())
	}
	if top == 0 && chat.PeerType == mtproto.PEER_USER {
		// No history with the user: no private chat to have read.
		return 0, mtproto.ErrPeerIdInvalid
	}
	return top, nil
}

// channelReadable tells whether userId may read the channel: a member, or
// anyone whose view of a public channel, or of the discussion group of a
// channel the user is in, is a channel (not channelForbidden). It is the
// rule of the channel methods (app/bff/channels/internal/core/channels.go
// readable).
func channelReadable(mc *channelpb.MutableChannel, userId int64) bool {
	p, _ := mc.GetParticipant(userId)
	if p.Member() {
		return true
	}
	return (mc.GetChannel().Public() || p.GetState() == channelpb.StateLinked) &&
		mc.ToChat(userId, int32(time.Now().Unix())).GetPredicateName() == mtproto.Predicate_channel
}
