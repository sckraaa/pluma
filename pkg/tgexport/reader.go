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

package tgexport

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ResultFile is the name of the export's JSON file.
const ResultFile = "result.json"

// Header is the head of the export of one chat.
type Header struct {
	// Name is the chat's title; empty when the export has none.
	Name string
	// Type is "private_group", "private_supergroup", "public_supergroup"
	// and so on (TypeString, export_output_json.cpp:2966-2981).
	Type string
	// Id is the bare id of the chat in Telegram.
	Id int64
}

// Group tells whether the export is of a group or a supergroup, the only
// chats that are imported.
func (h Header) Group() bool {
	switch h.Type {
	case "private_group", "private_supergroup", "public_supergroup":
		return true
	}
	return false
}

// Export is an export folder.
type Export struct {
	dir    string
	header Header
}

// ErrNotChatExport: result.json is not the export of one chat.
var ErrNotChatExport = errors.New("tgexport: result.json is not the export of one chat (export the group itself: Export chat history, format JSON)")

// Open reads the head of dir/result.json.
func Open(dir string) (*Export, error) {
	e := &Export{dir: dir}
	found := false
	err := e.walk(func(key string, dec *json.Decoder) (bool, error) {
		switch key {
		case "name":
			var name *string
			if err := dec.Decode(&name); err != nil {
				return false, err
			}
			if name != nil {
				e.header.Name = *name
			}
		case "type":
			return true, dec.Decode(&e.header.Type)
		case "id":
			return true, dec.Decode(&e.header.Id)
		case "messages":
			found = true
			return false, nil
		default:
			var skip json.RawMessage
			return true, dec.Decode(&skip)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if !found || e.header.Id == 0 || e.header.Type == "" {
		return nil, ErrNotChatExport
	}
	return e, nil
}

// Dir is the export's folder.
func (e *Export) Dir() string { return e.dir }

// Header is the head of the export.
func (e *Export) Header() Header { return e.header }

// walk reads the keys of the root object: fn decodes the value of key and
// tells whether to go on. The value of a key fn stops at is left unread.
func (e *Export) walk(fn func(key string, dec *json.Decoder) (bool, error)) error {
	f, err := os.Open(filepath.Join(e.dir, ResultFile))
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	if err = expect(dec, json.Delim('{')); err != nil {
		return err
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return fmt.Errorf("tgexport: %s: %w", ResultFile, err)
		}
		key, ok := t.(string)
		if !ok {
			return fmt.Errorf("tgexport: %s: a key expected at offset %d", ResultFile, dec.InputOffset())
		}
		more, err := fn(key, dec)
		var cb callbackError
		if errors.As(err, &cb) {
			return cb.err
		}
		if err != nil {
			return fmt.Errorf("tgexport: %s: %q: %w", ResultFile, key, err)
		}
		if !more {
			return nil
		}
	}
	return nil
}

func expect(dec *json.Decoder, want json.Delim) error {
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("tgexport: %s: %w", ResultFile, err)
	}
	if d, ok := t.(json.Delim); !ok || d != want {
		return fmt.Errorf("tgexport: %s: %q expected at offset %d", ResultFile, want, dec.InputOffset())
	}
	return nil
}

// Each calls fn for every record of the export, in its order. The file is
// read as a stream: a record is in memory only while fn has it. An error of
// fn stops the reading and is returned.
func (e *Export) Each(fn func(*Record) error) error {
	return e.walk(func(key string, dec *json.Decoder) (bool, error) {
		if key != "messages" {
			var skip json.RawMessage
			return true, dec.Decode(&skip)
		}
		if err := expect(dec, json.Delim('[')); err != nil {
			return false, err
		}
		var seq int64
		for dec.More() {
			seq++
			var raw rawMessage
			if err := dec.Decode(&raw); err != nil {
				return false, fmt.Errorf("record %d: %w", seq, err)
			}
			rec, err := raw.record(seq)
			if err != nil {
				return false, fmt.Errorf("record %d: %w", seq, err)
			}
			if err = fn(rec); err != nil {
				return false, callbackError{err}
			}
		}
		return true, expect(dec, json.Delim(']'))
	})
}

// callbackError carries an error of Each's fn through walk unchanged.
type callbackError struct{ err error }

func (c callbackError) Error() string { return c.err.Error() }
func (c callbackError) Unwrap() error { return c.err }

// ErrOutside: a path of the export leads out of its folder.
var ErrOutside = errors.New("tgexport: the path leads out of the export folder")

// Path is the file rel of the export on disk; ErrOutside for a path that
// would leave the export's folder.
func (e *Export) Path(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", ErrOutside
	}
	return filepath.Join(e.dir, filepath.FromSlash(rel)), nil
}

// Stat returns the size of the file rel of the export; fs.ErrNotExist when
// the folder does not have it.
func (e *Export) Stat(rel string) (int64, error) {
	p, err := e.Path(rel)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	if !st.Mode().IsRegular() {
		return 0, fs.ErrNotExist
	}
	return st.Size(), nil
}

// OpenFile opens the file rel of the export.
func (e *Export) OpenFile(rel string) (*os.File, error) {
	p, err := e.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// rawMessage is an element of "messages" as SerializeMessage writes it.
type rawMessage struct {
	Id            int64   `json:"id"`
	Type          string  `json:"type"`
	Date          string  `json:"date"`
	DateUnix      string  `json:"date_unixtime"`
	EditedUnix    string  `json:"edited_unixtime"`
	From          *string `json:"from"`
	FromId        string  `json:"from_id"`
	Actor         *string `json:"actor"`
	ActorId       string  `json:"actor_id"`
	Action        string  `json:"action"`
	ForwardedFrom *string `json:"forwarded_from"`
	ForwardedId   string  `json:"forwarded_from_id"`
	ReplyTo       int64   `json:"reply_to_message_id"`
	ReplyToPeer   string  `json:"reply_to_peer_id"`
	PinnedId      int64   `json:"message_id"`
	ViaBot        string  `json:"via_bot"`

	Text         json.RawMessage `json:"text"`
	TextEntities json.RawMessage `json:"text_entities"`
	RichMessage  json.RawMessage `json:"rich_message"`

	Photo         string `json:"photo"`
	PhotoFileSize int64  `json:"photo_file_size"`
	File          string `json:"file"`
	FileName      string `json:"file_name"`
	FileSize      int64  `json:"file_size"`
	MediaType     string `json:"media_type"`
	StickerEmoji  string `json:"sticker_emoji"`
	Performer     string `json:"performer"`
	Title         string `json:"title"`
	MimeType      string `json:"mime_type"`
	Duration      int32  `json:"duration_seconds"`
	Width         int32  `json:"width"`
	Height        int32  `json:"height"`
	MediaSpoiler  bool   `json:"media_spoiler"`

	Contact *struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Phone     string `json:"phone_number"`
	} `json:"contact_information"`
	// Location is null for a point the export holds as not valid.
	Location *struct {
		Lat  float64 `json:"latitude"`
		Long float64 `json:"longitude"`
	} `json:"location_information"`
	hasLocation bool
	PlaceName   string `json:"place_name"`
	Address     string `json:"address"`

	Poll *struct {
		Question json.RawMessage `json:"question"`
		Closed   bool            `json:"closed"`
		Total    int             `json:"total_voters"`
		Answers  []struct {
			Text   json.RawMessage `json:"text"`
			Voters int             `json:"voters"`
			Chosen bool            `json:"chosen"`
		} `json:"answers"`
	} `json:"poll"`
	// The items of a checklist are under "answers"
	// (export_output_json.cpp:2261-2270).
	Todo *struct {
		Title             json.RawMessage `json:"title"`
		OthersCanAppend   bool            `json:"others_can_append"`
		OthersCanComplete bool            `json:"others_can_complete"`
		Items             []struct {
			Text json.RawMessage `json:"text"`
			Id   int32           `json:"id"`
		} `json:"answers"`
	} `json:"todo_list"`

	GameTitle       *string         `json:"game_title"`
	Invoice         json.RawMessage `json:"invoice_information"`
	Giveaway        json.RawMessage `json:"giveaway_information"`
	GiveawayResults json.RawMessage `json:"giveaway_results"`
	PaidStars       *int64          `json:"paid_stars_amount"`

	Members []*string       `json:"members"`
	Inviter *string         `json:"inviter"`
	Buttons json.RawMessage `json:"inline_bot_buttons"`

	Reactions []struct {
		Type   string `json:"type"`
		Count  int    `json:"count"`
		Emoji  string `json:"emoji"`
		Recent []struct {
			From   *string `json:"from"`
			FromId string  `json:"from_id"`
			Date   string  `json:"date"`
		} `json:"recent"`
	} `json:"reactions"`
}

// UnmarshalJSON notes whether "location_information" is there at all: null
// is a message with a point that is not valid, no key is no point.
func (r *rawMessage) UnmarshalJSON(b []byte) error {
	type plain rawMessage
	if err := json.Unmarshal(b, (*plain)(r)); err != nil {
		return err
	}
	var keys struct {
		Location json.RawMessage `json:"location_information"`
	}
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	r.hasLocation = keys.Location != nil
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// isoLayout is how the export writes a date for people: Qt::ISODate of the
// exporter's local time, without a zone (SerializeDate,
// export_output_json.cpp:81-84).
const isoLayout = "2006-01-02T15:04:05"

// localOffset is how far the exporter's clock was from UTC at a moment
// whose two forms the export gives: seconds to take from a local date.
func localOffset(local string, unix int64) (int64, bool) {
	t, err := time.Parse(isoLayout, local)
	if err != nil {
		return 0, false
	}
	return t.Unix() - unix, true
}

func (r *rawMessage) record(seq int64) (*Record, error) {
	rec := &Record{Seq: seq, Id: r.Id}
	if r.Id == 0 {
		return nil, errors.New("no id")
	}
	switch r.Type {
	case "unsupported":
		rec.Kind = KindUnsupported
		return rec, nil
	case "message":
		rec.Kind = KindMessage
	case "service":
		rec.Kind = KindService
	default:
		return nil, fmt.Errorf("message %d: unknown type %q", r.Id, r.Type)
	}
	var err error
	if r.DateUnix == "" {
		return nil, fmt.Errorf("message %d: no date_unixtime (the export of an old Telegram Desktop: export again with a current one)", r.Id)
	}
	if rec.Date, err = strconv.ParseInt(r.DateUnix, 10, 64); err != nil || rec.Date <= 0 {
		return nil, fmt.Errorf("message %d: date_unixtime %q", r.Id, r.DateUnix)
	}
	if r.EditedUnix != "" {
		if rec.Edited, err = strconv.ParseInt(r.EditedUnix, 10, 64); err != nil {
			return nil, fmt.Errorf("message %d: edited_unixtime %q", r.Id, r.EditedUnix)
		}
	}
	rec.ReplyTo, rec.ReplyToPeer, rec.ViaBot = r.ReplyTo, r.ReplyToPeer, r.ViaBot
	rec.Buttons = len(r.Buttons) > 0

	if rec.Kind == KindService {
		rec.From = Peer{Key: r.ActorId, Name: deref(r.Actor)}
		a := &Action{Name: r.Action, Title: r.Title, Inviter: deref(r.Inviter)}
		for _, m := range r.Members {
			a.Members = append(a.Members, deref(m))
		}
		if r.Action == "pin_message" {
			rec.ReplyTo = r.PinnedId
		}
		if r.Action == "edit_group_photo" {
			a.Photo = r.photo()
		}
		rec.Action = a
		return rec, nil
	}

	rec.From = Peer{Key: r.FromId, Name: deref(r.From)}
	if r.ForwardedId != "" || r.ForwardedFrom != nil {
		rec.Forward = &Peer{Key: r.ForwardedId, Name: deref(r.ForwardedFrom)}
	}
	// text_entities holds every part as an object; text is its short form
	// for people, a string when nothing is formatted.
	text := r.TextEntities
	if text == nil {
		text = r.Text
	}
	if rec.Text, err = parseText(text); err != nil {
		return nil, fmt.Errorf("message %d: text: %w", r.Id, err)
	}
	if rec.Media, err = r.media(); err != nil {
		return nil, fmt.Errorf("message %d: %w", r.Id, err)
	}
	offset, hasOffset := localOffset(r.Date, rec.Date)
	for _, x := range r.Reactions {
		reaction := Reaction{Type: x.Type, Count: x.Count, Emoji: x.Emoji}
		for _, n := range x.Recent {
			// A recent reaction has the local form of its date alone; the
			// zone is the one of the message's own date, so a change of
			// the clock between the two moves it by that change.
			date := rec.Date
			if t, err := time.Parse(isoLayout, n.Date); err == nil && hasOffset {
				date = max(t.Unix()-offset, rec.Date)
			}
			reaction.Recent = append(reaction.Recent, RecentReaction{From: Peer{Key: n.FromId, Name: deref(n.From)}, Date: date})
		}
		rec.Reactions = append(rec.Reactions, reaction)
	}
	return rec, nil
}

// missingMarks are the strings pushPath writes in place of a path
// (export_output_json.cpp:1656-1678), possibly after the file's name.
var missingMarks = []struct {
	mark   string
	reason Missing
}{
	{"(File unavailable, please try again later)", MissingUnavailable},
	{"(File exceeds maximum size. Change data exporting settings to download.)", MissingTooLarge},
	{"(File not included. Change data exporting settings to download.)", MissingNotIncluded},
}

// MissingPath tells why path, a file's path as the export writes it, is no
// path.
func MissingPath(path string) (Missing, bool) {
	for _, m := range missingMarks {
		if strings.HasSuffix(path, m.mark) {
			return m.reason, true
		}
	}
	return "", false
}

func filePath(m *Media, path string) {
	if reason, missing := MissingPath(path); missing {
		m.Missing = reason
		return
	}
	m.Path = path
}

func (r *rawMessage) photo() *Media {
	if r.Photo == "" {
		return nil
	}
	m := &Media{Type: MediaPhoto, Size: r.PhotoFileSize, Width: r.Width, Height: r.Height, Spoiler: r.MediaSpoiler}
	filePath(m, r.Photo)
	return m
}

// media is what the message carries, nil for a text
// (export_output_json.cpp:2125-2328).
func (r *rawMessage) media() (*Media, error) {
	switch {
	case r.RichMessage != nil:
		return &Media{Type: MediaOther, Other: "rich_message"}, nil
	case r.Photo != "":
		return r.photo(), nil
	case r.File != "":
		m := &Media{
			Type: MediaFile, Size: r.FileSize, FileName: r.FileName, MimeType: r.MimeType, Duration: r.Duration,
			Width: r.Width, Height: r.Height, Spoiler: r.MediaSpoiler,
		}
		switch r.MediaType {
		case "sticker":
			m.Type, m.StickerEmoji = MediaSticker, r.StickerEmoji
		case "video_message":
			m.Type = MediaRound
		case "voice_message":
			m.Type = MediaVoice
		case "animation":
			m.Type = MediaAnimation
		case "video_file":
			m.Type = MediaVideo
		case "audio_file":
			m.Type, m.Performer, m.Title = MediaAudio, r.Performer, r.Title
		case "":
		default:
			return nil, fmt.Errorf("unknown media_type %q", r.MediaType)
		}
		filePath(m, r.File)
		return m, nil
	case r.Contact != nil:
		return &Media{Type: MediaContact, Contact: &Contact{FirstName: r.Contact.FirstName, LastName: r.Contact.LastName, Phone: r.Contact.Phone}}, nil
	case r.PlaceName != "" || r.Address != "":
		v := &Venue{Title: r.PlaceName, Address: r.Address}
		if r.Location != nil {
			v.Geo = Geo{Lat: r.Location.Lat, Long: r.Location.Long}
		}
		return &Media{Type: MediaVenue, Venue: v}, nil
	case r.hasLocation:
		if r.Location == nil {
			return &Media{Type: MediaOther, Other: "location_invalid"}, nil
		}
		return &Media{Type: MediaGeo, Geo: &Geo{Lat: r.Location.Lat, Long: r.Location.Long}}, nil
	case r.Poll != nil:
		p := &Poll{Closed: r.Poll.Closed, Total: r.Poll.Total}
		var err error
		if p.Question, err = parseText(r.Poll.Question); err != nil {
			return nil, fmt.Errorf("poll: %w", err)
		}
		for _, a := range r.Poll.Answers {
			text, err := parseText(a.Text)
			if err != nil {
				return nil, fmt.Errorf("poll: %w", err)
			}
			p.Answers = append(p.Answers, PollAnswer{Text: text, Voters: a.Voters, Chosen: a.Chosen})
		}
		return &Media{Type: MediaPoll, Poll: p}, nil
	case r.Todo != nil:
		t := &Todo{OthersCanAppend: r.Todo.OthersCanAppend, OthersCanComplete: r.Todo.OthersCanComplete}
		var err error
		if t.Title, err = parseText(r.Todo.Title); err != nil {
			return nil, fmt.Errorf("todo_list: %w", err)
		}
		for _, i := range r.Todo.Items {
			text, err := parseText(i.Text)
			if err != nil {
				return nil, fmt.Errorf("todo_list: %w", err)
			}
			t.Items = append(t.Items, TodoItem{Id: i.Id, Text: text})
		}
		return &Media{Type: MediaTodo, Todo: t}, nil
	case r.GameTitle != nil:
		return &Media{Type: MediaOther, Other: "game"}, nil
	case r.Invoice != nil:
		return &Media{Type: MediaOther, Other: "invoice"}, nil
	case r.Giveaway != nil:
		return &Media{Type: MediaOther, Other: "giveaway"}, nil
	case r.GiveawayResults != nil:
		return &Media{Type: MediaOther, Other: "giveaway_results"}, nil
	case r.PaidStars != nil:
		return &Media{Type: MediaOther, Other: "paid_media"}, nil
	}
	return nil, nil
}

// rawPart is an element of a text: a string, or an object with its type.
type rawPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Href      string          `json:"href"`
	Language  string          `json:"language"`
	UserId    json.RawMessage `json:"user_id"`
	Collapsed bool            `json:"collapsed"`
}

// parseText reads a text as SerializeText writes it: a string, or an array
// of strings and objects.
func parseText(raw json.RawMessage) ([]TextPart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		if s == "" {
			return nil, nil
		}
		return []TextPart{{Type: "plain", Text: s}}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]TextPart, 0, len(items))
	for _, item := range items {
		if len(item) > 0 && item[0] == '"' {
			if err := json.Unmarshal(item, &s); err != nil {
				return nil, err
			}
			out = append(out, TextPart{Type: "plain", Text: s})
			continue
		}
		var p rawPart
		if err := json.Unmarshal(item, &p); err != nil {
			return nil, err
		}
		part := TextPart{Type: p.Type, Text: p.Text, Href: p.Href, Language: p.Language, Collapsed: p.Collapsed}
		if p.Type == "mention_name" && len(p.UserId) > 0 {
			// A number; an old export may have written it as a string.
			id, err := strconv.ParseInt(strings.Trim(string(p.UserId), `"`), 10, 64)
			if err == nil {
				part.UserId = id
			}
		}
		out = append(out, part)
	}
	return out, nil
}

// Plain is the text of parts without its formatting.
func Plain(parts []TextPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}
