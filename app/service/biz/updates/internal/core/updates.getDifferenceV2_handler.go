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
// user_state).
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
	updateList, lastPts, lastSeq, lastDate, full, err := c.GetMergedUpdateList(in.UserId, in.AuthKeyId, in.Pts, st.Pts, int32(in.Date), limit)
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if lastDate == 0 {
		lastDate = time.Now().Unix()
	}
	if lastSeq == 0 {
		lastSeq = st.Seq
	}

	if len(updateList) == 0 && in.Pts >= st.Pts {
		return updates.MakeTLDifferenceEmpty(&updates.Difference{
			State: mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
				Pts:  st.Pts,
				Date: int32(lastDate),
				Seq:  lastSeq,
			}).To_Updates_State(),
		}).To_Difference(), nil
	}

	merge := newMergeUpdatesHelper()
	for _, update := range updateList {
		merge.merge(update, lastPts)
	}

	if full {
		return updates.MakeTLDifferenceSlice(&updates.Difference{
			NewMessages:  merge.newMessages,
			OtherUpdates: merge.toUpdates(),
			IntermediateState: mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
				Pts:  lastPts,
				Date: int32(lastDate),
				Seq:  lastSeq,
			}).To_Updates_State(),
		}).To_Difference(), nil
	}
	return updates.MakeTLDifference(&updates.Difference{
		NewMessages:  merge.newMessages,
		OtherUpdates: merge.toUpdates(),
		State: mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
			Pts:  st.Pts,
			Date: int32(lastDate),
			Seq:  lastSeq,
		}).To_Updates_State(),
	}).To_Difference(), nil
}

func (c *UpdatesCore) addPtsUpdate(updates []*mtproto.Update, do *dataobject.UserPtsUpdatesDO) []*mtproto.Update {
	update := &mtproto.Update{}
	err := jsonx.UnmarshalFromString(do.UpdateData, update)
	if err != nil {
		c.Logger.Errorf("unmarshal pts's update(%d)error: %v", do.Id, err)
		return updates
	}
	if mtproto.GetUpdateType(update) != do.UpdateType {
		c.Logger.Errorf("update data error.")
		return updates
	}
	updates = append(updates, update.FixData())
	return updates
}

func (c *UpdatesCore) addSeqUpdate(updates []*mtproto.Update, do *dataobject.AuthSeqUpdatesDO) []*mtproto.Update {
	update := &mtproto.Update{}
	err := jsonx.UnmarshalFromString(do.UpdateData, update)
	if err != nil {
		c.Logger.Errorf("unmarshal pts's update(%d)error: %v", do.Id, err)
		return updates
	}
	updates = append(updates, update.FixData())
	return updates
}

// GetMergedUpdateList returns the journal rows of the box in (pts, maxPts],
// at most limit, merged by date with the seq updates of the auth key after
// date, and the last pts, seq and date of what it returns. full reports that
// the journal rows reached the limit.
func (c *UpdatesCore) GetMergedUpdateList(userId int64, authId int64, pts, maxPts, date, limit int32) (list []*mtproto.Update, lastPts, lastSeq int32, lastDate int64, full bool, err error) {
	a, err := c.svcCtx.Dao.UserPtsUpdatesDAO.SelectByPtsRange(c.ctx, userId, pts, maxPts, limit)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	b, err := c.svcCtx.Dao.AuthSeqUpdatesDAO.SelectByGtDate(c.ctx, authId, userId, int64(date), limit)
	if err != nil {
		return nil, 0, 0, 0, false, err
	}
	full = len(a) >= int(limit)

	//创建结果集数组
	c2 := make([]*mtproto.Update, 0, len(a)+len(b))

	alen := len(a)
	blen := len(b)

	//默认数组从0开始
	aIndex := 0
	bIndex := 0
	cIndex := 0

	//开始遍历数组
	for aIndex < alen && bIndex < blen {
		if a[aIndex].Date2 < b[bIndex].Date2 {
			c2 = c.addPtsUpdate(c2, &a[aIndex])
			if a[aIndex].Pts > pts {
				pts = a[aIndex].Pts
			}
			// if a[aIndex].Date2 > lastDate {
			// 	lastDate = a[aIndex].Date2
			// }
			cIndex++
			aIndex++
		} else {
			c2 = c.addSeqUpdate(c2, &b[bIndex])
			if b[bIndex].Seq > lastSeq {
				lastSeq = b[bIndex].Seq
			}
			if b[bIndex].Date2 > lastDate {
				lastDate = b[bIndex].Date2
			}
			cIndex++
			bIndex++
		}
	}
	//当其中一个数组遍历完,另一个数组还没遍历完应.当将另一个数组遍历完
	for aIndex < alen {
		c2 = c.addPtsUpdate(c2, &a[aIndex])
		if a[aIndex].Pts > pts {
			pts = a[aIndex].Pts
		}
		// if a[aIndex].Date2 > lastDate {
		// 	lastDate = a[aIndex].Date2
		// }
		cIndex++
		aIndex++
	}
	for bIndex < blen {
		c2 = c.addSeqUpdate(c2, &b[bIndex])
		if b[bIndex].Seq > lastSeq {
			lastSeq = b[bIndex].Seq
		}
		if b[bIndex].Date2 > lastDate {
			lastDate = b[bIndex].Date2
		}
		cIndex++
		bIndex++
	}

	for i := 0; i < len(b); i++ {
		if b[i].Date2 > lastDate {
			lastDate = b[i].Date2
		}
	}

	return c2, pts, lastSeq, lastDate, full, nil
}
