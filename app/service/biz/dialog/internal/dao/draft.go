// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
)

func (d *Dao) GetNoCacheAllDraftIdList(ctx context.Context, userId int64) ([]int64, error) {
	var (
		draftIdList []int64
	)

	_, err := d.DialogsDAO.SelectAllDraftsWithCB(
		ctx,
		userId,
		func(sz, i int, v *dataobject.DialogsDO) {
			if i == 0 {
				draftIdList = make([]int64, 0, sz)
			}
			draftIdList = append(draftIdList, v.Id)
		})
	if err != nil {
		return nil, err
	}

	if draftIdList == nil {
		draftIdList = make([]int64, 0)
	}

	return draftIdList, nil
}

func (d *Dao) GetAllDraftIdList(ctx context.Context, userId int64) ([]int64, error) {
	var (
		draftIdList []int64
	)

	err := d.CachedConn.QueryRow(
		ctx,
		&draftIdList,
		dialog.GetAllDraftIdListCacheKey(userId),
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			idList, err := d.GetNoCacheAllDraftIdList(ctx, userId)
			if err != nil {
				return err
			}
			*(v.(*[]int64)) = idList

			return nil
		})
	if err != nil {
		return nil, err
	}

	if draftIdList == nil {
		draftIdList = make([]int64, 0)
	}

	return draftIdList, nil
}

// GetDraftMessage returns draft message by id.
func (d *Dao) GetDraftMessage(ctx context.Context, userId, peerDialogId int64) (*mtproto.DraftMessage, error) {
	dlgExt, err := d.GetDialogByPeerDialogId(ctx, userId, peerDialogId)
	if err != nil {
		return nil, err
	}

	return dlgExt.GetDialog().GetDraft(), nil
}

func (d *Dao) GetAllDraftMessageList(ctx context.Context, userId int64) ([]*mtproto.DraftMessage, error) {
	idList, err := d.GetAllDraftIdList(ctx, userId)
	if err != nil {
		return nil, err
	}

	dlgExtList, err := d.GetDialogListByIdList(ctx, userId, idList)
	if err != nil {
		return nil, err
	}

	vList := make([]*mtproto.DraftMessage, 0, len(idList))
	for _, dlgExt := range dlgExtList {
		if dlgExt.GetDialog().GetDraft() != nil {
			vList = append(vList, dlgExt.GetDialog().GetDraft())
		}
	}

	return vList, nil
}

// SaveDialogDraft stores userId's draft of the dialog with the peer, a row
// of its dialogs. A chat without a dialog yet gets a hidden row (deleted),
// which holds the draft and shows no chat: messages.getAllDrafts gives it,
// and the first message brings the dialog up with it (task 063).
func (d *Dao) SaveDialogDraft(ctx context.Context, userId int64, peerType int32, peerId int64, data string, date int64) error {
	_, _, err := d.CachedConn.Exec(ctx, func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
		_, err := conn.Exec(ctx,
			"INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_type, draft_message_data, date2, deleted) VALUES (?, ?, ?, ?, 2, ?, ?, 1)"+
				" ON DUPLICATE KEY UPDATE draft_type = 2, draft_message_data = VALUES(draft_message_data)",
			userId, peerType, peerId, mtproto.MakePeerDialogId(peerType, peerId), data, date)
		return 0, 0, err
	}, dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId), dialog.GetAllDraftIdListCacheKey(userId))
	return err
}

// ClearDialogDraft clears userId's draft of the dialog with the peer and
// tells whether there was one.
func (d *Dao) ClearDialogDraft(ctx context.Context, userId int64, peerType int32, peerId int64, emptyData string) (bool, error) {
	var cleared bool
	_, _, err := d.CachedConn.Exec(ctx, func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
		r, err := conn.Exec(ctx,
			"UPDATE dialogs SET draft_type = 1, draft_message_data = ? WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND draft_type = 2",
			emptyData, userId, peerType, peerId)
		if err != nil {
			return 0, 0, err
		}
		n, err := r.RowsAffected()
		cleared = n > 0
		return 0, n, err
	}, dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId), dialog.GetAllDraftIdListCacheKey(userId))
	return cleared, err
}

// threadDraftRow is a draft of a topic.
type threadDraftRow struct {
	PeerType         int32  `db:"peer_type"`
	PeerId           int64  `db:"peer_id"`
	TopMsgId         int32  `db:"top_msg_id"`
	DraftMessageData string `db:"draft_message_data"`
}

// ThreadDrafts returns userId's drafts of topics.
func (d *Dao) ThreadDrafts(ctx context.Context, userId int64) ([]threadDraftRow, error) {
	var rows []threadDraftRow
	err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT peer_type, peer_id, top_msg_id, draft_message_data FROM thread_drafts WHERE user_id = ?", userId)
	return rows, err
}

// SaveThreadDraft stores userId's draft of a topic.
func (d *Dao) SaveThreadDraft(ctx context.Context, userId int64, peerType int32, peerId int64, topMsgId int32, data string, date int64) error {
	_, err := d.Mysql.DB.Exec(ctx,
		"INSERT INTO thread_drafts (user_id, peer_type, peer_id, top_msg_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE draft_message_data = VALUES(draft_message_data), date2 = VALUES(date2)",
		userId, peerType, peerId, topMsgId, data, date)
	return err
}

// ClearThreadDraft deletes userId's draft of a topic and tells whether there
// was one.
func (d *Dao) ClearThreadDraft(ctx context.Context, userId int64, peerType int32, peerId int64, topMsgId int32) (bool, error) {
	r, err := d.Mysql.DB.Exec(ctx, "DELETE FROM thread_drafts WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND top_msg_id = ?",
		userId, peerType, peerId, topMsgId)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// ClearThreadDrafts deletes every draft of userId's topics.
func (d *Dao) ClearThreadDrafts(ctx context.Context, userId int64) error {
	_, err := d.Mysql.DB.Exec(ctx, "DELETE FROM thread_drafts WHERE user_id = ?", userId)
	return err
}
