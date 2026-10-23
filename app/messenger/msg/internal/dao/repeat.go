package dao

import (
	"encoding/json"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// A send repeated with the same random_id (ADR 0012, p. 6): a client that
// lost the reply, or the same request from two devices, gets the reply of
// the first send instead of a second message. The sender's copies are
// unique by (user_id, out_random_id), for good, without a time window.

type sentRow struct {
	UserMessageBoxId int32 `db:"user_message_box_id"`
	RandomId         int64 `db:"random_id"`
	DialogId1        int64 `db:"dialog_id1"`
	DialogId2        int64 `db:"dialog_id2"`
	Deleted          bool  `db:"deleted"`
	Pts              int32 `db:"pts"`
}

type sentUpdate struct {
	Pts      int32  `db:"pts"`
	PtsCount int32  `db:"pts_count"`
	Data     string `db:"update_data"`
}

// SentBefore looks for the sender's messages with randomIds in b's box,
// under the box lock, so a repeat waits for the first send to commit and
// then sees it:
//
//   - none of them: nil, the send goes on;
//   - all of them, in the dialog with peer, not deleted, written since
//     task 003 (pts > 0): their boxes as the first send's reply had them,
//     with their pts and pts_count from the journal;
//   - otherwise (some only, another dialog, deleted, older):
//     RANDOM_ID_DUPLICATE, as in Telegram.
func (d *Dao) SentBefore(b *userbox.Box, peerType int32, peerId int64, randomIds []int64) ([]*mtproto.MessageBox, error) {
	if len(randomIds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(randomIds)+1)
	args = append(args, b.UserId())
	for _, id := range randomIds {
		args = append(args, id)
	}
	var rows []sentRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT user_message_box_id, random_id, dialog_id1, dialog_id2, deleted, pts FROM messages WHERE user_id = ? AND out_random_id IN ("+inList(len(randomIds))+")",
		args...); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != len(randomIds) {
		return nil, mtproto.ErrRandomIdDuplicate
	}
	did := boxPeer{PeerType: peerType, PeerId: peerId}.dialogId(b.UserId())
	byRandom := make(map[int64]sentRow, len(rows))
	ptsArgs := make([]any, 0, len(rows)+1)
	ptsArgs = append(ptsArgs, b.UserId())
	for _, r := range rows {
		if r.DialogId1 != did.A || r.DialogId2 != did.B || r.Deleted || r.Pts <= 0 {
			return nil, mtproto.ErrRandomIdDuplicate
		}
		byRandom[r.RandomId] = r
		ptsArgs = append(ptsArgs, r.Pts)
	}

	var updates []sentUpdate
	if err := b.Tx().QueryRowsPartial(&updates,
		"SELECT pts, pts_count, update_data FROM user_pts_updates WHERE user_id = ? AND pts IN ("+inList(len(rows))+")",
		ptsArgs...); err != nil {
		return nil, err
	}
	byPts := make(map[int32]sentUpdate, len(updates))
	for _, u := range updates {
		byPts[u.Pts] = u
	}
	boxes := make([]*mtproto.MessageBox, 0, len(randomIds))
	for _, randomId := range randomIds {
		r := byRandom[randomId]
		u, ok := byPts[r.Pts]
		if !ok {
			return nil, mtproto.ErrRandomIdDuplicate
		}
		update := &mtproto.Update{}
		if err := json.Unmarshal([]byte(u.Data), update); err != nil {
			return nil, err
		}
		message := update.GetMessage_MESSAGE()
		if update.GetPredicateName() != mtproto.Predicate_updateNewMessage || message.GetId() != r.UserMessageBoxId {
			return nil, mtproto.ErrRandomIdDuplicate
		}
		boxes = append(boxes, &mtproto.MessageBox{
			UserId:       b.UserId(),
			MessageId:    r.UserMessageBoxId,
			SenderUserId: b.UserId(),
			PeerType:     peerType,
			PeerId:       peerId,
			RandomId:     randomId,
			DialogId1:    r.DialogId1,
			DialogId2:    r.DialogId2,
			Message:      message,
			Pts:          u.Pts,
			PtsCount:     u.PtsCount,
		})
	}
	return boxes, nil
}

// RepeatedRandomIds tells whether a request carries a random_id twice:
// its second message could never be written.
func RepeatedRandomIds(randomIds []int64) bool {
	sorted := slices.Clone(randomIds)
	slices.Sort(sorted)
	return len(slices.Compact(sorted)) != len(randomIds)
}
