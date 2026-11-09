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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// fakeFiles has photo and document 5, with the access hash 50.
type fakeFiles struct {
	media_client.MediaClient
}

func (fakeFiles) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: in.PhotoId, AccessHash: 50}).To_Photo(), nil
}

func (fakeFiles) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: in.Id, AccessHash: 50}).To_Document(), nil
}

// A bot gives out a file only with its access hash.
func TestInlineMediaNeedsAccessHash(t *testing.T) {
	c := testCore(&dao.Dao{MediaClient: fakeFiles{}}, 20)
	send := mtproto.MakeTLInputBotInlineMessageMediaAuto(&mtproto.InputBotInlineMessage{}).To_InputBotInlineMessage()
	photo := func(hash int64) *mtproto.InputBotInlineResult {
		return mtproto.MakeTLInputBotInlineResultPhoto(&mtproto.InputBotInlineResult{Id: "a", Type: "photo", SendMessage: send,
			Photo: mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: hash}).To_InputPhoto()}).To_InputBotInlineResult()
	}
	document := func(hash int64) *mtproto.InputBotInlineResult {
		return mtproto.MakeTLInputBotInlineResultDocument(&mtproto.InputBotInlineResult{Id: "a", Type: "file", SendMessage: send,
			Document: mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: 5, AccessHash: hash}).To_InputDocument()}).To_InputBotInlineResult()
	}
	if _, err := c.inlineResult(photo(50)); err != nil {
		t.Errorf("photo: %v", err)
	}
	if _, err := c.inlineResult(document(50)); err != nil {
		t.Errorf("document: %v", err)
	}
	if _, err := c.inlineResult(photo(1)); !errors.Is(err, mtproto.ErrPhotoInvalid) {
		t.Errorf("photo, wrong hash: %v", err)
	}
	if _, err := c.inlineResult(document(1)); !errors.Is(err, mtproto.ErrDocumentInvalid) {
		t.Errorf("document, wrong hash: %v", err)
	}
}
