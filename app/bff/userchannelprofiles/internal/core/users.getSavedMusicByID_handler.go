// Copyright 2025 Teamgram Authors
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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// UsersGetSavedMusicByID
// users.getSavedMusicByID#7573a4e9 id:InputUser documents:Vector<InputDocument> = users.SavedMusic;
func (c *UserChannelProfilesCore) UsersGetSavedMusicByID(in *mtproto.TLUsersGetSavedMusicByID) (*mtproto.Users_SavedMusic, error) {
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetId())
	empty := mtproto.MakeTLUsersSavedMusic(&mtproto.Users_SavedMusic{
		Count:     0,
		Documents: []*mtproto.Document{},
	}).To_Users_SavedMusic()

	// Only the user's own saved music, and only when the user's privacy
	// lets the caller see it (task 034): any other document id is not
	// answered.
	visible, err := c.canSee(peer.PeerId, mtproto.SAVED_MUSIC)
	if err != nil {
		c.Logger.Errorf("users.getSavedMusicByID - error: %v", err)
		return nil, err
	}
	if !visible {
		return empty, nil
	}
	saved, err := c.svcCtx.Dao.UserClient.UserGetSavedMusicIdList(c.ctx, &user.TLUserGetSavedMusicIdList{
		UserId: peer.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("users.getSavedMusicByID - error: %v", err)
		return nil, err
	}
	idList := make([]int64, 0, len(in.GetDocuments()))
	for _, d := range in.GetDocuments() {
		for _, id := range saved.GetDatas() {
			if id == d.GetId() {
				idList = append(idList, id)
				break
			}
		}
	}
	if len(idList) == 0 {
		return empty, nil
	}

	dList, err := c.svcCtx.Dao.MediaClient.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{
		IdList: idList,
	})
	if err != nil {
		c.Logger.Errorf("users.getSavedMusicByID - error: %v", err)
		return nil, err
	}

	return mtproto.MakeTLUsersSavedMusic(&mtproto.Users_SavedMusic{
		Count:     int32(len(dList.GetDatas())),
		Documents: dList.GetDatas(),
	}).To_Users_SavedMusic(), nil
}
