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
	"math"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
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
		secret      *secretchat.Difference
	)
	if updatesDiff.GetPredicateName() != updates.Predicate_differenceTooLong {
		// The queue of this authorization; a client that fails to get
		// it asks again.
		if secret, err = c.svcCtx.Dao.SecretChats.Difference(c.ctx, secretchat.DifferenceInput{
			Device:    c.device(),
			Qts:       in.Qts,
			Date:      in.Date,
			OnlyQueue: in.Date == math.MaxInt32,
		}); err != nil {
			c.Logger.Errorf("updates.getDifference - secret chats of user %d: %v", c.MD.UserId, err)
			return nil, mtproto.ErrInternalServerError
		}
	}

	switch updatesDiff.GetPredicateName() {
	case updates.Predicate_differenceEmpty:
		if secret.Empty(in.Qts) {
			return mtproto.MakeTLUpdatesDifferenceEmpty(&mtproto.Updates_Difference{
				Date: updatesDiff.GetState().GetDate(),
				Seq:  updatesDiff.GetState().GetSeq(),
			}).To_Updates_Difference(), nil
		}
		// A difference only of the secret chats, at the same pts.
		rDifference = mtproto.MakeTLUpdatesDifference(&mtproto.Updates_Difference{
			NewMessages:          []*mtproto.Message{},
			NewEncryptedMessages: []*mtproto.EncryptedMessage{},
			OtherUpdates:         []*mtproto.Update{},
			State: mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
				Pts:  in.Pts,
				Date: updatesDiff.GetState().GetDate(),
				Seq:  updatesDiff.GetState().GetSeq(),
			}).To_Updates_State(),
		}).To_Updates_Difference()
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

	if secret != nil {
		rDifference = c.withSecretChats(rDifference, secret)
		for _, u := range secret.Updates {
			idHelper.AppendUsers(u.Chat.AdminId, u.Chat.ParticipantId)
		}
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

// withSecretChats adds to the difference d the page of the queue and the
// changes of the chats, the qts of the state being the page's. An unfinished
// queue makes it a differenceSlice, as a long box does: the client asks
// again from the qts it was given. A finished one marks the state given.
func (c *UpdatesCore) withSecretChats(d *mtproto.Updates_Difference, s *secretchat.Difference) *mtproto.Updates_Difference {
	me := c.device()
	for _, m := range s.Messages {
		d.NewEncryptedMessages = append(d.NewEncryptedMessages, m.Message())
	}
	for _, u := range s.Updates {
		d.OtherUpdates = append(d.OtherUpdates, secretUpdate(u, me))
	}
	switch {
	case d.GetPredicateName() == mtproto.Predicate_updates_differenceSlice:
		d.IntermediateState.Qts = s.Qts
	case s.More:
		state := d.State
		state.Qts = s.Qts
		d = mtproto.MakeTLUpdatesDifferenceSlice(&mtproto.Updates_Difference{
			NewMessages:          d.NewMessages,
			NewEncryptedMessages: d.NewEncryptedMessages,
			OtherUpdates:         d.OtherUpdates,
			Chats:                d.Chats,
			Users:                d.Users,
			IntermediateState:    state,
		}).To_Updates_Difference()
	default:
		d.State.Qts = s.Qts
		if err := c.svcCtx.Dao.SecretChats.MarkDifference(c.ctx, me); err != nil {
			c.Logger.Errorf("updates.getDifference - state of user %d: %v", c.MD.UserId, err)
		}
	}
	return d
}

// secretUpdate is the update of one change of a chat for me.
func secretUpdate(u *secretchat.DifferenceUpdate, me secretchat.Device) *mtproto.Update {
	if u.ReadAt != 0 {
		return mtproto.MakeTLUpdateEncryptedMessagesRead(&mtproto.Update{
			ChatId_INT32: u.Chat.Id, MaxDate: u.ReadMax, Date_INT32: u.ReadAt,
		}).To_Update()
	}
	return u.Chat.UpdateEncryption(me, u.Chat.StateAt)
}
