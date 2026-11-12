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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/transcribe"
)

// MessagesRateTranscribedAudio
// messages.rateTranscribedAudio#7f1d072f peer:InputPeer msg_id:int transcription_id:long good:Bool = Bool;
//
// The caller's rating of the text it was given (ADR 0056, p. 6). The
// clients send the id they keep with the text and do not read the answer
// (tdesktop api_transcribes.cpp:42-56, Android ChatActivity.java:32552-32556,
// iOS Transcription.swift:100-143, Web K appMessagesManager.ts:3197-3215).
// The ratings are counted by model: that is where a bad model shows.
func (c *TranscriptionCore) MessagesRateTranscribedAudio(in *mtproto.TLMessagesRateTranscribedAudio) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	at := transcribe.Message{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.GetMsgId()}
	switch peer.PeerType {
	case mtproto.PEER_SELF:
		at.PeerType, at.PeerId = mtproto.PEER_USER, c.MD.UserId
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	// The id is the proof: a request exists only for a message its reader
	// could read when it asked.
	good := mtproto.FromBool(in.GetGood())
	model, changed, ok, err := c.svcCtx.Store.Rate(c.ctx, at, in.GetTranscriptionId(), good)
	if err != nil {
		c.Logger.Errorf("messages.rateTranscribedAudio - %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		return nil, mtproto.ErrMsgIdInvalid
	}
	if changed {
		transcribe.CountRating(model, good)
	}
	return mtproto.BoolTrue, nil
}
