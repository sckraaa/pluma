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

package layercompat

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// Voice transcription (task 090) has one form on every layer the server
// speaks: messages.transcribedAudio#cfb9d957 and updateTranscribedAudio#84cd5a
// reach a client of layer 228 and of layer 233 whole.
func TestTranscriptionOnEveryLayer(t *testing.T) {
	for _, layer := range []int32{228, 233} {
		pending := mtproto.MakeTLMessagesTranscribedAudio(&mtproto.Messages_TranscribedAudio{Pending: true, TranscriptionId: 41}).To_Messages_TranscribedAudio()
		b, err := EncodeFor(pending, layer)
		if err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		if id := uint32(int32FromBytes(b)); id != 0xcfb9d957 {
			t.Fatalf("layer %d: constructor %x", layer, id)
		}
		// flags.0 is pending; flags.1, the trial fields, is never set.
		if flags := int32FromBytes(b[4:]); flags != 1 {
			t.Fatalf("layer %d: flags %b of a pending answer", layer, flags)
		}
		got := &mtproto.Messages_TranscribedAudio{}
		if err = got.Decode(mtproto.NewDecodeBuf(b)); err != nil || !got.GetPending() || got.GetTranscriptionId() != 41 || got.GetText() != "" {
			t.Fatalf("layer %d: decoded %v, %v", layer, got, err)
		}

		final := mtproto.MakeTLMessagesTranscribedAudio(&mtproto.Messages_TranscribedAudio{TranscriptionId: 41, Text: "Привет."}).To_Messages_TranscribedAudio()
		if b, err = EncodeFor(final, layer); err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		if flags := int32FromBytes(b[4:]); flags != 0 {
			t.Fatalf("layer %d: flags %b of a final answer", layer, flags)
		}

		updates := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateTranscribedAudio(&mtproto.Update{
			Peer_PEER:       mtproto.MakePeerChannel(5),
			MsgId_INT32:     9,
			TranscriptionId: 41,
			Text:            "Привет.",
		}).To_Update())
		o, err := For(updates, layer)
		if err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		if kept := o.(*mtproto.Updates).GetUpdates(); len(kept) != 1 {
			t.Fatalf("layer %d: the update was left out: %v", layer, kept)
		}
		if b, err = EncodeFor(updates, layer); err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		back := &mtproto.Updates{}
		if err = back.Decode(mtproto.NewDecodeBuf(b)); err != nil || len(back.GetUpdates()) != 1 {
			t.Fatalf("layer %d: decoded %v, %v", layer, back, err)
		}
		u := back.GetUpdates()[0]
		if u.GetPredicateName() != mtproto.Predicate_updateTranscribedAudio || u.GetPending() || u.GetPeer_PEER().GetChannelId() != 5 ||
			u.GetMsgId_INT32() != 9 || u.GetTranscriptionId() != 41 || u.GetText() != "Привет." {
			t.Fatalf("layer %d: update %v", layer, u)
		}
	}
}
