package channelbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// BasePts is the pts of a channel nobody has written to yet: its first
// update gets pts BasePts+1, as in a user's box (userbox.BasePts).
const BasePts = 1

// ErrNoChannel: no row of channels has the id.
var ErrNoChannel = errors.New("channelbox: no such channel")

// State is the committed state of a channel box: the counters of its row
// of channels.
type State struct {
	Pts           int32 // last committed pts
	LastMessageId int32 // last issued message id; the first message is 1
	Date          int64 // unix time of the last change of pts or ids (date2)
}

// NewChannel is the row of a channel Create inserts: what a channel has from
// its creation (channels.createChannel, a group's migration). Its box starts
// in the base state.
type NewChannel struct {
	Id                 int64
	AccessHash         int64
	CreatorUserId      int64
	Title              string
	About              string
	Broadcast          bool
	Megagroup          bool
	Forum              bool
	MigratedFromChatId int64
	MigratedFromMaxId  int32
	Date               int64
}

// journalRow is a channel_pts_updates row written by the transaction.
type journalRow struct {
	pts        int32
	ptsCount   int32
	updateType int32
	data       string
}

// Channel is a channel locked by a user's box transaction, given by Open or
// Create. It is valid only inside the function of userbox.Run; after the
// commit, Results gives what was written.
type Channel struct {
	b     *userbox.Box
	id    int64
	state State // locked at Open or created by Create
	pts   int32 // last allocated pts
	msgId int32 // last issued message id

	journal []journalRow
	updates []*mtproto.Update
}

var _ userbox.Part = (*Channel)(nil)

const (
	lockQuery   = "SELECT pts, last_message_id, date2 FROM channels WHERE id = ? FOR UPDATE"
	createQuery = "INSERT INTO channels (id, access_hash, creator_user_id, title, about, broadcast, megagroup, forum," +
		" migrated_from_chat_id, migrated_from_max_id, pts, last_message_id, date, date2)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)"
	// updateQuery checks in the database that the counters are what the
	// transaction locked; it must change exactly one row.
	updateQuery = "UPDATE channels SET pts = ?, last_message_id = ?, date2 = ?" +
		" WHERE id = ? AND pts = ? AND last_message_id = ?"
	insertJournalRow = "(?, ?, ?, ?, ?, ?)"
	insertJournal    = "INSERT INTO channel_pts_updates (channel_id, pts, pts_count, update_type, update_data, date2) VALUES "
)

// stateRow is a row of channels as go-zero scans it.
type stateRow struct {
	Pts           int32 `db:"pts"`
	LastMessageId int32 `db:"last_message_id"`
	Date2         int64 `db:"date2"`
}

func (r stateRow) state() State {
	return State{Pts: r.Pts, LastMessageId: r.LastMessageId, Date: r.Date2}
}

func misuse(format string, args ...any) error {
	return fmt.Errorf("%w: channelbox: %s", userbox.ErrMisuse, fmt.Sprintf(format, args...))
}

// attached returns the channel channelId if the transaction holds it, and
// checks the lock order for a new one: a channel row is locked only above
// every channel row the transaction holds.
func attached(b *userbox.Box, channelId int64) (*Channel, error) {
	if err := b.Err(); err != nil {
		return nil, err
	}
	if channelId <= 0 {
		return nil, misuse("invalid channel id %d", channelId)
	}
	for _, p := range b.Parts() {
		c, ok := p.(*Channel)
		if !ok {
			continue
		}
		if c.id == channelId {
			return c, nil
		}
		if c.id > channelId {
			err := misuse("channel %d locked after channel %d: channels lock in ascending id", channelId, c.id)
			b.Fail(err)
			return nil, err
		}
	}
	return nil, nil
}

// Open locks channelId's row in b's transaction and returns its box. A
// channel the transaction holds already is returned again. No row is
// ErrNoChannel, which leaves the transaction to go on or end as fn decides.
func Open(b *userbox.Box, channelId int64) (*Channel, error) {
	c, err := attached(b, channelId)
	if c != nil || err != nil {
		return c, err
	}
	var row stateRow
	if err = b.Tx().QueryRow(&row, lockQuery, channelId); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoChannel
		}
		return nil, err
	}
	c = &Channel{b: b, id: channelId, state: row.state()}
	c.pts, c.msgId = c.state.Pts, c.state.LastMessageId
	b.Attach(c)
	return c, nil
}

// Create inserts the row of a new channel in b's transaction, which locks
// it, and returns its box in the base state: the first update gets pts
// BasePts+1 and the first message id 1. An id in use fails the statement
// (a duplicate key) and the transaction.
func Create(b *userbox.Box, nc NewChannel) (*Channel, error) {
	c, err := attached(b, nc.Id)
	if err != nil {
		return nil, err
	}
	if c != nil {
		err = misuse("channel %d created twice", nc.Id)
		b.Fail(err)
		return nil, err
	}
	if _, err = b.Tx().Exec(createQuery, nc.Id, nc.AccessHash, nc.CreatorUserId, nc.Title, nc.About,
		nc.Broadcast, nc.Megagroup, nc.Forum, nc.MigratedFromChatId, nc.MigratedFromMaxId,
		BasePts, nc.Date, b.Date()); err != nil {
		return nil, err
	}
	c = &Channel{b: b, id: nc.Id, state: State{Pts: BasePts, Date: b.Date()}}
	c.pts = BasePts
	b.Attach(c)
	return c, nil
}

// Id is the channel's id.
func (c *Channel) Id() int64 { return c.id }

// State is the channel's state locked at Open, or the base state of a
// channel Create made.
func (c *Channel) State() State { return c.state }

// Pts is the last pts allocated so far: the channel's pts once the
// transaction commits.
func (c *Channel) Pts() int32 { return c.pts }

// NextMessageId issues the next message id of the channel. The id is spent
// only if the transaction commits. 0 means a misuse, which rolls the
// transaction back.
func (c *Channel) NextMessageId() int32 {
	if c.b.Err() != nil {
		return 0
	}
	if c.msgId == math.MaxInt32 {
		c.b.Fail(misuse("channel %d: message ids exhausted", c.id))
		return 0
	}
	c.msgId++
	return c.msgId
}

// AddPtsUpdate allocates ptsCount pts of the channel for u, sets u's pts and
// pts_count and adds its journal row. Only the updates with the channel's
// pts, of this channel, may be added. u is written to the journal as it is
// now: it must be complete and must not change afterwards. A wrong update
// or count rolls the transaction back.
func (c *Channel) AddPtsUpdate(u *mtproto.Update, ptsCount int32) {
	if c.b.Err() != nil {
		return
	}
	switch {
	case u == nil:
		c.b.Fail(misuse("channel %d: nil update", c.id))
		return
	case ptsCount < 1:
		c.b.Fail(misuse("channel %d: pts_count %d of %s", c.id, ptsCount, u.PredicateName))
		return
	case c.pts > math.MaxInt32-ptsCount:
		c.b.Fail(misuse("channel %d: pts exhausted", c.id))
		return
	}
	updateType := mtproto.GetUpdateType(u)
	if !channelUpdate(updateType) {
		c.b.Fail(misuse("%s is not an update of a channel box", u.PredicateName))
		return
	}
	if id := channelOf(u); id != c.id {
		c.b.Fail(misuse("%s of channel %d added to channel %d", u.PredicateName, id, c.id))
		return
	}

	c.pts += ptsCount
	u.Pts_INT32 = c.pts
	u.PtsCount = ptsCount
	data, err := jsonx.Marshal(u)
	if err != nil {
		c.b.Fail(misuse("encode %s: %v", u.PredicateName, err))
		return
	}
	c.journal = append(c.journal, journalRow{pts: c.pts, ptsCount: ptsCount, updateType: updateType, data: string(data)})
	c.updates = append(c.updates, u)
}

// channelUpdate tells whether an update type carries a channel's pts: the
// five updates of a channel's journal (ADR 0034, p. 1).
func channelUpdate(t int32) bool {
	switch t {
	case mtproto.PTS_UPDATE_NEW_CHANNEL_MESSAGE,
		mtproto.PTS_UPDATE_EDIT_CHANNEL_MESSAGE,
		mtproto.PTS_UPDATE_DELETE_CHANNEL_MESSAGES,
		mtproto.PTS_UPDATE_EDIT_CHANNEL_WEBPAGE,
		mtproto.PTS_UPDATE_PINNED_CHANNEL_MESSAGES:
		return true
	}
	return false
}

// channelOf is the channel an update of a channel's journal is about: the
// peer of its message, or its channel_id.
func channelOf(u *mtproto.Update) int64 {
	switch u.PredicateName {
	case mtproto.Predicate_updateNewChannelMessage, mtproto.Predicate_updateEditChannelMessage:
		return u.GetMessage_MESSAGE().GetPeerId().GetChannelId()
	}
	return u.GetChannelId()
}

// allocated tells whether the transaction changes the counters.
func (c *Channel) allocated() bool {
	return c.pts != c.state.Pts || c.msgId != c.state.LastMessageId
}

// checkJournal checks that the journal rows cover (state.pts, pts] once, in
// order.
func (c *Channel) checkJournal() error {
	next := c.state.Pts
	for _, r := range c.journal {
		if r.ptsCount < 1 || r.pts-r.ptsCount != next {
			return misuse("channel %d: journal row pts %d (count %d) does not follow pts %d", c.id, r.pts, r.ptsCount, next)
		}
		next = r.pts
	}
	if next != c.pts {
		return misuse("channel %d: journal ends at pts %d, allocated up to %d", c.id, next, c.pts)
	}
	return nil
}

// Flush writes the journal rows and the new counters (userbox.Part).
func (c *Channel) Flush(context.Context) error {
	if !c.allocated() {
		return nil
	}
	if err := c.checkJournal(); err != nil {
		return err
	}
	tx := c.b.Tx()
	if len(c.journal) > 0 {
		args := make([]any, 0, len(c.journal)*6)
		for _, r := range c.journal {
			args = append(args, c.id, r.pts, r.ptsCount, r.updateType, r.data, c.b.Date())
		}
		query := insertJournal + strings.Repeat(insertJournalRow+", ", len(c.journal)-1) + insertJournalRow
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}
	res, err := tx.Exec(updateQuery, c.pts, c.msgId, c.b.Date(), c.id, c.state.Pts, c.state.LastMessageId)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: channel %d", userbox.ErrStateChanged, c.id)
	}
	return nil
}

// Journaled tells whether the transaction wrote journal rows of the
// channel (userbox.Part).
func (c *Channel) Journaled() bool { return len(c.journal) > 0 }

// Applied tells whether the journal rows of a COMMIT that failed are there
// (userbox.Part). tx holds the user's user_state lock, which the transaction
// checked held to its end, so that transaction is over and a plain read
// sees its outcome; locking the channel's row again would only wait for the
// channel's other writers. A pts below the transaction's means not applied
// (pts never goes down); otherwise every row must be there as written,
// compared as JSON by the server.
func (c *Channel) Applied(ctx context.Context, tx *sql.Tx) (bool, error) {
	var pts int32
	err := tx.QueryRowContext(ctx, "SELECT pts FROM channels WHERE id = ?", c.id).Scan(&pts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // the row was to be created by that transaction
	}
	if err != nil {
		return false, err
	}
	if pts < c.pts {
		return false, nil
	}
	for _, r := range c.journal {
		var n int
		err = tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM channel_pts_updates WHERE channel_id = ? AND pts = ? AND pts_count = ? AND update_type = ? AND update_data = CAST(? AS JSON)",
			c.id, r.pts, r.ptsCount, r.updateType, r.data).Scan(&n)
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, nil
		}
	}
	return true, nil
}

// Result is what a committed transaction wrote to a channel.
type Result struct {
	ChannelId int64
	// State is the committed state after the transaction; with nothing
	// allocated, the state the transaction found.
	State State
	// Updates are the journaled updates, in ascending pts, with their pts
	// and pts_count set.
	Updates []*mtproto.Update
}

// Results are the channels res changed, in the order they were locked.
func Results(res *userbox.Result) []Result {
	if res == nil {
		return nil
	}
	var out []Result
	for _, p := range res.Parts {
		c, ok := p.(*Channel)
		if !ok {
			continue
		}
		st := c.state
		if c.allocated() {
			st = State{Pts: c.pts, LastMessageId: c.msgId, Date: c.b.Date()}
		}
		out = append(out, Result{ChannelId: c.id, State: st, Updates: c.updates})
	}
	return out
}
