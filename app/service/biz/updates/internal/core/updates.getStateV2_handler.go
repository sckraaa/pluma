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
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// UpdatesGetStateV2
// updates.getStateV2 auth_key_id:long user_id:long = updates.State;
//
// The state is the committed state of the user's box (user_state): every
// pts up to it is in the journal. The client starts from it, so it becomes
// the authorization's seq cursor (ADR 0020): a later difference brings the
// updates with seq after it. A failure to store it is logged: the
// difference then goes by the date.
func (c *UpdatesCore) UpdatesGetStateV2(in *updates.TLUpdatesGetStateV2) (*mtproto.Updates_State, error) {
	st, err := userbox.Read(c.ctx, c.svcCtx.Dao.BoxDB, in.UserId)
	if err != nil {
		c.Logger.Errorf("updates.getStateV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	now := time.Now().Unix()
	if in.AuthKeyId != 0 {
		if err = c.svcCtx.Dao.SetSeqCursor(c.ctx, in.UserId, in.AuthKeyId, st.Seq, now); err != nil {
			c.Logger.Errorf("updates.getStateV2 - seq cursor of user %d: %v", in.UserId, err)
		}
	}
	return mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
		Pts:         st.Pts,
		Qts:         st.Qts,
		Seq:         stateSeq(st),
		Date:        int32(now),
		UnreadCount: 0,
	}).To_Updates_State(), nil
}

// stateSeq is the seq of the state that updates.getState and every
// difference give: one value, or a client that takes the seq of a
// difference would see a gap in the next update with seq (ADR 0019).
func stateSeq(st userbox.State) int32 {
	return st.Seq
}
