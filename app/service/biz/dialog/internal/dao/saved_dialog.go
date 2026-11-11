// Copyright 2024 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package dao

import (
	"context"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
)

func (d *Dao) MakeSavedDialog(do *dataobject.SavedDialogsDO) *mtproto.SavedDialog {
	return mtproto.MakeTLSavedDialog(&mtproto.SavedDialog{
		Pinned:     do.Pinned > 0,
		Peer:       mtproto.MakePeer(do.PeerType, do.PeerId),
		TopMessage: do.TopMessage,
	}).To_SavedDialog()
}

// SavedDialogsByIdList returns the dialogs of userId's Saved Messages with
// the peers of idList, in its order and once each. A peer with no messages
// left there has no dialog and is left out; one that is not a user, a chat
// or a channel has none. One query reads them all, however many peers the
// request names.
func (d *Dao) SavedDialogsByIdList(ctx context.Context, userId int64, idList []*mtproto.PeerUtil) ([]*mtproto.SavedDialog, error) {
	type peerKey struct {
		peerType int32
		peerId   int64
	}
	var (
		keys = make([]peerKey, 0, len(idList))
		seen = make(map[peerKey]bool, len(idList))
	)
	for _, p := range idList {
		switch p.GetPeerType() {
		case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		default:
			continue
		}
		k := peerKey{p.GetPeerType(), p.GetPeerId()}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return []*mtproto.SavedDialog{}, nil
	}

	args := make([]interface{}, 0, 1+2*len(keys))
	args = append(args, userId)
	for _, k := range keys {
		args = append(args, k.peerType, k.peerId)
	}
	var rows []dataobject.SavedDialogsDO
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, peer_type, peer_id, pinned, top_message FROM saved_dialogs WHERE user_id = ? AND deleted = 0 AND (peer_type, peer_id) IN ("+
			strings.TrimSuffix(strings.Repeat("(?, ?), ", len(keys)), ", ")+")",
		args...); err != nil {
		return nil, err
	}
	found := make(map[peerKey]*dataobject.SavedDialogsDO, len(rows))
	for i := range rows {
		found[peerKey{rows[i].PeerType, rows[i].PeerId}] = &rows[i]
	}

	dialogs := make([]*mtproto.SavedDialog, 0, len(rows))
	for _, k := range keys {
		if do := found[k]; do != nil {
			dialogs = append(dialogs, d.MakeSavedDialog(do))
		}
	}
	return dialogs, nil
}
