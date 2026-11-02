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
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MediaImportDocument
// media.importDocument file_name:string document:Document = Bool;
//
// pluma: stores a document made by dfs.importDocument (task 024, ADR 0028)
// with its thumbnails as they are.
func (c *MediaCore) MediaImportDocument(in *media.TLMediaImportDocument) (*mtproto.Bool, error) {
	document := in.GetDocument()
	if document.GetPredicateName() != mtproto.Predicate_document || document.GetId() == 0 {
		return nil, mtproto.ErrMediaInvalid
	}
	if err := c.svcCtx.Dao.ImportDocument(c.ctx, in.GetFileName(), document); err != nil {
		c.Logger.Errorf("media.importDocument - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
