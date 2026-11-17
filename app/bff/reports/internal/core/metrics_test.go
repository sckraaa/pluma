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
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/readmetrics"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	reader      = int64(1001)
	stranger    = int64(1002)
	friend      = int64(1003)
	channelId   = int64(77)
	channelHash = int64(4242)
	publicId    = int64(78)
	chatId      = int64(55)
	musicId     = int64(9001)
	musicHash   = int64(9002)
)

// world is the biz of the tests: a private channel whose member is
// reader, a public one nobody joined, a basic group of reader and a
// private chat of reader and friend.
type world struct {
	channel_client.ChannelClient
	message_client.MessageClient
	chat_client.ChatClient
	media_client.MediaClient

	top          int32 // the newest message of the channels
	boxTop       int32 // the newest message of reader's box with friend and in the group
	channelReads int
}

func (w *world) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	w.channelReads++
	switch in.ChannelId {
	case channelId:
		return &channelpb.MutableChannel{
			Channel:      &channelpb.ImmutableChannel{Id: channelId, AccessHash: channelHash, Broadcast: true, TopMessage: w.top, Title: "news"},
			Participants: []*channelpb.ImmutableChannelParticipant{{UserId: reader, State: channelpb.StateIn}},
		}, nil
	case publicId:
		return &channelpb.MutableChannel{
			Channel: &channelpb.ImmutableChannel{Id: publicId, AccessHash: channelHash, Broadcast: true, Username: "open", TopMessage: w.top, Title: "open"},
		}, nil
	}
	return nil, mtproto.ErrChannelInvalid
}

func (w *world) ChatGetMutableChat(_ context.Context, in *chatpb.TLChatGetMutableChat) (*mtproto.MutableChat, error) {
	if in.ChatId != chatId {
		return nil, mtproto.ErrChatIdInvalid
	}
	return &mtproto.MutableChat{
		Chat:             &mtproto.ImmutableChat{Id: chatId},
		ChatParticipants: []*mtproto.ImmutableChatParticipant{{UserId: reader, State: mtproto.ChatMemberStateNormal}, {UserId: stranger, State: mtproto.ChatMemberStateLeft}},
	}, nil
}

func (w *world) MessageGetHistoryMessages(_ context.Context, in *messagepb.TLMessageGetHistoryMessages) (*messagepb.Vector_MessageBox, error) {
	out := &messagepb.Vector_MessageBox{}
	if in.UserId == reader && in.Limit == 1 && (in.PeerType == mtproto.PEER_USER && in.PeerId == friend || in.PeerType == mtproto.PEER_CHAT && in.PeerId == chatId) {
		out.Datas = []*mtproto.MessageBox{{UserId: reader, MessageId: w.boxTop, Message: mtproto.MakeTLMessage(&mtproto.Message{Id: w.boxTop}).To_Message()}}
	}
	return out, nil
}

func (w *world) MediaGetDocument(_ context.Context, in *mediapb.TLMediaGetDocument) (*mtproto.Document, error) {
	if in.Id == musicId {
		return mtproto.MakeTLDocument(&mtproto.Document{Id: musicId, AccessHash: musicHash}).To_Document(), nil
	}
	return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.Id}).To_Document(), nil
}

type added struct {
	chat    readmetrics.Chat
	metrics []readmetrics.Metric
}

type listen struct {
	user, doc int64
	duration  int32
}

type memMetrics struct {
	added   []added
	listens []listen
	fail    error
}

func (m *memMetrics) AddMetrics(_ context.Context, userId int64, peerType int32, peerId int64, ms []readmetrics.Metric) (int64, error) {
	if m.fail != nil {
		return 0, m.fail
	}
	m.added = append(m.added, added{readmetrics.Chat{UserId: userId, PeerType: peerType, PeerId: peerId}, ms})
	return int64(len(ms)), nil
}

func (m *memMetrics) AddListen(_ context.Context, userId, documentId int64, duration int32) error {
	if m.fail != nil {
		return m.fail
	}
	m.listens = append(m.listens, listen{userId, documentId, duration})
	return nil
}

func metricsCore(w *world, store *memMetrics, userId int64, bot bool) *ReportsCore {
	ctx := context.Background()
	return &ReportsCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{
			ChannelClient: w,
			MessageClient: w,
			Chat:          w,
			Media:         w,
			ReadMetrics:   store,
			Access:        readmetrics.NewAccessCache(),
		}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: userId, IsBot: bot},
	}
}

func metric(msgId int32) *mtproto.InputMessageReadMetric {
	return mtproto.MakeTLInputMessageReadMetric(&mtproto.InputMessageReadMetric{
		MsgId: msgId, ViewId: int64(msgId) * 10, TimeInViewMs: 1500, ActiveTimeInViewMs: 700, HeightToViewportRatioPermille: 1800, SeenRangeRatioPermille: 1000,
	}).To_InputMessageReadMetric()
}

func channelPeer(id, hash int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: hash}).To_InputPeer()
}

func userPeer(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
}

func chatPeer(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: id}).To_InputPeer()
}

func readMetrics(peer *mtproto.InputPeer, ms ...*mtproto.InputMessageReadMetric) *mtproto.TLMessagesReportReadMetrics {
	return &mtproto.TLMessagesReportReadMetrics{Peer: peer, Metrics: ms}
}

func TestReportReadMetricsStores(t *testing.T) {
	w, store := &world{top: 20, boxTop: 300}, &memMetrics{}
	c := metricsCore(w, store, reader, false)
	for _, tc := range []struct {
		name string
		peer *mtproto.InputPeer
		ids  []int32
		chat readmetrics.Chat
		kept int
	}{
		{"a channel of the reader", channelPeer(channelId, channelHash), []int32{5, 20}, readmetrics.Chat{UserId: reader, PeerType: mtproto.PEER_CHANNEL, PeerId: channelId}, 2},
		{"a public channel not joined", channelPeer(publicId, channelHash), []int32{3}, readmetrics.Chat{UserId: reader, PeerType: mtproto.PEER_CHANNEL, PeerId: publicId}, 1},
		{"a basic group", chatPeer(chatId), []int32{299, 300}, readmetrics.Chat{UserId: reader, PeerType: mtproto.PEER_CHAT, PeerId: chatId}, 2},
		{"a private chat", userPeer(friend), []int32{300}, readmetrics.Chat{UserId: reader, PeerType: mtproto.PEER_USER, PeerId: friend}, 1},
		// A message newer than the chat has is not kept.
		{"beyond the newest", channelPeer(channelId, channelHash), []int32{20, 21}, readmetrics.Chat{UserId: reader, PeerType: mtproto.PEER_CHANNEL, PeerId: channelId}, 1},
	} {
		store.added = nil
		ms := make([]*mtproto.InputMessageReadMetric, 0, len(tc.ids))
		for _, id := range tc.ids {
			ms = append(ms, metric(id))
		}
		ok, err := c.MessagesReportReadMetrics(readMetrics(tc.peer, ms...))
		if err != nil || !mtproto.FromBool(ok) {
			t.Errorf("%s: %v, %v", tc.name, ok, err)
			continue
		}
		if len(store.added) != 1 || store.added[0].chat != tc.chat || len(store.added[0].metrics) != tc.kept {
			t.Errorf("%s: stored %+v, want %d metrics of %+v", tc.name, store.added, tc.kept, tc.chat)
			continue
		}
		if m := store.added[0].metrics[0]; m.MsgId != tc.ids[0] || m.ViewId != int64(tc.ids[0])*10 || m.HeightToViewportRatioPermille != 1800 {
			t.Errorf("%s: the stored metric %+v", tc.name, m)
		}
	}
}

// The batches of an open chat read the channel once a minute; a message
// newer than the cached top reads it again.
func TestReportReadMetricsCachesAccess(t *testing.T) {
	w, store := &world{top: 20}, &memMetrics{}
	c := metricsCore(w, store, reader, false)
	for range 3 {
		if _, err := c.MessagesReportReadMetrics(readMetrics(channelPeer(channelId, channelHash), metric(7))); err != nil {
			t.Fatal(err)
		}
	}
	if w.channelReads != 1 {
		t.Errorf("%d reads of the channel for three batches", w.channelReads)
	}
	w.top = 21
	if _, err := c.MessagesReportReadMetrics(readMetrics(channelPeer(channelId, channelHash), metric(21))); err != nil {
		t.Fatal(err)
	}
	if w.channelReads != 2 || len(store.added[len(store.added)-1].metrics) != 1 {
		t.Errorf("a new message: %d reads, stored %+v", w.channelReads, store.added[len(store.added)-1])
	}
}

func TestReportReadMetricsRefuses(t *testing.T) {
	w := &world{top: 20, boxTop: 300}
	tooMany := make([]*mtproto.InputMessageReadMetric, readmetrics.MaxMetrics+1)
	for i := range tooMany {
		tooMany[i] = metric(1)
	}
	badSeen := metric(1)
	badSeen.SeenRangeRatioPermille = 1001
	for _, tc := range []struct {
		name string
		user int64
		bot  bool
		req  *mtproto.TLMessagesReportReadMetrics
		want error
	}{
		{"a bot", reader, true, readMetrics(channelPeer(channelId, channelHash), metric(1)), mtproto.ErrBotMethodInvalid},
		{"no peer", reader, false, readMetrics(nil, metric(1)), mtproto.ErrPeerIdInvalid},
		{"an empty peer", reader, false, readMetrics(mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), metric(1)), mtproto.ErrPeerIdInvalid},
		{"too many", reader, false, readMetrics(channelPeer(channelId, channelHash), tooMany...), mtproto.ErrLimitInvalid},
		{"msg_id 0", reader, false, readMetrics(channelPeer(channelId, channelHash), metric(0)), mtproto.ErrMsgIdInvalid},
		{"seen over the whole", reader, false, readMetrics(channelPeer(channelId, channelHash), badSeen), mtproto.ErrInputRequestInvalid},
		{"a private channel of others", stranger, false, readMetrics(channelPeer(channelId, channelHash), metric(1)), mtproto.ErrPeerIdInvalid},
		{"a wrong access hash", reader, false, readMetrics(channelPeer(channelId, channelHash+1), metric(1)), mtproto.ErrPeerIdInvalid},
		{"a group left", stranger, false, readMetrics(chatPeer(chatId), metric(1)), mtproto.ErrPeerIdInvalid},
		{"a private chat without history", reader, false, readMetrics(userPeer(stranger), metric(1)), mtproto.ErrPeerIdInvalid},
	} {
		store := &memMetrics{}
		_, err := metricsCore(w, store, tc.user, tc.bot).MessagesReportReadMetrics(tc.req)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
		if len(store.added) != 0 {
			t.Errorf("%s: stored %+v", tc.name, store.added)
		}
	}
	// No metrics is nothing to check or keep.
	store, fresh := &memMetrics{}, &world{top: 20}
	if ok, err := metricsCore(fresh, store, stranger, false).MessagesReportReadMetrics(readMetrics(channelPeer(channelId, channelHash))); err != nil || !mtproto.FromBool(ok) || fresh.channelReads != 0 {
		t.Errorf("no metrics: %v, %v, %d reads", ok, err, fresh.channelReads)
	}
	// A failed write is the client's error, not a success.
	failing := &memMetrics{fail: errors.New("mysql down")}
	if _, err := metricsCore(w, failing, reader, false).MessagesReportReadMetrics(readMetrics(channelPeer(channelId, channelHash), metric(1))); err == nil {
		t.Error("a failed write answered true")
	}
}

func musicListen(id *mtproto.InputDocument, duration int32) *mtproto.TLMessagesReportMusicListen {
	return &mtproto.TLMessagesReportMusicListen{Id: id, ListenedDuration: duration}
}

func inputDocument(id, hash int64) *mtproto.InputDocument {
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: id, AccessHash: hash}).To_InputDocument()
}

func TestReportMusicListen(t *testing.T) {
	w, store := &world{}, &memMetrics{}
	ok, err := metricsCore(w, store, reader, false).MessagesReportMusicListen(musicListen(inputDocument(musicId, musicHash), 187))
	if err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("a listen: %v, %v", ok, err)
	}
	if len(store.listens) != 1 || store.listens[0] != (listen{reader, musicId, 187}) {
		t.Errorf("stored %+v", store.listens)
	}
	if _, err = metricsCore(w, store, reader, false).MessagesReportMusicListen(musicListen(inputDocument(musicId, musicHash), 0)); err != nil {
		t.Errorf("zero seconds: %v", err)
	}

	for _, tc := range []struct {
		name string
		bot  bool
		req  *mtproto.TLMessagesReportMusicListen
		want error
	}{
		{"a bot", true, musicListen(inputDocument(musicId, musicHash), 10), mtproto.ErrBotMethodInvalid},
		{"no document", false, musicListen(nil, 10), mtproto.ErrDocumentInvalid},
		{"inputDocumentEmpty", false, musicListen(mtproto.MakeTLInputDocumentEmpty(nil).To_InputDocument(), 10), mtproto.ErrDocumentInvalid},
		{"id 0", false, musicListen(inputDocument(0, musicHash), 10), mtproto.ErrDocumentInvalid},
		{"negative seconds", false, musicListen(inputDocument(musicId, musicHash), -1), mtproto.ErrInputRequestInvalid},
		{"a document the server lacks", false, musicListen(inputDocument(musicId+5, musicHash), 10), mtproto.ErrDocumentInvalid},
		{"a wrong access hash", false, musicListen(inputDocument(musicId, musicHash+1), 10), mtproto.ErrDocumentInvalid},
	} {
		store := &memMetrics{}
		if _, err := metricsCore(w, store, reader, tc.bot).MessagesReportMusicListen(tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
		if len(store.listens) != 0 {
			t.Errorf("%s: stored %+v", tc.name, store.listens)
		}
	}
}
