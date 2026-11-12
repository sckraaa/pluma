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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// importT0 is the date of the first record of testdata/chat_export; each
// record is a minute after the one before.
const importT0 = 1700000000

// adminTry runs tools/pluma-admin in the bff container, as admin does, and
// returns its failure instead of ending the test.
func adminTry(args ...string) (string, error) {
	adminMu.Lock()
	defer adminMu.Unlock()
	out, err := exec.Command(runScript, append([]string{"admin"}, args...)...).CombinedOutput()
	return string(out), err
}

// importExportDir copies testdata/chat_export into a new folder as the
// export of chat chatId.
func importExportDir(t *testing.T, chatId int64) string {
	t.Helper()
	dir := t.TempDir()
	err := filepath.Walk("testdata/chat_export", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/chat_export", path)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if rel == "result.json" {
			b = bytes.Replace(b, []byte(`"id": 990000001`), []byte(fmt.Sprintf(`"id": %d`, chatId)), 1)
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// importMapping writes the mapping of the export: Alice the creator and the
// one who made the export, Bob, and Carol when she has an account.
func importMapping(t *testing.T, path string, chatId int64, alice, bob, carol *user) {
	t.Helper()
	author := func(u *user) map[string]any { return map[string]any{"pluma": u.self.GetId(), "confirmed": true} }
	authors := map[string]any{"user111": author(alice), "user222": author(bob)}
	if carol != nil {
		authors["user333"] = author(carol)
	}
	b, err := json.Marshal(map[string]any{
		"export_chat_id": chatId, "creator": alice.self.GetId(), "exporter": alice.self.GetId(),
		"authors": authors, "service_names": map[string]any{}, "extra_members": []int64{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func composeRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(runScript, append([]string{"compose"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("compose %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

var importJournaled = regexp.MustCompile(`(\d+) records journaled`)

// importInterrupted starts the import of the export at dst in the bff
// container as a group titled title, two records a transaction, and kills
// it once the first
// transaction after the creation is written: the import is left running,
// with three of its records journaled.
func importInterrupted(t *testing.T, dst, title string) {
	t.Helper()
	// One pluma-admin of another test at a time beside this one: the
	// container's memory is small.
	adminMu.Lock()
	defer adminMu.Unlock()
	pid := dst + ".pid"
	// The pause after a transaction is long, so the kill finds the tool
	// asleep after its first one.
	run := exec.Command(runScript, "compose", "exec", "-T", "bff", "sh", "-c",
		`echo $$ > "$0"; exec ./pluma-admin -f ../etc2/bff.yaml import-chat run -dir "$1" -title "$2" -free-bytes 6000000000 -batch 2 -pause 10m`, pid, dst, title)
	var out bytes.Buffer
	run.Stdout, run.Stderr = &out, &out
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- run.Wait() }()
	status := func() string {
		b, _ := exec.Command(runScript, "admin", "import-chat", "status", "-dir", dst).CombinedOutput()
		return string(b)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if m := importJournaled.FindStringSubmatch(status()); m != nil && m[1] == "3" {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the import ended before it was interrupted: %v\n%s", err, out.String())
		case <-time.After(time.Second):
		}
		if time.Now().After(deadline) {
			t.Fatalf("the import wrote no transaction in time:\n%s\n%s", out.String(), status())
		}
	}
	if b, err := exec.Command(runScript, "compose", "exec", "-T", "bff", "sh", "-c", `kill "$(cat "$0")"`, pid).CombinedOutput(); err != nil {
		t.Fatalf("kill the import: %v\n%s", err, b)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("the killed import ended well:\n%s", out.String())
		}
	case <-time.After(time.Minute):
		t.Fatal("the killed import did not end")
	}
	if s := status(); !strings.Contains(s, "running") || !strings.Contains(s, "3 records journaled") {
		t.Fatalf("the import after the kill: %s", s)
	}
}

// importedChannel finds the channel titled title in u's dialogs, waiting
// for it to come, and returns it with u's dialog with it.
func importedChannel(ctx context.Context, t *testing.T, u *user, title string) (*mtproto.Chat, *mtproto.Dialog) {
	t.Helper()
	for {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogs),
			OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
			Limit:       100,
		})
		if err != nil {
			t.Fatalf("messages.getDialogs: %v", err)
		}
		ds := o.(interface {
			To_Messages_Dialogs() *mtproto.Messages_Dialogs
		}).To_Messages_Dialogs()
		for _, c := range ds.GetChats() {
			if c.GetTitle() != title {
				continue
			}
			for _, d := range ds.GetDialogs() {
				if d.GetPeer().GetChannelId() == c.GetId() {
					return c, d
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the imported group %q is not in the dialogs of %d", title, u.self.GetId())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func downloadDocument(ctx context.Context, t *testing.T, u *user, doc *mtproto.Document) []byte {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
		Location: mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
			Id: doc.GetId(), AccessHash: doc.GetAccessHash(), FileReference: doc.GetFileReference(),
		}).To_InputFileLocation(),
		Limit: 1 << 20,
	})
	if err != nil {
		t.Fatalf("upload.getFile of document %d: %v", doc.GetId(), err)
	}
	return o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes()
}

// TestImportChat (task 083, ADR 0051): a group exported from Telegram
// Desktop is imported with pluma-admin import-chat. An author without an
// account stops it before anything is written; an import that is killed
// goes on with the same command; the members then find the group with the
// messages of their own accounts, dated as they were, with the files, the
// replies, the forward, the edit, the poll and the reactions the export
// names, all of it read: no unread count, mention or reaction, the voice
// and the round video message played. What is sent afterwards arrives as in
// any group.
func TestImportChat(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	for _, u := range []*user{alice, bob, carol} {
		getState(ctx, t, u)
	}

	chatId := 900000000 + randomID()%90000000
	title := fmt.Sprintf("Import e2e %d", chatId)
	dir := importExportDir(t, chatId)
	dst := fmt.Sprintf("/tmp/import-%d", chatId)
	mapping := filepath.Join(dir, "mapping.json")
	importMapping(t, mapping, chatId, alice, bob, nil)
	composeRun(t, "cp", dir, "bff:"+dst)

	// Carol wrote in the group and the mapping does not know her: neither
	// the plan nor the import go on, and nothing is written.
	out, err := adminTry("import-chat", "plan", "-dir", dst)
	if err == nil || !strings.Contains(out, `NO ACCOUNT: user333 ["Carol C"], 1 messages (missing)`) || !strings.Contains(out, "author user333 (1 messages) has no confirmed Pluma account") {
		t.Fatalf("the plan without Carol: %v\n%s", err, out)
	}
	if strings.Contains(out, "import first") || strings.Contains(out, "import photo caption") {
		t.Errorf("the plan prints texts of messages:\n%s", out)
	}
	if out, err = adminTry("import-chat", "run", "-dir", dst, "-free-bytes", "6000000000", "-title", title); err == nil || !strings.Contains(out, "the import cannot start") {
		t.Fatalf("the import without Carol: %v\n%s", err, out)
	}
	if out, err = adminTry("import-chat", "status", "-dir", dst); err == nil || !strings.Contains(out, "no such import") {
		t.Fatalf("a refused import left something: %v\n%s", err, out)
	}

	// With everyone mapped the plan says what will be imported.
	importMapping(t, mapping, chatId, alice, bob, carol)
	composeRun(t, "cp", mapping, "bff:"+dst+"/mapping.json")
	out = admin(t, "import-chat", "plan", "-dir", dst)
	for _, want := range []string{"To import: 12 messages", "Files: 3,", "Reactions: 5 by the counters, 2 with their author named, 2 to import", "Members besides the creator: 2", "The import can start."} {
		if !strings.Contains(out, want) {
			t.Fatalf("the plan lacks %q:\n%s", want, out)
		}
	}

	// The import is killed after its first transaction and run again.
	importInterrupted(t, dst, title)
	out = admin(t, "import-chat", "run", "-dir", dst, "-free-bytes", "6000000000", "-batch", "2", "-title", title)
	if !strings.Contains(out, "goes on from record 4") || !strings.Contains(out, "12 messages, 0 records not imported") || !strings.Contains(out, "2 members besides the creator") {
		t.Fatalf("the import run again:\n%s", out)
	}
	// The photo went with the transaction of the run that was killed: the
	// second run uploads the two files left.
	if !strings.Contains(out, "2 files uploaded now") {
		t.Errorf("the files of the second run:\n%s", out)
	}
	if out = admin(t, "import-chat", "status", "-dir", dst); !strings.Contains(out, "finished") || !strings.Contains(out, "12 records journaled, 12 messages") {
		t.Errorf("the status of the finished import: %s", out)
	}
	// A third run adds nothing.
	admin(t, "import-chat", "run", "-dir", dst, "-free-bytes", "6000000000", "-title", title)

	ids := map[string]int64{"alice": alice.self.GetId(), "bob": bob.self.GetId(), "carol": carol.self.GetId()}
	voiceFile, err := os.ReadFile("testdata/chat_export/voice_messages/audio_1.ogg")
	if err != nil {
		t.Fatal(err)
	}
	roundFile, err := os.ReadFile("testdata/chat_export/round_video_messages/round_1.mp4")
	if err != nil {
		t.Fatal(err)
	}

	// Each member gets the group by an event of its own: all three have it
	// before any of them is looked at, as one's outbox is read by the others.
	members := []struct {
		name string
		u    *user
	}{{"alice", alice}, {"bob", bob}, {"carol", carol}}
	for _, member := range members {
		importedChannel(ctx, t, member.u, title)
	}
	var channelId int64
	for _, member := range members {
		name, u := member.name, member.u
		ch, _ := importedChannel(ctx, t, u, title)
		channelId = ch.GetId()
		if !ch.GetMegagroup() || ch.GetLeft() || ch.GetCreator() != (name == "alice") {
			t.Errorf("%s: the group: %v", name, ch)
		}
		peer := &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
		// Everything is read: the group was added to the dialogs with its
		// history behind it, and the others have read what one wrote.
		if d := peerDialogs(ctx, t, u, peer).GetDialogs()[0]; d.GetTopMessage() != 13 || d.GetUnreadCount() != 0 || d.GetUnreadMentionsCount() != 0 || d.GetUnreadReactionsCount() != 0 ||
			d.GetReadOutboxMaxId() != 13 || name != "alice" && d.GetReadInboxMaxId() != 13 || name == "alice" && d.GetReadInboxMaxId() < 12 {
			t.Errorf("%s: the dialog: top %d, unread %d, mentions %d, reactions %d, read inbox %d, outbox %d", name,
				d.GetTopMessage(), d.GetUnreadCount(), d.GetUnreadMentionsCount(), d.GetUnreadReactionsCount(), d.GetReadInboxMaxId(), d.GetReadOutboxMaxId())
		}

		history := historyMessages(ctx, t, u, peer)
		if len(history) != 13 {
			t.Fatalf("%s: %d messages in the history", name, len(history))
		}
		m := map[int32]*mtproto.Message{}
		for _, x := range history {
			if m[x.GetId()] != nil {
				t.Fatalf("%s: message %d twice", name, x.GetId())
			}
			m[x.GetId()] = x
		}
		// The authors, the dates and whose message each is.
		authors := map[int32]string{1: "alice", 2: "alice", 3: "bob", 4: "alice", 5: "bob", 6: "bob", 7: "alice", 8: "bob", 9: "alice", 10: "alice", 11: "carol", 12: "bob", 13: "alice"}
		for id := int32(1); id <= 13; id++ {
			x := m[id]
			if x == nil {
				t.Fatalf("%s: no message %d", name, id)
			}
			if x.GetFromId().GetUserId() != ids[authors[id]] || x.GetOut() != (authors[id] == name) || x.GetMediaUnread() {
				t.Errorf("%s: message %d is from %d, out %v, media_unread %v", name, id, x.GetFromId().GetUserId(), x.GetOut(), x.GetMediaUnread())
			}
			dated := int32(importT0 + 60*(id-1))
			switch {
			case id == 8 || id == 9:
				dated = int32(importT0 + 60*(id+1))
			case id >= 10 && id <= 12:
				dated = int32(importT0 + 60*(id+3))
			}
			if id <= 12 && x.GetDate() != dated {
				t.Errorf("%s: message %d is dated %d, want %d", name, id, x.GetDate(), dated)
			}
		}
		if now := int32(time.Now().Unix()); m[13].GetDate() < now-600 || m[13].GetDate() > now+60 {
			t.Errorf("%s: the members were added at %d", name, m[13].GetDate())
		}
		if a := m[1].GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionChannelCreate {
			t.Errorf("%s: message 1: %v", name, a)
		}
		if m[2].GetMessage() != "import first" || m[2].GetReplyTo() != nil {
			t.Errorf("%s: message 2: %v", name, m[2])
		}

		// The photo with its caption: its sizes are pictures.
		photo := m[3].GetMedia().GetPhoto_FLAGPHOTO()
		if m[3].GetMessage() != "import photo caption" || photo.GetId() == 0 || len(photo.GetSizes()) == 0 {
			t.Fatalf("%s: the photo: %v", name, m[3])
		}
		for _, size := range photo.GetSizes() {
			if size.GetPredicateName() != mtproto.Predicate_photoSize {
				continue
			}
			o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
				Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
				Location: mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{
					Id: photo.GetId(), AccessHash: photo.GetAccessHash(), FileReference: photo.GetFileReference(), ThumbSize: size.GetType(),
				}).To_InputFileLocation(),
				Limit: 1 << 20,
			})
			if err != nil {
				t.Fatalf("%s: upload.getFile of the photo's size %s: %v", name, size.GetType(), err)
			}
			b := o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes()
			if _, err = jpeg.Decode(bytes.NewReader(b)); err != nil {
				t.Errorf("%s: the photo's size %s: %d bytes, not a JPEG: %v", name, size.GetType(), len(b), err)
			}
		}

		// The voice message: a reply, with its waveform, played; the file
		// is the export's.
		voice := m[4].GetMedia().GetDocument()
		var waveform []byte
		for _, a := range voice.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeAudio && a.GetVoice() {
				waveform = a.GetWaveform()
			}
		}
		if !mtproto.IsVoiceMessage(m[4]) || m[4].GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != 2 || len(waveform) != 63 || voice.GetSize2_INT64() != int64(len(voiceFile)) {
			t.Errorf("%s: the voice message: reply %v, %d bytes of waveform, %d bytes", name, m[4].GetReplyTo(), len(waveform), voice.GetSize2_INT64())
		}
		if got := downloadDocument(ctx, t, u, voice); !bytes.Equal(got, voiceFile) {
			t.Errorf("%s: the voice message downloads as %d bytes, not the %d of the export", name, len(got), len(voiceFile))
		}
		// The round video message.
		round := m[5].GetMedia().GetDocument()
		if !mtproto.IsRoundVideoMessage(m[5]) || round.GetMimeType() != "video/mp4" || round.GetSize2_INT64() != int64(len(roundFile)) {
			t.Errorf("%s: the round video message: %v", name, round)
		}
		if got := downloadDocument(ctx, t, u, round); !bytes.Equal(got, roundFile) {
			t.Errorf("%s: the round video message downloads as %d bytes", name, len(got))
		}
		// The forward, the edit.
		if f := m[6].GetFwdFrom(); f.GetFromId().GetUserId() != ids["alice"] || m[6].GetMessage() != "import forwarded" {
			t.Errorf("%s: the forward: %v", name, f)
		}
		if m[7].GetEditDate().GetValue() != importT0+8*60 || m[7].GetMessage() != "import edited" {
			t.Errorf("%s: the edited message: edit date %v", name, m[7].GetEditDate())
		}
		// The poll, with the vote of the one who made the export.
		poll := m[8].GetMedia()
		if poll.GetPredicateName() != mtproto.Predicate_messageMediaPoll || len(poll.GetPoll().GetAnswers()) != 2 || poll.GetPoll().GetClosed() || poll.GetResults().GetTotalVoters().GetValue() != 1 {
			t.Errorf("%s: the poll: %v", name, poll)
		}
		if name == "alice" {
			chosen := 0
			for _, r := range poll.GetResults().GetResults() {
				if r.GetChosen() {
					chosen++
				}
			}
			if chosen != 1 {
				t.Errorf("the exporter's vote is not in the poll: %v", poll.GetResults())
			}
		}
		// The reactions the export names, of Bob and Carol; the three
		// hearts it only counts are nobody's and are not there.
		reactions := m[9].GetReactions()
		if len(reactions.GetResults()) != 1 || reactions.GetResults()[0].GetReaction_REACTION().GetEmoticon() != "\U0001F44D" || reactions.GetResults()[0].GetCount() != 2 {
			t.Errorf("%s: the reactions: %v", name, reactions)
		}
		reactors := map[int64]bool{}
		for _, r := range reactions.GetRecentReactions() {
			reactors[r.GetPeerId().GetUserId()] = true
			if r.GetUnread() {
				t.Errorf("%s: an imported reaction is unread", name)
			}
		}
		if len(reactors) != 2 || !reactors[ids["bob"]] || !reactors[ids["carol"]] {
			t.Errorf("%s: the reactions are of %v", name, reactors)
		}
		// The service message of the export, by the names it gives.
		if a := m[10].GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionChatAddUser || len(a.GetUsers()) != 1 || a.GetUsers()[0] != ids["bob"] {
			t.Errorf("%s: the member added in the export: %v", name, a)
		}
		// The reply to Carol with a mention of Alice by name.
		if m[11].GetMessage() != "import from carol" || m[12].GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != 11 || len(m[12].GetEntities()) != 1 ||
			m[12].GetEntities()[0].GetPredicateName() != mtproto.Predicate_messageEntityMentionName || m[12].GetEntities()[0].GetUserId_INT64() != ids["alice"] {
			t.Errorf("%s: the reply with a mention: %v", name, m[12])
		}
		if a := m[13].GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionChatAddUser || len(a.GetUsers()) != 2 {
			t.Errorf("%s: the members added after the import: %v", name, a)
		}
	}

	// A message after the import is an ordinary one: pushed, and unread.
	chAlice, d := importedChannel(ctx, t, alice, title)
	if d.GetPeer().GetChannelId() != channelId {
		t.Fatalf("the channel of Alice is %d, of the others %d", d.GetPeer().GetChannelId(), channelId)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: channelId, AccessHash: chAlice.GetAccessHash_FLAGINT64().GetValue()},
		Message:     "after the import",
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMessage after the import: %v", err)
	}
	pushed, _ := pushedChannelMessage(ctx, t, bob, channelId, "after the import")
	if pushed.GetId() != 14 {
		t.Errorf("the message after the import has id %d", pushed.GetId())
	}
	chBob, _ := importedChannel(ctx, t, bob, title)
	peerBob := &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: channelId, AccessHash: chBob.GetAccessHash_FLAGINT64().GetValue()}
	if d = peerDialogs(ctx, t, bob, peerBob).GetDialogs()[0]; d.GetUnreadCount() != 1 || d.GetTopMessage() != 14 {
		t.Errorf("Bob's dialog after a new message: unread %d, top %d", d.GetUnreadCount(), d.GetTopMessage())
	}
}
