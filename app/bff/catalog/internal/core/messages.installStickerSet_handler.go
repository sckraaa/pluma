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
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
)

// MessagesInstallStickerSet
// messages.installStickerSet#c78fe460 stickerset:InputStickerSet archived:Bool = messages.StickerSetInstallResult;
//
// Installs a set at the top of its kind (or archives it if archived), as
// installed.Store.Install does; the account's other devices get the set
// whole (updateNewStickerSet). Nothing is ever archived to make room, so
// the answer is always stickerSetInstallResultSuccess.
func (c *CatalogCore) MessagesInstallStickerSet(in *mtproto.TLMessagesInstallStickerSet) (*mtproto.Messages_StickerSetInstallResult, error) {
	set, err := c.findSet("messages.installStickerSet", in.GetStickerset())
	if err != nil {
		return nil, err
	}
	ms, err := c.decodeSet("messages.installStickerSet", set)
	if err != nil {
		return nil, err
	}
	kind := installed.KindOf(ms.GetSet())
	err = c.svcCtx.Installed.Install(c.ctx, c.MD.UserId, kind, set.ID, mtproto.FromBool(in.GetArchived()),
		func(installedAt int64) *mtproto.Update {
			return mtproto.MakeTLUpdateNewStickerSet(&mtproto.Update{
				Stickerset_MESSAGES_STICKERSET: asInstalled(ms, installedAt),
			}).To_Update()
		})
	if err != nil {
		c.Logger.Errorf("messages.installStickerSet - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesStickerSetInstallResultSuccess(nil).To_Messages_StickerSetInstallResult(), nil
}
