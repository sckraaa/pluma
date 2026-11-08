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

package channelperm

import "github.com/teamgram/proto/mtproto"

// MessageActions are the actions sending m is, by what it carries
// (chatBannedRights, core.telegram.org/constructor/chatBannedRights): a
// plain message, or a message with its kind of media, and one through an
// inline bot also SendInline. A text with a link preview is a plain
// message: the preview is EmbedLinks, which the sender drops rather than
// refuse the message (CanEmbedLinks). A caption goes with its media.
func MessageActions(m *mtproto.Message) []Action {
	var out []Action
	media := m.GetMedia()
	switch media.GetPredicateName() {
	case "", mtproto.Predicate_messageMediaEmpty, mtproto.Predicate_messageMediaWebPage:
		out = append(out, SendPlain)
	case mtproto.Predicate_messageMediaPhoto:
		out = append(out, SendPhotos)
	case mtproto.Predicate_messageMediaDocument:
		out = append(out, documentAction(media.GetDocument()))
	case mtproto.Predicate_messageMediaPoll, mtproto.Predicate_messageMediaToDo:
		// A checklist is posted under the right to post polls
		// (tdesktop PeerData::canCreateTodoLists).
		out = append(out, SendPolls)
	case mtproto.Predicate_messageMediaGame:
		out = append(out, SendGames)
	case mtproto.Predicate_messageMediaDice:
		// An animated emoji, sent from the sticker panel.
		out = append(out, SendStickers)
	default:
		// Location, contact, venue and the rest have no flag of their own:
		// send_media forbids them.
		out = append(out, SendMedia)
	}
	if m.GetViaBotId() != nil && m.GetViaBotId().GetValue() != 0 {
		out = append(out, SendInline)
	}
	return out
}

// documentAction is the kind of a document by its attributes.
func documentAction(d *mtproto.Document) Action {
	kind := SendDocs
	for _, a := range d.GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeSticker:
			return SendStickers
		case mtproto.Predicate_documentAttributeAnimated:
			return SendGifs
		case mtproto.Predicate_documentAttributeVideo:
			if a.GetRoundMessage() {
				return SendRoundVideos
			}
			kind = SendVideos
		case mtproto.Predicate_documentAttributeAudio:
			if a.GetVoice() {
				return SendVoices
			}
			kind = SendAudios
		}
	}
	return kind
}
