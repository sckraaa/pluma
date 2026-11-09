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
	"github.com/teamgram/proto/mtproto"
)

// saveDocument records a stored file as a document (with its thumbnails),
// so that media.getDocument finds it by its id later: account.createTheme
// asks for the file of a theme by the id of the document uploaded.
func saveDocument(c *MediaCore, fileName string, document *mtproto.Document) error {
	if len(document.GetThumbs()) > 0 {
		if err := c.svcCtx.Dao.SavePhotoSizeV2(c.ctx, document.GetId(), document.GetThumbs()); err != nil {
			return err
		}
	}
	c.svcCtx.Dao.SaveDocumentV2(c.ctx, fileName, document)
	return nil
}
