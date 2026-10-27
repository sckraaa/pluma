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
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
)

// UpdatesGetDifference
// updates.getDifference#25939651 flags:# pts:int pts_total_limit:flags.0?int date:int qts:int = updates.Difference;
func (c *UpdatesCore) UpdatesGetDifference(in *mtproto.TLUpdatesGetDifference) (*mtproto.Updates_Difference, error) {
	// The difference is of the account's box; the authorization tells
	// which updates with seq it may have missed (its seq cursor, ADR 0020).
	updatesDiff, err := c.svcCtx.Dao.UpdatesClient.UpdatesGetDifferenceV2(c.ctx, &updates.TLUpdatesGetDifferenceV2{
		AuthKeyId:     c.MD.PermAuthKeyId,
		UserId:        c.MD.UserId,
		Pts:           in.Pts,
		PtsTotalLimit: in.PtsTotalLimit,
		Date:          int64(in.Date),
	})
	if err != nil {
		c.Logger.Errorf("updates.getDifference - error: %v", err)
		return nil, err
	}

	var (
		idHelper    = mtproto.NewIDListHelper(c.MD.UserId)
		rDifference *mtproto.Updates_Difference
	)

	switch updatesDiff.GetPredicateName() {
	case updates.Predicate_differenceEmpty:
		return mtproto.MakeTLUpdatesDifferenceEmpty(&mtproto.Updates_Difference{
			Date: updatesDiff.GetState().GetDate(),
			Seq:  updatesDiff.GetState().GetSeq(),
		}).To_Updates_Difference(), nil
	case updates.Predicate_difference:
		rDifference = mtproto.MakeTLUpdatesDifference(&mtproto.Updates_Difference{
			NewMessages:          updatesDiff.NewMessages,
			NewEncryptedMessages: []*mtproto.EncryptedMessage{},
			OtherUpdates:         updatesDiff.OtherUpdates,
			Chats:                nil,
			Users:                nil,
			State:                updatesDiff.State,
		}).To_Updates_Difference()
	case updates.Predicate_differenceSlice:
		rDifference = mtproto.MakeTLUpdatesDifferenceSlice(&mtproto.Updates_Difference{
			NewMessages:          updatesDiff.NewMessages,
			NewEncryptedMessages: []*mtproto.EncryptedMessage{},
			OtherUpdates:         updatesDiff.OtherUpdates,
			Chats:                nil,
			Users:                nil,
			IntermediateState:    updatesDiff.IntermediateState,
		}).To_Updates_Difference()

	case updates.Predicate_differenceTooLong:
		// Only when the client set pts_total_limit (biz, ADR 0019).
		rDifference = mtproto.MakeTLUpdatesDifferenceTooLong(&mtproto.Updates_Difference{
			Pts: updatesDiff.Pts,
		}).To_Updates_Difference()
	default:
		c.Logger.Errorf("updates.getDifference - difference %s", updatesDiff.GetPredicateName())
		return nil, mtproto.ErrInternalServerError
	}

	idHelper.PickByMessages(rDifference.NewMessages...)
	idHelper.PickByUpdates(rDifference.OtherUpdates...)
	seqpeers.Pick(idHelper, c.MD.UserId, rDifference.OtherUpdates...)

	// The difference goes with the peers found: a client asks again for
	// one it cannot resolve.
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, c.MD.UserId, idHelper)
	if err != nil {
		c.Logger.Errorf("updates.getDifference - users and chats of user %d: %v", c.MD.UserId, err)
	}
	rDifference.Users = peers.Users
	rDifference.Chats = peers.Chats

	return rDifference, nil
}
