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
	"path/filepath"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/transcribe"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	me          = int64(1001)
	friend      = int64(1002)
	voiceDoc    = int64(9001)
	channelId   = int64(77)
	channelHash = int64(4242)
)

func voiceDocument(id int64, seconds int32, size int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:          id,
		AccessHash:  id + 1,
		MimeType:    "audio/ogg",
		Size2_INT64: size,
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration_INT32: seconds}).To_DocumentAttribute(),
		},
	}).To_Document()
}

func roundDocument(id int64, seconds float64, size int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:          id,
		AccessHash:  id + 1,
		MimeType:    "video/mp4",
		Size2_INT64: size,
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{RoundMessage: true, W: 384, H: 384, Duration_FLOAT64: seconds}).To_DocumentAttribute(),
		},
	}).To_Document()
}

func withDocument(doc *mtproto.Document) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id:    5,
		Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
	}).To_Message()
}

func voiceMessage() *mtproto.Message { return withDocument(voiceDocument(voiceDoc, 12, 4096)) }

func TestRecordOf(t *testing.T) {
	rec, err := recordOf(voiceMessage())
	if err != nil || rec != (transcribe.Record{DocumentId: voiceDoc, AccessHash: voiceDoc + 1, Duration: 12, FileSize: 4096}) {
		t.Fatalf("a voice message: %+v, %v", rec, err)
	}
	rec, err = recordOf(withDocument(roundDocument(9002, 3.2, 100_000)))
	if err != nil || rec.DocumentId != 9002 || rec.Duration != 4 || rec.FileSize != 100_000 {
		t.Fatalf("a round video: %+v, %v", rec, err)
	}
	// No length or size is refused: four hours, half a gigabyte.
	rec, err = recordOf(withDocument(voiceDocument(1, 4*3600, 512<<20)))
	if err != nil || rec.Duration != 4*3600 || rec.FileSize != 512<<20 {
		t.Fatalf("a long record: %+v, %v", rec, err)
	}
	if _, err = recordOf(withDocument(roundDocument(2, 3*3600+0.5, 2<<30))); err != nil {
		t.Fatalf("a long round video: %v", err)
	}
}

func TestRecordOfRefuses(t *testing.T) {
	music := voiceDocument(1, 180, 4096)
	music.Attributes[0].Voice = false
	video := roundDocument(2, 30, 4096)
	video.Attributes[0].RoundMessage = false
	once := voiceMessage()
	once.Media.TtlSeconds = wrapperspb.Int32(0x7fffffff)
	noSize := voiceDocument(3, 5, 0)
	link := mtproto.MakeTLMessage(&mtproto.Message{Media: mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
		Webpage: mtproto.MakeTLWebPage(&mtproto.WebPage{Document: voiceDocument(4, 5, 4096)}).To_WebPage(),
	}).To_MessageMedia()}).To_Message()

	cases := []struct {
		name string
		m    *mtproto.Message
		want error
	}{
		{"a text", mtproto.MakeTLMessage(&mtproto.Message{Message: "hello"}).To_Message(), errVoiceMissing},
		{"a photo", mtproto.MakeTLMessage(&mtproto.Message{Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()}).To_Message(), errVoiceMissing},
		{"music", withDocument(music), errVoiceMissing},
		{"a video that is not round", withDocument(video), errVoiceMissing},
		{"a voice message in a link preview", link, errVoiceMissing},
		{"a voice message that plays once", once, errVoiceMissing},
		{"a document without a size", withDocument(noSize), errVoiceMissing},
		{"a service message", mtproto.MakeTLMessageService(&mtproto.Message{}).To_Message(), errVoiceMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := recordOf(c.m); !isError(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// fakeMessages is the box of every user: message id to its dialog.
type fakeMessages map[int32]*mtproto.MessageBox

func (f fakeMessages) MessageGetUserMessage(_ context.Context, in *messagepb.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	if box, ok := f[in.Id]; ok {
		return box, nil
	}
	return nil, mtproto.ErrMessageIdInvalid
}

// fakeChannels is one channel that me can read or cannot.
type fakeChannels struct {
	private  bool
	deleted  bool
	messages map[int32]*mtproto.Message
}

func (f *fakeChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if in.ChannelId != channelId {
		return nil, mtproto.ErrChannelInvalid
	}
	return &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: channelId, AccessHash: channelHash, Deleted: f.deleted}}, nil
}

func (f *fakeChannels) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	if f.private {
		return nil, mtproto.ErrChannelPrivate
	}
	out := &channelpb.Vector_Message{}
	for _, id := range in.Id {
		if m, ok := f.messages[id]; ok {
			out.Datas = append(out.Datas, m)
		}
	}
	return out, nil
}

type noFiles struct{}

func (noFiles) DfsDownloadFile(context.Context, *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	return nil, mtproto.ErrInternalServerError
}

// box is a message me sent to peerId, as msg keeps it in the box of me.
func box(peerType int32, peerId int64, m *mtproto.Message) *mtproto.MessageBox {
	return &mtproto.MessageBox{UserId: me, SenderUserId: me, PeerType: peerType, PeerId: peerId, Message: m}
}

// received is the copy of a private message from sender in the box of
// receiver, as msg keeps it: the peer is that of the send, the receiver.
func received(receiver, sender int64, m *mtproto.Message) *mtproto.MessageBox {
	return &mtproto.MessageBox{UserId: receiver, SenderUserId: sender, PeerType: mtproto.PEER_USER, PeerId: receiver, Message: m}
}

func testCore(messages svc.Messages, channels svc.Channels) *TranscriptionCore {
	ctx := context.Background()
	return &TranscriptionCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{
			Config:   config.Config{Transcribe: transcribe.Config{}.WithDefaults()},
			Messages: messages,
			Channels: channels,
			Files:    noFiles{},
		},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: me},
	}
}

func inputUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: 1}).To_InputPeer()
}

func inputChat(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: id}).To_InputPeer()
}

func inputChannel(id, hash int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: hash}).To_InputPeer()
}

var inputSelf = mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()

func TestVoiceFindsTheMessageOfEveryDialog(t *testing.T) {
	messages := fakeMessages{
		5: box(mtproto.PEER_USER, friend, voiceMessage()),
		6: box(mtproto.PEER_CHAT, 300, voiceMessage()),
		7: box(mtproto.PEER_USER, me, voiceMessage()), // Saved Messages
		9: received(me, friend, voiceMessage()),
	}
	channels := &fakeChannels{messages: map[int32]*mtproto.Message{8: voiceMessage()}}
	c := testCore(messages, channels)
	cases := []struct {
		name string
		peer *mtproto.InputPeer
		msg  int32
		want transcribe.Message
		// update is the peer of updateTranscribedAudio for the reader.
		update *mtproto.Peer
	}{
		{"a private chat", inputUser(friend), 5, transcribe.Message{UserId: me, PeerType: mtproto.PEER_USER, PeerId: friend, MsgId: 5}, mtproto.MakePeerUser(friend)},
		{"a private chat, received", inputUser(friend), 9, transcribe.Message{UserId: me, PeerType: mtproto.PEER_USER, PeerId: friend, MsgId: 9}, mtproto.MakePeerUser(friend)},
		{"a group", inputChat(300), 6, transcribe.Message{UserId: me, PeerType: mtproto.PEER_CHAT, PeerId: 300, MsgId: 6}, mtproto.MakePeerChat(300)},
		{"Saved Messages", inputSelf, 7, transcribe.Message{UserId: me, PeerType: mtproto.PEER_USER, PeerId: me, MsgId: 7}, mtproto.MakePeerUser(me)},
		{"Saved Messages by id", inputUser(me), 7, transcribe.Message{UserId: me, PeerType: mtproto.PEER_USER, PeerId: me, MsgId: 7}, mtproto.MakePeerUser(me)},
		{"a channel", inputChannel(channelId, channelHash), 8, transcribe.Message{UserId: me, PeerType: mtproto.PEER_CHANNEL, PeerId: channelId, MsgId: 8}, mtproto.MakePeerChannel(channelId)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at, rec, err := c.voice(tc.peer, tc.msg)
			if err != nil {
				t.Fatal(err)
			}
			if at != tc.want || rec.DocumentId != voiceDoc {
				t.Fatalf("got %+v, %+v", at, rec)
			}
			u := finalUpdate(transcribe.Owed{RequestId: 31, Message: at, Text: "the text"}).GetUpdates()
			if len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateTranscribedAudio {
				t.Fatalf("updates %v", u)
			}
			got := u[0]
			if got.GetPending() || got.GetTranscriptionId() != 31 || got.GetText() != "the text" || got.GetMsgId_INT32() != tc.msg {
				t.Errorf("update %v", got)
			}
			if p := got.GetPeer_PEER(); p.GetPredicateName() != tc.update.GetPredicateName() || p.GetUserId() != tc.update.GetUserId() ||
				p.GetChatId() != tc.update.GetChatId() || p.GetChannelId() != tc.update.GetChannelId() {
				t.Errorf("peer %v, want %v", p, tc.update)
			}
		})
	}
}

func TestVoiceErrors(t *testing.T) {
	messages := fakeMessages{
		5:  box(mtproto.PEER_USER, friend, voiceMessage()),
		9:  box(mtproto.PEER_USER, friend, mtproto.MakeTLMessage(&mtproto.Message{Message: "text"}).To_Message()),
		11: received(me, friend, voiceMessage()),
	}
	open := &fakeChannels{messages: map[int32]*mtproto.Message{
		8:  voiceMessage(),
		10: mtproto.MakeTLMessageEmpty(&mtproto.Message{Id: 10}).To_Message(),
	}}
	cases := []struct {
		name     string
		channels *fakeChannels
		peer     *mtproto.InputPeer
		msg      int32
		want     error
	}{
		{"no peer", open, mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), 5, mtproto.ErrPeerIdInvalid},
		{"no such message", open, inputUser(friend), 404, mtproto.ErrMsgIdInvalid},
		{"a message of another dialog", open, inputChat(300), 5, mtproto.ErrMsgIdInvalid},
		{"a message of another user's dialog", open, inputUser(1003), 5, mtproto.ErrMsgIdInvalid},
		{"a received message of another user's dialog", open, inputUser(1003), 11, mtproto.ErrMsgIdInvalid},
		{"a received message named as Saved Messages", open, inputSelf, 11, mtproto.ErrMsgIdInvalid},
		{"not a voice message", open, inputUser(friend), 9, errVoiceMissing},
		{"no such channel", open, inputChannel(78, channelHash), 8, mtproto.ErrPeerIdInvalid},
		{"a wrong access hash", open, inputChannel(channelId, 1), 8, mtproto.ErrPeerIdInvalid},
		{"a channel the caller cannot read", &fakeChannels{private: true}, inputChannel(channelId, channelHash), 8, mtproto.ErrPeerIdInvalid},
		{"a deleted channel", &fakeChannels{deleted: true}, inputChannel(channelId, channelHash), 8, mtproto.ErrPeerIdInvalid},
		{"no such message of the channel", open, inputChannel(channelId, channelHash), 404, mtproto.ErrMsgIdInvalid},
		{"a deleted message of the channel", open, inputChannel(channelId, channelHash), 10, mtproto.ErrMsgIdInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testCore(messages, tc.channels)
			if _, _, err := c.voice(tc.peer, tc.msg); !isError(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			// The handler answers the same, with or without an engine.
			_, err := c.MessagesTranscribeAudio(&mtproto.TLMessagesTranscribeAudio{Peer: tc.peer, MsgId: tc.msg})
			if !isError(err, tc.want) {
				t.Fatalf("the handler: got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTranscriptionOffIsAFailure(t *testing.T) {
	// No STT_URL: no engine, no store is touched, and the answer is the
	// error the clients show as a failed transcription.
	c := testCore(fakeMessages{5: box(mtproto.PEER_USER, friend, voiceMessage())}, &fakeChannels{})
	_, err := c.MessagesTranscribeAudio(&mtproto.TLMessagesTranscribeAudio{Peer: inputUser(friend), MsgId: 5})
	if !isError(err, mtproto.ErrTranscriptionFailed) {
		t.Fatalf("got %v", err)
	}
}

func TestBotsDoNotTranscribe(t *testing.T) {
	c := testCore(fakeMessages{}, &fakeChannels{})
	c.MD.IsBot = true
	if _, err := c.MessagesTranscribeAudio(&mtproto.TLMessagesTranscribeAudio{Peer: inputUser(friend), MsgId: 5}); !isError(err, mtproto.ErrBotMethodInvalid) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.MessagesRateTranscribedAudio(&mtproto.TLMessagesRateTranscribedAudio{Peer: inputUser(friend), MsgId: 5, TranscriptionId: 1}); !isError(err, mtproto.ErrBotMethodInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestPreparationGrowsWithTheDocument(t *testing.T) {
	// The claim covers the start of an attempt; the attempt then holds the
	// job for the time its document needs, and stops by itself before
	// another may take the job.
	if claimLease-leaseSlack < prepareTime(0) {
		t.Fatalf("a claim of %s, a preparation of %s", claimLease, prepareTime(0))
	}
	// An hour of a voice message is up to 15 MB; a large round video, a
	// gigabyte: each gets more time than the one before, by its size.
	short, hour, huge := prepareTime(64<<10), prepareTime(15<<20), prepareTime(1<<30)
	if short != prepareBase || hour < short+time.Minute || huge < time.Hour {
		t.Fatalf("%s, %s, %s", short, hour, huge)
	}
}

func TestRoom(t *testing.T) {
	dir := t.TempDir()
	if err := room(dir, 1<<50); !errors.Is(err, errNoRoom) {
		t.Fatalf("a record larger than the disk: %v", err)
	}
	if err := room(dir, 1<<10); err != nil {
		t.Fatalf("a small record (the disk of the test has a free GiB): %v", err)
	}
	// A directory that is not there is a failure, not a full disk.
	if err := room(filepath.Join(dir, "no-such"), 0); err == nil || errors.Is(err, errNoRoom) {
		t.Fatalf("no directory: %v", err)
	}
}
