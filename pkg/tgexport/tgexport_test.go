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
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// exportFiles are the files of testdata/everything.json that are in its
// folder, with their sizes.
var exportFiles = map[string]int{
	"photos/photo_1.jpg": 11, "video_files/v.mp4": 12, "animations/g.mp4": 13, "files/doc.pdf": 14, "files/song.mp3": 15,
	"voice_messages/a.ogg": 16, "round_video_messages/r.mp4": 17, "stickers/s.webp": 18, "photos/chat.jpg": 19,
}

// writeExport makes an export folder of result, the content of result.json,
// with files of the given sizes.
func writeExport(t *testing.T, result string, files map[string]int) *Export {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ResultFile), []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, size := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ex, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func everything(t *testing.T) *Export {
	t.Helper()
	b, err := os.ReadFile("testdata/everything.json")
	if err != nil {
		t.Fatal(err)
	}
	return writeExport(t, string(b), exportFiles)
}

func records(t *testing.T, ex *Export) map[int64]*Record {
	t.Helper()
	out := map[int64]*Record{}
	seq := int64(0)
	if err := ex.Each(func(r *Record) error {
		seq++
		if r.Seq != seq {
			t.Errorf("record %d has seq %d", seq, r.Seq)
		}
		out[r.Id] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

const t0 = 1600000000

// TestReadEverything: every kind of record of an export is read as
// SerializeMessage writes it.
func TestReadEverything(t *testing.T) {
	ex := everything(t)
	if h := ex.Header(); h != (Header{Name: "Friends", Type: "private_supergroup", Id: 1234567890}) || !h.Group() {
		t.Fatalf("header %+v", h)
	}
	recs := records(t, ex)
	if len(recs) != 42 {
		t.Fatalf("%d records", len(recs))
	}

	// A service message: its actor, action, title and the names of members.
	create := recs[-999999990]
	if create.Kind != KindService || create.From != (Peer{Key: "user111", Name: "Ivan Petrov"}) || create.Date != t0 ||
		create.Action.Name != "create_group" || create.Action.Title != "Friends" || !reflect.DeepEqual(create.Action.Members, []string{"Ivan Petrov", "Masha"}) {
		t.Errorf("create_group: %+v %+v", create, create.Action)
	}
	// A text as one string, and the reply of the migrated history, whose
	// ids are shifted together.
	if r := recs[-999999989]; r.Kind != KindMessage || Plain(r.Text) != "hello" || r.Media != nil {
		t.Errorf("text: %+v", r)
	}
	formatted := recs[-999999988]
	if formatted.ReplyTo != -999999989 || len(formatted.Text) != 23 {
		t.Errorf("formatted: reply %d, %d parts", formatted.ReplyTo, len(formatted.Text))
	}
	if p := formatted.Text[7]; p.Type != "pre" || p.Language != "go" {
		t.Errorf("pre: %+v", p)
	}
	if p := formatted.Text[8]; p.Type != "blockquote" || !p.Collapsed {
		t.Errorf("blockquote: %+v", p)
	}
	if p := formatted.Text[9]; p.Type != "text_link" || p.Href != "https://example.org/" {
		t.Errorf("text_link: %+v", p)
	}
	if p := formatted.Text[19]; p.Type != "mention_name" || p.UserId != 111 {
		t.Errorf("mention_name: %+v", p)
	}

	media := map[int64]Media{
		2:  {Type: MediaPhoto, Path: "photos/photo_1.jpg", Size: 11, Width: 90, Height: 60, Spoiler: true},
		3:  {Type: MediaVideo, Path: "video_files/v.mp4", Size: 12, FileName: "v.mp4", MimeType: "video/mp4", Duration: 5, Width: 640, Height: 360},
		4:  {Type: MediaAnimation, Path: "animations/g.mp4", Size: 13, FileName: "g.mp4", MimeType: "video/mp4", Duration: 2, Width: 320, Height: 240},
		5:  {Type: MediaFile, Path: "files/doc.pdf", Size: 14, FileName: "Report.pdf", MimeType: "application/pdf"},
		6:  {Type: MediaAudio, Path: "files/song.mp3", Size: 15, FileName: "song.mp3", MimeType: "audio/mpeg", Duration: 180, Performer: "Band", Title: "Song"},
		7:  {Type: MediaVoice, Path: "voice_messages/a.ogg", Size: 16, MimeType: "audio/ogg", Duration: 3},
		8:  {Type: MediaRound, Path: "round_video_messages/r.mp4", Size: 17, MimeType: "video/mp4", Duration: 4},
		9:  {Type: MediaSticker, Path: "stickers/s.webp", Size: 18, MimeType: "image/webp", StickerEmoji: "\U0001F600", Width: 512, Height: 512},
		29: {Type: MediaPhoto, Missing: MissingNotIncluded},
		30: {Type: MediaFile, Missing: MissingTooLarge, FileName: "big.zip", MimeType: "application/zip"},
		31: {Type: MediaVoice, Missing: MissingUnavailable, MimeType: "audio/ogg"},
		35: {Type: MediaOther, Other: "game"},
		36: {Type: MediaOther, Other: "invoice"},
	}
	for id, want := range media {
		if got := recs[id].Media; got == nil || !reflect.DeepEqual(*got, want) {
			t.Errorf("media of %d: %+v, want %+v", id, got, want)
		}
	}
	if recs[2].From.Key != "user222" || Plain(recs[2].Text) != "look" || recs[7].ReplyTo != 2 {
		t.Errorf("photo with a caption, voice reply: %+v %+v", recs[2], recs[7])
	}
	if c := recs[10].Media; c.Type != MediaContact || *c.Contact != (Contact{FirstName: "Oleg", LastName: "Sidorov", Phone: "+7 999 000 00 00"}) {
		t.Errorf("contact: %+v", c)
	}
	if g := recs[11].Media; g.Type != MediaGeo || *g.Geo != (Geo{Lat: 55.75, Long: 37.61}) {
		t.Errorf("location: %+v", g)
	}
	if v := recs[12].Media; v.Type != MediaVenue || *v.Venue != (Venue{Geo: Geo{Lat: 55.7, Long: 37.6}, Title: "Cafe", Address: "Main st. 1"}) {
		t.Errorf("venue: %+v", v)
	}
	poll := recs[13].Media.Poll
	if poll == nil || Plain(poll.Question) != "Where?" || poll.Closed || poll.Total != 2 || len(poll.Answers) != 2 ||
		Plain(poll.Answers[0].Text) != "Here" || !poll.Answers[0].Chosen || Plain(poll.Answers[1].Text) != "There" || poll.Answers[1].Chosen {
		t.Errorf("poll: %+v", poll)
	}
	todo := recs[14].Media.Todo
	if todo == nil || Plain(todo.Title) != "Trip" || !todo.OthersCanAppend || todo.OthersCanComplete || len(todo.Items) != 2 || todo.Items[1].Id != 2 || Plain(todo.Items[1].Text) != "Hotel" {
		t.Errorf("todo list: %+v", todo)
	}
	if r := recs[15]; r.ReplyTo != 5 || r.ReplyToPeer != "channel999" {
		t.Errorf("a reply to another chat: %+v", r)
	}
	if r := recs[16]; r.Forward == nil || *r.Forward != (Peer{Key: "user222", Name: "Masha"}) {
		t.Errorf("a forward with an id: %+v", r.Forward)
	}
	if r := recs[17]; r.Forward == nil || *r.Forward != (Peer{Name: "Some Channel"}) {
		t.Errorf("a forward with a name alone: %+v", r.Forward)
	}
	if r := recs[39]; r.Forward == nil || *r.Forward != (Peer{Key: "user555"}) {
		t.Errorf("a forward from a deleted account: %+v", r.Forward)
	}
	if r := recs[18]; r.Edited != t0+22*60 || r.Date != t0+21*60 {
		t.Errorf("edited: %+v", r)
	}
	if a := recs[20].Action; a.Name != "edit_group_photo" || a.Photo == nil || a.Photo.Path != "photos/chat.jpg" || a.Photo.Size != 19 {
		t.Errorf("edit_group_photo: %+v", a)
	}
	if a := recs[22].Action; !reflect.DeepEqual(a.Members, []string{"Masha", "Oleg Sidorov"}) {
		t.Errorf("invite_members: %+v", a)
	}
	if a := recs[24].Action; a.Name != "join_group_by_link" || a.Inviter != "Ivan Petrov" {
		t.Errorf("join_group_by_link: %+v", a)
	}
	if r := recs[26]; r.Action.Name != "pin_message" || r.ReplyTo != 2 {
		t.Errorf("pin_message: %+v", r)
	}
	if r := recs[28]; r.Kind != KindUnsupported {
		t.Errorf("unsupported: %+v", r)
	}
	// Reactions: the counters, and those named with the date of the
	// exporter's clock turned into a unix time by the message's own date.
	reactions := recs[32].Reactions
	if len(reactions) != 4 || reactions[0].Type != "emoji" || reactions[0].Count != 3 || reactions[0].Emoji != "\U0001F44D" || len(reactions[0].Recent) != 2 {
		t.Fatalf("reactions: %+v", reactions)
	}
	if n := reactions[0].Recent[0]; n.From != (Peer{Key: "user111", Name: "Ivan Petrov"}) || n.Date != t0+3200 {
		t.Errorf("a named reaction: %+v", n)
	}
	if reactions[1].Count != 2 || len(reactions[1].Recent) != 0 || reactions[2].Type != "custom_emoji" || reactions[2].Emoji != "" || reactions[3].Type != "paid" {
		t.Errorf("reactions: %+v", reactions)
	}
	if r := recs[34]; r.From != (Peer{Key: "user444"}) {
		t.Errorf("a deleted account: %+v", r.From)
	}
	if r := recs[37]; !r.Buttons || r.ViaBot != "@gif" {
		t.Errorf("a bot's message: %+v", r)
	}
}

func TestMissingPath(t *testing.T) {
	for path, want := range map[string]Missing{
		"(File not included. Change data exporting settings to download.)":         MissingNotIncluded,
		"(File exceeds maximum size. Change data exporting settings to download.)": MissingTooLarge,
		"a.ogg (File unavailable, please try again later)":                         MissingUnavailable,
	} {
		if got, ok := MissingPath(path); !ok || got != want {
			t.Errorf("%q: %q %v", path, got, ok)
		}
	}
	if _, ok := MissingPath("photos/photo_1@01-01-2024_10-00-00.jpg"); ok {
		t.Error("a path is missing")
	}
}

// TestEntities: each part of a text becomes its entity, with offsets in
// UTF-16 code units; a mention by name is of the Pluma account the mapping
// gives, plain text when it gives none, and so is a custom emoji.
func TestEntities(t *testing.T) {
	recs := records(t, everything(t))
	user := func(id int64) (int64, bool) { return 1000 + id, id == 111 }
	text, entities, mentioned := Entities(recs[-999999988].Text, user)
	if want := "a biusxcpqthttps://example.coma@example.org+10000000000@masha#tag$USD/start4242424242424242 \U0001F600 IvanNobody\U0001F525?"; text != want {
		t.Fatalf("text %q", text)
	}
	type e struct {
		predicate      string
		offset, length int32
	}
	var got []e
	for _, x := range entities {
		got = append(got, e{x.GetPredicateName(), x.GetOffset(), x.GetLength()})
	}
	want := []e{
		{mtproto.Predicate_messageEntityBold, 2, 1}, {mtproto.Predicate_messageEntityItalic, 3, 1},
		{mtproto.Predicate_messageEntityUnderline, 4, 1}, {mtproto.Predicate_messageEntityStrike, 5, 1},
		{mtproto.Predicate_messageEntitySpoiler, 6, 1}, {mtproto.Predicate_messageEntityCode, 7, 1},
		{mtproto.Predicate_messageEntityPre, 8, 1}, {mtproto.Predicate_messageEntityBlockquote, 9, 1},
		{mtproto.Predicate_messageEntityTextUrl, 10, 1}, {mtproto.Predicate_messageEntityUrl, 11, 19},
		{mtproto.Predicate_messageEntityEmail, 30, 13}, {mtproto.Predicate_messageEntityPhone, 43, 12},
		{mtproto.Predicate_messageEntityMention, 55, 6}, {mtproto.Predicate_messageEntityHashtag, 61, 4},
		{mtproto.Predicate_messageEntityCashtag, 65, 4}, {mtproto.Predicate_messageEntityBotCommand, 69, 6},
		{mtproto.Predicate_messageEntityBankCard, 75, 16},
		// " 😀 " is four code units: the emoji is a surrogate pair.
		{mtproto.Predicate_messageEntityMentionName, 95, 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entities\n got %v\nwant %v", got, want)
	}
	if entities[6].GetLanguage() != "go" || !entities[7].GetCollapsed() || entities[8].GetUrl() != "https://example.org/" || entities[17].GetUserId_INT64() != 1111 {
		t.Errorf("what the entities carry: %v", entities)
	}
	if !reflect.DeepEqual(mentioned, []int64{1111}) {
		t.Errorf("mentioned %v", mentioned)
	}
	if text, entities, _ = Entities(nil, nil); text != "" || entities != nil {
		t.Errorf("no text: %q %v", text, entities)
	}
}

func id(v int64) *int64 { return &v }

// fullMapping maps Ivan and Masha, and names Oleg for the service messages.
func fullMapping() *Mapping {
	return &Mapping{
		ExportChatId: 1234567890, Creator: 501, Exporter: 501,
		Authors: map[string]*Author{
			"user111": {Pluma: id(501), Confirmed: true},
			"user222": {Pluma: id(502), Confirmed: true},
			"user444": {Skip: true},
		},
		ServiceNames: map[string]*int64{"Oleg Sidorov": id(503)},
		ExtraMembers: []int64{504},
	}
}

func items(t *testing.T, ex *Export, m *Mapping, opts Options) (map[int64]*Item, *Plan) {
	t.Helper()
	census, err := Scan(ex)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ex, census, m, opts)
	out := map[int64]*Item{}
	if err = ex.Each(func(rec *Record) error {
		it, err := r.Next(rec)
		if err != nil {
			return err
		}
		out[rec.Id] = it
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out, r.Plan()
}

// TestPlanEverything: what becomes of each record with a complete mapping,
// when the operator goes on without the missing files.
func TestPlanEverything(t *testing.T) {
	ex := everything(t)
	m := fullMapping()
	its, _ := items(t, ex, m, Options{SkipMissingMedia: true})

	skips := map[int64]string{
		-999999987: SkipAction, 1: SkipAction, 27: SkipAction, 38: SkipAction,
		28: SkipUnsupported, 30: SkipMissing, 31: SkipMissing, 34: SkipAuthor, 35: SkipMedia,
	}
	for rid, it := range its {
		if it.Skip != skips[rid] {
			t.Errorf("record %d: skip %q, want %q", rid, it.Skip, skips[rid])
		}
	}
	if it := its[-999999990]; !it.Create || it.Sender != 501 {
		t.Errorf("create_group: %+v", it)
	}
	if it := its[-999999988]; it.Sender != 502 || it.ReplyTo != -999999989 {
		t.Errorf("a reply of the migrated history: %+v", it)
	}
	if it := its[7]; it.ReplyTo != 2 || it.File == nil || it.File.Size != 16 {
		t.Errorf("a voice reply: %+v", it)
	}
	if it := its[15]; it.ReplyTo != 0 {
		t.Errorf("a reply to another chat is kept: %+v", it)
	}
	if it := its[16]; it.ForwardUser != 502 || it.ForwardName != "" {
		t.Errorf("a forward from a mapped user: %+v", it)
	}
	if it := its[17]; it.ForwardUser != 0 || it.ForwardName != "Some Channel" {
		t.Errorf("a forward by name: %+v", it)
	}
	if it := its[39]; it.ForwardUser != 0 || it.ForwardName != "" || it.Skip != "" {
		t.Errorf("a forward from nobody: %+v", it)
	}
	if it := its[20]; it.File == nil || it.File.Path != "photos/chat.jpg" {
		t.Errorf("edit_group_photo: %+v", it)
	}
	// Masha is the one user of that name in the export; Oleg never wrote,
	// and the owner named him.
	if it := its[22]; !reflect.DeepEqual(it.ActionUsers, []int64{502, 503}) {
		t.Errorf("invite_members: %+v", it)
	}
	if it := its[23]; !reflect.DeepEqual(it.ActionUsers, []int64{503}) {
		t.Errorf("remove_members: %+v", it)
	}
	if it := its[24]; it.Sender != 502 || !reflect.DeepEqual(it.ActionUsers, []int64{501}) {
		t.Errorf("join_group_by_link: %+v", it)
	}
	if it := its[26]; it.ReplyTo != 2 {
		t.Errorf("pin_message: %+v", it)
	}
	if it := its[29]; it.File != nil || it.Skip != "" {
		t.Errorf("a photo that is not there, with a caption: %+v", it)
	}
	// Of the reactions only the one the export names a mapped author of.
	if it := its[32]; !reflect.DeepEqual(it.Reactions, []ItemReaction{{User: 501, Emoji: "\U0001F44D", Date: t0 + 3200}}) {
		t.Errorf("reactions: %+v", it.Reactions)
	}

	p, _, err := Build(ex, m, Options{SkipMissingMedia: true}, func(int64) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if faults := p.Faults(); len(faults) != 0 {
		t.Errorf("faults of a complete plan: %v", faults)
	}
	if p.Records != 42 || p.Import != 33 {
		t.Errorf("%d records, %d to import", p.Records, p.Import)
	}
	wantKinds := map[string]int64{
		"service": 9, "text": 11, "photo": 1, "video": 1, "animation": 1, "file": 1, "audio": 1, "voice": 1, "round_video": 1, "sticker": 1,
		"contact": 1, "location": 1, "venue": 1, "poll": 1, "todo_list": 1,
	}
	if !reflect.DeepEqual(p.ByKind, wantKinds) {
		t.Errorf("by kind %v", p.ByKind)
	}
	wantSkipped := map[string]int64{SkipAction: 4, SkipUnsupported: 1, SkipMissing: 2, SkipAuthor: 1, SkipMedia: 1}
	if !reflect.DeepEqual(p.Skipped, wantSkipped) {
		t.Errorf("skipped %v", p.Skipped)
	}
	if !reflect.DeepEqual(p.Actions, map[string]int64{"migrate_to_supergroup": 1, "migrate_from_group": 1, "group_call": 1, "topic_created": 1}) || p.ForumTopics != 1 {
		t.Errorf("actions %v, topics %d", p.Actions, p.ForumTopics)
	}
	if p.Files != 9 || p.Bytes != 11+12+13+14+15+16+17+18+19 {
		t.Errorf("%d files, %d bytes", p.Files, p.Bytes)
	}
	if !reflect.DeepEqual(p.Missing, map[Missing]int64{MissingNotIncluded: 1, MissingTooLarge: 1, MissingUnavailable: 1}) || !reflect.DeepEqual(p.MissingIds, []int64{29, 30, 31}) {
		t.Errorf("missing %v %v", p.Missing, p.MissingIds)
	}
	if p.Reactions != (ReactionCounts{Counted: 7, Named: 2, Imported: 1, Unmapped: 1, Custom: 1, Paid: 1}) {
		t.Errorf("reactions %+v", p.Reactions)
	}
	if p.RepliesElsewhere != 1 || p.RepliesLost != 0 || p.ForwardsDropped != 1 || p.ContentDropped != 1 || p.ButtonsDropped != 1 {
		t.Errorf("replies elsewhere %d, lost %d, forwards dropped %d, content dropped %d, buttons %d",
			p.RepliesElsewhere, p.RepliesLost, p.ForwardsDropped, p.ContentDropped, p.ButtonsDropped)
	}
	if !reflect.DeepEqual(p.Members, []int64{502, 503, 504}) || p.FirstDate != t0 || p.LastDate != t0+42*60 {
		t.Errorf("members %v, %d..%d", p.Members, p.FirstDate, p.LastDate)
	}
	// Whose messages become whose, peer by peer, and how many of them
	// keep an edit date.
	authors := []PlanAuthor{
		{Key: "user111", Names: []string{"Ivan Petrov"}, Messages: 20, Service: 11, Reactions: 1, Pluma: 501},
		{Key: "user222", Names: []string{"Masha"}, Messages: 7, Service: 2, Pluma: 502},
		{Key: "user333", Names: []string{"Stranger"}, Reactions: 1},
		{Key: "user444", Messages: 1, Skip: true},
	}
	if !reflect.DeepEqual(p.Authors, authors) {
		t.Errorf("authors\n got %+v\nwant %+v", p.Authors, authors)
	}
	if p.Edited != 1 {
		t.Errorf("%d messages with an edit date", p.Edited)
	}
}

// TestMembersExporter: the exporter, whose poll votes are imported, joins
// the group though it wrote nothing in the export.
func TestMembersExporter(t *testing.T) {
	m := fullMapping()
	m.Exporter = 505
	if got := m.Members(); !reflect.DeepEqual(got, []int64{502, 503, 504, 505}) {
		t.Errorf("members %v", got)
	}
}

// TestPlanFaults: an author without a confirmed account stops the import,
// and so do files that are not in the export unless the operator chose to
// go on; a file the export names and the folder lacks is one of them.
func TestPlanFaults(t *testing.T) {
	ex := everything(t)
	if err := os.Remove(filepath.Join(ex.Dir(), "files", "doc.pdf")); err != nil {
		t.Fatal(err)
	}
	m := fullMapping()
	m.Authors["user222"].Confirmed = false
	delete(m.Authors, "user444")
	m.Creator = 0
	p, _, err := Build(ex, m, Options{}, func(id int64) bool { return id != 503 })
	if err != nil {
		t.Fatal(err)
	}
	want := []Unmatched{
		{Key: "user222", Names: []string{"Masha"}, Messages: 7, Why: "unconfirmed"},
		{Key: "user444", Names: nil, Messages: 1, Why: "missing"},
	}
	if !reflect.DeepEqual(p.Unmatched, want) {
		t.Errorf("unmatched %+v", p.Unmatched)
	}
	faults := strings.Join(p.Faults(), "\n")
	for _, part := range []string{`"creator" is not set`, "author user222 (7 messages)", "author user444 (1 messages)", "no Pluma account 503", "not_on_disk: 1", "-missing-media=skip"} {
		if !strings.Contains(faults, part) {
			t.Errorf("faults lack %q:\n%s", part, faults)
		}
	}
	if p.Missing[MissingOnDisk] != 1 || !p.MissingFault {
		t.Errorf("missing %v", p.Missing)
	}
	// A reaction of one who is not mapped is not an error, and the
	// author of a service message alone is not one either.
	if strings.Contains(faults, "user333") {
		t.Errorf("a reactor is a fault:\n%s", faults)
	}

	// A wrong chat, an account and a skip together.
	m = fullMapping()
	m.ExportChatId = 5
	m.Authors["user444"].Pluma = id(9)
	_, more := m.Validate(&Census{Header: ex.Header(), Peers: map[string]*Seen{"user444": {Messages: 1}}}, nil)
	if len(more) != 2 || !strings.Contains(more[0], "chat 5") || !strings.Contains(more[1], "skip") {
		t.Errorf("faults %v", more)
	}
}

const head = `{"name":"G","type":"private_group","id":7,"messages":[`

func text(id, date int64) string {
	return `{"id":` + itoa(id) + `,"type":"message","date":"2020-01-01T00:00:00","date_unixtime":"` + itoa(date) + `","from":"A","from_id":"user1","text":"t","text_entities":[{"type":"plain","text":"t"}]}`
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestOrder: the ids of an export go up, or nothing can be decided; a date
// that goes back is a fault of the plan.
func TestOrder(t *testing.T) {
	m := &Mapping{ExportChatId: 7, Creator: 1, Authors: map[string]*Author{"user1": {Pluma: id(1), Confirmed: true}}}
	ex := writeExport(t, head+text(5, 100)+","+text(4, 200)+"]}", nil)
	if _, _, err := Build(ex, m, Options{}, nil); err == nil || !strings.Contains(err.Error(), "does not follow") {
		t.Errorf("ids going down: %v", err)
	}
	ex = writeExport(t, head+text(1, 300)+","+text(2, 200)+","+text(3, 400)+"]}", nil)
	p, _, err := Build(ex, m, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Disorder, []int64{2}) || len(p.Faults()) != 1 || !strings.Contains(p.Faults()[0], "dated before") {
		t.Errorf("a date going back: %v %v", p.Disorder, p.Faults())
	}
	// A reply to a message that is not in the export is lost, not a fault.
	reply := strings.Replace(text(9, 500), `"from":`, `"reply_to_message_id":8,"from":`, 1)
	ex = writeExport(t, head+text(1, 300)+","+reply+"]}", nil)
	if p, _, err = Build(ex, m, Options{}, nil); err != nil || p.RepliesLost != 1 || len(p.Faults()) != 0 {
		t.Errorf("a reply to nothing: %+v, %v", p, err)
	}
}

func TestOpenRefuses(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, ResultFile), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The export of a whole account has no chat at its root.
	write(`{"about":"x","chats":{"list":[]}}`)
	if _, err := Open(dir); !errors.Is(err, ErrNotChatExport) {
		t.Errorf("an account's export: %v", err)
	}
	write(`[1]`)
	if _, err := Open(dir); err == nil {
		t.Error("an array was opened")
	}
	// An export from before date_unixtime is refused when read.
	write(head + `{"id":1,"type":"message","date":"2020-01-01T00:00:00","from":"A","from_id":"user1","text":"t"}]}`)
	ex, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = ex.Each(func(*Record) error { return nil }); err == nil || !strings.Contains(err.Error(), "date_unixtime") {
		t.Errorf("no date_unixtime: %v", err)
	}
	// An error of the callback comes back as it is.
	stop := errors.New("stop")
	ex = writeExport(t, head+text(1, 1)+"]}", nil)
	if err = ex.Each(func(*Record) error { return stop }); err != stop {
		t.Errorf("the callback's error: %v", err)
	}
	// A path out of the folder is refused.
	for _, p := range []string{"../x", "/etc/passwd", "a/../../x", ""} {
		if _, err = ex.Stat(p); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: %v", p, err)
		}
	}
}

// TestSuggest: a mapping names the Pluma account of a peer when exactly one
// has its name, keeps what the owner filled in before, and lists the names
// of service messages nobody of the export has alone.
func TestSuggest(t *testing.T) {
	ex := everything(t)
	census, err := Scan(ex)
	if err != nil {
		t.Fatal(err)
	}
	accounts := []Account{{Id: 501, Name: "Ivan Petrov"}, {Id: 502, Name: "Masha"}, {Id: 602, Name: "Masha"}, {Id: 700, Name: "Stranger"}}
	m := Suggest(census, accounts, nil)
	ivan := m.Authors["user111"]
	if ivan == nil || ivan.Pluma == nil || *ivan.Pluma != 501 || !ivan.Suggested || ivan.Confirmed || ivan.PlumaName != "Ivan Petrov" ||
		ivan.Messages != 20 || ivan.Service != 11 || ivan.Reactions != 1 || !reflect.DeepEqual(ivan.Names, []string{"Ivan Petrov"}) {
		t.Errorf("Ivan: %+v", ivan)
	}
	// Two accounts are called Masha: none is suggested.
	if masha := m.Authors["user222"]; masha == nil || masha.Pluma != nil || masha.Messages != 7 {
		t.Errorf("Masha: %+v", masha)
	}
	if s := m.Authors["user333"]; s == nil || s.Pluma == nil || *s.Pluma != 700 || s.Messages != 0 || s.Reactions != 1 {
		t.Errorf("the reactor: %+v", s)
	}
	if len(m.Authors) != 4 || m.ExportChatId != 1234567890 {
		t.Errorf("authors %v", m.Authors)
	}
	if v, ok := m.ServiceNames["Oleg Sidorov"]; !ok || v != nil || len(m.ServiceNames) != 1 {
		t.Errorf("service names %v", m.ServiceNames)
	}

	// The owner fills the file; a second map keeps it.
	m.Creator = 501
	m.Authors["user222"].Pluma, m.Authors["user222"].Confirmed = id(602), true
	m.Authors["user444"].Skip = true
	m.ServiceNames["Oleg Sidorov"] = id(503)
	path := filepath.Join(t.TempDir(), "mapping.json")
	if err = m.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	again := Suggest(census, accounts, loaded)
	if again.Creator != 501 || *again.Authors["user222"].Pluma != 602 || !again.Authors["user222"].Confirmed || !again.Authors["user444"].Skip ||
		*again.ServiceNames["Oleg Sidorov"] != 503 || !again.Authors["user111"].Suggested {
		t.Errorf("the second mapping lost what was filled in: %+v", again)
	}
	// The name beside an id is the account's own: it follows an id the
	// owner changed by hand, and is empty for an id that is nobody's.
	if again.Authors["user222"].PlumaName != "Masha" {
		t.Errorf("the name of the account the owner gave: %+v", again.Authors["user222"])
	}
	again.Authors["user111"].Pluma = id(700)
	again.Authors["user333"].Pluma = id(999)
	third := Suggest(census, accounts, again)
	if a := third.Authors["user111"]; *a.Pluma != 700 || a.PlumaName != "Stranger" {
		t.Errorf("the name after the owner changed the id: %+v", a)
	}
	if a := third.Authors["user333"]; *a.Pluma != 999 || a.PlumaName != "" {
		t.Errorf("the name of an id that is nobody's: %+v", a)
	}
	if _, err = LoadMapping(filepath.Join(t.TempDir(), "none.json")); !IsNotExist(err) {
		t.Errorf("no file: %v", err)
	}
	if err = os.WriteFile(path, []byte(`{"export_chat_id":1,"authorz":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadMapping(path); err == nil {
		t.Error("a mapping with an unknown key was read")
	}
}
