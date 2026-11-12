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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Voice transcription (task 090, ADR 0056) through the whole stack, with
// test/e2e/fakestt as the speech engine. The engine has one plan for
// everybody, so these tests do not run in parallel, with each other or
// with the rest.

// Set by test/e2e/run.sh: host:port of fakestt.
var fakeSTTAddr = os.Getenv("PLUMA_E2E_FAKESTT")

// sttModel is STT_MODEL of the stand (run.sh).
const sttModel = "e2e-model"

type sttPlan struct {
	Text    string `json:"text"`
	DelayMs int    `json:"delay_ms"`
	Fail    int    `json:"fail"`
	Status  int    `json:"status"`
	Hang    bool   `json:"hang"`
}

type sttSeen struct {
	Calls    int     `json:"calls"`
	Seconds  float64 `json:"seconds"`
	Model    string  `json:"model"`
	Language string  `json:"language"`
}

// planSTT sets what the engine answers from now on and zeroes its counter.
func planSTT(t *testing.T, p sttPlan) {
	t.Helper()
	if fakeSTTAddr == "" {
		t.Skip("PLUMA_E2E_FAKESTT is not set: the stand has no speech engine")
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post("http://"+fakeSTTAddr+"/e2e/plan", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("fakestt: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fakestt: plan: status %d", resp.StatusCode)
	}
}

// seenSTT is what the engine was given since the last plan.
func seenSTT(t *testing.T) sttSeen {
	t.Helper()
	resp, err := http.Get("http://" + fakeSTTAddr + "/e2e/calls")
	if err != nil {
		t.Fatalf("fakestt: %v", err)
	}
	defer resp.Body.Close()
	var s sttSeen
	if err = json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("fakestt: calls: %v", err)
	}
	return s
}

func record(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// voiceMedia uploads data as a voice message that says it is seconds long:
// Ogg/Opus, as the clients record it.
func voiceMedia(ctx context.Context, t *testing.T, u *user, data []byte, seconds int32) *mtproto.InputMedia {
	t.Helper()
	return mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
		File:     uploadFile(ctx, t, u, data, "voice.ogg"),
		MimeType: "audio/ogg",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration: float64(seconds), Duration_INT32: seconds}).To_DocumentAttribute(),
		},
	}).To_InputMedia()
}

// roundMedia uploads data as a round video: MP4 with the sound next to the
// picture.
func roundMedia(ctx context.Context, t *testing.T, u *user, data []byte) *mtproto.InputMedia {
	t.Helper()
	return mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
		File:     uploadFile(ctx, t, u, data, "round.mp4"),
		MimeType: "video/mp4",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{RoundMessage: true, W: 240, H: 240, Duration: 2, Duration_FLOAT64: 2, Duration_INT32: 2}).To_DocumentAttribute(),
		},
	}).To_InputMedia()
}

// uploadPart is the size of a part of an upload: the largest the server
// takes.
const uploadPart = 512 << 10

func uploadFile(ctx context.Context, t *testing.T, u *user, data []byte, name string) *mtproto.InputFile {
	t.Helper()
	fileId := randomID()
	parts := int32(0)
	for from := 0; from < len(data); from += uploadPart {
		if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_saveFilePart),
			FileId:      fileId,
			FilePart:    parts,
			Bytes:       data[from:min(from+uploadPart, len(data))],
		}); err != nil {
			t.Fatalf("upload.saveFilePart %d: %v", parts, err)
		}
		parts++
	}
	sum := md5.Sum(data)
	return mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileId, Parts: parts, Name: name, Md5Checksum: hex.EncodeToString(sum[:])}).To_InputFile()
}

// sendMedia sends media of u to peer and returns the id of the message.
func sendMedia(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, media *mtproto.InputMedia) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peer,
		Media:       media,
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	return sentID(t, o)
}

// sentID is the id of the message a send or a forward answered with.
func sentID(t *testing.T, o mtproto.TLObject) int32 {
	t.Helper()
	up, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("the send returned %T", o)
	}
	for _, x := range up.To_Updates().GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage, mtproto.Predicate_updateNewChannelMessage:
			return x.GetMessage_MESSAGE().GetId()
		}
	}
	t.Fatalf("the send answered without the message: %v", up.To_Updates().GetUpdates())
	return 0
}

// incomingRecord waits until u has a voice message or a round video in the
// dialog with peer, and returns its id: the copy of the receiver.
func incomingRecord(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer,
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			if mtproto.IsVoiceMessage(m) || mtproto.IsRoundVideoMessage(m) {
				return m.GetId()
			}
		}
	}
	t.Fatal("the record did not reach the receiver")
	return 0
}

func transcribe(ctx context.Context, u *user, peer *mtproto.InputPeer, id int32) (*mtproto.Messages_TranscribedAudio, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesTranscribeAudio{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_transcribeAudio),
		Peer:        peer,
		MsgId:       id,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface {
		To_Messages_TranscribedAudio() *mtproto.Messages_TranscribedAudio
	}).To_Messages_TranscribedAudio(), nil
}

// transcribed checks a final answer with text and returns its id.
func transcribed(t *testing.T, r *mtproto.Messages_TranscribedAudio, err error, text string) int64 {
	t.Helper()
	if err != nil {
		t.Fatalf("messages.transcribeAudio: %v", err)
	}
	if r.GetPending() || r.GetText() != text || r.GetTranscriptionId() == 0 {
		t.Fatalf("messages.transcribeAudio: %v, want the final text %q", r, text)
	}
	noTrial(t, r)
	return r.GetTranscriptionId()
}

// stillPending checks a pending answer and returns its id.
func stillPending(t *testing.T, r *mtproto.Messages_TranscribedAudio, err error) int64 {
	t.Helper()
	if err != nil {
		t.Fatalf("messages.transcribeAudio: %v", err)
	}
	if !r.GetPending() || r.GetText() != "" || r.GetTranscriptionId() == 0 {
		t.Fatalf("messages.transcribeAudio: %v, want pending without a text", r)
	}
	noTrial(t, r)
	return r.GetTranscriptionId()
}

// noTrial: flags.1 (trial_remains_*) locks the button on iOS until the date
// it names, and Premium is everyone's.
func noTrial(t *testing.T, r *mtproto.Messages_TranscribedAudio) {
	t.Helper()
	if r.GetTrialRemainsNum() != nil || r.GetTrialRemainsUntilDate() != nil {
		t.Fatalf("trial fields in %v", r)
	}
}

// refused checks that the answer is the error want, and never a flood wait.
func refused(t *testing.T, what string, err error, want string) {
	t.Helper()
	if mtclient.IsRPCError(err, "FLOOD_WAIT_") || mtclient.IsRPCError(err, "PREMIUM_ACCOUNT_REQUIRED") {
		t.Fatalf("%s: %v: an answer that locks the button", what, err)
	}
	if !mtclient.IsRPCError(err, want) {
		t.Fatalf("%s: got %v, want %s", what, err, want)
	}
}

// finalUpdate waits on u for the updateTranscribedAudio of id and returns it.
func finalUpdate(ctx context.Context, t *testing.T, u *user, id int64) *mtproto.Update {
	t.Helper()
	var got *mtproto.Update
	_, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateTranscribedAudio && x.GetTranscriptionId() == id {
				got = x
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("no updateTranscribedAudio of %d: %v", id, err)
	}
	if got.GetPending() {
		t.Fatalf("a pending update: %v", got)
	}
	return got
}

// noUpdate checks that u gets no updateTranscribedAudio within wait.
func noUpdate(ctx context.Context, t *testing.T, u *user, wait time.Duration, why string) {
	t.Helper()
	cctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if us, err := u.c.WaitUpdate(cctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateTranscribedAudio {
				return true
			}
		}
		return false
	}); err == nil {
		t.Fatalf("%s: %v", why, us.GetUpdates())
	}
}

func rate(ctx context.Context, u *user, peer *mtproto.InputPeer, msg int32, id int64, good bool) error {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesRateTranscribedAudio{
		Constructor:     u.c.Constructor(mtproto.Predicate_messages_rateTranscribedAudio),
		Peer:            peer,
		MsgId:           msg,
		TranscriptionId: id,
		Good:            mtproto.ToBool(good),
	})
	if err != nil {
		return err
	}
	if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
		return &mtclient.RPCError{Code: 0, Message: "not boolTrue"}
	}
	return nil
}

// TestTranscribeVoice: the text of a voice message comes in the answer; the
// record is recognized once for its sender, its receiver and a forward,
// each of whom has an id of its own; a rating is taken from the reader the
// id belongs to.
func TestTranscribeVoice(t *testing.T) {
	const text = "Привет, это голосовое сообщение."
	planSTT(t, sttPlan{Text: "  " + text + "\n"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice, bob, toBob := twoUsers(ctx, t)
	toAlice := peerOf(ctx, t, bob, alice)

	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, record(t, "voice.ogg"), 2))
	r, err := transcribe(ctx, alice, toBob, sent)
	id := transcribed(t, r, err, text)

	// The engine got the Ogg/Opus record as a WAV of its two seconds, with
	// the model of the configuration.
	seen := seenSTT(t)
	if seen.Calls != 1 || seen.Model != sttModel || seen.Seconds < 1.9 || seen.Seconds > 2.2 {
		t.Fatalf("the engine saw %+v", seen)
	}

	// Asked again: the same id, the stored text.
	r, err = transcribe(ctx, alice, toBob, sent)
	if again := transcribed(t, r, err, text); again != id {
		t.Errorf("the second answer has the id %d, the first %d", again, id)
	}
	// The receiver's copy: the text at once, an id of its own.
	got := incomingRecord(ctx, t, bob, toAlice)
	r, err = transcribe(ctx, bob, toAlice, got)
	bobId := transcribed(t, r, err, text)
	// A forward to Saved Messages: the same record, another message.
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    toBob,
		Id:          []int32{sent},
		RandomId:    []int64{randomID()},
		ToPeer:      &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
	})
	if err != nil {
		t.Fatalf("messages.forwardMessages: %v", err)
	}
	saved := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	r, err = transcribe(ctx, alice, saved, sentID(t, o))
	forwardId := transcribed(t, r, err, text)
	if bobId == id || forwardId == id || forwardId == bobId {
		t.Errorf("ids %d (sender), %d (receiver), %d (forward): tdesktop finds a message by the id alone", id, bobId, forwardId)
	}
	if n := seenSTT(t).Calls; n != 1 {
		t.Fatalf("%d recognitions of one record", n)
	}

	// Ratings: with the reader's own id, for the message it was given for.
	if err = rate(ctx, alice, toBob, sent, id, true); err != nil {
		t.Fatalf("messages.rateTranscribedAudio: %v", err)
	}
	if err = rate(ctx, alice, toBob, sent, id, false); err != nil {
		t.Fatalf("messages.rateTranscribedAudio, a changed mind: %v", err)
	}
	refused(t, "a rating with somebody else's id", rate(ctx, bob, toAlice, got, id, true), "MSG_ID_INVALID")
	refused(t, "a rating of another message", rate(ctx, alice, saved, sent, id, true), "MSG_ID_INVALID")
}

// TestTranscribeRoundVideo: the sound of a round video (MP4) reaches the
// engine as the same WAV.
func TestTranscribeRoundVideo(t *testing.T) {
	const text = "Это кружок."
	planSTT(t, sttPlan{Text: text})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice, _, toBob := twoUsers(ctx, t)
	sent := sendMedia(ctx, t, alice, toBob, roundMedia(ctx, t, alice, record(t, "round.mp4")))
	r, err := transcribe(ctx, alice, toBob, sent)
	transcribed(t, r, err, text)
	if seen := seenSTT(t); seen.Calls != 1 || seen.Seconds < 1.9 || seen.Seconds > 2.2 {
		t.Fatalf("the engine saw %+v", seen)
	}
}

// TestTranscribeLongRecord: a record of half an hour, which its client
// declares a minute long, is taken (no length is refused) and reaches the
// engine whole: bff times it by the decoded length, and the download,
// ffmpeg and the WAV on its way to the engine stay within the memory of its
// container; run.sh fails the run if the container was killed at its limit.
func TestTranscribeLongRecord(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg makes the record of this test: %v", err)
	}
	const (
		text     = "Длинная запись."
		seconds  = 1800
		declared = 60
	)
	planSTT(t, sttPlan{Text: text})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	path := t.TempDir() + "/long.ogg"
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1800",
		"-ac", "1", "-c:a", "libopus", "-b:a", "16k", path).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	long, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	alice, _, toBob := twoUsers(ctx, t)
	getState(ctx, t, alice)
	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, long, declared))
	r, err := transcribe(ctx, alice, toBob, sent)
	if err != nil {
		t.Fatalf("messages.transcribeAudio: %v", err)
	}
	if r.GetPending() {
		// Reading and decoding half an hour may take longer than the wait.
		if u := finalUpdate(ctx, t, alice, r.GetTranscriptionId()); u.GetText() != text {
			t.Fatalf("the update %v", u)
		}
	} else {
		transcribed(t, r, err, text)
	}
	if seen := seenSTT(t); seen.Calls != 1 || seen.Seconds < seconds-2 || seen.Seconds > seconds+2 {
		t.Fatalf("the engine saw %+v, want one record of about %d s", seen, seconds)
	}
}

// TestTranscribeInChats: a group, a supergroup and a channel. A subscriber
// reads the text of a post; somebody who cannot read the channel is told
// there is no such peer.
func TestTranscribeInChats(t *testing.T) {
	const text = "Запись в чате."
	planSTT(t, sttPlan{Text: text})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	voice := record(t, "voice.ogg")

	group := createChat(ctx, t, alice, "voices", bob)
	sent := sendMedia(ctx, t, alice, chatPeer(alice, group), voiceMedia(ctx, t, alice, voice, 2))
	r, err := transcribe(ctx, alice, chatPeer(alice, group), sent)
	transcribed(t, r, err, text)
	r, err = transcribe(ctx, bob, chatPeer(bob, group), incomingRecord(ctx, t, bob, chatPeer(bob, group)))
	transcribed(t, r, err, text)

	for _, megagroup := range []bool{true, false} {
		_, ch := createChannel(ctx, t, alice, "voices", megagroup)
		hash := ch.GetAccessHash_FLAGINT64().GetValue()
		peer := func(u *user) *mtproto.InputPeer {
			return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
		}
		post := sendMedia(ctx, t, alice, peer(alice), voiceMedia(ctx, t, alice, voice, 2))
		r, err = transcribe(ctx, alice, peer(alice), post)
		transcribed(t, r, err, text)

		invite := channelInviteHash(ctx, t, alice, &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash})
		if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        invite,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
		// In a channel a message has one id for everybody; the reader's id
		// of the transcription is still its own.
		r, err = transcribe(ctx, bob, peer(bob), post)
		transcribed(t, r, err, text)
		_, err = transcribe(ctx, carol, peer(carol), post)
		refused(t, "a channel the caller is not in", err, "PEER_ID_INVALID")
		_, err = transcribe(ctx, bob, &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash + 1}, post)
		refused(t, "a wrong access hash", err, "PEER_ID_INVALID")
	}
	// Three records: the group's, the supergroup's, the channel's.
	if n := seenSTT(t).Calls; n != 3 {
		t.Fatalf("%d recognitions of three records", n)
	}
}

// TestTranscribePendingThenUpdate: a record that takes longer than the
// request waits is answered "pending" without a text, and then exactly one
// updateTranscribedAudio brings the text, to every device of the reader,
// with the id of the answer and the dialog as the reader sees it. The
// receiver asks at the same time: one recognition, an id and an update of
// its own.
func TestTranscribePendingThenUpdate(t *testing.T) {
	const text = "Долгая запись."
	planSTT(t, sttPlan{Text: text, DelayMs: 6000})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)
	// A session is pushed updates once it has asked for the state.
	getState(ctx, t, alice)
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, record(t, "voice.ogg"), 2))
	got := incomingRecord(ctx, t, bob, toAlice)
	type answer struct {
		r   *mtproto.Messages_TranscribedAudio
		err error
	}
	bobs := make(chan answer, 1)
	go func() {
		r, err := transcribe(ctx, bob, toAlice, got)
		bobs <- answer{r, err}
	}()
	start := time.Now()
	r, err := transcribe(ctx, alice, toBob, sent)
	id := stillPending(t, r, err)
	b := <-bobs
	bobId := stillPending(t, b.r, b.err)
	if bobId == id {
		t.Fatalf("the sender and the receiver share the id %d: tdesktop finds a message by the id alone", id)
	}
	if waited := time.Since(start); waited < 2*time.Second || waited > 5500*time.Millisecond {
		t.Errorf("the pending answer came after %s, want about 3s", waited)
	}
	// The update comes after the answer, never with it: tdesktop learns
	// the id from the answer and drops an update with an id it does not know.
	noUpdate(ctx, t, alice, 500*time.Millisecond, "an update with the pending answer")

	u := finalUpdate(ctx, t, alice, id)
	if u.GetText() != text || u.GetMsgId_INT32() != sent || u.GetPeer_PEER().GetPredicateName() != mtproto.Predicate_peerUser || u.GetPeer_PEER().GetUserId() != bob.self.GetId() {
		t.Fatalf("the update %v, want message %d of the dialog with user %d", u, sent, bob.self.GetId())
	}
	if u = finalUpdate(ctx, t, laptop, id); u.GetText() != text || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update of the other device: %v", u)
	}
	if u = finalUpdate(ctx, t, bob, bobId); u.GetText() != text || u.GetMsgId_INT32() != got || u.GetPeer_PEER().GetUserId() != alice.self.GetId() {
		t.Fatalf("the update of the receiver %v, want message %d of the dialog with user %d", u, got, alice.self.GetId())
	}
	// Exactly one.
	noUpdate(ctx, t, alice, 5*time.Second, "a second update")
	if n := seenSTT(t).Calls; n != 1 {
		t.Fatalf("%d recognitions", n)
	}
	// From now on the text is in the answer, under the same id.
	r, err = transcribe(ctx, alice, toBob, sent)
	if again := transcribed(t, r, err, text); again != id {
		t.Errorf("id %d, then %d", id, again)
	}
}

// TestTranscribeEngineFailures: a sound the engine cannot read is an error
// in the answer; an engine that cannot be used (404: the configuration)
// is waited for, without an attempt spent; an engine that fails is tried
// again before anybody is told, and when it keeps failing the reader who
// was told "pending" gets the final update with an empty text. Asked
// again, such a record is "pending" again, never an error that Android
// would keep for good, and is tried once FailedTTL has passed.
func TestTranscribeEngineFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	alice, _, toBob := twoUsers(ctx, t)
	getState(ctx, t, alice)
	voice := record(t, "voice.ogg")

	// 400: the record itself. Final at once, never tried again.
	planSTT(t, sttPlan{Fail: -1, Status: http.StatusBadRequest})
	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, voice, 2))
	_, err := transcribe(ctx, alice, toBob, sent)
	refused(t, "a record the engine cannot read", err, "TRANSCRIPTION_FAILED")
	_, err = transcribe(ctx, alice, toBob, sent)
	refused(t, "the same record again", err, "TRANSCRIPTION_FAILED")
	if n := seenSTT(t).Calls; n != 1 {
		t.Fatalf("%d requests for a record that cannot be read", n)
	}

	// 404 three times, more than the attempts of the stand (two), then the
	// text: an engine that refuses the configuration is waited for, and the
	// reader gets the text when it is fixed. After the first refusal it is
	// asked with a sound of no samples until it takes one (two more
	// refusals, then the fourth request), and only then sent the record.
	planSTT(t, sttPlan{Text: "Движок вернулся.", Fail: 3, Status: http.StatusNotFound})
	sent = sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, voice, 2))
	r, err := transcribe(ctx, alice, toBob, sent)
	id := stillPending(t, r, err)
	if u := finalUpdate(ctx, t, alice, id); u.GetText() != "Движок вернулся." || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update %v", u)
	}
	if seen := seenSTT(t); seen.Calls != 5 || seen.Seconds < 1.9 || seen.Seconds > 2.2 {
		t.Fatalf("%d requests, the last of %.1f s of sound; want the record refused, two empty sounds refused, one taken, then the record", seen.Calls, seen.Seconds)
	}

	// 502 once, then the text: the reader waits through the pause and is
	// never told of the failure.
	planSTT(t, sttPlan{Text: "Со второй попытки.", Fail: 1, Status: http.StatusBadGateway})
	sent = sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, voice, 2))
	r, err = transcribe(ctx, alice, toBob, sent)
	id = stillPending(t, r, err)
	if u := finalUpdate(ctx, t, alice, id); u.GetText() != "Со второй попытки." || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update %v", u)
	}
	if n := seenSTT(t).Calls; n != 2 {
		t.Fatalf("%d requests, want the failed one and the second", n)
	}

	// 500 for good: after the attempts of the stand (two), one final
	// update without a text; a client that waits must be released.
	planSTT(t, sttPlan{Fail: -1, Status: http.StatusInternalServerError})
	sent = sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, voice, 2))
	r, err = transcribe(ctx, alice, toBob, sent)
	id = stillPending(t, r, err)
	if u := finalUpdate(ctx, t, alice, id); u.GetText() != "" || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update %v, want an empty final one", u)
	}
	if n := seenSTT(t).Calls; n != 2 {
		t.Fatalf("%d requests, want two attempts", n)
	}
	noUpdate(ctx, t, alice, 3*time.Second, "a second update")
	planSTT(t, sttPlan{Text: "После паузы."})
	r, err = transcribe(ctx, alice, toBob, sent)
	if again := stillPending(t, r, err); again != id {
		t.Fatalf("asked again: id %d, want %d", again, id)
	}
	if n := seenSTT(t).Calls; n != 0 {
		t.Fatalf("%d requests before FailedTTL passed", n)
	}
	if u := finalUpdate(ctx, t, alice, id); u.GetText() != "После паузы." || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update after FailedTTL %v", u)
	}
}

// TestTranscribeRefusals: what is not a voice message a reader can read is
// refused with the errors of the method, and the engine is never asked.
func TestTranscribeRefusals(t *testing.T) {
	planSTT(t, sttPlan{Text: "never"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice, bob, toBob := twoUsers(ctx, t)
	voice := record(t, "voice.ogg")

	text := sendMessageID(ctx, t, alice, toBob, "just a text", randomID())
	_, err := transcribe(ctx, alice, toBob, text)
	refused(t, "a text", err, "MSG_VOICE_MISSING")

	photo := sendMedia(ctx, t, alice, toBob, uploadPhoto(ctx, t, alice))
	_, err = transcribe(ctx, alice, toBob, photo)
	refused(t, "a photo", err, "MSG_VOICE_MISSING")

	music := sendMedia(ctx, t, alice, toBob, mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
		File:     uploadFile(ctx, t, alice, voice, "song.ogg"),
		MimeType: "audio/ogg",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Duration: 2, Duration_INT32: 2, Title: mtproto.MakeFlagsString("Song")}).To_DocumentAttribute(),
		},
	}).To_InputMedia())
	_, err = transcribe(ctx, alice, toBob, music)
	refused(t, "music", err, "MSG_VOICE_MISSING")

	// A voice message and a round video that play once are never kept as
	// text (the owner's decision of 2026-11-12); the clients do not offer
	// them either.
	onceVoice := voiceMedia(ctx, t, alice, voice, 2)
	onceVoice.TtlSeconds = wrapperspb.Int32(0x7fffffff)
	once := sendMedia(ctx, t, alice, toBob, onceVoice)
	_, err = transcribe(ctx, alice, toBob, once)
	refused(t, "a voice message that plays once", err, "MSG_VOICE_MISSING")
	// The receiver's copy too, which is the one that has not been played.
	toAlice := peerOf(ctx, t, bob, alice)
	_, err = transcribe(ctx, bob, toAlice, incomingRecord(ctx, t, bob, toAlice))
	refused(t, "the receiver's voice message that plays once", err, "MSG_VOICE_MISSING")
	onceRound := roundMedia(ctx, t, alice, record(t, "round.mp4"))
	onceRound.TtlSeconds = wrapperspb.Int32(0x7fffffff)
	once = sendMedia(ctx, t, alice, toBob, onceRound)
	_, err = transcribe(ctx, alice, toBob, once)
	refused(t, "a round video that plays once", err, "MSG_VOICE_MISSING")

	_, err = transcribe(ctx, alice, toBob, 1_000_000)
	refused(t, "no such message", err, "MSG_ID_INVALID")
	// A message of another dialog of the caller.
	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, voice, 2))
	_, err = transcribe(ctx, alice, &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}, sent)
	refused(t, "a message of another dialog", err, "MSG_ID_INVALID")
	_, err = transcribe(ctx, bob, &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)}, sent)
	refused(t, "no peer", err, "PEER_ID_INVALID")

	if n := seenSTT(t).Calls; n != 0 {
		t.Fatalf("the engine was asked %d times", n)
	}
}

// TestTranscribeAcrossBffRestart: bff stops while a record is with the
// engine. The job goes back to the queue, the new process takes it, and the
// reader who was told "pending" gets the one final update.
func TestTranscribeAcrossBffRestart(t *testing.T) {
	const text = "После перезапуска."
	planSTT(t, sttPlan{Text: text, DelayMs: 25000})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	alice, _, toBob := twoUsers(ctx, t)
	getState(ctx, t, alice)
	t.Cleanup(func() { restore(t) })

	sent := sendMedia(ctx, t, alice, toBob, voiceMedia(ctx, t, alice, record(t, "voice.ogg"), 2))
	r, err := transcribe(ctx, alice, toBob, sent)
	id := stillPending(t, r, err)

	stack(t, "restart", "bff")
	restore(t)
	// The engine answers at once from now on.
	planSTT(t, sttPlan{Text: text})
	waitServes(ctx, t, alice)

	if u := finalUpdate(ctx, t, alice, id); u.GetText() != text || u.GetMsgId_INT32() != sent {
		t.Fatalf("the update %v", u)
	}
	noUpdate(ctx, t, alice, 5*time.Second, "a second update")
	r, err = transcribe(ctx, alice, toBob, sent)
	if again := transcribed(t, r, err, text); again != id {
		t.Errorf("id %d, then %d", id, again)
	}
}

// fakeSTTPort asks compose for the current port of fakestt: a container
// that was recreated has a new one.
func fakeSTTPort() (string, error) {
	out, err := exec.Command(runScript, "compose", "port", "fakestt", "8080").Output()
	return strings.TrimSpace(string(out)), err
}
