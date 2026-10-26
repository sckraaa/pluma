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

// UpdatesGetDifferenceV2
// updates.getDifferenceV2 flags:# auth_key_id:long user_id:long pts:int pts_total_limit:flags.0?int date:long = Difference;
//
// It reads the committed state S of the box first and then the journal rows
// in (pts, S.pts]: every row up to S.pts committed before S became visible,
// so the rows are complete; rows committed after are left for the next push
// or getDifference. With fewer than the limit, the reply is a difference up
// to S.pts, even with no rows (a jump of the state, e.g. by the migration of
// user_state). The rows are folded as Telegram answers (differenceBuilder);
// the seq of the state is the one updates.getState gives.
func (c *UpdatesCore) UpdatesGetDifferenceV2(in *updates.TLUpdatesGetDifferenceV2) (*updates.Difference, error) {
	limit := in.GetPtsTotalLimit().GetValue()
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}

	st, err := userbox.Read(c.ctx, c.svcCtx.Dao.BoxDB, in.UserId)
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	rows, err := c.svcCtx.Dao.UserPtsUpdatesDAO.SelectByPtsRange(c.ctx, in.UserId, in.Pts, st.Pts, limit)
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
	if len(rows) == 0 && in.Pts >= st.Pts {
		return updates.MakeTLDifferenceEmpty(&updates.Difference{State: state(st.Pts)}).To_Difference(), nil
	}

	end := st.Pts
	full := len(rows) >= int(limit)
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
