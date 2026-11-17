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

package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/webfile"
)

// The thumbnail and content of an article go out as webDocument signed by
// the server, which upload.getWebFile proxies; without a key as
// webDocumentNoProxy, which clients fetch themselves (ADR 0062, p. 5).
func TestInlineWebDocumentIsSigned(t *testing.T) {
	const thumb, content = "https://bot.example/t.jpg", "https://bot.example/c.mp4"
	result := mtproto.MakeTLInputBotInlineResult(&mtproto.InputBotInlineResult{
		Id: "a", Type: "article", Title: mtproto.MakeFlagsString("T"),
		Thumb:       mtproto.MakeTLInputWebDocument(&mtproto.InputWebDocument{Url: thumb, Size2: 10, MimeType: "image/jpeg"}).To_InputWebDocument(),
		Content:     mtproto.MakeTLInputWebDocument(&mtproto.InputWebDocument{Url: content, Size2: 20, MimeType: "video/mp4"}).To_InputWebDocument(),
		SendMessage: mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: "hi"}).To_InputBotInlineMessage(),
	}).To_InputBotInlineResult()

	c := testCore(&dao.Dao{}, 20)
	key := []byte("0123456789abcdef0123456789abcdef")
	c.svcCtx.WebFileKey = key
	r, err := c.inlineResult(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		d   *mtproto.WebDocument
		url string
	}{{r.GetThumb(), thumb}, {r.GetContent(), content}} {
		if tc.d.GetPredicateName() != mtproto.Predicate_webDocument || tc.d.GetUrl() != tc.url {
			t.Fatalf("%s: %v", tc.url, tc.d)
		}
		if tc.d.GetAccessHash() == 0 || tc.d.GetAccessHash() != webfile.Sign(key, tc.url) {
			t.Errorf("%s: access_hash %d, want the signature", tc.url, tc.d.GetAccessHash())
		}
	}
	if r.GetThumb().GetSize2() != 10 || r.GetThumb().GetMimeType() != "image/jpeg" {
		t.Errorf("thumb %v", r.GetThumb())
	}

	c.svcCtx.WebFileKey = nil
	if r, err = c.inlineResult(result); err != nil {
		t.Fatal(err)
	}
	if p := r.GetThumb().GetPredicateName(); p != mtproto.Predicate_webDocumentNoProxy {
		t.Errorf("without a key: %s", p)
	}
}
