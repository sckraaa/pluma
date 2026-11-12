// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/monoforum"
)

// MessagesClearAllDrafts
// messages.clearAllDrafts#7e58ee9c = Bool;
func (c *DraftsCore) MessagesClearAllDrafts(in *mtproto.TLMessagesClearAllDrafts) (*mtproto.Bool, error) {
	rValues, err := c.svcCtx.Dao.DialogClient.DialogClearAllDrafts(c.ctx, &dialog.TLDialogClearAllDrafts{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("messages.clearAllDrafts: %v", err)
		return nil, err
	}

	// The drafts in the sublists of monoforums go as well (task 084).
	if c.svcCtx.Sublists != nil {
		cleared, err := monoforum.ClearDrafts(c.ctx, c.svcCtx.Sublists, c.MD.UserId)
		if err != nil {
			c.Logger.Errorf("messages.clearAllDrafts: %v", err)
			return nil, err
		}
		for _, d := range cleared {
			syncUpdates := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{
				Peer_PEER:   mtproto.MakePeerChannel(d.MonoforumId),
				SavedPeerId: mtproto.MakePeerUser(d.PeerId),
				Draft:       mtproto.MakeTLDraftMessageEmpty(nil).To_DraftMessage(),
			}).To_Update())
			if err = c.addDraftPeer(syncUpdates, mtproto.PEER_CHANNEL, d.MonoforumId); err != nil {
				c.Logger.Errorf("messages.clearAllDrafts - error: %v", err)
				continue
			}
			if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
				UserId:        c.MD.UserId,
				PermAuthKeyId: c.MD.PermAuthKeyId,
				Updates:       syncUpdates,
			}); err != nil {
				c.Logger.Errorf("messages.clearAllDrafts - error: %v", err)
			}
		}
	}

	if len(rValues.Datas) == 0 {
		return mtproto.BoolTrue, nil
	}

	// sync
	for _, v := range rValues.Datas {
		syncUpdates := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{
			Peer_PEER:          v.Peer,
			TopMsgId_FLAGINT32: mtproto.MakeFlagsInt32(v.TopMsgId),
			Draft:              v.Draft,
		}).To_Update())
		peer := mtproto.FromPeer(v.Peer)
		if err = c.addDraftPeer(syncUpdates, peer.PeerType, peer.PeerId); err != nil {
			c.Logger.Errorf("messages.clearAllDrafts - error: %v", err)
			continue
		}
		if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
			UserId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			Updates:       syncUpdates,
		}); err != nil {
			c.Logger.Errorf("messages.clearAllDrafts - error: %v", err)
		}
	}

	return mtproto.BoolTrue, nil
}
