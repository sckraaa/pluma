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

package msgsearch

import (
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// The kinds a filter of messages.search asks for that have no stored
// message_filter_type of their own; the others are mtproto.MEDIA_*.
const (
	KindEmpty       = int32(mtproto.MEDIA_EMPTY)
	KindGeo         = int32(100)
	KindContact     = int32(101)
	KindPoll        = int32(102)
	KindMyMentions  = int32(103)
	KindMissedCalls = int32(104)
)

// KindOf is the kind the filter asks for; ok is false for a filter no kind
// answers. inputMessagesFilterPoll is known here by its name: it has no
// mtproto.MessagesFilterType.
func KindOf(f *mtproto.MessagesFilter) (kind int32, ok bool) {
	switch f.GetPredicateName() {
	case mtproto.Predicate_inputMessagesFilterEmpty, "":
		return KindEmpty, true
	case mtproto.Predicate_inputMessagesFilterPhotos:
		return mtproto.MEDIA_PHOTOS_ONLY, true
	case mtproto.Predicate_inputMessagesFilterVideo:
		return mtproto.MEDIA_VIDEOS_ONLY, true
	case mtproto.Predicate_inputMessagesFilterPhotoVideo:
		return mtproto.MEDIA_PHOTOVIDEO, true
	case mtproto.Predicate_inputMessagesFilterDocument:
		return mtproto.MEDIA_FILE, true
	case mtproto.Predicate_inputMessagesFilterUrl:
		return mtproto.MEDIA_URL, true
	case mtproto.Predicate_inputMessagesFilterGif:
		return mtproto.MEDIA_GIF, true
	case mtproto.Predicate_inputMessagesFilterVoice:
		return mtproto.MEDIA_VOICE_FILE, true
	case mtproto.Predicate_inputMessagesFilterMusic:
		return mtproto.MEDIA_MUSIC, true
	case mtproto.Predicate_inputMessagesFilterChatPhotos:
		return mtproto.MEDIA_CHAT_PHOTO, true
	case mtproto.Predicate_inputMessagesFilterPhoneCalls:
		if f.GetMissed() {
			return KindMissedCalls, true
		}
		return mtproto.MEDIA_PHONE_CALL, true
	case mtproto.Predicate_inputMessagesFilterRoundVoice:
		return mtproto.MEDIA_AUDIO, true
	case mtproto.Predicate_inputMessagesFilterRoundVideo:
		return mtproto.MEDIA_ROUND_FILE, true
	case mtproto.Predicate_inputMessagesFilterMyMentions:
		return KindMyMentions, true
	case mtproto.Predicate_inputMessagesFilterGeo:
		return KindGeo, true
	case mtproto.Predicate_inputMessagesFilterContacts:
		return KindContact, true
	case mtproto.Predicate_inputMessagesFilterPinned:
		return mtproto.MEDIA_PINNED, true
	case mtproto.Predicate_inputMessagesFilterPoll:
		return KindPoll, true
	}
	return 0, false
}

// Where is the conditions of a query on messages, each after " AND ".
type Where struct {
	SQL  string
	Args []any
}

// And adds one condition.
func (w *Where) And(cond string, args ...any) {
	w.SQL += " AND " + cond
	w.Args = append(w.Args, args...)
}

// Columns name the columns of the table queried: Prefix ("m." with a table
// alias) goes before each of them.
type Columns struct {
	Prefix string
}

func (c Columns) col(name string) string { return c.Prefix + name }

// Kind narrows w to messages of kind. Service messages are left out but
// for the ones a kind is about (a change of the photo, a call).
// KindMyMentions is the caller's: only it knows where mentions are kept.
func (c Columns) Kind(w *Where, kind int32) {
	switch kind {
	case KindMyMentions:
		return
	case mtproto.MEDIA_CHAT_PHOTO:
		w.And(c.col("message_filter_type")+" = ?", kind)
		return
	case mtproto.MEDIA_PHONE_CALL, KindMissedCalls:
		w.And(c.col("message_filter_type")+" = ?", mtproto.MEDIA_PHONE_CALL)
		if kind == KindMissedCalls {
			w.And("JSON_UNQUOTE(JSON_EXTRACT("+c.col("message_data")+", '$.action.reason.predicate_name')) = ?",
				mtproto.Predicate_phoneCallDiscardReasonMissed)
		}
		return
	}
	w.And("JSON_UNQUOTE(JSON_EXTRACT("+c.col("message_data")+", '$.predicate_name')) = ?", mtproto.Predicate_message)
	switch kind {
	case KindEmpty:
	case mtproto.MEDIA_PINNED:
		w.And(c.col("pinned") + " = 1")
	case mtproto.MEDIA_PHOTOVIDEO:
		w.And(c.col("message_filter_type")+" IN (?, ?)", mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_VIDEOS_ONLY)
	case mtproto.MEDIA_VOICE_FILE:
		c.audio(w, "voice")
	case mtproto.MEDIA_ROUND_FILE:
		c.audio(w, "round_message")
	case KindGeo:
		c.mediaIn(w, mtproto.Predicate_messageMediaGeo, mtproto.Predicate_messageMediaGeoLive, mtproto.Predicate_messageMediaVenue)
	case KindContact:
		c.mediaIn(w, mtproto.Predicate_messageMediaContact)
	case KindPoll:
		c.mediaIn(w, mtproto.Predicate_messageMediaPoll)
	default:
		w.And(c.col("message_filter_type")+" = ?", kind)
	}
}

// audio is the stored MEDIA_AUDIO (voices and round videos together) with
// the flag of the document's attribute: documentAttributeAudio.voice,
// documentAttributeVideo.round_message.
func (c Columns) audio(w *Where, flag string) {
	w.And(c.col("message_filter_type")+" = ?", mtproto.MEDIA_AUDIO)
	w.And("JSON_CONTAINS(JSON_EXTRACT("+c.col("message_data")+", '$.media.document.attributes'), ?)", `{"`+flag+`":true}`)
}

func (c Columns) mediaIn(w *Where, predicates ...string) {
	args := make([]any, len(predicates))
	for i, p := range predicates {
		args[i] = p
	}
	w.And("JSON_UNQUOTE(JSON_EXTRACT("+c.col("message_data")+", '$.media.predicate_name')) IN (?"+
		strings.Repeat(", ?", len(predicates)-1)+")", args...)
}

// Text narrows w to the messages that match q (Patterns). For the kinds
// of the file lists a file's name and the title and performer of a track
// are searched with the caption: those lists search by name (Android
// SharedMediaLayout, iOS AttachmentFileController with
// messages.searchSentMedia).
func (c Columns) Text(w *Where, kind int32, q string) {
	patterns := Patterns(q)
	if len(patterns) == 0 {
		return
	}
	expr := c.col("message")
	if kind == mtproto.MEDIA_FILE || kind == mtproto.MEDIA_MUSIC {
		data := c.col("message_data")
		expr = "CONCAT_WS(' ', " + c.col("message") +
			", JSON_EXTRACT(" + data + ", '$.media.document.attributes[*].file_name')" +
			", JSON_EXTRACT(" + data + ", '$.media.document.attributes[*].title.value')" +
			", JSON_EXTRACT(" + data + ", '$.media.document.attributes[*].performer.value'))"
	}
	for _, p := range patterns {
		w.And("REGEXP_LIKE("+expr+", ?, 'i')", p)
	}
}

// Dates narrows w by min_date and max_date (0: no bound).
func (c Columns) Dates(w *Where, minDate, maxDate int32) {
	if minDate > 0 {
		w.And(c.col("date2")+" >= ?", minDate)
	}
	if maxDate > 0 {
		w.And(c.col("date2")+" <= ?", maxDate)
	}
}
