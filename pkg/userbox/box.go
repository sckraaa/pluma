package userbox

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Event is a side effect of a box transaction: one Kafka message, exactly
// what the MQ clients send (a push to sync, an event for another user's
// inbox). Run returns the events in Result; they are published after commit
// (Publish), never from inside the transaction.
type Event struct {
	Topic     string // Kafka topic
	Recipient int64  // the user the event is addressed to; the Kafka key is its decimal form
	Method    string // the "method" header: the request's proto message name
	Data      []byte // JSON of the request
}

// Result is the outcome of a committed box transaction.
type Result struct {
	UserId int64
	// State is the committed state after the transaction. With nothing
	// allocated it is the state the transaction found.
	State State
	// Updates are the journaled updates, in ascending pts, with their
	// pts and pts_count set.
	Updates []*mtproto.Update
	// Events, in the order emitted, to publish after commit.
	Events []Event
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
	boxId int32 // last issued message box id

	journal []journalRow
	updates []*mtproto.Update
	events  []Event
	keys    []string

	// err is a misuse of the Box (bad pts_count, an update that is not of
	// the common box, ids out of range): the transaction is rolled back.
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
		b.err = fmt.Errorf("userbox: user %d: message box ids exhausted", b.userId)
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
		b.err = errors.New("userbox: nil update")
		return
	case ptsCount < 1:
		b.err = fmt.Errorf("userbox: user %d: pts_count %d of %s", b.userId, ptsCount, u.PredicateName)
		return
	case b.pts > math.MaxInt32-ptsCount:
		b.err = fmt.Errorf("userbox: user %d: pts exhausted", b.userId)
		return
	}
	updateType := mtproto.GetUpdateType(u)
	if !commonBoxUpdate(updateType) {
		b.err = fmt.Errorf("userbox: %s is not an update of the common message box", u.PredicateName)
		return
	}

	b.pts += ptsCount
	u.Pts_INT32 = b.pts
	u.PtsCount = ptsCount
	data, err := jsonx.Marshal(u)
	if err != nil {
		b.err = fmt.Errorf("userbox: encode %s: %w", u.PredicateName, err)
		return
	}
	b.journal = append(b.journal, journalRow{pts: b.pts, ptsCount: ptsCount, updateType: updateType, data: string(data)})
	b.updates = append(b.updates, u)
}

// InvalidateCache adds cache keys to delete after commit.
func (b *Box) InvalidateCache(keys ...string) {
	b.keys = append(b.keys, keys...)
}

// Emit adds an event to publish after commit, in this order.
func (b *Box) Emit(ev Event) {
	b.events = append(b.events, ev)
}

// allocated tells whether the transaction changes the counters.
func (b *Box) allocated() bool {
	return b.pts != b.state.Pts || b.boxId != b.state.LastMessageBoxId
}

// checkJournal checks that the journal rows cover (state.pts, pts] once,
// in order: the sum of pts_count is the pts allocated.
func (b *Box) checkJournal() error {
	next := b.state.Pts
	for _, r := range b.journal {
		if r.ptsCount < 1 || r.pts-r.ptsCount != next {
			return fmt.Errorf("userbox: user %d: journal row pts %d (count %d) does not follow pts %d", b.userId, r.pts, r.ptsCount, next)
		}
		next = r.pts
	}
	if next != b.pts {
		return fmt.Errorf("userbox: user %d: journal ends at pts %d, allocated up to %d", b.userId, next, b.pts)
	}
	return nil
}

// result is the Result of the attempt once committed.
func (b *Box) result() *Result {
	st := b.state
	if b.allocated() {
		st.Pts = b.pts
		st.LastMessageBoxId = b.boxId
		st.Date = b.now
	}
	return &Result{
		UserId:    b.userId,
		State:     st,
		Updates:   b.updates,
		Events:    b.events,
		CacheKeys: b.keys,
	}
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
