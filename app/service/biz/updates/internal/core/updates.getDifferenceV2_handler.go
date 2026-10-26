/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// sliceRows is the most journal rows one answer folds: a longer range is
// answered in differenceSlice pages (Telegram recommends 1000 to 10000).
var sliceRows int32 = 1000

// UpdatesGetDifferenceV2
// updates.getDifferenceV2 flags:# auth_key_id:long user_id:long pts:int pts_total_limit:flags.0?int date:long = Difference;
//
// It reads the committed state S of the box first and then the journal rows
// in (pts, S.pts]: every row up to S.pts committed before S became visible,
// so the rows are complete; rows committed after are left for the next push
// or getDifference. With fewer than sliceRows, the reply is a difference up
// to S.pts, even with no rows (a jump of the state, e.g. by the migration of
// user_state). The rows are folded as Telegram answers (differenceBuilder);
// the seq of the state is the one updates.getState gives.
//
// pts_total_limit is the client's (ADR 0019): with it, a range longer than
// it is differenceTooLong. A client ahead of the box gets the box's state.
func (c *UpdatesCore) UpdatesGetDifferenceV2(in *updates.TLUpdatesGetDifferenceV2) (*updates.Difference, error) {
	st, err := userbox.Read(c.ctx, c.svcCtx.Dao.BoxDB, in.UserId)
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	state := func(pts int32) *mtproto.Updates_State {
		return mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
			Pts:  pts,
			Date: int32(time.Now().Unix()),
			Seq:  stateSeq(st),
		}).To_Updates_State()
	}
	// The client asks for differenceTooLong over pts_total_limit (Android on
	// its first getDifference, Web K) and then reloads its dialogs. Without
	// the limit there is none: Telegram Desktop does not support it, and the
	// notification extension of iOS crashes on it.
	if limit := in.GetPtsTotalLimit().GetValue(); limit > 0 && int64(in.Pts)+int64(limit) < int64(st.Pts) {
		return updates.MakeTLDifferenceTooLong(&updates.Difference{Pts: st.Pts}).To_Difference(), nil
	}
	// A client ahead of the box (it was given a pts the box no longer has,
	// e.g. after a restore from a backup) takes the box's lower pts from a
	// difference; differenceEmpty would leave it ahead, dropping every push
	// with a pts it believes it has.
	if in.Pts > st.Pts {
		return updates.MakeTLDifference(&updates.Difference{State: state(st.Pts)}).To_Difference(), nil
	}
	rows, err := c.svcCtx.Dao.UserPtsUpdatesDAO.SelectByPtsRange(c.ctx, in.UserId, in.Pts, st.Pts, sliceRows)
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if len(rows) == 0 && in.Pts == st.Pts {
		return updates.MakeTLDifferenceEmpty(&updates.Difference{State: state(st.Pts)}).To_Difference(), nil
	}

	end := st.Pts
	full := len(rows) >= int(sliceRows)
	if full {
		end = rows[len(rows)-1].Pts
	}
	b := newDifferenceBuilder(end)
	for i := range rows {
		if update := c.ptsUpdate(&rows[i]); update != nil {
			b.add(update)
		}
	}
	randomIds, err := c.svcCtx.Dao.OwnRandomIds(c.ctx, in.UserId, b.newMessageIds())
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	b.addMessageIds(randomIds)
	for _, r := range b.inboxReads() {
		if r.StillUnreadCount, err = c.svcCtx.Dao.UnreadAfter(c.ctx, in.UserId, r.GetPeer_PEER(), r.GetMaxId(), end); err != nil {
			c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
	}

	if full {
		return updates.MakeTLDifferenceSlice(&updates.Difference{
			NewMessages:       b.messages(),
			OtherUpdates:      b.otherUpdates(),
			IntermediateState: state(end),
		}).To_Difference(), nil
	}
	return updates.MakeTLDifference(&updates.Difference{
		NewMessages:  b.messages(),
		OtherUpdates: b.otherUpdates(),
		State:        state(st.Pts),
	}).To_Difference(), nil
}

// ptsUpdate decodes a journal row; nil if it is broken.
func (c *UpdatesCore) ptsUpdate(do *dataobject.UserPtsUpdatesDO) *mtproto.Update {
	update := &mtproto.Update{}
	if err := jsonx.UnmarshalFromString(do.UpdateData, update); err != nil {
		c.Logger.Errorf("unmarshal the journal row of pts %d: %v", do.Pts, err)
		return nil
	}
	if mtproto.GetUpdateType(update) != do.UpdateType {
		c.Logger.Errorf("the journal row of pts %d: update type %d, want %d", do.Pts, mtproto.GetUpdateType(update), do.UpdateType)
		return nil
	}
	return update.FixData()
}
