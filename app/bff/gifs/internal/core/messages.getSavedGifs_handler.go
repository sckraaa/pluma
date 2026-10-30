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

// MessagesGetSavedGifs
// messages.getSavedGifs#5cf09635 hash:long = messages.SavedGifs;
//
// The saved GIFs of the account, the newest first; a hash equal to theirs
// is savedGifsNotModified.
func (c *GifsCore) MessagesGetSavedGifs(in *mtproto.TLMessagesGetSavedGifs) (*mtproto.Messages_SavedGifs, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getSavedGifs - error: %v", err)
		return nil, err
	}
	ids, err := c.svcCtx.Dao.UserClient.UserGetSavedGifIdList(c.ctx, &userpb.TLUserGetSavedGifIdList{UserId: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("messages.getSavedGifs - error: %v", err)
		return nil, err
	}
	gifs := []*mtproto.Document{}
	if len(ids.GetDatas()) > 0 {
		docs, err := c.svcCtx.Dao.MediaClient.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: ids.GetDatas()})
		if err != nil {
			c.Logger.Errorf("messages.getSavedGifs - error: %v", err)
			return nil, err
		}
		gifs = inOrder(ids.GetDatas(), docs.GetDatas())
	}
	hash := documentsHash(gifs)
	if in.GetHash() != 0 && in.GetHash() == hash {
		return mtproto.MakeTLMessagesSavedGifsNotModified(nil).To_Messages_SavedGifs(), nil
	}
	return mtproto.MakeTLMessagesSavedGifs(&mtproto.Messages_SavedGifs{
		Hash: hash,
		Gifs: gifs,
	}).To_Messages_SavedGifs(), nil
}
