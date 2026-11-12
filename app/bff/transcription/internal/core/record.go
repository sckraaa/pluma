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
	"math"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
)

// voice finds the message the caller asks about and the record in it. The
// message is read as every other handler reads it: the caller's copy in a
// private chat or a group, the message of a channel the caller can read.
// The errors are those of core.telegram.org/method/messages.transcribeAudio:
//
//   - PEER_ID_INVALID: no such dialog for the caller, a channel it cannot
//     read among them;
//   - MSG_ID_INVALID: no such message in the dialog;
//   - MSG_VOICE_MISSING: the message is no voice message and no round
//     video, or one that plays once.
//
// MSG_VOICE_TOO_LONG is never answered: a record of any length is
// transcribed (the owner's decision of 2026-11-12, ADR 0056).
//
// Scheduled messages and secret chats never get here as such: the first
// are in a store of their own under ids of their own, the second have no
// InputPeer.
func (c *TranscriptionCore) voice(in *mtproto.InputPeer, msgId int32) (transcribe.Message, transcribe.Record, error) {
	var (
		peer = mtproto.FromInputPeer2(c.MD.UserId, in)
		at   = transcribe.Message{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: msgId}
		m    *mtproto.Message
	)
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
		if peer.PeerType == mtproto.PEER_SELF {
			// Saved Messages are the dialog with oneself.
			at.PeerType, at.PeerId = mtproto.PEER_USER, c.MD.UserId
		}
		box, err := c.svcCtx.Messages.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgId})
		if isError(err, mtproto.ErrMessageIdInvalid) {
			return at, transcribe.Record{}, mtproto.ErrMsgIdInvalid
		}
		if err != nil {
			c.Logger.Errorf("messages.transcribeAudio - message %d of user %d: %v", msgId, c.MD.UserId, err)
			return at, transcribe.Record{}, err
		}
		if box.GetMessage() == nil || box.GetPeerType() != at.PeerType || dialogOf(box) != at.PeerId {
			return at, transcribe.Record{}, mtproto.ErrMsgIdInvalid
		}
		m = box.GetMessage()
	case mtproto.PEER_CHANNEL:
		mc, err := c.svcCtx.Channels.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
		if isError(err, mtproto.ErrChannelInvalid) {
			return at, transcribe.Record{}, mtproto.ErrPeerIdInvalid
		}
		if err != nil {
			c.Logger.Errorf("messages.transcribeAudio - channel %d: %v", peer.PeerId, err)
			return at, transcribe.Record{}, err
		}
		if mc.GetChannel().GetAccessHash() != peer.AccessHash || mc.GetChannel().GetDeleted() {
			return at, transcribe.Record{}, mtproto.ErrPeerIdInvalid
		}
		// channel.getMessages gives only what the caller may read: a
		// channel that is private to it is CHANNEL_PRIVATE.
		messages, err := c.svcCtx.Channels.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{msgId}})
		if isError(err, mtproto.ErrChannelPrivate) {
			return at, transcribe.Record{}, mtproto.ErrPeerIdInvalid
		}
		if err != nil {
			c.Logger.Errorf("messages.transcribeAudio - message %d of channel %d: %v", msgId, peer.PeerId, err)
			return at, transcribe.Record{}, err
		}
		if len(messages.GetDatas()) == 0 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
			return at, transcribe.Record{}, mtproto.ErrMsgIdInvalid
		}
		m = messages.GetDatas()[0]
	default:
		return at, transcribe.Record{}, mtproto.ErrPeerIdInvalid
	}
	rec, err := recordOf(m)
	return at, rec, err
}

// dialogOf is the id of the user, chat or channel of the dialog box is in,
// for its owner. msg keeps the peer of the send in every copy of a private
// message (app/messenger/msg/internal/dao/send.go): the receiver's copy
// names the receiver, and its dialog is the sender's.
func dialogOf(box *mtproto.MessageBox) int64 {
	if box.GetPeerType() == mtproto.PEER_USER && box.GetPeerId() == box.GetUserId() {
		return box.GetSenderUserId()
	}
	return box.GetPeerId()
}

// recordOf is the voice message or the round video of m. The clients offer
// to transcribe those and nothing else: not music, not a video, not what a
// link preview shows (Android ChatMessageCell.java:11143-11173, tdesktop
// history_view_document.cpp:501-510 and history_view_gif.cpp:2730-2747,
// iOS ChatMessageInteractiveFileNode.swift:787-793, Web K
// wrappers/document.ts:153).
func recordOf(m *mtproto.Message) (transcribe.Record, error) {
	media := m.GetMedia()
	doc := media.GetDocument()
	if media.GetPredicateName() != mtproto.Predicate_messageMediaDocument || doc.GetPredicateName() != mtproto.Predicate_document {
		return transcribe.Record{}, errVoiceMissing
	}
	// A record that plays once is not kept as text (the owner's decision
	// of 2026-11-12); the clients do not offer it either (Android
	// ChatMessageCell.java:11143-11173, tdesktop
	// history_view_document.cpp:501-510, iOS
	// ChatMessageInteractiveFileNode.swift:787-793).
	if media.GetTtlSeconds() != nil {
		return transcribe.Record{}, errVoiceMissing
	}
	var seconds float64
	switch {
	case mtproto.IsVoiceDocument(doc):
		for _, a := range doc.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeAudio {
				seconds = max(float64(a.GetDuration_INT32()), a.GetDuration())
			}
		}
	case mtproto.IsRoundVideoDocument(doc):
		for _, a := range doc.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeVideo {
				seconds = max(a.GetDuration_FLOAT64(), a.GetDuration(), float64(a.GetDuration_INT32()))
			}
		}
	default:
		return transcribe.Record{}, errVoiceMissing
	}
	size := max(doc.GetSize2_INT64(), doc.GetSize2(), int64(doc.GetSize2_INT32()))
	if doc.GetId() == 0 || size <= 0 {
		return transcribe.Record{}, errVoiceMissing
	}
	// The duration is the client's word, kept for the operator: nothing is
	// refused or timed by it.
	return transcribe.Record{
		DocumentId: doc.GetId(),
		AccessHash: doc.GetAccessHash(),
		Duration:   int32(math.Ceil(max(seconds, 0))),
		FileSize:   size,
	}, nil
}

// peerOf is the dialog of at as the updates name it to its reader: the
// clients find the message of updateTranscribedAudio by this peer and the
// id (iOS UpdateInterpreter+Messages.swift:223-226, Web K
// appMessagesManager.ts:13053-13077, Android
// MessagesController.java:20800-20811).
func peerOf(at transcribe.Message) *mtproto.Peer {
	switch at.PeerType {
	case mtproto.PEER_CHAT:
		return mtproto.MakePeerChat(at.PeerId)
	case mtproto.PEER_CHANNEL:
		return mtproto.MakePeerChannel(at.PeerId)
	}
	return mtproto.MakePeerUser(at.PeerId)
}
