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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// webFileParts loads the web file at loc in parts of part bytes, as the
// clients do (Android FileLoadOperation.java, tdesktop
// file_download_mtproto.cpp), and checks what every part says of the file:
// one size (tdesktop cancels a load whose parts differ,
// file_download_mtproto.cpp:170-181), image/jpeg and storage.fileJpeg. A
// part past the end has no bytes and the same size.
func webFileParts(ctx context.Context, u *user, loc *mtproto.InputWebFileLocation, part int32) ([]byte, error) {
	get := func(offset int32) (*mtproto.Upload_WebFile, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetWebFile{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_getWebFile),
			Location:    loc,
			Offset:      offset,
			Limit:       part,
		})
		if err != nil {
			return nil, err
		}
		f, ok := o.(interface {
			To_Upload_WebFile() *mtproto.Upload_WebFile
		})
		if !ok {
			return nil, fmt.Errorf("upload.getWebFile returned %T", o)
		}
		wf := f.To_Upload_WebFile()
		if wf.GetMimeType() != "image/jpeg" || wf.GetFileType().GetPredicateName() != mtproto.Predicate_storage_fileJpeg {
			return nil, fmt.Errorf("a part of %s, %s", wf.GetMimeType(), wf.GetFileType().GetPredicateName())
		}
		return wf, nil
	}
	var (
		body []byte
		size int32 = -1
	)
	for offset := int32(0); size < 0 || offset < size; {
		wf, err := get(offset)
		if err != nil {
			return nil, err
		}
		switch {
		case size < 0:
			if size = wf.GetSize2(); size <= 0 {
				return nil, fmt.Errorf("the first part tells size %d", size)
			}
		case wf.GetSize2() != size:
			return nil, fmt.Errorf("the part at %d tells size %d, the first %d", offset, wf.GetSize2(), size)
		}
		n := int32(len(wf.GetBytes()))
		if n == 0 || n > part || (n < part && offset+n != size) {
			return nil, fmt.Errorf("the part at %d of %d has %d bytes", offset, size, n)
		}
		body = append(body, wf.GetBytes()...)
		offset += n
	}
	past, err := get(size)
	if err != nil {
		return nil, fmt.Errorf("the part past the end: %w", err)
	}
	if len(past.GetBytes()) != 0 || past.GetSize2() != size {
		return nil, fmt.Errorf("the part past the end: %d bytes, size %d", len(past.GetBytes()), past.GetSize2())
	}
	return body, nil
}

// fakeInlineFile is the file fakeinline serves at path, fetched from the
// test host.
func fakeInlineFile(t *testing.T, path string) []byte {
	t.Helper()
	resp, err := http.Get("http://" + fakeInlineAddr + path)
	if err != nil {
		t.Fatalf("fakeinline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fakeinline: %s: status %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("fakeinline: %s: %v", path, err)
	}
	return b
}

// TestWebFileProxy (ADR 0062, p. 5): the thumbnail a third-party inline bot
// names by its address reaches the user as a webDocument signed by the
// server, never webDocumentNoProxy, which the device would fetch from the
// bot's host itself. upload.getWebFile, sent to webfile_dc_id, gives the
// file in parts, all of one copy the server fetched once; an address or a
// hash the server did not sign together is LOCATION_INVALID, a file the
// host does not serve WEBFILE_NOT_AVAILABLE.
func TestWebFileProxy(t *testing.T) {
	needFakeInline(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phone, username)
	getState(ctx, t, bot)
	toBot := resolveBot(ctx, t, alice, username)
	admin(t, "bot", "set", username, "-inline", "Search...")

	name := fmt.Sprintf("proxy%x", uint32(randomID()))
	// A megapixel of noise: several parts of 128 KB.
	thumbPath := "/pic/" + name + ".jpg?w=1000&h=1000"
	gonePath := "/pic/" + name + "-gone.jpg"
	planInline(t, "path", gonePath, http.StatusNotFound, 0, 1, nil)
	thumb := func(path string, w, h int32) *mtproto.InputWebDocument {
		return mtproto.MakeTLInputWebDocument(&mtproto.InputWebDocument{
			Url:      "http://" + fakeInlineHost + path,
			MimeType: "image/jpeg",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: w, H: h}).To_DocumentAttribute(),
			},
		}).To_InputWebDocument()
	}
	article := func(id, title string, th *mtproto.InputWebDocument) *mtproto.InputBotInlineResult {
		return mtproto.MakeTLInputBotInlineResult(&mtproto.InputBotInlineResult{
			Id: id, Type: "article", Title: wrapperspb.String(title), Thumb: th,
			SendMessage: mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: title}).To_InputBotInlineMessage(),
		}).To_InputBotInlineResult()
	}

	query := inlineText("thumbs")
	inline := make(chan error, 1)
	var results *mtproto.Messages_BotResults
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
			Bot:         inputUserOfPeer(alice.c, toBot), Peer: toBot, Query: query,
		})
		if err == nil {
			results = o.(interface {
				To_Messages_BotResults() *mtproto.Messages_BotResults
			}).To_Messages_BotResults()
		}
		inline <- err
	}()
	var queryId int64
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotInlineQuery, func(u *mtproto.Update) bool {
		if u.GetQuery() != query {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSetInlineBotResults{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setInlineBotResults),
		QueryId:     queryId,
		Results: []*mtproto.InputBotInlineResult{
			article("1", "Square", thumb(thumbPath, 1000, 1000)),
			article("2", "Gone", thumb(gonePath, 320, 240)),
		},
	}); err != nil {
		t.Fatalf("messages.setInlineBotResults: %v", err)
	}
	if err := <-inline; err != nil {
		t.Fatalf("messages.getInlineBotResults: %v", err)
	}
	if len(results.GetResults()) != 2 {
		t.Fatalf("%d results", len(results.GetResults()))
	}
	location := func(d *mtproto.WebDocument) *mtproto.InputWebFileLocation {
		return &mtproto.InputWebFileLocation{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputWebFileLocation),
			Url:         d.GetUrl(),
			AccessHash:  d.GetAccessHash(),
		}
	}
	square, gone := results.GetResults()[0].GetThumb(), results.GetResults()[1].GetThumb()
	for _, d := range []*mtproto.WebDocument{square, gone} {
		if d.GetPredicateName() != mtproto.Predicate_webDocument || d.GetAccessHash() == 0 || !strings.HasPrefix(d.GetUrl(), "http://"+fakeInlineHost+"/pic/") {
			t.Fatalf("a thumbnail of the bot: %v", d)
		}
	}

	body, err := webFileParts(ctx, alice, location(square), 128<<10)
	if err != nil {
		t.Fatalf("upload.getWebFile: %v", err)
	}
	if len(body) <= 2*128<<10 {
		t.Errorf("the file has %d bytes, fewer than three parts", len(body))
	}
	if !bytes.Equal(body, fakeInlineFile(t, thumbPath)) {
		t.Error("the file is not the one the host serves")
	}
	fetched := 0
	for _, r := range inlineRequests(t, "path", "/pic/"+name+".jpg") {
		if strings.Contains(r.UserAgent, "PlumaBot") {
			fetched++
		}
	}
	if fetched != 1 {
		t.Errorf("the server fetched the file %d times for its parts, want once", fetched)
	}

	getWebFile := func(loc *mtproto.InputWebFileLocation) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLUploadGetWebFile{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_getWebFile),
			Location:    loc,
			Limit:       128 << 10,
		})
		return err
	}
	forged := location(square)
	forged.AccessHash++
	if err = getWebFile(forged); !mtclient.IsRPCError(err, "LOCATION_INVALID") {
		t.Errorf("another access_hash: %v", err)
	}
	moved := location(square)
	moved.Url = "http://" + fakeInlineHost + "/pic/" + name + ".jpg?w=1001&h=1000"
	if err = getWebFile(moved); !mtclient.IsRPCError(err, "LOCATION_INVALID") {
		t.Errorf("another address with the hash of the file: %v", err)
	}
	if err = getWebFile(location(gone)); !mtclient.IsRPCError(err, "WEBFILE_NOT_AVAILABLE") {
		t.Errorf("a file the host does not serve: %v", err)
	}
}
