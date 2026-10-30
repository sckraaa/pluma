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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MessagesSaveGif
// messages.saveGif#327a30cb id:InputDocument unsave:Bool = Bool;
//
// A GIF to save is a document with its access hash that clients show as a
// GIF; anything else is GIF_ID_INVALID (core.telegram.org/method/
// messages.saveGif). A GIF is unsaved by its id alone: the document may be
// gone. The account's devices get updateSavedGifs.
func (c *GifsCore) MessagesSaveGif(in *mtproto.TLMessagesSaveGif) (*mtproto.Bool, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.saveGif - error: %v", err)
		return nil, err
	}
	id := in.GetId()
	if id.GetPredicateName() != mtproto.Predicate_inputDocument || id.GetId() == 0 {
		err := mtproto.ErrGifIdInvalid
		c.Logger.Errorf("messages.saveGif - error: %v", err)
		return nil, err
	}
	unsave := mtproto.FromBool(in.GetUnsave())
	if !unsave {
		doc, err := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: id.GetId()})
		if err != nil {
			c.Logger.Errorf("messages.saveGif - error: %v", err)
			return nil, err
		}
		if !isGif(doc) || doc.GetAccessHash() != id.GetAccessHash() {
			err = mtproto.ErrGifIdInvalid
			c.Logger.Errorf("messages.saveGif - document %d: %v", id.GetId(), err)
			return nil, err
		}
	}
	if _, err := c.svcCtx.Dao.UserClient.UserSaveGif(c.ctx, &userpb.TLUserSaveGif{
		UserId: c.MD.UserId,
		Id:     id.GetId(),
		Unsave: mtproto.ToBool(unsave),
	}); err != nil {
		c.Logger.Errorf("messages.saveGif - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
