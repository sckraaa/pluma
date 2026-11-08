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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// parseDraft reads a stored draft, nil for one that is empty.
func parseDraft(data string) (*mtproto.DraftMessage, error) {
	draft := &mtproto.DraftMessage{}
	if err := jsonx.UnmarshalFromString(data, draft); err != nil {
		return nil, err
	}
	if draft.GetPredicateName() != mtproto.Predicate_draftMessage {
		return nil, nil
	}
	return draft, nil
}

// DialogGetAllDrafts
// dialog.getAllDrafts user_id:long = Vector<PeerWithDraftMessage>;
//
// The drafts of the user's chats, those of chats with no dialog yet among
// them, and of topics.
func (c *DialogCore) DialogGetAllDrafts(in *dialog.TLDialogGetAllDrafts) (*dialog.Vector_PeerWithDraftMessage, error) {
	rValues := &dialog.Vector_PeerWithDraftMessage{
		Datas: []*dialog.PeerWithDraftMessage{},
	}
	add := func(peerType int32, peerId int64, topMsgId int32, draft *mtproto.DraftMessage) {
		if draft == nil {
			return
		}
		rValues.Datas = append(rValues.Datas, dialog.MakeTLUpdateDraftMessage(&dialog.PeerWithDraftMessage{
			Peer:     mtproto.MakePeer(peerType, peerId),
			Draft:    draft,
			TopMsgId: topMsgId,
		}).To_PeerWithDraftMessage())
	}
	var parseErr error
	if _, err := c.svcCtx.Dao.DialogsDAO.SelectAllDraftsWithCB(
		c.ctx,
		in.UserId,
		func(sz, i int, v *dataobject.DialogsDO) {
			draft, err := parseDraft(v.DraftMessageData)
			if err != nil {
				parseErr = err
				return
			}
			add(v.PeerType, v.PeerId, 0, draft)
		}); err != nil {
		c.Logger.Errorf("dialog.getAllDrafts - error: %v", err)
		return nil, err
	}
	if parseErr != nil {
		c.Logger.Errorf("dialog.getAllDrafts - error: %v", parseErr)
		return nil, parseErr
	}
	threads, err := c.svcCtx.Dao.ThreadDrafts(c.ctx, in.UserId)
	if err != nil {
		c.Logger.Errorf("dialog.getAllDrafts - error: %v", err)
		return nil, err
	}
	for _, t := range threads {
		draft, err := parseDraft(t.DraftMessageData)
		if err != nil {
			c.Logger.Errorf("dialog.getAllDrafts - error: %v", err)
			return nil, err
		}
		add(t.PeerType, t.PeerId, t.TopMsgId, draft)
	}
	return rValues, nil
}
