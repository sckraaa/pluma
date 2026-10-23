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
// pts up to it is in the journal.
func (c *UpdatesCore) UpdatesGetStateV2(in *updates.TLUpdatesGetStateV2) (*mtproto.Updates_State, error) {
	st, err := userbox.Read(c.ctx, c.svcCtx.Dao.BoxDB, in.UserId)
	if err != nil {
		c.Logger.Errorf("updates.getStateV2 - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	seq := st.Seq
	if seq == 0 {
		seq = -1
	}
	return mtproto.MakeTLUpdatesState(&mtproto.Updates_State{
		Pts:         st.Pts,
		Qts:         st.Qts,
		Seq:         seq,
		Date:        int32(time.Now().Unix()),
		UnreadCount: 0,
	}).To_Updates_State(), nil
}
