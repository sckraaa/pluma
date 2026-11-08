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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// DialogSaveDraftMessage
// dialog.saveDraftMessage user_id:long peer_type:int peer_id:long message:DraftMessage = Bool;
func (c *DialogCore) DialogSaveDraftMessage(in *dialog.TLDialogSaveDraftMessage) (*mtproto.Bool, error) {
	data, err := jsonx.MarshalToString(in.Message)
	if err != nil {
		c.Logger.Errorf("dialog.saveDraftMessage - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Dao.SaveDialogDraft(c.ctx, in.UserId, in.PeerType, in.PeerId, data, int64(in.Message.GetDate_INT32())); err != nil {
		c.Logger.Errorf("dialog.saveDraftMessage - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// DialogSaveThreadDraft
// dialog.saveThreadDraft user_id:long peer_type:int peer_id:long top_msg_id:int message:DraftMessage = Bool;
//
// The draft of a topic (updateDraftMessage.top_msg_id); draftMessageEmpty
// clears it. It tells whether anything changed.
func (c *DialogCore) DialogSaveThreadDraft(in *dialog.TLDialogSaveThreadDraft) (*mtproto.Bool, error) {
	if in.Message.GetPredicateName() != mtproto.Predicate_draftMessage {
		cleared, err := c.svcCtx.Dao.ClearThreadDraft(c.ctx, in.UserId, in.PeerType, in.PeerId, in.TopMsgId)
		if err != nil {
			c.Logger.Errorf("dialog.saveThreadDraft - error: %v", err)
			return nil, err
		}
		return mtproto.ToBool(cleared), nil
	}
	data, err := jsonx.MarshalToString(in.Message)
	if err != nil {
		c.Logger.Errorf("dialog.saveThreadDraft - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Dao.SaveThreadDraft(c.ctx, in.UserId, in.PeerType, in.PeerId, in.TopMsgId, data, int64(in.Message.GetDate_INT32())); err != nil {
		c.Logger.Errorf("dialog.saveThreadDraft - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
