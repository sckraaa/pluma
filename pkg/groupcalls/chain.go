// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package groupcalls

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// The blockchain of a call's end-to-end encryption (ADR 0060 §2). The
// clients write the blocks; the Store checks each with pkg/tde2e and the
// server policy and appends it in the transaction that locks the call's
// row, the one that also changes the participants' version: a block on a
// height another block took first is stale, and a join's block and its
// participant row commit or fail together. The state the next block is
// checked against is restored from the last block of sub-chain 0, which
// carries the group state and the shared key in its changes or its proof.
// The server never writes a block: one with a new key can only come from
// someone who knows the key.

// Sub-chains of a call (phone.getGroupCallChainBlocks sub_chain_id).
const (
	// SubChainMain holds the blocks of the group state and the shared key:
	// idx is the height.
	SubChainMain = 0
	// SubChainBroadcast holds the commits and reveals of the emoji
	// verification: idx counts them over the life of the call.
	SubChainBroadcast = 1
)

// ErrNoChainBlock: a join came without a block; every group call is
// end-to-end encrypted.
var ErrNoChainBlock = errors.New("groupcalls: a join without a block")

// ChainJoin is the chain part of a join: the participant's public key and
// the block that adds him.
type ChainJoin struct {
	PublicKey [32]byte
	Block     []byte
}

// ChainBlock is one stored block or broadcast, in the local form.
type ChainBlock struct {
	SubChain int8   `db:"sub_chain"`
	Idx      int32  `db:"idx"`
	Block    []byte `db:"block"`
	Hash     []byte `db:"hash"`
	UserId   int64  `db:"user_id"`
	// Height is the height of a block of sub-chain 0 and the chain_height
	// a broadcast names.
	Height int32 `db:"height"`
	Date   int32 `db:"date"`
}

// Server is the block in the form the server hands out: the magic plus one
// (tde2e Blockchain.cpp:742-760).
func (b *ChainBlock) Server() ([]byte, error) {
	return tde2e.ToServer(b.Block)
}

// ChainWrite is a block or broadcast appended, with what its delivery
// needs.
type ChainWrite struct {
	Call *Call
	// Block is what was appended; nil for a broadcast of an earlier height,
	// which changes nothing and is not kept (clients skip it, tde2e
	// Call.cpp:112-116).
	Block *ChainBlock
	// Next is next_offset of the sub-chain after the write: the height plus
	// one for sub-chain 0, the count of broadcasts for sub-chain 1.
	Next int32
	// Members are the users of the group state the block went to: before
	// and after a block of sub-chain 0 (a removed user gets his removal),
	// the current ones for a broadcast.
	Members []int64
	// Changes are the participants a kick took out of the call.
	Changes []Change
}

// insertBlock writes a block or broadcast.
func insertBlock(tx *sqlx.Tx, callId int64, b *ChainBlock) error {
	_, err := tx.Exec("insert into group_call_chain_blocks (call_id, sub_chain, idx, block, hash, user_id, height, `date`) values (?, ?, ?, ?, ?, ?, ?, ?)",
		callId, b.SubChain, b.Idx, b.Block, b.Hash, b.UserId, b.Height, b.Date)
	return err
}

// lastBlock reads the block of sub-chain 0 at the call's height.
func lastBlock(ctx context.Context, q querier, call *Call) (*ChainBlock, error) {
	var rows []ChainBlock
	err := q.QueryRowsPartial(ctx, &rows, "select sub_chain, idx, block, hash, user_id, height, `date` from group_call_chain_blocks where call_id = ? and sub_chain = 0 and idx = ?",
		call.Id, call.ChainHeight)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("groupcalls: call %d has no block at its height %d", call.Id, call.ChainHeight)
	}
	return &rows[0], nil
}

// chainOf restores the chain of call: empty before the first block, else
// from the last block, which must hash to the call's chain_hash.
func chainOf(ctx context.Context, q querier, call *Call) (*tde2e.Chain, error) {
	if call.ChainHeight < 0 {
		return tde2e.NewChain(), nil
	}
	b, err := lastBlock(ctx, q, call)
	if err != nil {
		return nil, err
	}
	ch, err := tde2e.RestoreChain(b.Block)
	if err != nil {
		return nil, fmt.Errorf("groupcalls: the last block of call %d: %w", call.Id, err)
	}
	if ch.Height() != call.ChainHeight || ch.LastHash() != call.ChainHash {
		return nil, fmt.Errorf("groupcalls: the last block of call %d does not match its chain state", call.Id)
	}
	return ch, nil
}

func memberIds(g *tde2e.GroupState) []int64 {
	out := make([]int64, 0, len(g.Participants))
	for _, p := range g.Participants {
		out = append(out, p.UserID)
	}
	return out
}

// appendMain stores a block of sub-chain 0 accepted on top of call by
// userId and moves the call's chain state to it.
func appendMain(tx *sqlx.Tx, call *Call, a *tde2e.Applied, userId int64, now time.Time) (*ChainBlock, error) {
	b := &ChainBlock{
		SubChain: SubChainMain, Idx: a.Block.Height, Block: a.Bytes, Hash: a.Hash[:],
		UserId: userId, Height: a.Block.Height, Date: int32(now.Unix()),
	}
	if err := insertBlock(tx, call.Id, b); err != nil {
		return nil, err
	}
	if _, err := tx.Exec("update group_calls set chain_height = ?, chain_hash = ? where id = ?", a.Block.Height, a.Hash[:], call.Id); err != nil {
		return nil, err
	}
	return b, nil
}

// joinChain checks and appends the block of a join of userId to call, in
// the transaction that locked the call's row.
func joinChain(ctx context.Context, tx *sqlx.Tx, call *Call, userId int64, j *ChainJoin, now time.Time) (*ChainWrite, error) {
	ch, err := chainOf(ctx, txq{tx}, call)
	if err != nil {
		return nil, err
	}
	before := memberIds(ch.GroupState())
	var ownerErr error
	a, err := ch.Join(tde2e.Join{
		UserID: userId, PublicKey: j.PublicKey, Block: j.Block,
		KeyOwner: func(pk [32]byte) (int64, bool) {
			var owners []int64
			if err := tx.QueryRowsPartial(&owners, "select user_id from group_call_chain_keys where call_id = ? and public_key = ?", call.Id, pk[:]); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
				ownerErr = err
				return 0, false
			}
			if len(owners) == 0 {
				return 0, false
			}
			return owners[0], true
		},
	})
	if ownerErr != nil {
		return nil, ownerErr
	}
	if err != nil {
		return nil, err
	}
	block, err := appendMain(tx, call, a, userId, now)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec("insert ignore into group_call_chain_keys (call_id, public_key, user_id) values (?, ?, ?)", call.Id, j.PublicKey[:], userId); err != nil {
		return nil, err
	}
	return &ChainWrite{Block: block, Next: a.Block.Height + 1, Members: uniqIds(append(before, memberIds(ch.GroupState())...))}, nil
}

func uniqIds(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RemoveParams is a block of phone.deleteConferenceCallParticipants.
type RemoveParams struct {
	CallId   int64
	CallerId int64
	Ids      []int64
	Block    []byte
	// Check sees the call and the rows of ids (those that ever joined)
	// under the lock and refuses the removal with an error: only_left of
	// one still in the call.
	Check func(call *Call, rows []*Participant) error
	// Kick takes those of ids who are in the call out of it with the
	// block.
	Kick bool
}

// RemoveFromChain appends a block that takes ids out of the group state of
// a call that is not over. The block goes to the group state before it, the
// removed included (ADR 0060 §2).
//
// ids name members of the group state, which holds tde2e.MaxParticipants at
// most: a longer list is refused before the call's row is locked, and the
// rows of ids are locked only once the block passed the chain, so that a
// request of one not in the chain costs the call nothing more.
func (s *Store) RemoveFromChain(ctx context.Context, r RemoveParams) (*ChainWrite, error) {
	if len(r.Ids) > tde2e.MaxParticipants {
		return nil, &tde2e.Error{Kind: tde2e.KindInvalid, Code: tde2e.CodePolicy,
			Msg: fmt.Sprintf("%d users to remove, the group state holds %d at most", len(r.Ids), tde2e.MaxParticipants)}
	}
	var res *ChainWrite
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		res = nil
		call, err := oneCall(ctx, txq{tx}, "where c.id = ? for update", r.CallId)
		if err != nil {
			return err
		}
		if call.Discarded {
			return ErrDiscarded
		}
		ch, err := chainOf(ctx, txq{tx}, call)
		if err != nil {
			return err
		}
		before := memberIds(ch.GroupState())
		// The chain first: of the removals of one who left, all but the
		// first are stale, whoever is in the call by then.
		a, err := ch.Remove(r.CallerId, r.Ids, r.Block)
		if err != nil {
			return err
		}
		args := []any{r.CallId}
		for _, id := range r.Ids {
			args = append(args, id)
		}
		// Remove refused an empty list.
		rows, err := participants(ctx, txq{tx}, "where call_id = ? and user_id in ("+placeholders(len(r.Ids))+") for update", args...)
		if err != nil {
			return err
		}
		if r.Check != nil {
			if err = r.Check(call, rows); err != nil {
				return err
			}
		}
		now := s.now()
		block, err := appendMain(tx, call, a, r.CallerId, now)
		if err != nil {
			return err
		}
		res = &ChainWrite{Block: block, Next: a.Block.Height + 1, Members: before}
		if r.Kick {
			for _, p := range rows {
				if !p.InCall {
					continue
				}
				if err = leaveRow(tx, p); err != nil {
					return err
				}
				if _, err = tx.Exec("update group_calls set version = version + 1 where id = ?", r.CallId); err != nil {
					return err
				}
				p.InCall, p.Source, p.PresentationSource, p.Presentation, p.RaiseHandRating = false, 0, 0, nil, 0
				res.Changes = append(res.Changes, Change{Participant: p, Versioned: true})
			}
		}
		res.Call, err = oneCall(ctx, txq{tx}, "where c.id = ?", r.CallId)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// verificationOf restores the emoji verification of the chain ch of call:
// the broadcasts of its height replayed in their order.
func verificationOf(ctx context.Context, q querier, call *Call, ch *tde2e.Chain) (*tde2e.Verification, error) {
	v := tde2e.NewVerification()
	v.Reset(ch)
	if ch.Height() < 0 {
		return v, nil
	}
	var rows []ChainBlock
	err := q.QueryRowsPartial(ctx, &rows, "select sub_chain, idx, block, hash, user_id, height, `date` from group_call_chain_blocks where call_id = ? and sub_chain = 1 and height = ? order by idx",
		call.Id, ch.Height())
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for i := range rows {
		b, err := tde2e.ParseBroadcast(rows[i].Block)
		if err != nil {
			return nil, fmt.Errorf("groupcalls: broadcast %d of call %d: %w", rows[i].Idx, call.Id, err)
		}
		if _, err = v.Apply(b); err != nil {
			return nil, fmt.Errorf("groupcalls: broadcast %d of call %d: %w", rows[i].Idx, call.Id, err)
		}
	}
	return v, nil
}

// AppendBroadcast appends a commit or reveal of sub-chain 1 that senderId
// sent to a call that is not over. A broadcast of an earlier height is
// answered as accepted and not kept (ChainWrite.Block nil).
func (s *Store) AppendBroadcast(ctx context.Context, callId, senderId int64, data []byte) (*ChainWrite, error) {
	var res *ChainWrite
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		res = nil
		call, err := oneCall(ctx, txq{tx}, "where c.id = ? for update", callId)
		if err != nil {
			return err
		}
		if call.Discarded {
			return ErrDiscarded
		}
		ch, err := chainOf(ctx, txq{tx}, call)
		if err != nil {
			return err
		}
		if _, ok := ch.GroupState().Participant(senderId); !ok {
			return &tde2e.Error{Kind: tde2e.KindForbidden, Code: tde2e.CodeBroadcastUnknownUserID}
		}
		v, err := verificationOf(ctx, txq{tx}, call, ch)
		if err != nil {
			return err
		}
		accepted, err := v.Accept(senderId, data)
		if err != nil {
			return err
		}
		res = &ChainWrite{Call: call, Next: call.BroadcastCount, Members: memberIds(ch.GroupState())}
		if accepted.Stale {
			return nil
		}
		hash := sha256.Sum256(accepted.Bytes)
		b := &ChainBlock{
			SubChain: SubChainBroadcast, Idx: call.BroadcastCount, Block: accepted.Bytes, Hash: hash[:],
			UserId: senderId, Height: accepted.Broadcast.ChainHeight, Date: int32(s.now().Unix()),
		}
		if err = insertBlock(tx, callId, b); err != nil {
			return err
		}
		if _, err = tx.Exec("update group_calls set broadcast_count = broadcast_count + 1 where id = ?", callId); err != nil {
			return err
		}
		call.BroadcastCount++
		res.Block, res.Next = b, call.BroadcastCount
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Blocks returns the blocks of a sub-chain of a call with idx in
// [from, to), in order.
func (s *Store) Blocks(ctx context.Context, callId int64, subChain int8, from, to int32) ([]*ChainBlock, error) {
	var rows []ChainBlock
	err := s.db.QueryRowsPartial(ctx, &rows, "select sub_chain, idx, block, hash, user_id, height, `date` from group_call_chain_blocks where call_id = ? and sub_chain = ? and idx >= ? and idx < ? order by idx",
		callId, subChain, from, to)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*ChainBlock, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}

// ChainHead is what a read of the chain needs of its call: one row read
// by its primary key, without the counts of Get.
type ChainHead struct {
	Id         int64
	AccessHash int64
	PeerType   int32
	PeerId     int64
	Discarded  bool
	// Height and Hash are the call's chain state; Broadcasts the count of
	// sub-chain 1.
	Height     int32
	Hash       [32]byte
	Broadcasts int32
}

// Head returns the chain state of a call; ErrNotFound if there is none.
func (s *Store) Head(ctx context.Context, callId int64) (*ChainHead, error) {
	var r struct {
		Id             int64        `db:"id"`
		AccessHash     int64        `db:"access_hash"`
		PeerType       int32        `db:"peer_type"`
		PeerId         int64        `db:"peer_id"`
		Live           sql.NullBool `db:"live"`
		ChainHeight    int32        `db:"chain_height"`
		ChainHash      []byte       `db:"chain_hash"`
		BroadcastCount int32        `db:"broadcast_count"`
	}
	err := s.db.QueryRowPartial(ctx, &r, "select id, access_hash, peer_type, peer_id, live, chain_height, chain_hash, broadcast_count from group_calls where id = ?", callId)
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h := &ChainHead{
		Id: r.Id, AccessHash: r.AccessHash, PeerType: r.PeerType, PeerId: r.PeerId, Discarded: !r.Live.Valid,
		Height: r.ChainHeight, Broadcasts: r.BroadcastCount,
	}
	copy(h.Hash[:], r.ChainHash)
	return h, nil
}

// Chain returns the chain of a call as of its last block: its group state
// for the reads, the collector and pluma-admin.
func (s *Store) Chain(ctx context.Context, callId int64) (*tde2e.Chain, error) {
	call, err := s.Get(ctx, callId)
	if err != nil {
		return nil, err
	}
	return chainOf(ctx, dbq{s.db}, call)
}

// Verification returns the emoji verification of a call at its height:
// who committed and who revealed.
func (s *Store) Verification(ctx context.Context, callId int64) (*tde2e.Chain, *tde2e.Verification, error) {
	call, err := s.Get(ctx, callId)
	if err != nil {
		return nil, nil, err
	}
	ch, err := chainOf(ctx, dbq{s.db}, call)
	if err != nil {
		return nil, nil, err
	}
	v, err := verificationOf(ctx, dbq{s.db}, call, ch)
	return ch, v, err
}

// ClearChains deletes the blocks, keys and diagnostics of at most limit
// calls that ended before before, and resets their chain state. It
// returns how many calls it cleared.
func (s *Store) ClearChains(ctx context.Context, before time.Time, limit int) (int, error) {
	var ids []int64
	err := s.db.QueryRowsPartial(ctx, &ids, "select id from group_calls where live is null and end_date < ? and chain_height >= 0 order by id limit ?", int32(before.Unix()), limit)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	for i, id := range ids {
		err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			for _, table := range []string{"group_call_chain_blocks", "group_call_chain_keys", "group_call_e2e_diag"} {
				if _, err := tx.Exec("delete from "+table+" where call_id = ?", id); err != nil {
					return err
				}
			}
			_, err := tx.Exec("update group_calls set chain_height = -1, chain_hash = '', broadcast_count = 0 where id = ? and live is null", id)
			return err
		})
		if err != nil {
			return i, err
		}
	}
	return len(ids), nil
}
