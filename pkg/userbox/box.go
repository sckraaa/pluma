package userbox

import (
	"context"
	"math"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Result is the outcome of a committed box transaction.
type Result struct {
	UserId int64
	// State is the committed state after the transaction. With nothing
	// allocated it is the state the transaction found.
	State State
	// Updates are the journaled updates, in ascending pts, with their
	// pts and pts_count set.
	Updates []*mtproto.Update
	// Emitted is the number of events the transaction wrote to mq_outbox.
	Emitted int
	// Taken is what TakeEvent decided in the transaction, 0 without it.
	// TakeDeferred returned rows to pending for the relay.
	Taken TakeOutcome
	// CacheKeys to delete after commit.
	CacheKeys []string
}

// journalRow is a user_pts_updates row written by the transaction.
type journalRow struct {
	pts        int32
	ptsCount   int32
	updateType int32
	data       string
}

// seqRow is a user_seq_updates row written by the transaction.
type seqRow struct {
	seq        int32
	pts        int32 // of the box when the update was added
	updateType string
	data       string
	except     int64 // the authorization that must not get it, 0 for none
}

// Box is one attempt of a box transaction, given to the function of Run.
// It is valid only inside that function.
type Box struct {
	ctx    context.Context
	rec    *recorder
	tx     *sqlx.Tx
	userId int64
	now    int64

	state State // locked at the start of the transaction
	pts   int32 // last allocated pts
	seq   int32 // last allocated seq
	boxId int32 // last issued message box id

	journal    []journalRow
	seqJournal []seqRow
	updates    []*mtproto.Update
	events     []outboxRow
	keys       []string
	taken      *takenEvent

	// err is a misuse of the Box (bad pts_count, an update that is not of
	// the common box, ids out of range, an invalid event): the transaction
	// is rolled back.
	err error
}

// Context is the context of the transaction attempt, marked as inside a box
// transaction: Run and Read refuse it.
func (b *Box) Context() context.Context { return b.ctx }

// Tx is the only way to the database inside the transaction: the generated
// DAOs' *Tx methods and tx.QueryRow/QueryRows for reads. It runs on the
// transaction's connection, sees its own writes, and stops at the first
// failed statement.
func (b *Box) Tx() *sqlx.Tx { return b.tx }

// UserId is the owner of the box.
func (b *Box) UserId() int64 { return b.userId }

// State is the state of the box locked at the start of the transaction.
func (b *Box) State() State { return b.state }

// Date is the unix time of the transaction, written to user_state.date2 and
// to the journal rows.
func (b *Box) Date() int64 { return b.now }

// NextMessageId issues the next user_message_box_id of the box. The id is
// spent only if the transaction commits.
func (b *Box) NextMessageId() int32 {
	if b.err != nil {
		return 0
	}
	if b.boxId == math.MaxInt32 {
		b.err = misuse("user %d: message box ids exhausted", b.userId)
		return 0
	}
	b.boxId++
	return b.boxId
}

// AddPtsUpdate allocates ptsCount pts for u, sets u's pts and pts_count and
// adds its journal row. u is written to the journal as it is now: it must be
// complete and must not change afterwards. A wrong update or count rolls the
// transaction back.
func (b *Box) AddPtsUpdate(u *mtproto.Update, ptsCount int32) {
	if b.err != nil {
		return
	}
	switch {
	case u == nil:
		b.err = misuse("nil update")
		return
	case ptsCount < 1:
		b.err = misuse("user %d: pts_count %d of %s", b.userId, ptsCount, u.PredicateName)
		return
	case b.pts > math.MaxInt32-ptsCount:
		b.err = misuse("user %d: pts exhausted", b.userId)
		return
	}
	updateType := mtproto.GetUpdateType(u)
	if !commonBoxUpdate(updateType) {
		b.err = misuse("%s is not an update of the common message box", u.PredicateName)
		return
	}

	b.pts += ptsCount
	u.Pts_INT32 = b.pts
	u.PtsCount = ptsCount
	data, err := jsonx.Marshal(u)
	if err != nil {
		b.err = misuse("encode %s: %v", u.PredicateName, err)
		return
	}
	b.journal = append(b.journal, journalRow{pts: b.pts, ptsCount: ptsCount, updateType: updateType, data: string(data)})
	b.updates = append(b.updates, u)
}

// AddSeqUpdate allocates the next seq of the account for u and adds its
// row to the seq journal (user_seq_updates), with the pts of the box at this
// point: updates.getDifference returns u among the pts updates where it
// happened. The caller pushes u alone in an updates container with the seq
// returned, to every device of the account (ADR 0020). u is written to the
// journal as it is now: it must be complete and must not change afterwards.
// Only the updates of SeqUpdate may be added; another one rolls the
// transaction back, and 0 is returned.
func (b *Box) AddSeqUpdate(u *mtproto.Update) int32 {
	return b.AddSeqUpdateExcept(u, 0)
}

// AddSeqUpdateExcept is AddSeqUpdate for an update that the authorization
// on the permanent key exceptAuthKeyId must not get (ADR 0022): neither its
// push nor its getDifference returns it. The caller pushes it to the other
// devices only.
func (b *Box) AddSeqUpdateExcept(u *mtproto.Update, exceptAuthKeyId int64) int32 {
	if b.err != nil {
		return 0
	}
	switch {
	case u == nil:
		b.err = misuse("nil update")
		return 0
	case !SeqUpdate(u.PredicateName):
		b.err = misuse("%s is not an update with seq", u.PredicateName)
		return 0
	case b.seq == math.MaxInt32:
		b.err = misuse("user %d: seq exhausted", b.userId)
		return 0
	}
	data, err := jsonx.Marshal(u)
	if err != nil {
		b.err = misuse("encode %s: %v", u.PredicateName, err)
		return 0
	}
	b.seq++
	b.seqJournal = append(b.seqJournal, seqRow{seq: b.seq, pts: b.pts, updateType: u.PredicateName, data: string(data), except: exceptAuthKeyId})
	return b.seq
}

// InvalidateCache adds cache keys to delete after commit.
func (b *Box) InvalidateCache(keys ...string) {
	b.keys = append(b.keys, keys...)
}

// allocated tells whether the transaction changes the counters.
func (b *Box) allocated() bool {
	return b.pts != b.state.Pts || b.seq != b.state.Seq || b.boxId != b.state.LastMessageBoxId
}

// checkJournal checks that the journal rows cover (state.pts, pts] once,
// in order: the sum of pts_count is the pts allocated; and that the seq
// rows are (state.seq, seq], one by one.
func (b *Box) checkJournal() error {
	nextSeq := b.state.Seq
	for _, r := range b.seqJournal {
		if r.seq != nextSeq+1 {
			return misuse("user %d: seq row %d does not follow seq %d", b.userId, r.seq, nextSeq)
		}
		nextSeq = r.seq
	}
	if nextSeq != b.seq {
		return misuse("user %d: seq rows end at %d, allocated up to %d", b.userId, nextSeq, b.seq)
	}

	next := b.state.Pts
	for _, r := range b.journal {
		if r.ptsCount < 1 || r.pts-r.ptsCount != next {
			return misuse("user %d: journal row pts %d (count %d) does not follow pts %d", b.userId, r.pts, r.ptsCount, next)
		}
		next = r.pts
	}
	if next != b.pts {
		return misuse("user %d: journal ends at pts %d, allocated up to %d", b.userId, next, b.pts)
	}
	return nil
}

// result is the Result of the attempt once committed.
func (b *Box) result() *Result {
	st := b.state
	if b.allocated() {
		st.Pts = b.pts
		st.Seq = b.seq
		st.LastMessageBoxId = b.boxId
		st.Date = b.now
	}
	return &Result{
		UserId:    b.userId,
		State:     st,
		Updates:   b.updates,
		Emitted:   len(b.events),
		Taken:     b.takenOutcome(),
		CacheKeys: b.keys,
	}
}

// SeqUpdate tells whether an update goes with seq (ADR 0020): a state of
// the account that another device must get when it missed the push and
// that is safe to apply twice. Updates of the common box have pts instead;
// drafts (an echo could overwrite what is being typed), service
// notifications (not idempotent), typing and statuses go without either.
// The states of basic groups and of other users come from another user's
// change, as an inbox event of the account (ADR 0021). A new login is a
// state of the authorization hash, which clients replace (ADR 0022).
func SeqUpdate(predicate string) bool {
	switch predicate {
	case mtproto.Predicate_updateNotifySettings,
		mtproto.Predicate_updateDialogPinned,
		mtproto.Predicate_updatePinnedDialogs,
		mtproto.Predicate_updateDialogFilter,
		mtproto.Predicate_updateDialogFilterOrder,
		mtproto.Predicate_updateDialogFilters,
		mtproto.Predicate_updateSavedDialogPinned,
		mtproto.Predicate_updatePinnedSavedDialogs,
		mtproto.Predicate_updateDialogUnreadMark,
		mtproto.Predicate_updateUserName,
		mtproto.Predicate_updateUser,
		mtproto.Predicate_updatePrivacy,
		mtproto.Predicate_updatePeerBlocked,
		mtproto.Predicate_updatePeerSettings,
		mtproto.Predicate_updateChatParticipants,
		mtproto.Predicate_updateChat,
		mtproto.Predicate_updateChatDefaultBannedRights,
		mtproto.Predicate_updatePendingJoinRequests,
		mtproto.Predicate_updateNewAuthorization:
		return true
	}
	return false
}

// commonBoxUpdate tells whether an update type belongs to the common message
// box (pts of the user): not channels, not qts.
func commonBoxUpdate(t int32) bool {
	switch t {
	case mtproto.PTS_UPDATE_NEW_MESSAGE,
		mtproto.PTS_UPDATE_DELETE_MESSAGES,
		mtproto.PTS_UPDATE_READ_HISTORY_OUTBOX,
		mtproto.PTS_UPDATE_READ_HISTORY_INBOX,
		mtproto.PTS_UPDATE_WEBPAGE,
		mtproto.PTS_UPDATE_READ_MESSAGE_CONENTS,
		mtproto.PTS_UPDATE_EDIT_MESSAGE,
		mtproto.PTS_UPDATE_FOLDER_PEERS,
		mtproto.PTS_UPDATE_PINNED_MESSAGES:
		return true
	}
	return false
}
