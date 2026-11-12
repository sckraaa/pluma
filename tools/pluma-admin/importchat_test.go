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

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/tgexport"

	"google.golang.org/protobuf/proto"
)

// The export of the tests is the one of pkg/tgexport, with every kind of
// record; importFiles are its files and their sizes.
var importTestFiles = map[string]int{
	"photos/photo_1.jpg": 11, "video_files/v.mp4": 12, "animations/g.mp4": 13, "files/doc.pdf": 14, "files/song.mp3": 15,
	"voice_messages/a.ogg": 16, "round_video_messages/r.mp4": 17, "stickers/s.webp": 18, "photos/chat.jpg": 19,
}

// importTestMapping maps the two authors of the export, skips the deleted
// account and names the member who never wrote.
const importTestMapping = `{
  "export_chat_id": 1234567890, "creator": 501, "exporter": 501,
  "authors": {
    "user111": {"pluma": 501, "confirmed": true},
    "user222": {"pluma": 502, "confirmed": true},
    "user444": {"skip": true}
  },
  "service_names": {"Oleg Sidorov": 503},
  "extra_members": [504]
}`

func importExport(t *testing.T, mapping string) string {
	t.Helper()
	result, err := os.ReadFile("../../pkg/tgexport/testdata/everything.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, b []byte) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(tgexport.ResultFile, result)
	for name, size := range importTestFiles {
		write(name, bytes.Repeat([]byte{'x'}, size))
	}
	if mapping != "" {
		write("mapping.json", []byte(mapping))
	}
	return dir
}

// fakeImport is the server of an import: the channel service with its
// journal, the uploads and the files remembered.
type fakeImport struct {
	state   *channel.ChannelImport
	journal []*channel.ImportedMessage
	creates int
	invited []int64
	invites int
	parts   map[int64]int
	bytes   map[int64]int64 // of the parts of a file
	// stored are the sizes of the documents media recorded, by their id,
	// and 0 for a photo; lost makes the n-th upload be answered and not
	// recorded (from 1).
	stored   map[int64]int64
	lost     int
	uploads  map[string]int // by the file's name
	media    map[string]*mtproto.MessageMedia
	nextId   int64
	accounts []tgexport.Account
	free     int64
	// fail makes the n-th call of a step fail (from 1); lose makes the
	// n-th batch be written and its answer lost.
	fail map[string]int
	lose int
	seen map[string]int
}

func newFakeImport() *fakeImport {
	return &fakeImport{
		parts: map[int64]int{}, bytes: map[int64]int64{}, stored: map[int64]int64{}, uploads: map[string]int{}, media: map[string]*mtproto.MessageMedia{}, fail: map[string]int{}, seen: map[string]int{},
		accounts: []tgexport.Account{{Id: 501, Name: "Ivan Petrov"}, {Id: 502, Name: "Masha"}, {Id: 503, Name: "Oleg"}, {Id: 504, Name: "Extra"}},
		free:     1 << 40,
	}
}

var errFake = errors.New("fake failure")

func (f *fakeImport) step(name string) error {
	f.seen[name]++
	if f.fail[name] == f.seen[name] {
		return fmt.Errorf("%s: %w", name, errFake)
	}
	return nil
}

func (f *fakeImport) backend() *importBackend {
	return &importBackend{
		accounts: f, media: f, channel: f, up: f, ids: f,
		free:     func(string) (int64, error) { return f.free, nil },
		waveform: func(context.Context, string) []byte { return []byte{1, 2, 3} },
		sleep:    func(time.Duration) {},
	}
}

func (f *fakeImport) Accounts(context.Context) ([]tgexport.Account, error) { return f.accounts, nil }

func (f *fakeImport) UploadedBytes(context.Context, int64) (int64, error) { return 0, nil }

func (f *fakeImport) NextId(context.Context) (int64, error) {
	f.nextId++
	return 9000 + f.nextId, nil
}

func (f *fakeImport) Media(_ context.Context, chat int64, hash []byte) (*mtproto.MessageMedia, error) {
	return f.media[fmt.Sprintf("%d/%x", chat, hash)], nil
}

func (f *fakeImport) SaveMedia(_ context.Context, chat int64, hash []byte, _ int64, m *mtproto.MessageMedia) error {
	if err := f.step("save"); err != nil {
		return err
	}
	f.media[fmt.Sprintf("%d/%x", chat, hash)] = m
	return nil
}

func (f *fakeImport) WritePart(_ context.Context, _, fileId int64, part int32, b []byte) error {
	if int(part) != f.parts[fileId] || len(b) == 0 {
		return errors.New("parts out of order")
	}
	f.parts[fileId]++
	f.bytes[fileId] += int64(len(b))
	return nil
}

// record keeps what an upload made, unless it is the one to lose.
func (f *fakeImport) record(id, size int64) {
	if f.seen["upload"] != f.lost {
		f.stored[id] = size
	}
}

func (f *fakeImport) Photo(_ context.Context, id int64) (*mtproto.Photo, error) {
	if _, ok := f.stored[id]; !ok {
		return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: id}).To_Photo(), nil
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: id, Sizes: []*mtproto.PhotoSize{mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "m"}).To_PhotoSize()}}).To_Photo(), nil
}

func (f *fakeImport) Document(_ context.Context, id int64) (*mtproto.Document, error) {
	size, ok := f.stored[id]
	if !ok {
		return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: id}).To_Document(), nil
	}
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, Size2_INT64: size}).To_Document(), nil
}

func (f *fakeImport) UploadPhoto(_ context.Context, _ int64, file *mtproto.InputFile) (*mtproto.Photo, error) {
	if err := f.step("upload"); err != nil {
		return nil, err
	}
	if int(file.GetParts()) != f.parts[file.GetId_INT64()] || file.GetMd5Checksum() == "" {
		return nil, errors.New("the photo's parts")
	}
	f.uploads[file.GetName()]++
	f.nextId++
	f.record(f.nextId, 0)
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: f.nextId}).To_Photo(), nil
}

func (f *fakeImport) UploadDocument(_ context.Context, _ int64, in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	if err := f.step("upload"); err != nil {
		return nil, err
	}
	if int(in.GetFile().GetParts()) != f.parts[in.GetFile().GetId_INT64()] {
		return nil, errors.New("the document's parts")
	}
	f.uploads[in.GetFile().GetName()]++
	f.nextId++
	f.record(f.nextId, f.bytes[in.GetFile().GetId_INT64()])
	return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Spoiler:  in.GetSpoiler(),
		Document: mtproto.MakeTLDocument(&mtproto.Document{Id: f.nextId, MimeType: in.GetMimeType(), Attributes: in.GetAttributes()}).To_Document(),
	}).To_MessageMedia(), nil
}

func (f *fakeImport) answer() *channel.ChannelImport {
	s := proto.Clone(f.state).(*channel.ChannelImport)
	s.NextSeq, s.LastSourceId, s.Messages, s.Skipped = 0, 0, 0, 0
	for _, m := range f.journal {
		s.NextSeq, s.LastSourceId = m.Seq+1, m.SourceId
		if m.SkipReason == "" {
			s.Messages++
		} else {
			s.Skipped++
		}
	}
	return s
}

func (f *fakeImport) State(_ context.Context, importId, chat int64) (*channel.ChannelImport, error) {
	if f.state == nil || importId != 0 && importId != f.state.Id || importId == 0 && (chat != f.state.SourceChatId || f.state.State == importAborted) {
		return nil, nil
	}
	return f.answer(), nil
}

func (f *fakeImport) Create(_ context.Context, in *channel.TLChannelImportCreate) (*channel.ChannelImport, error) {
	if err := f.step("create"); err != nil {
		return nil, err
	}
	if f.state != nil {
		f.state.Mapping = in.Mapping
		return f.answer(), nil
	}
	if in.First.GetMessage().GetAction().GetPredicateName() != mtproto.Predicate_messageActionChannelCreate {
		return nil, errors.New("IMPORT_INVALID")
	}
	f.creates++
	f.state = &channel.ChannelImport{Id: 7, SourceType: in.SourceType, SourceChatId: in.SourceChatId, ChannelId: 70, CreatorId: in.CreatorId, Mapping: in.Mapping, Date: 1}
	f.journal = append(f.journal, in.First)
	return f.answer(), nil
}

func (f *fakeImport) Messages(_ context.Context, importId, firstSeq int64, batch []*channel.ImportedMessage) (*channel.ChannelImport, error) {
	if err := f.step("messages"); err != nil {
		return nil, err
	}
	if f.state == nil || f.state.Id != importId || f.state.State != importRunning {
		return nil, errors.New("IMPORT_FINISHED")
	}
	if next := f.answer().NextSeq; firstSeq != next || len(batch) == 0 {
		return nil, errors.New("IMPORT_OUT_OF_ORDER")
	}
	for i, m := range batch {
		if m.Seq != firstSeq+int64(i) {
			return nil, errors.New("IMPORT_OUT_OF_ORDER")
		}
	}
	f.journal = append(f.journal, batch...)
	if f.lose == f.seen["messages"] {
		return nil, fmt.Errorf("the answer of a batch: %w", errFake)
	}
	return f.answer(), nil
}

func (f *fakeImport) Finish(_ context.Context, importId int64) (*channel.ChannelImport, error) {
	if err := f.step("finish"); err != nil {
		return nil, err
	}
	f.state.State = importFinished
	return f.answer(), nil
}

func (f *fakeImport) Abort(_ context.Context, importId int64) (*channel.ChannelImport, error) {
	if f.state.State == importFinished {
		return nil, errors.New("IMPORT_FINISHED")
	}
	f.state.State = importAborted
	return f.answer(), nil
}

func (f *fakeImport) Invite(_ context.Context, creator, channelId int64, users []int64) error {
	if err := f.step("invite"); err != nil {
		return err
	}
	if f.state.State != importFinished || creator != f.state.CreatorId || channelId != f.state.ChannelId || len(users) > importInviteMax {
		return errors.New("a wrong invite")
	}
	f.invites++
	for _, u := range users {
		if !slices.Contains(f.invited, u) {
			f.invited = append(f.invited, u)
		}
	}
	return nil
}

// importCmd runs import-chat with f behind it.
func importCmd(f *fakeImport, args ...string) (string, error) {
	var out bytes.Buffer
	err := importChat(context.Background(), append([]string{"import-chat"}, args...), "", &out, func(string, bool) (*importBackend, error) {
		return f.backend(), nil
	})
	return out.String(), err
}

func (f *fakeImport) bySource(t *testing.T, source int64) *channel.ImportedMessage {
	t.Helper()
	for _, m := range f.journal {
		if m.SourceId == source {
			return m
		}
	}
	t.Fatalf("record %d is not journaled", source)
	return nil
}

func attributes(m *channel.ImportedMessage) map[string]*mtproto.DocumentAttribute {
	out := map[string]*mtproto.DocumentAttribute{}
	for _, a := range m.GetMessage().GetMedia().GetDocument().GetAttributes() {
		out[a.GetPredicateName()] = a
	}
	return out
}

// checkImported checks the journal of a whole import of the export: every
// record once, in order, each kind built as a client would have sent it.
func checkImported(t *testing.T, f *fakeImport) {
	t.Helper()
	if len(f.journal) != 42 || f.creates != 1 {
		t.Fatalf("%d records journaled by %d creates", len(f.journal), f.creates)
	}
	written := 0
	for i, m := range f.journal {
		if m.Seq != int64(i+1) {
			t.Fatalf("record %d has seq %d", i+1, m.Seq)
		}
		if m.SkipReason == "" {
			written++
		}
	}
	if written != 33 {
		t.Errorf("%d messages written", written)
	}
	// The export's own creation of the group is the first message.
	first := f.journal[0]
	if first.SourceId != -999999990 || first.SenderId != 501 || first.Message.GetDate() != 1600000000 ||
		first.Message.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChannelCreate || first.Message.GetAction().GetTitle_STRING() != "Friends" {
		t.Errorf("the first message: %v", first)
	}
	formatted := f.bySource(t, -999999988)
	if formatted.SenderId != 502 || formatted.ReplyToSourceId != -999999989 || len(formatted.Message.GetEntities()) != 18 || !reflect.DeepEqual(formatted.Mentioned, []int64{501}) {
		t.Errorf("a formatted reply: sender %d, reply %d, %d entities, mentions %v", formatted.SenderId, formatted.ReplyToSourceId, len(formatted.Message.GetEntities()), formatted.Mentioned)
	}
	photo := f.bySource(t, 2).GetMessage()
	if photo.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto || photo.GetMedia().GetPhoto_FLAGPHOTO().GetId() == 0 || !photo.GetMedia().GetSpoiler() || photo.GetDate() != 1600000300 {
		t.Errorf("the photo: %v", photo)
	}
	if a := attributes(f.bySource(t, 3)); a[mtproto.Predicate_documentAttributeVideo].GetDuration_FLOAT64() != 5 || a[mtproto.Predicate_documentAttributeVideo].GetW() != 640 ||
		a[mtproto.Predicate_documentAttributeVideo].GetRoundMessage() || a[mtproto.Predicate_documentAttributeFilename].GetFileName() != "v.mp4" {
		t.Errorf("the video: %v", a)
	}
	if a := attributes(f.bySource(t, 4)); a[mtproto.Predicate_documentAttributeAnimated] == nil || !a[mtproto.Predicate_documentAttributeVideo].GetNosound() {
		t.Errorf("the animation: %v", a)
	}
	if a := attributes(f.bySource(t, 5)); len(a) != 1 || a[mtproto.Predicate_documentAttributeFilename].GetFileName() != "Report.pdf" {
		t.Errorf("the file: %v", a)
	}
	if a := attributes(f.bySource(t, 6))[mtproto.Predicate_documentAttributeAudio]; a.GetVoice() || a.GetDuration_INT32() != 180 || a.GetTitle().GetValue() != "Song" || a.GetPerformer().GetValue() != "Band" {
		t.Errorf("the music: %v", a)
	}
	voice := f.bySource(t, 7)
	if a := attributes(voice)[mtproto.Predicate_documentAttributeAudio]; !a.GetVoice() || a.GetDuration_INT32() != 3 || !bytes.Equal(a.GetWaveform(), []byte{1, 2, 3}) || voice.ReplyToSourceId != 2 ||
		voice.Message.GetMedia().GetDocument().GetMimeType() != "audio/ogg" {
		t.Errorf("the voice message: %v", voice)
	}
	if a := attributes(f.bySource(t, 8))[mtproto.Predicate_documentAttributeVideo]; !a.GetRoundMessage() || a.GetDuration_FLOAT64() != 4 {
		t.Errorf("the round video message: %v", a)
	}
	if a := attributes(f.bySource(t, 9)); a[mtproto.Predicate_documentAttributeSticker].GetAlt() != "\U0001F600" ||
		a[mtproto.Predicate_documentAttributeSticker].GetStickerset().GetPredicateName() != mtproto.Predicate_inputStickerSetEmpty || a[mtproto.Predicate_documentAttributeImageSize].GetW() != 512 {
		t.Errorf("the sticker: %v", a)
	}
	if c := f.bySource(t, 10).GetMessage().GetMedia(); c.GetPredicateName() != mtproto.Predicate_messageMediaContact || c.GetPhoneNumber() != "79990000000" || c.GetFirstName() != "Oleg" {
		t.Errorf("the contact's media: %s", c.GetPredicateName())
	}
	if g := f.bySource(t, 11).GetMessage().GetMedia(); g.GetPredicateName() != mtproto.Predicate_messageMediaGeo || g.GetGeo().GetLat() != 55.75 || g.GetGeo().GetLong() != 37.61 {
		t.Errorf("the location: %v", g)
	}
	if v := f.bySource(t, 12).GetMessage().GetMedia(); v.GetPredicateName() != mtproto.Predicate_messageMediaVenue || v.GetTitle() != "Cafe" || v.GetAddress() != "Main st. 1" || v.GetGeo().GetLat() != 55.7 {
		t.Errorf("the venue: %v", v)
	}
	poll := f.bySource(t, 13)
	if p := poll.GetMessage().GetMedia().GetPoll(); poll.PollId == 0 || poll.PollVoterId != 501 || !reflect.DeepEqual(poll.PollChosen, []int32{0}) ||
		!p.GetCreator() || p.GetQuestion_TEXTWITHENTITIES().GetText() != "Where?" || len(p.GetAnswers()) != 2 || len(p.GetAnswers()[1].GetOption()) == 0 {
		t.Errorf("the poll: %v", poll)
	}
	if l := f.bySource(t, 14).GetMessage().GetMedia().GetTodo(); l.GetTitle().GetText() != "Trip" || len(l.GetList()) != 2 || l.GetList()[1].GetId() != 2 || !l.GetOthersCanAppend() {
		t.Errorf("the checklist: %v", l)
	}
	if m := f.bySource(t, 15); m.ReplyToSourceId != 0 {
		t.Errorf("a reply to another chat: %d", m.ReplyToSourceId)
	}
	if h := f.bySource(t, 16).GetMessage().GetFwdFrom(); h.GetFromId().GetUserId() != 502 || h.GetFromName() != nil || h.GetDate() != 1600001140 {
		t.Errorf("a forward from a member: %v", h)
	}
	if h := f.bySource(t, 17).GetMessage().GetFwdFrom(); h.GetFromId() != nil || h.GetFromName().GetValue() != "Some Channel" {
		t.Errorf("a forward by name: %v", h)
	}
	if m := f.bySource(t, 39).GetMessage(); m.GetFwdFrom() != nil || m.GetPredicateName() != mtproto.Predicate_message {
		t.Errorf("a forward from nobody: %v", m.GetFwdFrom())
	}
	if m := f.bySource(t, 18).GetMessage(); m.GetEditDate().GetValue() != 1600001320 {
		t.Errorf("the edit date: %v", m.GetEditDate())
	}
	actions := map[int64]string{
		19: mtproto.Predicate_messageActionChatEditTitle, 20: mtproto.Predicate_messageActionChatEditPhoto, 21: mtproto.Predicate_messageActionChatDeletePhoto,
		22: mtproto.Predicate_messageActionChatAddUser, 23: mtproto.Predicate_messageActionChatDeleteUser, 24: mtproto.Predicate_messageActionChatJoinedByLink,
		25: mtproto.Predicate_messageActionChatJoinedByRequest, 26: mtproto.Predicate_messageActionPinMessage,
	}
	for source, want := range actions {
		m := f.bySource(t, source).GetMessage()
		if m.GetPredicateName() != mtproto.Predicate_messageService || m.GetAction().GetPredicateName() != want {
			t.Errorf("record %d: %s %s, want %s", source, m.GetPredicateName(), m.GetAction().GetPredicateName(), want)
		}
	}
	if a := f.bySource(t, 20).GetMessage().GetAction(); a.GetPhoto().GetId() == 0 {
		t.Error("the group's photo was not uploaded")
	}
	if a := f.bySource(t, 22).GetMessage().GetAction(); !reflect.DeepEqual(a.GetUsers(), []int64{502, 503}) {
		t.Errorf("the members added: %v", a.GetUsers())
	}
	if f.bySource(t, 23).GetMessage().GetAction().GetUserId() != 503 || f.bySource(t, 24).GetMessage().GetAction().GetInviterId() != 501 || f.bySource(t, 24).SenderId != 502 {
		t.Error("the member removed, the inviter of a link")
	}
	if f.bySource(t, 26).ReplyToSourceId != 2 {
		t.Error("the pin does not name its message")
	}
	skips := map[int64]string{
		-999999987: tgexport.SkipAction, 1: tgexport.SkipAction, 27: tgexport.SkipAction, 28: tgexport.SkipUnsupported, 30: tgexport.SkipMissing,
		31: tgexport.SkipMissing, 34: tgexport.SkipAuthor, 35: tgexport.SkipMedia, 38: tgexport.SkipAction,
	}
	for source, want := range skips {
		if m := f.bySource(t, source); m.SkipReason != want || m.SenderId != 0 {
			t.Errorf("record %d: skip %q, want %q", source, m.SkipReason, want)
		}
	}
	// A photo that is not in the export leaves its caption.
	if m := f.bySource(t, 29).GetMessage(); m.GetMedia() != nil || m.GetMessage() != "caption" {
		t.Errorf("a caption without its photo: %v", m.GetMedia())
	}
	// The one reaction with a named, mapped author.
	if r := f.bySource(t, 32).Reactions; len(r) != 1 || r[0].UserId != 501 || r[0].Reaction != "e\U0001F44D" || r[0].Date != 1600003200 {
		t.Errorf("the reactions: %v", r)
	}
	// Every file once.
	want := map[string]int{"photo_1.jpg": 1, "v.mp4": 1, "g.mp4": 1, "doc.pdf": 1, "song.mp3": 1, "a.ogg": 1, "r.mp4": 1, "s.webp": 1, "chat.jpg": 1}
	if !reflect.DeepEqual(f.uploads, want) {
		t.Errorf("uploads %v", f.uploads)
	}
	if f.state.State != importFinished || !reflect.DeepEqual(f.invited, []int64{502, 503, 504}) {
		t.Errorf("state %d, invited %v", f.state.State, f.invited)
	}
	if f.state.Mapping != `{"user111":501,"user222":502,"user444":0}` {
		t.Errorf("the mapping kept: %s", f.state.Mapping)
	}
}

// importTexts are texts of messages of the export: none may be printed.
var importTexts = []string{"hello", "look", "caption", "elsewhere", "edited", "smile at this", "Where?", "Tickets", "Report.pdf", "999 000", "Main st", "example.org"}

func checkNoTexts(t *testing.T, out string) {
	t.Helper()
	for _, s := range importTexts {
		if strings.Contains(out, s) {
			t.Errorf("the output holds %q of a message:\n%s", s, out)
		}
	}
}

func TestImportRun(t *testing.T) {
	dir := importExport(t, importTestMapping)
	f := newFakeImport()
	out, err := importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000", "-batch", "5")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	checkImported(t, f)
	checkNoTexts(t, out)
	for _, want := range []string{"To import: 33 messages", "Reactions: 7 by the counters, 2 with their author named, 1 to import", "Import 7 is finished: channel 70, 33 messages, 9 records not imported, 9 files uploaded now (135 bytes)", "3 members besides the creator",
		`-> Pluma 501 "Ivan Petrov"`, `-> Pluma 502 "Masha"`, "-> skipped", "-> no account", "Edit dates: 1 messages"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	// A second run changes nothing and uploads nothing.
	batches := f.seen["messages"]
	if out, err = importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000"); err != nil {
		t.Fatalf("again: %v\n%s", err, out)
	}
	checkImported(t, f)
	if f.seen["messages"] != batches || f.seen["finish"] != 1 {
		t.Errorf("a second run wrote: %v", f.seen)
	}
	if out, err = importCmd(f, "status", "-dir", dir); err != nil || !strings.Contains(out, "Import 7 of telegram chat 1234567890: finished; channel 70, creator 501; 42 records journaled, 33 messages, 9 not imported") {
		t.Errorf("status: %q, %v", out, err)
	}
	if _, err = importCmd(f, "abort", "7"); err == nil {
		t.Error("a finished import was aborted")
	}
}

// TestImportResume: an import that fails at any step goes on with the same
// command and ends as one that never failed: every record once, every file
// uploaded once.
func TestImportResume(t *testing.T) {
	steps := []struct {
		name string
		set  func(f *fakeImport)
	}{
		{"the first upload", func(f *fakeImport) { f.fail["upload"] = 1 }},
		{"an upload in the middle", func(f *fakeImport) { f.fail["upload"] = 5 }},
		{"the creation", func(f *fakeImport) { f.fail["create"] = 1 }},
		{"the first batch", func(f *fakeImport) { f.fail["messages"] = 1 }},
		{"a batch in the middle", func(f *fakeImport) { f.fail["messages"] = 4 }},
		{"the answer of a batch", func(f *fakeImport) { f.lose = 3 }},
		{"the end", func(f *fakeImport) { f.fail["finish"] = 1 }},
		{"the members", func(f *fakeImport) { f.fail["invite"] = 1 }},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			dir := importExport(t, importTestMapping)
			f := newFakeImport()
			s.set(f)
			args := []string{"run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000", "-batch", "5"}
			out, err := importCmd(f, args...)
			if !errors.Is(err, errFake) || !strings.Contains(err.Error(), "run the same command again") && !strings.Contains(err.Error(), "Run the same command again") {
				t.Fatalf("the failing run: %v\n%s", err, out)
			}
			if out, err = importCmd(f, args...); err != nil {
				t.Fatalf("the second run: %v\n%s", err, out)
			}
			checkImported(t, f)
		})
	}
	// A file uploaded whose media could not be remembered is uploaded once
	// more: the one object a failure leaves behind.
	dir := importExport(t, importTestMapping)
	f := newFakeImport()
	f.fail["save"] = 2
	args := []string{"run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000"}
	if _, err := importCmd(f, args...); !errors.Is(err, errFake) {
		t.Fatalf("the failing run: %v", err)
	}
	if _, err := importCmd(f, args...); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range f.uploads {
		total += n
	}
	if total != 10 || len(f.journal) != 42 {
		t.Errorf("%d uploads, %d records", total, len(f.journal))
	}

	// An upload media answered and did not record stops the import before
	// a message is written with it; the next run uploads the file again.
	for _, lost := range []int{1, 4} {
		f = newFakeImport()
		f.lost = lost
		_, err := importCmd(f, args...)
		if err == nil || !strings.Contains(err.Error(), "was not recorded") {
			t.Fatalf("upload %d not recorded: %v", lost, err)
		}
		for _, m := range f.journal {
			if id := m.GetMessage().GetMedia().GetDocument().GetId() + m.GetMessage().GetMedia().GetPhoto_FLAGPHOTO().GetId(); id != 0 {
				if _, ok := f.stored[id]; !ok {
					t.Fatalf("record %d is written with media %d that was not recorded", m.Seq, id)
				}
			}
		}
		if _, err = importCmd(f, args...); err != nil {
			t.Fatal(err)
		}
		// The file whose upload was not recorded is uploaded once more, and
		// that is the only file uploaded twice.
		var again []string
		for name, n := range f.uploads {
			if n == 2 {
				again = append(again, name)
				f.uploads[name] = 1
			}
		}
		if len(again) != 1 {
			t.Errorf("upload %d not recorded: files uploaded twice %v, uploads %v", lost, again, f.uploads)
		}
		checkImported(t, f)
	}
}

// TestImportRefuses: nothing is written when the mapping leaves an author
// without an account, a file is not in the export, the storage has no room
// or the export is not the one the import began with.
func TestImportRefuses(t *testing.T) {
	untouched := func(f *fakeImport) {
		t.Helper()
		if f.state != nil || len(f.uploads) != 0 || f.seen["create"] != 0 {
			t.Errorf("a refused import wrote: %v %v", f.state, f.uploads)
		}
	}
	// An author without a confirmed account.
	dir := importExport(t, strings.Replace(importTestMapping, `"user222": {"pluma": 502, "confirmed": true}`, `"user222": {"pluma": 502, "confirmed": false}`, 1))
	f := newFakeImport()
	out, err := importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000")
	if err == nil || !strings.Contains(err.Error(), "author user222 (7 messages) has no confirmed Pluma account") || !strings.Contains(out, `NO ACCOUNT: user222 ["Masha"], 7 messages (unconfirmed)`) {
		t.Errorf("an unconfirmed author: %v\n%s", err, out)
	}
	untouched(f)
	if out, err = importCmd(f, "plan", "-dir", dir, "-missing-media=skip"); err == nil || strings.Contains(out, "The import can start") {
		t.Errorf("the plan with an unconfirmed author: %v\n%s", err, out)
	}
	checkNoTexts(t, out)

	// An account that does not exist.
	dir = importExport(t, strings.Replace(importTestMapping, `"pluma": 502`, `"pluma": 599`, 1))
	if _, err = importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000"); err == nil || !strings.Contains(err.Error(), "no Pluma account 599") {
		t.Errorf("an account that is not there: %v", err)
	}
	untouched(f)

	// Files that are not in the export stop the import unless told to go on.
	dir = importExport(t, importTestMapping)
	if _, err = importCmd(f, "run", "-dir", dir, "-free-bytes", "9000000000"); err == nil || !strings.Contains(err.Error(), "-missing-media=skip") {
		t.Errorf("missing files: %v", err)
	}
	untouched(f)
	if out, err = importCmd(f, "plan", "-dir", dir, "-missing-media=skip"); err != nil || !strings.Contains(out, "The import can start") {
		t.Errorf("the plan: %v\n%s", err, out)
	}

	// The room of the storage: asked for, then not enough.
	if _, err = importCmd(f, "run", "-dir", dir, "-missing-media=skip"); err == nil || !strings.Contains(err.Error(), "-storage-dir") {
		t.Errorf("no word on the storage: %v", err)
	}
	if _, err = importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-free-bytes", "5000000000"); err == nil || !strings.Contains(err.Error(), "not enough room") {
		t.Errorf("a full storage: %v", err)
	}
	f.free = 1 << 20
	if _, err = importCmd(f, "run", "-dir", dir, "-missing-media=skip", "-storage-dir", "/data"); err == nil || !strings.Contains(err.Error(), "not enough room") {
		t.Errorf("a full disk: %v", err)
	}
	untouched(f)

	// Another export of the chat under the import that runs, and another
	// creator.
	f = newFakeImport()
	f.fail["messages"] = 3
	args := []string{"run", "-dir", dir, "-missing-media=skip", "-free-bytes", "9000000000", "-batch", "5"}
	if _, err = importCmd(f, args...); !errors.Is(err, errFake) {
		t.Fatal(err)
	}
	written := len(f.journal)
	f.journal[written-1].SourceId = 77777
	if _, err = importCmd(f, args...); err == nil || !strings.Contains(err.Error(), "not the export the import began with") {
		t.Errorf("another export: %v", err)
	}
	f.state.CreatorId = 502
	if _, err = importCmd(f, args...); err == nil || !strings.Contains(err.Error(), "started for creator 502") {
		t.Errorf("another creator: %v", err)
	}
	if len(f.journal) != written {
		t.Error("a refused resume wrote")
	}
	// An import under way can be aborted.
	f.state.CreatorId = 501
	if out, err = importCmd(f, "abort", "7"); err != nil || !strings.Contains(out, "aborted") {
		t.Errorf("abort: %q, %v", out, err)
	}
	if _, err = importCmd(f, "status", "-dir", dir); err == nil {
		t.Error("the status of a chat whose import is aborted")
	}

	for _, bad := range [][]string{{}, {"run"}, {"plan", "-dir", dir, "extra"}, {"run", "-dir", dir, "-missing-media=maybe"}, {"run", "-dir", dir, "-batch", "0"}, {"abort"}, {"abort", "x"}, {"status"}, {"nothing"}} {
		if _, err = importCmd(f, bad...); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v, want usage", bad, err)
		}
	}
}

// TestImportMap: map writes who is who for the owner to fill, suggesting an
// account by name, and keeps what is filled in when run again.
func TestImportMap(t *testing.T) {
	dir := importExport(t, "")
	f := newFakeImport()
	out, err := importCmd(f, "map", "-dir", dir)
	if err != nil || !strings.Contains(out, "4 peers of chat 1234567890; 2 with an account found by name") {
		t.Fatalf("map: %q, %v", out, err)
	}
	m, err := tgexport.LoadMapping(filepath.Join(dir, "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	if a := m.Authors["user111"]; a.Pluma == nil || *a.Pluma != 501 || a.Confirmed || !a.Suggested {
		t.Errorf("the suggestion: %+v", a)
	}
	// Not filled in yet: the plan fails and names who is left.
	if out, err = importCmd(f, "plan", "-dir", dir); err == nil || !strings.Contains(out, "NO ACCOUNT: user111") || !strings.Contains(err.Error(), `"creator" is not set`) {
		t.Errorf("the plan of a new mapping: %v\n%s", err, out)
	}
	m.Creator = 501
	m.Authors["user111"].Confirmed = true
	if err = m.Save(filepath.Join(dir, "mapping.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = importCmd(f, "map", "-dir", dir); err != nil {
		t.Fatal(err)
	}
	if m, err = tgexport.LoadMapping(filepath.Join(dir, "mapping.json")); err != nil || m.Creator != 501 || !m.Authors["user111"].Confirmed {
		t.Errorf("the mapping after a second map: %+v, %v", m, err)
	}
	if out, err = importCmd(f, "accounts"); err != nil || !strings.Contains(out, "501") || !strings.Contains(out, "Ivan Petrov") {
		t.Errorf("accounts: %q, %v", out, err)
	}
}

// TestImportBigFile: a file is sent in parts of 512 KB, and one over 10 MB
// as a big file, without a checksum.
// TestImportSharedFile: two messages with one file of the export upload it
// once, and each keeps its own spoiler.
func TestImportSharedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, tgexport.ResultFile), []byte(`{"name":"G","type":"private_group","id":7,"messages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.jpg"), []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	ex, err := tgexport.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeImport()
	files := &importFiles{ex: ex, up: f, store: f}
	ctx := context.Background()
	hidden, err := files.media(ctx, &tgexport.Media{Type: tgexport.MediaPhoto, Path: "p.jpg", Size: 4, Spoiler: true})
	if err != nil {
		t.Fatal(err)
	}
	open, err := files.media(ctx, &tgexport.Media{Type: tgexport.MediaPhoto, Path: "p.jpg", Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	again, err := files.media(ctx, &tgexport.Media{Type: tgexport.MediaPhoto, Path: "p.jpg", Size: 4, Spoiler: true})
	if err != nil {
		t.Fatal(err)
	}
	if f.uploads["p.jpg"] != 1 || files.uploaded != 1 || files.reused != 2 {
		t.Errorf("uploads %d, uploaded %d, reused %d", f.uploads["p.jpg"], files.uploaded, files.reused)
	}
	if !hidden.GetSpoiler() || open.GetSpoiler() || !again.GetSpoiler() {
		t.Errorf("the spoilers: %v, %v, %v", hidden.GetSpoiler(), open.GetSpoiler(), again.GetSpoiler())
	}
	if open.GetPhoto_FLAGPHOTO().GetId() != hidden.GetPhoto_FLAGPHOTO().GetId() {
		t.Errorf("the photos: %d and %d", hidden.GetPhoto_FLAGPHOTO().GetId(), open.GetPhoto_FLAGPHOTO().GetId())
	}
}

func TestImportBigFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, tgexport.ResultFile), []byte(`{"name":"G","type":"private_group","id":7,"messages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	size := bigFileSize + 100
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	ex, err := tgexport.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeImport()
	files := &importFiles{ex: ex, up: f, store: f}
	m := &tgexport.Media{Type: tgexport.MediaFile, Path: "big.bin", Size: int64(size)}
	file, err := files.send(context.Background(), 1, m)
	if err != nil {
		t.Fatal(err)
	}
	if file.GetPredicateName() != mtproto.Predicate_inputFileBig || file.GetParts() != 21 || file.GetMd5Checksum() != "" || f.parts[file.GetId_INT64()] != 21 {
		t.Errorf("the big file: %v", file)
	}
	m.Size = 5
	if _, err = files.send(context.Background(), 1, m); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a file of another size than planned: %v", err)
	}
	if got := mimeType(&tgexport.Media{Path: "a/b.PDF"}); got != "application/pdf" {
		t.Errorf("the type of a .PDF: %q", got)
	}
	if got := mimeType(&tgexport.Media{Path: "a/b"}); got != "application/octet-stream" {
		t.Errorf("the type of a file without an extension: %q", got)
	}
}

// TestWaveform: the peaks of a hundred spans, scaled against 1.8 times
// their mean, five bits each.
func TestWaveform(t *testing.T) {
	if got := encodeWaveform([]byte{1, 2}); !bytes.Equal(got, []byte{0x41, 0x00}) {
		t.Errorf("two values packed: %x", got)
	}
	if got := encodeWaveform([]byte{31, 31, 31, 31, 31, 31, 31, 31}); !bytes.Equal(got, []byte{0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("eight values packed: %x", got)
	}
	if encodeWaveform(nil) != nil || waveformOf(make([]uint16, waveformSamples-1)) != nil {
		t.Error("a waveform of nothing")
	}
	// A loud half and a quiet half, three windows a span.
	peaks := make([]uint16, 0, 3*waveformSamples)
	for i := range 3 * waveformSamples {
		p := uint16(1000)
		if i < 3*waveformSamples/2 {
			p = 10000
		}
		peaks = append(peaks, p)
	}
	w := waveformOf(peaks)
	if len(w) != waveformSamples || w[0] != 31 || w[49] != 31 || w[50] != 3 || w[99] != 3 {
		t.Errorf("the waveform: %v", w)
	}
	// Silence stays flat: the scale never falls under 2500.
	if w = waveformOf(make([]uint16, waveformSamples)); w[0] != 0 {
		t.Errorf("silence: %v", w)
	}
	if len(encodeWaveform(w)) != 63 {
		t.Errorf("%d bytes of a waveform", len(encodeWaveform(w)))
	}
	// The peaks of windows of samples: the largest magnitude of each.
	pcm := make([]byte, 2*(waveformWindow+3))
	binary.LittleEndian.PutUint16(pcm[10:], uint16(0x8000)) // -32768
	binary.LittleEndian.PutUint16(pcm[2*waveformWindow+2:], 500)
	got, err := windowPeaks(bytes.NewReader(pcm))
	if err != nil || !reflect.DeepEqual(got, []uint16{32767, 500}) {
		t.Errorf("the peaks: %v, %v", got, err)
	}
}
