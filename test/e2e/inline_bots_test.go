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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The built-in inline bots @gif and @pic (task 099, ADR 0062). The stand's
// SearXNG and the sites of the pictures it finds are test/e2e/fakeinline,
// which bff may fetch from (WebFetch.AllowHosts in the config of run.sh).
// Every test searches texts of its own, so neither the shared cache of the
// answers nor the pages SearXNG gave, both in Redis, are shared between
// tests or runs.

// Set by test/e2e/run.sh: host:port of fakeinline.
var fakeInlineAddr = os.Getenv("PLUMA_E2E_FAKEINLINE")

// fakeInlineHost is fakeinline as bff reaches it on the network of the
// services.
const fakeInlineHost = "fakeinline:8080"

func needFakeInline(t *testing.T) {
	t.Helper()
	if fakeInlineAddr == "" {
		t.Skip("PLUMA_E2E_FAKEINLINE is not set: the stand has no fakeinline")
	}
}

// inlineRequest is a request fakeinline got from bff.
type inlineRequest struct {
	Seq        int    `json:"seq"`
	Kind       string `json:"kind"`
	Q          string `json:"q"`
	PageNo     string `json:"pageno"`
	Language   string `json:"language"`
	SafeSearch string `json:"safesearch"`
	Categories string `json:"categories"`
	Format     string `json:"format"`
	Path       string `json:"path"`
	UserAgent  string `json:"user_agent"`
	Status     int    `json:"status"`
}

// inlineRequests is what fakeinline got for a search text (by "q") or a
// file path (by "path"), oldest first.
func inlineRequests(t *testing.T, by, value string) []inlineRequest {
	t.Helper()
	resp, err := http.Get("http://" + fakeInlineAddr + "/e2e/requests?" + url.Values{by: {value}}.Encode())
	if err != nil {
		t.Fatalf("fakeinline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fakeinline: requests: status %d", resp.StatusCode)
	}
	var out []inlineRequest
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("fakeinline: requests: %v", err)
	}
	return out
}

// planInline makes the next count requests of a search text (by "q") or a
// file path (by "path") wait delay and answer status; extra are more
// parameters of the plan (results, unresponsive).
func planInline(t *testing.T, by, value string, status int, delay time.Duration, count int, extra url.Values) {
	t.Helper()
	v := url.Values{by: {value}, "status": {strconv.Itoa(status)}, "count": {strconv.Itoa(count)},
		"delay_ms": {strconv.FormatInt(delay.Milliseconds(), 10)}}
	for k, vs := range extra {
		v[k] = vs
	}
	resp, err := http.Post("http://"+fakeInlineAddr+"/e2e/plan?"+v.Encode(), "", nil)
	if err != nil {
		t.Fatalf("fakeinline: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fakeinline: plan: status %d", resp.StatusCode)
	}
}

// fakePicturePath is the path of the i-th picture fakeinline names for a
// page of a search text: /pic/ and the first 8 bytes in hex of
// sha256(text, 0, page, 0, i), then .jpg (pictureName of
// test/e2e/fakeinline).
func fakePicturePath(text string, pageno, i int) string {
	h := sha256.Sum256([]byte(text + "\x00" + strconv.Itoa(pageno) + "\x00" + strconv.Itoa(i)))
	return "/pic/" + hex.EncodeToString(h[:8]) + ".jpg"
}

// inlineText is a search text no other test and no earlier run asked.
func inlineText(word string) string {
	return fmt.Sprintf("%s %x", word, uint32(randomID()))
}

// builtinBot resolves a built-in bot by its username as clients do
// (Android PhotoPickerActivity.java:1599, EmojiView.java:8948; tdesktop
// gifs_list_widget.cpp:901; iOS GifContext.swift:56).
func builtinBot(ctx context.Context, t *testing.T, u *user, username string) *mtproto.User {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLContactsResolveUsername{
		Constructor: u.c.Constructor(mtproto.Predicate_contacts_resolveUsername),
		Username:    username,
	})
	if err != nil {
		t.Fatalf("contacts.resolveUsername(%s): %v", username, err)
	}
	for _, x := range o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	}).To_Contacts_ResolvedPeer().GetUsers() {
		if x.GetUsername().GetValue() == username {
			return x
		}
	}
	t.Fatalf("contacts.resolveUsername(%s): no user", username)
	return nil
}

func botInput(c *mtclient.Client, bot *mtproto.User) *mtproto.InputUser {
	return &mtproto.InputUser{Constructor: c.Constructor(mtproto.Predicate_inputUser), UserId: bot.GetId(), AccessHash: bot.GetAccessHash().GetValue()}
}

// queryInline asks bot through messages.getInlineBotResults in the chat
// peer.
func queryInline(ctx context.Context, u *user, bot *mtproto.User, peer *mtproto.InputPeer, text, offset string) (*mtproto.Messages_BotResults, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
		Bot:         botInput(u.c, bot),
		Peer:        peer,
		Query:       text,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Messages_BotResults() *mtproto.Messages_BotResults
	})
	if !ok {
		return nil, fmt.Errorf("messages.getInlineBotResults returned %T", o)
	}
	return r.To_Messages_BotResults(), nil
}

func mustQueryInline(ctx context.Context, t *testing.T, u *user, bot *mtproto.User, peer *mtproto.InputPeer, text, offset string) *mtproto.Messages_BotResults {
	t.Helper()
	r, err := queryInline(ctx, u, bot, peer, text, offset)
	if err != nil {
		t.Fatalf("messages.getInlineBotResults(@%s, offset %q): %v", bot.GetUsername().GetValue(), offset, err)
	}
	return r
}

// sendInline sends a result and returns the message of the answer.
func sendInline(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, queryId int64, id string, hideVia bool) *mtproto.Message {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendInlineBotResult{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendInlineBotResult),
		Peer:        peer,
		RandomId:    randomID(),
		QueryId:     queryId,
		Id:          id,
		HideVia:     hideVia,
	})
	if err != nil {
		t.Fatalf("messages.sendInlineBotResult: %v", err)
	}
	up, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("messages.sendInlineBotResult returned %T", o)
	}
	ms := newMessages(up.To_Updates())
	if len(ms) != 1 {
		t.Fatalf("messages.sendInlineBotResult: %d new messages in the answer", len(ms))
	}
	return ms[0]
}

// downloadPhoto returns the bytes of the largest size of a photo.
func downloadPhoto(ctx context.Context, t *testing.T, u *user, p *mtproto.Photo) []byte {
	t.Helper()
	var (
		thumb string
		area  int32
	)
	for _, s := range p.GetSizes() {
		switch s.GetPredicateName() {
		case mtproto.Predicate_photoSize, mtproto.Predicate_photoSizeProgressive:
			if a := s.GetW() * s.GetH(); a > area {
				thumb, area = s.GetType(), a
			}
		}
	}
	if thumb == "" {
		t.Fatalf("photo %d has no size to download", p.GetId())
	}
	o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
		Constructor: u.c.Constructor(mtproto.Predicate_upload_getFile),
		Location: mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{
			Id: p.GetId(), AccessHash: p.GetAccessHash(), FileReference: p.GetFileReference(), ThumbSize: thumb,
		}).To_InputFileLocation(),
		Limit: 1 << 20,
	})
	if err != nil {
		t.Fatalf("upload.getFile of photo %d (%s): %v", p.GetId(), thumb, err)
	}
	return o.(interface{ To_Upload_File() *mtproto.Upload_File }).To_Upload_File().GetBytes()
}

// TestInlineBotsConfig: help.getConfig names the built-in bots in the
// fields the clients find them by (Android MessagesController.java:5688-
// 5693, iOS ManagedConfigurationUpdates.swift:68, tdesktop
// mtproto_config.cpp:283, Web K appGifsManager.ts:70) and sends web files to
// the server's own DC; each name resolves to the bot of the registry, with
// the placeholder without which iOS shows no GIF tab (GifContext.swift:65)
// and its description in the viewer's language. The places are not served
// (the owner's decision of 2026-11-16): no field names a bot of ours for
// them.
func TestInlineBotsConfig(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	viewers := map[string]*user{
		"ru": loginIn(ctx, t, phones[0], "Alice", "ru"),
		"en": loginIn(ctx, t, phones[1], "Bob", "en"),
	}

	o, err := viewers["en"].c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: viewers["en"].c.Constructor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}
	conf, ok := o.(*mtproto.TLConfig)
	if !ok {
		t.Fatalf("help.getConfig: %T", o)
	}
	if got := conf.GetGifSearchUsername().GetValue(); got != inlinebots.Gif.Username {
		t.Errorf("gif_search_username %q", got)
	}
	if got := conf.GetImgSearchUsername().GetValue(); got != inlinebots.Pic.Username {
		t.Errorf("img_search_username %q", got)
	}
	if got := conf.GetVenueSearchUsername().GetValue(); got == inlinebots.Gif.Username || got == inlinebots.Pic.Username || got == "places" {
		t.Errorf("venue_search_username %q names a bot of the server", got)
	}
	if conf.GetWebfileDcId() != conf.GetThisDc() {
		t.Errorf("webfile_dc_id %d, this_dc %d", conf.GetWebfileDcId(), conf.GetThisDc())
	}

	for _, b := range inlinebots.All() {
		for lang, u := range viewers {
			bot := builtinBot(ctx, t, u, b.Username)
			if bot.GetId() != b.Id || !bot.GetBot() || bot.GetUsername().GetValue() != b.Username {
				t.Errorf("@%s for %s: user %d, bot %v, username %q", b.Username, lang, bot.GetId(), bot.GetBot(), bot.GetUsername().GetValue())
			}
			if bot.GetBotInlinePlaceholder().GetValue() == "" || bot.GetBotInlineGeo() {
				t.Errorf("@%s: placeholder %q, inline geo %v", b.Username, bot.GetBotInlinePlaceholder().GetValue(), bot.GetBotInlineGeo())
			}
			o, err := u.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
				Constructor: u.c.Constructor(mtproto.Predicate_users_getFullUser),
				Id:          botInput(u.c, bot),
			})
			if err != nil {
				t.Fatalf("users.getFullUser(@%s): %v", b.Username, err)
			}
			about := o.(interface {
				To_Users_UserFull() *mtproto.Users_UserFull
			}).To_Users_UserFull().GetFullUser().GetBotInfo().GetDescription_FLAGSTRING().GetValue()
			if want := langpack.InlineBotText(b.About, lang); about == "" || about != want {
				t.Errorf("@%s in %s: description %q, want %q", b.Username, lang, about, want)
			}
		}
	}
}

// TestInlinePic: @pic searches the text with SearXNG, in the language of
// the app and with safe search, imports the pictures it finds into the
// server and answers with photos, which Android's picture search sends as
// photos of its own (PhotoPickerActivity.java:1615-1740,
// SendMessagesHelper.java:11188-11189); the device never fetches from the
// web. next_offset pages within the page of SearXNG, which is asked once;
// the answer is shared with everyone who asks the same. A result sent is a
// photo via @pic, which the other side gets. An empty text asks nothing.
func TestInlinePic(t *testing.T) {
	needFakeInline(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginIn(ctx, t, phones[0], "Alice", "de")
	bob := loginIn(ctx, t, phones[1], "Bob", "en")
	getState(ctx, t, bob)
	pic := builtinBot(ctx, t, alice, inlinebots.Pic.Username)
	toBob := peerOf(ctx, t, alice, bob)
	text := inlineText("cat")

	first := mustQueryInline(ctx, t, alice, pic, toBob, text, "")
	if !first.GetGallery() || first.GetCacheTime() != 3600 || len(first.GetResults()) != 12 || first.GetNextOffset().GetValue() != "p1.12" {
		t.Fatalf("the first page: gallery %v, cache_time %d, %d results, next_offset %q",
			first.GetGallery(), first.GetCacheTime(), len(first.GetResults()), first.GetNextOffset().GetValue())
	}
	ids := map[string]bool{}
	for _, r := range first.GetResults() {
		if r.GetPredicateName() != mtproto.Predicate_botInlineMediaResult || r.GetType() != "photo" ||
			r.GetPhoto().GetPredicateName() != mtproto.Predicate_photo || r.GetPhoto().GetId() == 0 || r.GetDocument() != nil ||
			r.GetSendMessage().GetPredicateName() != mtproto.Predicate_botInlineMessageMediaAuto || len(r.GetId()) > 64 || ids[r.GetId()] {
			t.Fatalf("a result of @pic: %v", r)
		}
		ids[r.GetId()] = true
	}
	searches := inlineRequests(t, "q", text)
	if len(searches) != 1 {
		t.Fatalf("%d searches of the text, want 1", len(searches))
	}
	if s := searches[0]; s.Categories != "images" || s.Format != "json" || s.SafeSearch != "1" || s.Language != "de" || s.PageNo != "1" {
		t.Errorf("the search: %+v", s)
	}
	// The pictures are fetched by the server, the first ones of the page.
	for i := 0; i < 12; i++ {
		got := inlineRequests(t, "path", fakePicturePath(text, 1, i))
		if len(got) == 0 || !strings.Contains(got[0].UserAgent, "PlumaBot") {
			t.Errorf("picture %d: fetched %d times, by %v", i, len(got), got)
		}
	}

	body := downloadPhoto(ctx, t, alice, first.GetResults()[0].GetPhoto())
	if _, err := jpeg.DecodeConfig(bytes.NewReader(body)); err != nil {
		t.Errorf("the photo of a result is not a JPEG: %v", err)
	}

	second := mustQueryInline(ctx, t, alice, pic, toBob, text, first.GetNextOffset().GetValue())
	if len(second.GetResults()) != 8 || second.GetNextOffset().GetValue() != "p2.0" {
		t.Fatalf("the second page: %d results, next_offset %q", len(second.GetResults()), second.GetNextOffset().GetValue())
	}
	for _, r := range second.GetResults() {
		if ids[r.GetId()] {
			t.Errorf("result %s on both pages", r.GetId())
		}
	}
	shared := mustQueryInline(ctx, t, bob, pic, peerOf(ctx, t, bob, alice), text, "")
	if shared.GetQueryId() != first.GetQueryId() || len(shared.GetResults()) != 12 {
		t.Errorf("the same query by another user: query %d (first %d), %d results", shared.GetQueryId(), first.GetQueryId(), len(shared.GetResults()))
	}
	if n := len(inlineRequests(t, "q", text)); n != 1 {
		t.Errorf("%d searches of the text after the second page and another user, want 1", n)
	}

	result := first.GetResults()[3]
	m := sendInline(ctx, t, alice, toBob, first.GetQueryId(), result.GetId(), false)
	if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto || m.GetMedia().GetPhoto_FLAGPHOTO().GetId() != result.GetPhoto().GetId() ||
		m.GetViaBotId().GetValue() != inlinebots.Pic.Id {
		t.Fatalf("the message sent: media %v, via %v", m.GetMedia(), m.GetViaBotId())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		got := u.GetMessage_MESSAGE()
		return got.GetMedia().GetPhoto_FLAGPHOTO().GetId() == result.GetPhoto().GetId() && got.GetViaBotId().GetValue() == inlinebots.Pic.Id
	})

	empty := mustQueryInline(ctx, t, alice, pic, toBob, "", "")
	if len(empty.GetResults()) != 0 || empty.GetNextOffset().GetValue() != "" {
		t.Errorf("an empty text: %d results, next_offset %q", len(empty.GetResults()), empty.GetNextOffset().GetValue())
	}
}

// clearPopularGifs forgets the popular GIFs the server keeps for 10
// minutes, the list and the answers of @gif to the empty query: they are
// the same for every test and run, and the test must see the saves it
// makes now.
func clearPopularGifs(t *testing.T) {
	t.Helper()
	stackOut(t, "exec", "-T", "redis", "sh", "-c", fmt.Sprintf(
		"redis-cli del inline_local_popular >/dev/null && redis-cli --scan --pattern 'bot_inline_cache:%d:0:*' | xargs -r redis-cli del >/dev/null",
		inlinebots.Gif.Id))
}

// gifSends is the sends of document counted from @gif's results.
func gifSends(t *testing.T, document int64) string {
	t.Helper()
	out := stackOut(t, "exec", "-T", "mysql", "sh", "-c", fmt.Sprintf(
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -N -B -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "SELECT COALESCE(SUM(sends), 0) FROM inline_gif_sends WHERE document_id = %d"`,
		document))
	return strings.TrimSpace(out)
}

// TestInlineGifTrending: the GIF tab asks @gif with an empty text (Android
// EmojiView.java:8939 with no chat, :9200; iOS GifContext.swift:66 with the
// own chat) and gets the GIFs that three people or more saved, as
// documents (Web K appGifsManager.ts:81, Android EmojiView.java:9244); a
// GIF two people saved is not shown to others. The tab sends with
// hide_via (Android ChatActivityEnterView.java:12491-12509, iOS
// ChatController.swift:2820), so the message has no via_bot_id, and the
// send counts for the popular GIFs. A text finds nothing while @gif has no
// search.
func TestInlineGifTrending(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	ann := login(ctx, t, phones[0], "Ann")
	ben := login(ctx, t, phones[1], "Ben")
	cat := login(ctx, t, phones[2], "Cat")
	dan := login(ctx, t, phones[3], "Dan")
	getState(ctx, t, ann)

	inputDoc := func(u *user, d *mtproto.Document) *mtproto.InputDocument {
		return &mtproto.InputDocument{Constructor: u.c.Constructor(mtproto.Predicate_inputDocument), Id: d.GetId(), AccessHash: d.GetAccessHash(), FileReference: d.GetFileReference()}
	}
	popular := sendGif(ctx, t, ann)
	for _, u := range []*user{ann, ben, cat} {
		saveGif(ctx, t, u, inputDoc(u, popular), false)
	}
	private := sendGif(ctx, t, ann)
	for _, u := range []*user{ann, ben} {
		saveGif(ctx, t, u, inputDoc(u, private), false)
	}
	clearPopularGifs(t)

	gif := builtinBot(ctx, t, dan, inlinebots.Gif.Username)
	noChat := &mtproto.InputPeer{Constructor: dan.c.Constructor(mtproto.Predicate_inputPeerEmpty)}
	var (
		found   *mtproto.BotInlineResult
		queryId int64
		offset  string
	)
	for page := 0; page < 10 && found == nil; page++ {
		r := mustQueryInline(ctx, t, dan, gif, noChat, "", offset)
		if !r.GetGallery() || r.GetCacheTime() != 600 {
			t.Errorf("a page of @gif: gallery %v, cache_time %d", r.GetGallery(), r.GetCacheTime())
		}
		for _, res := range r.GetResults() {
			if res.GetPredicateName() != mtproto.Predicate_botInlineMediaResult || res.GetType() != "gif" ||
				res.GetDocument().GetPredicateName() != mtproto.Predicate_document || res.GetPhoto() != nil {
				t.Fatalf("a result of @gif: %v", res)
			}
			switch res.GetDocument().GetId() {
			case popular.GetId():
				found, queryId = res, r.GetQueryId()
			case private.GetId():
				t.Fatal("a GIF two people saved is popular")
			}
		}
		if offset = r.GetNextOffset().GetValue(); offset == "" {
			break
		}
	}
	if found == nil {
		t.Fatal("the GIF three people saved is not in the popular ones")
	}
	self := &mtproto.InputPeer{Constructor: dan.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	if again := mustQueryInline(ctx, t, dan, gif, self, "", ""); len(again.GetResults()) == 0 {
		t.Error("the GIF tab of iOS, from the own chat: no results")
	}

	toAnn := peerOf(ctx, t, dan, ann)
	m := sendInline(ctx, t, dan, toAnn, queryId, found.GetId(), true)
	if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaDocument || m.GetMedia().GetDocument().GetId() != popular.GetId() || m.GetViaBotId() != nil {
		t.Fatalf("the GIF sent from the tab: media %v, via %v", m.GetMedia(), m.GetViaBotId())
	}
	waitUpdate(ctx, t, ann, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		got := u.GetMessage_MESSAGE()
		return got.GetMedia().GetDocument().GetId() == popular.GetId() && got.GetFromId().GetUserId() == dan.self.GetId() && got.GetViaBotId() == nil
	})
	if n := gifSends(t, popular.GetId()); n != "1" {
		t.Errorf("sends of the GIF counted: %s, want 1", n)
	}

	text := mustQueryInline(ctx, t, dan, gif, toAnn, inlineText("cats"), "")
	if len(text.GetResults()) != 0 || text.GetCacheTime() != 600 {
		t.Errorf("a text to @gif: %d results, cache_time %d", len(text.GetResults()), text.GetCacheTime())
	}
}

// TestInlineProviderDown: a SearXNG that fails, answers late or has no
// engine left, and pictures that cannot be fetched, give an empty list
// that is not cached (cache_time 0), never an error, which clients would
// show or repeat; the next query gets the pictures.
func TestInlineProviderDown(t *testing.T) {
	needFakeInline(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	pic := builtinBot(ctx, t, alice, inlinebots.Pic.Username)
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}

	// A provider that fails gives no next_offset. Pictures found and not
	// fetched may leave the offset after them, which clients drop with an
	// answer that adds nothing (tdesktop inline_results_widget.cpp:415-417,
	// Android PhotoPickerActivity.java:1740), so it is not checked then.
	emptyThenFull := func(name, text string, maxWait time.Duration, providerFailed bool) {
		t.Helper()
		start := time.Now()
		r, err := queryInline(ctx, alice, pic, self, text, "")
		took := time.Since(start)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(r.GetResults()) != 0 || r.GetCacheTime() != 0 || (providerFailed && r.GetNextOffset().GetValue() != "") {
			t.Errorf("%s: %d results, cache_time %d, next_offset %q", name, len(r.GetResults()), r.GetCacheTime(), r.GetNextOffset().GetValue())
		}
		if took > maxWait {
			t.Errorf("%s: answered in %v", name, took)
		}
		if r = mustQueryInline(ctx, t, alice, pic, self, text, ""); len(r.GetResults()) == 0 {
			t.Errorf("%s, then working: no results", name)
		}
	}

	down := inlineText("down")
	planInline(t, "q", down, http.StatusServiceUnavailable, 0, 1, nil)
	emptyThenFull("SearXNG answers 503", down, 5*time.Second, true)

	// bff waits 6 s for SearXNG and 7 s for the whole answer.
	late := inlineText("late")
	planInline(t, "q", late, http.StatusOK, 9*time.Second, 1, nil)
	emptyThenFull("SearXNG answers after 9 s", late, 8500*time.Millisecond, true)

	blocked := inlineText("blocked")
	planInline(t, "q", blocked, http.StatusOK, 0, 1, url.Values{"results": {"0"}, "unresponsive": {"1"}})
	emptyThenFull("no engine answers", blocked, 5*time.Second, true)

	// One picture whose image and thumbnail (one path) both fail: nothing
	// is imported, and the page SearXNG gave, kept in Redis, is imported
	// on the next query.
	gone := inlineText("gone")
	planInline(t, "q", gone, http.StatusOK, 0, 1, url.Values{"results": {"1"}})
	planInline(t, "path", fakePicturePath(gone, 1, 0), http.StatusNotFound, 0, 2, nil)
	emptyThenFull("the picture cannot be fetched", gone, 5*time.Second, false)
	if n := len(inlineRequests(t, "q", gone)); n != 1 {
		t.Errorf("%d searches of a text whose pictures failed, want 1", n)
	}
}
