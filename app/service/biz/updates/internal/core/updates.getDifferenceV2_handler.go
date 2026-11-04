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
	"math"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/internal/dao"
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
// or getDifference. With sliceRows or fewer, the reply is a difference up
// to S.pts, even with no rows (a jump of the state, e.g. by the migration of
// user_state). The rows are folded as Telegram answers (differenceBuilder);
// the seq of the state is the one updates.getState gives.
//
// The last page also brings the updates with seq up to S.seq that the
// authorization may have missed (ADR 0020): those after the state it was
// last given (its cursor), and those written since its date, less the
// longest a box transaction takes: a device that moved its date with other
// updates is found by the cursor, a lost reply that moved the cursor by the
// date. They go where they happened among the journal updates. A request
// with date INT32_MAX (pts only: TDLib, the iOS notification extension)
// gets none and leaves the cursor.
//
// The last page also marks with updateChannelTooLong the channels of the
// account that changed since the earlier of the request's date and its
// cursor's, less MaxRunTime (ADR 0034, p. 6; see channelsTooLong).
//
// pts_total_limit is the client's (ADR 0019): with it, a range longer than
// it is differenceTooLong. A client ahead of the box gets the box's state.
func (c *UpdatesCore) UpdatesGetDifferenceV2(in *updates.TLUpdatesGetDifferenceV2) (*updates.Difference, error) {
	st, err := userbox.Read(c.ctx, c.svcCtx.Dao.BoxDB, in.UserId)
	if err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	now := time.Now().Unix()
	state := func(pts int32, date int64) *mtproto.Updates_State {
		return mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
			Pts:  pts,
			Date: int32(date),
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
	ahead := in.Pts > st.Pts
	var rows []dataobject.UserPtsUpdatesDO
	if !ahead {
		// One row more than a page tells whether this page is the last.
		if rows, err = c.svcCtx.Dao.UserPtsUpdatesDAO.SelectByPtsRange(c.ctx, in.UserId, in.Pts, st.Pts, sliceRows+1); err != nil {
			c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
	}
	end := st.Pts
	full := len(rows) > int(sliceRows)
	if full {
		rows = rows[:sliceRows]
		end = rows[len(rows)-1].Pts
	}

	var seq seqRange
	if !full {
		if seq, err = c.seqRange(in, st); err != nil {
			c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
	}
	var channels []*mtproto.Update
	if !full {
		if channels, err = c.channelsTooLong(in, seq); err != nil {
			c.Logger.Errorf("updates.getDifferenceV2 - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
	}
	if len(rows) == 0 && len(seq.updates) == 0 && len(channels) == 0 && in.Pts == st.Pts {
		c.setSeqCursor(in, st, seq)
		return updates.MakeTLDifferenceEmpty(&updates.Difference{State: state(st.Pts, now)}).To_Difference(), nil
	}

	b := newDifferenceBuilder(end)
	next := 0
	for i := range rows {
		// An update with seq goes after the journal updates committed
		// before it, up to the pts it was written at.
		for ; next < len(seq.updates) && seq.updates[next].pts < rows[i].Pts; next++ {
			b.addSeq(seq.updates[next].update)
		}
		if update := c.ptsUpdate(&rows[i]); update != nil {
			b.add(update)
		}
	}
	for ; next < len(seq.updates); next++ {
		b.addSeq(seq.updates[next].update)
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
		// The next page is asked with this date: it brings the updates
		// with seq since it.
		date := in.Date
		if date <= 0 || date > now {
			date = now
		}
		return updates.MakeTLDifferenceSlice(&updates.Difference{
			NewMessages:       b.messages(),
			OtherUpdates:      b.otherUpdates(),
			IntermediateState: state(end, date),
		}).To_Difference(), nil
	}
	c.setSeqCursor(in, st, seq)
	return updates.MakeTLDifference(&updates.Difference{
		NewMessages:  b.messages(),
		OtherUpdates: append(b.otherUpdates(), channels...),
		State:        state(st.Pts, now),
	}).To_Difference(), nil
}

// seqUpdate is an update of the seq journal with the pts of the box it was
// written at.
type seqUpdate struct {
	pts    int32
	update *mtproto.Update
}

// seqRange is what the last page of a difference reads of the seq journal.
type seqRange struct {
	asked     bool // the request takes updates with seq
	cursor    dao.SeqCursor
	hasCursor bool
	updates   []seqUpdate
}

// seqRange reads the updates with seq the authorization of in may have
// missed, up to the state st.
func (c *UpdatesCore) seqRange(in *updates.TLUpdatesGetDifferenceV2, st userbox.State) (seqRange, error) {
	if in.Date >= math.MaxInt32 {
		return seqRange{}, nil
	}
	r := seqRange{asked: true}
	after := st.Seq // without a cursor, by the date alone
	if in.AuthKeyId != 0 {
		cursor, ok, err := c.svcCtx.Dao.SeqCursor(c.ctx, in.UserId, in.AuthKeyId)
		if err != nil {
			return seqRange{}, err
		}
		r.cursor, r.hasCursor = cursor, ok
		// A cursor above the state was given before a restore of the
		// database: the date alone tells what the device has.
		if ok && cursor.Seq <= st.Seq {
			after = cursor.Seq
		}
	}
	since := in.Date - int64(userbox.MaxRunTime/time.Second)
	rows, err := c.svcCtx.Dao.SeqUpdates(c.ctx, in.UserId, in.AuthKeyId, st.Seq, after, since)
	if err != nil {
		return seqRange{}, err
	}
	for i := range rows {
		if u := c.seqUpdate(&rows[i]); u != nil {
			r.updates = append(r.updates, seqUpdate{pts: rows[i].Pts, update: u})
		}
	}
	return r, nil
}

// cursorRefresh is how old the date of an unchanged cursor may get before a
// difference stores it again: the date bounds the channels marked with
// updateChannelTooLong (channelsTooLong), so a stale one would mark more of
// them each time.
const cursorRefresh = time.Minute

// setSeqCursor stores the state st as given to the authorization of in,
// once the reply has brought it every update with seq up to st.seq. A
// failure is logged: the next difference returns some updates again.
func (c *UpdatesCore) setSeqCursor(in *updates.TLUpdatesGetDifferenceV2, st userbox.State, r seqRange) {
	now := time.Now().Unix()
	if !r.asked || in.AuthKeyId == 0 ||
		(r.hasCursor && r.cursor.Seq == st.Seq && r.cursor.Date >= now-int64(cursorRefresh/time.Second)) {
		return
	}
	if err := c.svcCtx.Dao.SetSeqCursor(c.ctx, in.UserId, in.AuthKeyId, st.Seq, now); err != nil {
		c.Logger.Errorf("updates.getDifferenceV2 - seq cursor of user %d: %v", in.UserId, err)
	}
}

// seqUpdate decodes a row of the seq journal; nil if it is broken.
func (c *UpdatesCore) seqUpdate(r *dao.SeqRow) *mtproto.Update {
	update := &mtproto.Update{}
	if err := jsonx.UnmarshalFromString(r.UpdateData, update); err != nil {
		c.Logger.Errorf("unmarshal the seq row %d: %v", r.Seq, err)
		return nil
	}
	if update.PredicateName != r.UpdateType {
		c.Logger.Errorf("the seq row %d: update %s, want %s", r.Seq, update.PredicateName, r.UpdateType)
		return nil
	}
	return update.FixData()
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

// channelsTooLong is the updateChannelTooLong of the last page of a
// difference (ADR 0034, p. 6): one, with the channel's pts, for each
// channel the account is in whose box changed since the earlier of the
// request's date and the date of its cursor, less MaxRunTime. A channel's
// updates reach a device by push only, so the client learns here where to
// call updates.getChannelDifference: the cursor finds the change whose push
// was lost while tdesktop moved its date with other updates (ADR 0020), and
// MaxRunTime the transaction that committed after its date. The pts lets
// Android, tdesktop and iOS skip a channel they have caught up with
// (MessagesController.java:19231-19233, api_updates.cpp:2484-2485,
// AccountStateManagementUtils.swift:952-957). Telegram's rule is not
// verified (U2): a channel marked in excess costs a call, not an update. A
// request without updates with seq (date INT32_MAX) gets none.
func (c *UpdatesCore) channelsTooLong(in *updates.TLUpdatesGetDifferenceV2, seq seqRange) ([]*mtproto.Update, error) {
	if !seq.asked {
		return nil, nil
	}
	since := in.Date
	if seq.hasCursor {
		since = min(since, seq.cursor.Date)
	}
	changed, err := c.svcCtx.Dao.ChangedChannels(c.ctx, in.UserId, since-int64(userbox.MaxRunTime/time.Second))
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.Update, 0, len(changed))
	for _, ch := range changed {
		out = append(out, mtproto.MakeTLUpdateChannelTooLong(&mtproto.Update{
			ChannelId:     ch.Id,
			Pts_FLAGINT32: mtproto.MakeFlagsInt32(ch.Pts),
		}).To_Update())
	}
	return out, nil
}
