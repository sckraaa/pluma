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

// Package tgexport reads the export of one chat that Telegram Desktop
// writes as JSON ("Export chat history", machine-readable JSON): result.json
// and the folders of its files. It reads no database and no network (task
// 083, ADR 0051).
//
// The format is the one of tdesktop v7.4.1, Telegram/SourceFiles/export:
// the root of a single chat is output/export_output_json.cpp:2956-3001
// (name, type, id, messages), a message is SerializeMessage, 1527-2466, its
// text SerializeText, 161-242. What the export does not hold (who read
// what, albums, the pinned state, the date of a forward's original, the
// waveform of a voice message, the ids of custom emoji) is listed in the
// ADR.
package tgexport

// Kind is what a record of the export is.
type Kind int

const (
	// KindMessage is a message of a user ("type": "message").
	KindMessage Kind = iota
	// KindService is a service message ("type": "service").
	KindService
	// KindUnsupported is a message the export could not describe
	// ("type": "unsupported": only its id is written).
	KindUnsupported
)

// Peer is who a record names: Key is the export's id of it, "user<id>",
// "channel<id>" or "chat<id>" (wrapPeerId, export_output_json.cpp:1586-1596),
// empty when the export gives a name alone. Name is empty for a deleted
// account ("from": null).
type Peer struct {
	Key  string
	Name string
}

// Record is one element of "messages".
type Record struct {
	// Seq is its place in the export, from 1.
	Seq int64
	// Id is the message's id in Telegram. The history of the basic group
	// a supergroup grew from comes with ids shifted by -1000000000
	// (export_data_types.cpp:33, 3515-3523), so it may be negative.
	Id   int64
	Kind Kind
	// Date and Edited are unix times; Edited is 0 for a message never
	// edited. Edited is the message's edit_date whatever its edit_hide
	// flag was (ParseMessage, export_data_types.cpp:2910-2912): the export
	// does not tell an edit Telegram's clients show from one they hide.
	Date   int64
	Edited int64
	// From is the author of a message, or the actor of a service message.
	From Peer
	// Forward is set for a forwarded message.
	Forward *Peer
	// ReplyTo is the id of the message replied to (of a service message:
	// the message it is about); ReplyToPeer is set when that message is
	// in another chat.
	ReplyTo     int64
	ReplyToPeer string
	ViaBot      string
	Text        []TextPart
	Media       *Media
	Action      *Action
	Reactions   []Reaction
	// Buttons tells that the message had inline buttons of a bot.
	Buttons bool
}

// TextPart is a run of text with one kind of formatting
// (SerializeText, export_output_json.cpp:161-242). The export never nests
// them.
type TextPart struct {
	Type      string
	Text      string
	Href      string // text_link
	Language  string // pre
	UserId    int64  // mention_name: the Telegram id of the user
	Collapsed bool   // blockquote
}

// MediaType is the kind of what a message carries.
type MediaType string

const (
	MediaPhoto     MediaType = "photo"
	MediaVideo     MediaType = "video"
	MediaRound     MediaType = "round_video"
	MediaVoice     MediaType = "voice"
	MediaAnimation MediaType = "animation"
	MediaAudio     MediaType = "audio"
	MediaSticker   MediaType = "sticker"
	MediaFile      MediaType = "file"
	MediaContact   MediaType = "contact"
	MediaGeo       MediaType = "location"
	MediaVenue     MediaType = "venue"
	MediaPoll      MediaType = "poll"
	MediaTodo      MediaType = "todo_list"
	// MediaOther is what has no counterpart in an imported group: a game,
	// an invoice, a giveaway, paid media, a rich message. Media.Other
	// names it.
	MediaOther MediaType = "other"
)

// HasFile tells whether media of this type is a file of the export.
func (t MediaType) HasFile() bool {
	switch t {
	case MediaPhoto, MediaVideo, MediaRound, MediaVoice, MediaAnimation, MediaAudio, MediaSticker, MediaFile:
		return true
	}
	return false
}

// Missing is why the export holds no file: a string stands in place of
// its path (pushPath, export_output_json.cpp:1656-1678).
type Missing string

const (
	// MissingNotIncluded: the kind of file was not ticked in the export
	// settings.
	MissingNotIncluded Missing = "not_included"
	// MissingTooLarge: the file is above the size limit of the export
	// settings.
	MissingTooLarge Missing = "too_large"
	// MissingUnavailable: Telegram did not give the file.
	MissingUnavailable Missing = "unavailable"
	// MissingOnDisk: the export names the file, the folder does not have
	// it (set by the plan, not by the reader).
	MissingOnDisk Missing = "not_on_disk"
)

// Media is what a message carries besides its text.
type Media struct {
	Type MediaType
	// Path is the file's path relative to the export's folder; empty
	// when Missing says why there is none.
	Path    string
	Missing Missing
	// Size is the size the export states.
	Size     int64
	FileName string
	MimeType string
	Duration int32
	Width    int32
	Height   int32
	// StickerEmoji is the emoji of a sticker; Performer and Title are of
	// an audio file.
	StickerEmoji string
	Performer    string
	Title        string
	Spoiler      bool

	Contact *Contact
	Geo     *Geo
	Venue   *Venue
	Poll    *Poll
	Todo    *Todo
	// Other names the media of MediaOther.
	Other string
}

// Contact is a shared contact.
type Contact struct {
	FirstName string
	LastName  string
	Phone     string
}

// Geo is a point of a map.
type Geo struct {
	Lat  float64
	Long float64
}

// Venue is a place: a point with a name and an address.
type Venue struct {
	Geo     Geo
	Title   string
	Address string
}

// Poll is a poll as the export holds it: no kind (quiz, several answers)
// and nobody's votes but the exporter's own (Chosen).
type Poll struct {
	Question []TextPart
	Closed   bool
	Total    int
	Answers  []PollAnswer
}

// PollAnswer is an answer of a poll.
type PollAnswer struct {
	Text   []TextPart
	Voters int
	Chosen bool
}

// Todo is a checklist; who ticked what is not in the export.
type Todo struct {
	Title             []TextPart
	OthersCanAppend   bool
	OthersCanComplete bool
	Items             []TodoItem
}

// TodoItem is an item of a checklist.
type TodoItem struct {
	Id   int32
	Text []TextPart
}

// Action is what a service message tells. Members and Inviter are names
// alone: the export writes no ids for them (pushUserNames,
// export_output_json.cpp:1633-1641); a deleted account is an empty name.
type Action struct {
	Name    string
	Title   string
	Members []string
	Inviter string
	// Photo is the new photo of edit_group_photo.
	Photo *Media
}

// Reaction is one reaction of a message: how many set it, and the few
// Telegram named (messageReactions.recent_reactions, ParseReactions,
// export_data_types.cpp:1259-1296). The export never asks for the full list.
type Reaction struct {
	// Type is "emoji", "custom_emoji" or "paid".
	Type  string
	Count int
	// Emoji is set for the type "emoji". A custom emoji has no id in the
	// export: the path of its downloaded file stands in its place
	// (export_api_wrap.cpp:2614-2681).
	Emoji  string
	Recent []RecentReaction
}

// RecentReaction is one named reaction.
type RecentReaction struct {
	From Peer
	Date int64
}
