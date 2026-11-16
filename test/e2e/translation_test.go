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
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The translation of messages (task 101, ADR 0064) through the whole stack,
// with test/e2e/faketranslate as the engine. Its "translation" is "[xx] "
// and the text in upper case, with the tags of the markup where they were,
// verbatim contents included; bff gives those back as they were. The engine
// has one plan for everybody, so these tests do not run in parallel, with
// each other or with the rest.

// Set by test/e2e/run.sh: host:port of faketranslate.
var fakeTranslateAddr = os.Getenv("PLUMA_E2E_FAKETRANSLATE")

type translatePlan struct {
	DelayMs  int  `json:"delay_ms"`
	Fail     int  `json:"fail"`
	Status   int  `json:"status"`
	DropTags bool `json:"drop_tags"`
	ExtraTag bool `json:"extra_tag"`
}

type translateSeen struct {
	Calls     int    `json:"calls"`
	Tagged    int    `json:"tagged"`
	Lang      string `json:"lang"`
	MaxTokens int    `json:"max_tokens"`
}

// planTranslate sets what the engine answers from now on and zeroes its
// counters.
func planTranslate(t *testing.T, p translatePlan) {
	t.Helper()
	if fakeTranslateAddr == "" {
		t.Skip("PLUMA_E2E_FAKETRANSLATE is not set: the stand has no translation engine")
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post("http://"+fakeTranslateAddr+"/e2e/plan", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("faketranslate: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("faketranslate: plan: status %d", resp.StatusCode)
	}
}

// seenTranslate is what the engine was asked since the last plan.
func seenTranslate(t *testing.T) translateSeen {
	t.Helper()
	resp, err := http.Get("http://" + fakeTranslateAddr + "/e2e/calls")
	if err != nil {
		t.Fatalf("faketranslate: %v", err)
	}
	defer resp.Body.Close()
	var s translateSeen
	if err = json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("faketranslate: calls: %v", err)
	}
	return s
}

// fakeTranslatePort asks compose for the current port of faketranslate.
func fakeTranslatePort() (string, error) {
	out, err := exec.Command(runScript, "compose", "port", "faketranslate", "8080").Output()
	return strings.TrimSpace(string(out)), err
}

func translatedOf(o mtproto.TLObject) []*mtproto.TextWithEntities {
	r, ok := o.(interface {
		To_Messages_TranslatedText() *mtproto.Messages_TranslatedText
	})
	if !ok {
		return nil
	}
	return r.To_Messages_TranslatedText().GetResult()
}

// translateIds asks the translation of the messages ids of peer.
func translateIds(ctx context.Context, u *user, peer *mtproto.InputPeer, ids []int32, lang string) ([]*mtproto.TextWithEntities, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesTranslateText{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_translateText),
		Peer:        peer,
		Id:          ids,
		ToLang:      lang,
	})
	if err != nil {
		return nil, err
	}
	return translatedOf(o), nil
}

// translateTexts asks the translation of texts, as the clients send a
// transcription, a poll or the translation screen.
func translateTexts(ctx context.Context, u *user, texts []*mtproto.TextWithEntities, lang string) ([]*mtproto.TextWithEntities, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesTranslateText{
		Constructor:                     u.c.Constructor(mtproto.Predicate_messages_translateText),
		Text_FLAGVECTORTEXTWITHENTITIES: texts,
		ToLang:                          lang,
	})
	if err != nil {
		return nil, err
	}
	return translatedOf(o), nil
}

func plainText(u *user, s string, ents ...*mtproto.MessageEntity) *mtproto.TextWithEntities {
	return &mtproto.TextWithEntities{Constructor: u.c.Constructor(mtproto.Predicate_textWithEntities), Text: s, Entities: ents}
}

// floodWait is X of FLOOD_WAIT_X.
func floodWait(err error) (time.Duration, bool) {
	var e *mtclient.RPCError
	if !errors.As(err, &e) {
		return 0, false
	}
	x, ok := strings.CutPrefix(e.Message, "FLOOD_WAIT_")
	if !ok {
		return 0, false
	}
	n, convErr := strconv.Atoi(x)
	if convErr != nil {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

// checkFloodWait checks that err is FLOOD_WAIT_X with X within the bounds of
// the translation (3 to 30 s: brief 101, "Пределы и ошибки") and returns X.
func checkFloodWait(t *testing.T, what string, err error) time.Duration {
	t.Helper()
	d, ok := floodWait(err)
	if !ok || d < 3*time.Second || d > 30*time.Second {
		t.Fatalf("%s: got %v, want FLOOD_WAIT_X with X from 3 to 30", what, err)
	}
	return d
}

// untilTranslated repeats call after each FLOOD_WAIT_X, as Android, tdesktop
// and Web K do, and returns its answer.
func untilTranslated(ctx context.Context, t *testing.T, what string, call func() ([]*mtproto.TextWithEntities, error)) []*mtproto.TextWithEntities {
	t.Helper()
	for {
		out, err := call()
		if err == nil {
			return out
		}
		d := checkFloodWait(t, what, err)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			t.Fatalf("%s: still %v when the test ran out of time", what, err)
		}
	}
}

// sendEntities sends text with ents from u to peer and returns the id of the
// message for u.
func sendEntities(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string, ents ...*mtproto.MessageEntity) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		Entities:    ents,
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	for _, up := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateMessageID {
			return up.GetId_INT32()
		}
	}
	t.Fatal("messages.sendMessage reply without updateMessageID")
	return 0
}

// incomingText waits until u has the message text in the dialog with peer
// and returns its id: the receiver's copy.
func incomingText(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) int32 {
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
			if m.GetMessage() == text {
				return m.GetId()
			}
		}
	}
	t.Fatalf("%q did not reach the receiver", text)
	return 0
}

// entityOf returns the one entity of r with predicate.
func entityOf(t *testing.T, what string, r *mtproto.TextWithEntities, predicate string) *mtproto.MessageEntity {
	t.Helper()
	var found *mtproto.MessageEntity
	for _, e := range r.GetEntities() {
		if e.GetPredicateName() != predicate {
			continue
		}
		if found != nil {
			t.Fatalf("%s: two %s in %v", what, predicate, r.GetEntities())
		}
		found = e
	}
	if found == nil {
		t.Fatalf("%s: no %s in %v", what, predicate, r.GetEntities())
	}
	return found
}

// covers checks that e covers sub, the first occurrence of it in text: an
// ASCII text, where a byte is a UTF-16 code unit.
func covers(t *testing.T, what string, e *mtproto.MessageEntity, text, sub string) {
	t.Helper()
	i := strings.Index(text, sub)
	if i < 0 || int(e.GetOffset()) != i || int(e.GetLength()) != len(sub) {
		t.Fatalf("%s: %s at %d+%d in %q, want %q at %d", what, e.GetPredicateName(), e.GetOffset(), e.GetLength(), text, sub, i)
	}
}

// uniq makes the texts of a run differ from those of another, whose
// translations are in the cache already.
func uniq() string { return strconv.FormatInt(randomID()&0xffffffff, 10) }

// TestTranslateMessages: B translates three messages A wrote him, by their
// ids in B's box, as the translation bar asks: one with a bold word, a link,
// a mention and code, a photo without a caption, a plain one. The answer
// has an item for each id in its order, the photo's empty; the entities are
// where the translation put their words, the verbatim ones (the mention,
// the code) as they were.
func TestTranslateMessages(t *testing.T) {
	planTranslate(t, translatePlan{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice, bob, toBob := twoUsers(ctx, t)
	toAlice := peerOf(ctx, t, bob, alice)
	n := uniq()

	rich := "Read the docs, Bob, and run make test now " + n
	sendEntities(ctx, t, alice, toBob, rich,
		mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity(),
		mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: 5, Length: 8, Url: "https://pluma.chat/docs"}).To_MessageEntity(),
		&mtproto.MessageEntity{
			Constructor:      alice.c.Constructor(mtproto.Predicate_inputMessageEntityMentionName),
			Offset:           15,
			Length:           3,
			UserId_INPUTUSER: inputUserOf(ctx, t, alice, bob),
		},
		mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: int32(strings.Index(rich, "make test")), Length: 9}).To_MessageEntity(),
	)
	sendPhoto(ctx, t, alice, toBob)
	plain := "Good morning, friend " + n
	sendEntities(ctx, t, alice, toBob, plain)

	ids := []int32{
		incomingText(ctx, t, bob, toAlice, rich),
		mediaMessageIn(ctx, t, bob, toAlice, mtproto.Predicate_messageMediaPhoto),
		incomingText(ctx, t, bob, toAlice, plain),
	}
	out, err := translateIds(ctx, bob, toAlice, ids, "ru")
	if err != nil {
		t.Fatalf("messages.translateText: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("%d items for 3 ids: %v", len(out), out)
	}

	want := "[ru] READ THE DOCS, Bob, AND RUN make test NOW " + n
	if out[0].GetText() != want || len(out[0].GetEntities()) != 4 {
		t.Fatalf("the first item %q %v, want %q with 4 entities", out[0].GetText(), out[0].GetEntities(), want)
	}
	covers(t, "bold", entityOf(t, "the first item", out[0], mtproto.Predicate_messageEntityBold), want, "READ")
	link := entityOf(t, "the first item", out[0], mtproto.Predicate_messageEntityTextUrl)
	covers(t, "link", link, want, "THE DOCS")
	if link.GetUrl() != "https://pluma.chat/docs" {
		t.Errorf("the link goes to %q", link.GetUrl())
	}
	mention := entityOf(t, "the first item", out[0], mtproto.Predicate_messageEntityMentionName)
	covers(t, "mention", mention, want, "Bob")
	if mention.GetUserId_INT64() != bob.self.GetId() {
		t.Errorf("the mention names %d, want %d", mention.GetUserId_INT64(), bob.self.GetId())
	}
	covers(t, "code", entityOf(t, "the first item", out[0], mtproto.Predicate_messageEntityCode), want, "make test")

	if out[1].GetText() != "" || len(out[1].GetEntities()) != 0 {
		t.Errorf("the photo without a caption: %q %v, want an empty item", out[1].GetText(), out[1].GetEntities())
	}
	if want = "[ru] GOOD MORNING, FRIEND " + n; out[2].GetText() != want || len(out[2].GetEntities()) != 0 {
		t.Errorf("the plain message: %q %v, want %q", out[2].GetText(), out[2].GetEntities(), want)
	}
	// The photo is not sent to the engine; the formatted message goes with
	// its tags.
	if seen := seenTranslate(t); seen.Calls != 2 || seen.Tagged != 1 || seen.Lang != "ru" || seen.MaxTokens <= 0 {
		t.Fatalf("the engine saw %+v", seen)
	}

	// Asked again: from the cache.
	again, err := translateIds(ctx, bob, toAlice, ids, "ru")
	if err != nil || len(again) != 3 || again[0].GetText() != out[0].GetText() || again[2].GetText() != out[2].GetText() {
		t.Fatalf("again: %v, %v", again, err)
	}
	if seen := seenTranslate(t); seen.Calls != 2 {
		t.Fatalf("the engine was asked %d times for two texts", seen.Calls)
	}
}

// TestTranslateTextBatch: texts as a client sends them, twenty with an empty
// one among them, into a language with a region and into Traditional
// Chinese; a language the model does not have, an item too long, too many
// items, a request without texts or ids.
func TestTranslateTextBatch(t *testing.T) {
	planTranslate(t, translatePlan{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Reader")
	n := uniq()

	const empty = 7
	items := make([]*mtproto.TextWithEntities, 20)
	for i := range items {
		s := fmt.Sprintf("line %d of batch %s", i, n)
		if i == empty {
			s = ""
		}
		items[i] = plainText(u, s)
	}
	for lang, code := range map[string]string{"pt-BR": "pt", "zh-TW": "zh-Hant"} {
		out, err := translateTexts(ctx, u, items, lang)
		if err != nil {
			t.Fatalf("%s: messages.translateText: %v", lang, err)
		}
		if len(out) != len(items) {
			t.Fatalf("%s: %d items for %d", lang, len(out), len(items))
		}
		for i, r := range out {
			want := fmt.Sprintf("[%s] LINE %d OF BATCH %s", code, i, n)
			if i == empty {
				want = ""
			}
			if r.GetText() != want {
				t.Errorf("%s: item %d is %q, want %q", lang, i, r.GetText(), want)
			}
		}
	}
	if seen := seenTranslate(t); seen.Calls != 38 {
		t.Fatalf("the engine was asked %d times for 2×19 texts", seen.Calls)
	}

	// The longest message is taken whole.
	long := strings.Repeat("abc ", 1024)
	out, err := translateTexts(ctx, u, []*mtproto.TextWithEntities{plainText(u, long[:4096])}, "en")
	if err != nil || len(out) != 1 || !strings.HasPrefix(out[0].GetText(), "[en] ABC ABC") {
		t.Fatalf("4096 characters: %v, %v", out, err)
	}

	for _, c := range []struct {
		what string
		err  func() error
		want string
	}{
		{"a language the model does not have", func() error {
			_, err := translateTexts(ctx, u, items[:1], "xx")
			return err
		}, "TO_LANG_INVALID"},
		{"Norwegian, which the model does not have", func() error {
			_, err := translateTexts(ctx, u, items[:1], "no")
			return err
		}, "TO_LANG_INVALID"},
		{"4097 characters", func() error {
			_, err := translateTexts(ctx, u, []*mtproto.TextWithEntities{plainText(u, long[:4096]+"a")}, "en")
			return err
		}, "INPUT_TEXT_TOO_LONG"},
		{"101 items", func() error {
			many := make([]*mtproto.TextWithEntities, 101)
			for i := range many {
				many[i] = plainText(u, "x")
			}
			_, err := translateTexts(ctx, u, many, "en")
			return err
		}, "INPUT_TEXT_TOO_LONG"},
		{"no texts and no ids", func() error {
			_, err := u.c.Invoke(ctx, &mtproto.TLMessagesTranslateText{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_translateText),
				ToLang:      "en",
			})
			return err
		}, "INPUT_TEXT_EMPTY"},
		{"a peer without ids", func() error {
			// An empty vector, not nil: peer and id share flags.0, and the
			// codec writes the vector only when it is not nil, so a nil one
			// would make a request no client sends and the server cannot
			// decode.
			_, err := translateIds(ctx, u, &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}, []int32{}, "en")
			return err
		}, "MSG_ID_INVALID"},
	} {
		if err := c.err(); !mtclient.IsRPCError(err, c.want) {
			t.Errorf("%s: got %v, want %s", c.what, err, c.want)
		}
	}
	if seen := seenTranslate(t); seen.Calls != 39 {
		t.Fatalf("the refused requests reached the engine: %d calls", seen.Calls)
	}
}

// TestTranslateChannelCache: a post of a channel is translated once for all
// its readers, whatever spelling of the language each sends; an edited post
// is another text and is translated again. Somebody who cannot read the
// channel is told there is no such peer.
func TestTranslateChannelCache(t *testing.T) {
	planTranslate(t, translatePlan{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")
	n := uniq()

	_, ch := createChannel(ctx, t, alice, "news "+n, false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	invite := channelInviteHash(ctx, t, alice, &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash})
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        invite,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	post := sendMessageID(ctx, t, alice, peer(alice), "News for everyone "+n, randomID())

	want := "[ru] NEWS FOR EVERYONE " + n
	for u, lang := range map[*user]string{bob: "ru", carol: "ru-RU"} {
		out, err := translateIds(ctx, u, peer(u), []int32{post}, lang)
		if err != nil || len(out) != 1 || out[0].GetText() != want {
			t.Fatalf("%s: %v, %v, want %q", lang, out, err, want)
		}
	}
	if seen := seenTranslate(t); seen.Calls != 1 {
		t.Fatalf("the engine was asked %d times for one post", seen.Calls)
	}

	editText(ctx, t, alice, peer(alice), post, "Edited news "+n)
	out, err := translateIds(ctx, bob, peer(bob), []int32{post}, "ru")
	if want = "[ru] EDITED NEWS " + n; err != nil || len(out) != 1 || out[0].GetText() != want {
		t.Fatalf("the edited post: %v, %v, want %q", out, err, want)
	}
	if seen := seenTranslate(t); seen.Calls != 2 {
		t.Fatalf("the engine was asked %d times for a post and its edit", seen.Calls)
	}

	_, err = translateIds(ctx, dave, peer(dave), []int32{post}, "ru")
	if !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Fatalf("a channel the caller is not in: %v, want PEER_ID_INVALID", err)
	}
}

// waitEngineUp waits until a translation goes through: the engine came back
// and bff found it healthy. It runs in a cleanup, after the test's context
// ended.
func waitEngineUp(t *testing.T, u *user) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	untilTranslated(ctx, t, "after the engine came back", func() ([]*mtproto.TextWithEntities, error) {
		return translateTexts(ctx, u, []*mtproto.TextWithEntities{plainText(u, "engine check "+uniq())}, "en")
	})
}

// TestTranslateEngineDown: an engine that answers 503 makes bff answer
// FLOOD_WAIT_10 and queue nothing new until its health check passes; then
// the item that found it down is translated, and the repeat of the client
// gets it. An engine slower than the wait is FLOOD_WAIT_X, and the repeat
// after X gets the translation the engine finished meanwhile, without a
// second request.
func TestTranslateEngineDown(t *testing.T) {
	planTranslate(t, translatePlan{Fail: -1, Status: http.StatusServiceUnavailable})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Reader")
	t.Cleanup(func() {
		planTranslate(t, translatePlan{})
		waitEngineUp(t, u)
	})
	n := uniq()

	first := []*mtproto.TextWithEntities{plainText(u, "The engine is down "+n)}
	_, err := translateTexts(ctx, u, first, "ru")
	if !mtclient.IsRPCError(err, "FLOOD_WAIT_10") {
		t.Fatalf("an engine that answers 503: %v, want FLOOD_WAIT_10", err)
	}
	_, err = translateTexts(ctx, u, []*mtproto.TextWithEntities{plainText(u, "Another text "+n)}, "ru")
	if !mtclient.IsRPCError(err, "FLOOD_WAIT_10") {
		t.Fatalf("while the engine is down: %v, want FLOOD_WAIT_10", err)
	}
	if seen := seenTranslate(t); seen.Calls != 1 {
		t.Fatalf("the engine was asked %d times while down, want only the first item", seen.Calls)
	}

	planTranslate(t, translatePlan{})
	out := untilTranslated(ctx, t, "the item that found the engine down", func() ([]*mtproto.TextWithEntities, error) {
		return translateTexts(ctx, u, first, "ru")
	})
	if want := "[ru] THE ENGINE IS DOWN " + n; len(out) != 1 || out[0].GetText() != want {
		t.Fatalf("after the engine came back: %v, want %q", out, want)
	}
	if seen := seenTranslate(t); seen.Calls != 1 {
		t.Fatalf("the engine was asked %d times for the waiting item", seen.Calls)
	}

	// Slower than the wait of the stand (SyncWaitSingle 5 s, run.sh).
	planTranslate(t, translatePlan{DelayMs: 8000})
	slow := []*mtproto.TextWithEntities{plainText(u, "A slow engine "+n)}
	_, err = translateTexts(ctx, u, slow, "ru")
	checkFloodWait(t, "a slow engine", err)
	out = untilTranslated(ctx, t, "a slow engine", func() ([]*mtproto.TextWithEntities, error) {
		return translateTexts(ctx, u, slow, "ru")
	})
	if want := "[ru] A SLOW ENGINE " + n; len(out) != 1 || out[0].GetText() != want {
		t.Fatalf("the slow translation: %v, want %q", out, want)
	}
	if seen := seenTranslate(t); seen.Calls != 1 {
		t.Fatalf("the engine was asked %d times for one slow item", seen.Calls)
	}
}

// TestTranslateMarkupFallback: an answer that lost a tag, or has one its
// source did not, is not used: the text goes by pieces between its verbatim
// entities, which keep their places, and the formatting is lost. A mention
// of a client's text (inputMessageEntityMentionName) comes back as the
// mention of its user.
func TestTranslateMarkupFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice, bob, _ := twoUsers(ctx, t)
	for _, plan := range []translatePlan{{DropTags: true}, {ExtraTag: true}} {
		planTranslate(t, plan)
		n := uniq()
		text := "Ask Alice to run make test today " + n
		item := plainText(bob, text,
			mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity(),
			&mtproto.MessageEntity{
				Constructor:      bob.c.Constructor(mtproto.Predicate_inputMessageEntityMentionName),
				Offset:           4,
				Length:           5,
				UserId_INPUTUSER: inputUserOf(ctx, t, bob, alice),
			},
			mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: int32(strings.Index(text, "make test")), Length: 9}).To_MessageEntity(),
		)
		out, err := translateTexts(ctx, bob, []*mtproto.TextWithEntities{item}, "ru")
		if err != nil || len(out) != 1 {
			t.Fatalf("%+v: %v, %v", plan, out, err)
		}
		// Three pieces: before the mention, between it and the code, after
		// the code.
		want := "[ru] ASK Alice [ru] TO RUN make test [ru] TODAY " + n
		if out[0].GetText() != want || len(out[0].GetEntities()) != 2 {
			t.Fatalf("%+v: %q %v, want %q with the mention and the code", plan, out[0].GetText(), out[0].GetEntities(), want)
		}
		mention := entityOf(t, "the fallback", out[0], mtproto.Predicate_messageEntityMentionName)
		covers(t, "mention", mention, want, "Alice")
		if mention.GetUserId_INT64() != alice.self.GetId() {
			t.Errorf("the mention names %d, want %d", mention.GetUserId_INT64(), alice.self.GetId())
		}
		covers(t, "code", entityOf(t, "the fallback", out[0], mtproto.Predicate_messageEntityCode), want, "make test")
		if seen := seenTranslate(t); seen.Calls != 4 || seen.Tagged != 1 {
			t.Fatalf("%+v: the engine saw %+v, want the tagged text and three pieces", plan, seen)
		}
	}
}

// fullUserFlag is translations_disabled of the full user who as u sees it.
func fullUserFlag(ctx context.Context, t *testing.T, u *user, who *mtproto.InputUser) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{Constructor: u.c.Constructor(mtproto.Predicate_users_getFullUser), Id: who})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	return o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull().GetFullUser().GetTranslationsDisabled()
}

func fullChatFlag(ctx context.Context, t *testing.T, u *user, chatId int64) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetFullChat{Constructor: u.c.Constructor(mtproto.Predicate_messages_getFullChat), ChatId: chatId})
	if err != nil {
		t.Fatalf("messages.getFullChat: %v", err)
	}
	return o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetTranslationsDisabled()
}

func fullChannelFlag(ctx context.Context, t *testing.T, u *user, channel *mtproto.InputChannel) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel), Channel: channel})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	return o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetTranslationsDisabled()
}

func togglePeerTranslations(ctx context.Context, u *user, peer *mtproto.InputPeer, disabled bool) error {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesTogglePeerTranslations{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_togglePeerTranslations),
		Disabled:    disabled,
		Peer:        peer,
	})
	if err != nil {
		return err
	}
	if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
		return fmt.Errorf("messages.togglePeerTranslations answered %v", o)
	}
	return nil
}

// TestTogglePeerTranslations: the translation bar of a private chat, a group
// and a channel is hidden and shown again; the reader's other device reads
// the flag from the full user, chat and channel, as the clients do (there is
// no update), and the other side of the chat is not affected. A channel the
// caller cannot read is no peer.
func TestTogglePeerTranslations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	// The private chat is Alice's once it has a message: the bar of a chat
	// she has no dialog with is no peer (ADR 0064, p. 8).
	sendTo(ctx, t, alice, toBob, "hello "+uniq())
	bobOnLaptop := inputUserOf(ctx, t, laptop, bob)
	group := createChat(ctx, t, alice, "translated "+uniq(), bob)
	_, ch := createChannel(ctx, t, alice, "translated "+uniq(), false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	channelPeer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	channelOnLaptop := &mtproto.InputChannel{Constructor: laptop.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}

	flags := func() [3]bool {
		return [3]bool{fullUserFlag(ctx, t, laptop, bobOnLaptop), fullChatFlag(ctx, t, laptop, group), fullChannelFlag(ctx, t, laptop, channelOnLaptop)}
	}
	if got := flags(); got != [3]bool{} {
		t.Fatalf("hidden before any toggle: %v", got)
	}
	for _, disabled := range []bool{true, false} {
		for _, peer := range []*mtproto.InputPeer{toBob, chatPeer(alice, group), channelPeer(alice)} {
			if err := togglePeerTranslations(ctx, alice, peer, disabled); err != nil {
				t.Fatalf("disabled %v for %v: %v", disabled, peer, err)
			}
		}
		if got := flags(); got != [3]bool{disabled, disabled, disabled} {
			t.Fatalf("the other device reads %v after disabled %v", got, disabled)
		}
		// Again: the same answer, nothing changes.
		if err := togglePeerTranslations(ctx, alice, toBob, disabled); err != nil {
			t.Fatalf("again: %v", err)
		}
	}

	// Alice's choice is hers: Bob's bar in the chat with her stays.
	if err := togglePeerTranslations(ctx, alice, toBob, true); err != nil {
		t.Fatal(err)
	}
	if fullUserFlag(ctx, t, bob, inputUserOf(ctx, t, bob, alice)) {
		t.Error("Bob's bar is hidden by Alice's choice")
	}
	if err := togglePeerTranslations(ctx, bob, channelPeer(bob), true); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("a private channel the caller is not in: %v, want PEER_ID_INVALID", err)
	}
}

// TestComposeMessageWithAITranslate: the translation screen of iOS asks
// messages.composeMessageWithAI with translate_to_lang and gets the text
// with its entities; every other form of the AI editor, and its tones, are
// not served.
func TestComposeMessageWithAITranslate(t *testing.T) {
	planTranslate(t, translatePlan{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Writer")
	n := uniq()
	compose := func(req *mtproto.TLMessagesComposeMessageWithAI) (*mtproto.Messages_ComposedMessageWithAI, error) {
		req.Constructor = u.c.Constructor(mtproto.Predicate_messages_composeMessageWithAI)
		o, err := u.c.Invoke(ctx, req)
		if err != nil {
			return nil, err
		}
		r, ok := o.(interface {
			To_Messages_ComposedMessageWithAI() *mtproto.Messages_ComposedMessageWithAI
		})
		if !ok {
			return nil, fmt.Errorf("messages.composeMessageWithAI returned %T", o)
		}
		return r.To_Messages_ComposedMessageWithAI(), nil
	}
	text := plainText(u, "Good evening "+n, mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity())

	r, err := compose(&mtproto.TLMessagesComposeMessageWithAI{Text: text, TranslateToLang: wrapperspb.String("ru")})
	if err != nil {
		t.Fatalf("the translate form: %v", err)
	}
	want := "[ru] GOOD EVENING " + n
	if r.GetResultText().GetText() != want || r.GetDiffText() != nil {
		t.Fatalf("the translate form: %v, want %q without a diff", r, want)
	}
	covers(t, "bold", entityOf(t, "the translate form", r.GetResultText(), mtproto.Predicate_messageEntityBold), want, "GOOD")
	if seen := seenTranslate(t); seen.Calls != 1 || seen.Tagged != 1 {
		t.Fatalf("the engine saw %+v", seen)
	}

	for _, c := range []struct {
		what string
		req  *mtproto.TLMessagesComposeMessageWithAI
		want string
	}{
		{"emojify", &mtproto.TLMessagesComposeMessageWithAI{Text: text, TranslateToLang: wrapperspb.String("ru"), Emojify: true}, "ERR_ENTERPRISE_IS_BLOCKED"},
		{"proofread", &mtproto.TLMessagesComposeMessageWithAI{Text: text, Proofread: true}, "ERR_ENTERPRISE_IS_BLOCKED"},
		{"a tone", &mtproto.TLMessagesComposeMessageWithAI{Text: text, ChangeTone: wrapperspb.String("formal")}, "ERR_ENTERPRISE_IS_BLOCKED"},
		{"a language the model does not have", &mtproto.TLMessagesComposeMessageWithAI{Text: text, TranslateToLang: wrapperspb.String("xx")}, "TO_LANG_INVALID"},
	} {
		if _, err = compose(c.req); !mtclient.IsRPCError(err, c.want) {
			t.Errorf("%s: got %v, want %s", c.what, err, c.want)
		}
	}
	_, err = u.c.Invoke(ctx, &mtproto.TLAicomposeGetTones{Constructor: u.c.Constructor(mtproto.Predicate_aicompose_getTones)})
	if !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("aicompose.getTones: got %v, want ERR_ENTERPRISE_IS_BLOCKED", err)
	}
	if seen := seenTranslate(t); seen.Calls != 1 {
		t.Fatalf("the refused forms reached the engine: %d calls", seen.Calls)
	}
}

// appConfigOf is the config help.getAppConfig gives u, retried while bff
// is coming back.
func appConfigOf(ctx context.Context, t *testing.T, u *user) map[string]any {
	t.Helper()
	for {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		o, err := u.c.Invoke(cctx, &mtproto.TLHelpGetAppConfig61E3F854{Constructor: u.c.Constructor(mtproto.Predicate_help_getAppConfig61E3F854)})
		cancel()
		if err == nil {
			r, ok := o.(*mtproto.TLHelpAppConfig)
			if !ok {
				t.Fatalf("help.getAppConfig returned %T", o)
			}
			m, _ := jsonOf(t, r.GetConfig()).(map[string]any)
			return m
		}
		if ctx.Err() != nil {
			t.Fatalf("help.getAppConfig: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestTranslationOff: bff without TRANSLATE_URL tells the clients that
// there is no translation (both keys "disabled", so that none falls back to
// a translation on the device) and answers ERR_ENTERPRISE_IS_BLOCKED, which
// they do not repeat; hiding the bar of a chat still works. With the engine
// back, so is the translation.
func TestTranslationOff(t *testing.T) {
	planTranslate(t, translatePlan{})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Reader")
	text := []*mtproto.TextWithEntities{plainText(u, "Off and on "+uniq())}
	if cfg := appConfigOf(ctx, t, u); cfg["translations_manual_enabled"] != "enabled" || cfg["translations_auto_enabled"] != "enabled" {
		t.Fatalf("with the engine: %v, %v", cfg["translations_manual_enabled"], cfg["translations_auto_enabled"])
	}

	t.Cleanup(func() { restore(t) })
	// The shell's variable wins over the env file of run.sh.
	cmd := exec.Command(runScript, "compose", "up", "--detach", "--no-deps", "--wait", "--wait-timeout", "180", "bff")
	cmd.Env = append(os.Environ(), "TRANSLATE_URL=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bff without TRANSLATE_URL: %v\n%s", err, out)
	}
	waitServes(ctx, t, u)

	cfg := appConfigOf(ctx, t, u)
	if cfg["translations_manual_enabled"] != "disabled" || cfg["translations_auto_enabled"] != "disabled" {
		t.Fatalf("without the engine: %v, %v", cfg["translations_manual_enabled"], cfg["translations_auto_enabled"])
	}
	if _, err := translateTexts(ctx, u, text, "ru"); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Fatalf("messages.translateText without the engine: %v", err)
	}
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesComposeMessageWithAI{
		Constructor:     u.c.Constructor(mtproto.Predicate_messages_composeMessageWithAI),
		Text:            text[0],
		TranslateToLang: wrapperspb.String("ru"),
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Fatalf("messages.composeMessageWithAI without the engine: %v", err)
	}
	if err := togglePeerTranslations(ctx, u, &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}, true); err != nil {
		t.Fatalf("messages.togglePeerTranslations without the engine: %v", err)
	}
	if seen := seenTranslate(t); seen.Calls != 0 {
		t.Fatalf("the engine was asked %d times while off", seen.Calls)
	}

	restore(t)
	waitServes(ctx, t, u)
	if cfg = appConfigOf(ctx, t, u); cfg["translations_manual_enabled"] != "enabled" {
		t.Fatalf("with the engine back: %v", cfg["translations_manual_enabled"])
	}
	out := untilTranslated(ctx, t, "with the engine back", func() ([]*mtproto.TextWithEntities, error) {
		return translateTexts(ctx, u, text, "ru")
	})
	if len(out) != 1 || !strings.HasPrefix(out[0].GetText(), "[ru] OFF AND ON ") {
		t.Fatalf("with the engine back: %v", out)
	}
}
