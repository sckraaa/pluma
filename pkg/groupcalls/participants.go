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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// InCall returns the participants of a call who are in it, in the order
// they joined.
func (s *Store) InCall(ctx context.Context, callId int64) ([]*Participant, error) {
	return participants(ctx, dbq{s.db}, "where call_id = ? and in_call = 1 order by `date`, peer_type, peer_id", callId)
}

// ParticipantOf returns the row of the account userId in a call, in it or
// not; ErrNotFound if it never joined.
func (s *Store) ParticipantOf(ctx context.Context, callId, userId int64) (*Participant, error) {
	list, err := participants(ctx, dbq{s.db}, "where call_id = ? and user_id = ? order by in_call desc limit 1", callId, userId)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// ByPeer returns the row of a peer in a call; ErrNotFound if it never
// joined.
func (s *Store) ByPeer(ctx context.Context, callId int64, peer PeerKey) (*Participant, error) {
	list, err := participants(ctx, dbq{s.db}, "where call_id = ? and peer_type = ? and peer_id = ?", callId, peer.Type, peer.Id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// Lookup returns the participants of a call in it among peers or whose
// sources (main or screen) are among sources.
func (s *Store) Lookup(ctx context.Context, callId int64, peers []PeerKey, sources []uint32) ([]*Participant, error) {
	var conds []string
	args := []any{callId}
	for _, p := range peers {
		conds = append(conds, "(peer_type = ? and peer_id = ?)")
		args = append(args, p.Type, p.Id)
	}
	if len(sources) > 0 {
		list := placeholders(len(sources))
		conds = append(conds, "source in ("+list+") or presentation_source in ("+list+")")
		for range 2 {
			for _, src := range sources {
				args = append(args, int64(src))
			}
		}
	}
	if len(conds) == 0 {
		return []*Participant{}, nil
	}
	return participants(ctx, dbq{s.db}, "where call_id = ? and in_call = 1 and ("+strings.Join(conds, " or ")+") order by `date`, peer_type, peer_id", args...)
}

// Page is one page of the participants of a call.
type Page struct {
	Participants []*Participant
	// Next is the offset of the next page, "" after the last.
	Next string
}

// List returns at most limit participants in the call after offset (""
// for the first page), in the order they joined. The offset is the
// position of the last one of a page.
func (s *Store) List(ctx context.Context, callId int64, offset string, limit int) (*Page, error) {
	where := "where call_id = ? and in_call = 1"
	args := []any{callId}
	if offset != "" {
		date, peerType, peerId, err := parseOffset(offset)
		if err != nil {
			return nil, err
		}
		where += " and (`date`, peer_type, peer_id) > (?, ?, ?)"
		args = append(args, date, peerType, peerId)
	}
	args = append(args, limit+1)
	list, err := participants(ctx, dbq{s.db}, where+" order by `date`, peer_type, peer_id limit ?", args...)
	if err != nil {
		return nil, err
	}
	page := &Page{Participants: list}
	if len(list) > limit {
		page.Participants = list[:limit]
		last := list[limit-1]
		page.Next = fmt.Sprintf("%d:%d:%d", last.Date, last.PeerType, last.PeerId)
	}
	return page, nil
}

// ErrBadOffset: an offset of getGroupParticipants this server did not give.
var ErrBadOffset = errors.New("groupcalls: bad offset")

func parseOffset(s string) (int32, int32, int64, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, 0, 0, ErrBadOffset
	}
	date, err1 := strconv.ParseInt(parts[0], 10, 32)
	peerType, err2 := strconv.ParseInt(parts[1], 10, 32)
	peerId, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, ErrBadOffset
	}
	return int32(date), int32(peerType), peerId, nil
}

// Edit changes the row of a participant in the call with fn under a lock of
// the row; the change goes out with the current version. ErrNotJoined if
// the peer is not in the call.
func (s *Store) Edit(ctx context.Context, callId int64, peer PeerKey, fn func(p *Participant) (bool, error)) (call *Call, p *Participant, changed bool, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		c, err := oneCall(ctx, txq{tx}, "where c.id = ?", callId)
		if err != nil {
			return err
		}
		if c.Discarded {
			return ErrDiscarded
		}
		list, err := participants(ctx, txq{tx}, "where call_id = ? and peer_type = ? and peer_id = ? and in_call = 1 for update", callId, peer.Type, peer.Id)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return ErrNotJoined
		}
		p = list[0]
		if changed, err = fn(p); err != nil || !changed {
			call = c
			return err
		}
		if err = updateRow(tx, p); err != nil {
			return err
		}
		call, err = oneCall(ctx, txq{tx}, "where c.id = ?", callId)
		return err
	})
	return call, p, changed, err
}

// updateRow writes what an edit may change.
func updateRow(tx *sqlx.Tx, p *Participant) error {
	video, err := marshalMedia(p.Video)
	if err != nil {
		return err
	}
	presentation, err := marshalMedia(p.Presentation)
	if err != nil {
		return err
	}
	_, err = tx.Exec("update group_call_participants set muted = ?, can_self_unmute = ?, volume = ?, raise_hand_rating = ?, video_stopped = ?, video_paused = ?, video = ?,"+
		" presentation_source = ?, presentation = ?, presentation_paused = ? where call_id = ? and peer_type = ? and peer_id = ?",
		p.Muted, p.CanSelfUnmute, p.Volume, p.RaiseHandRating, p.VideoStopped, p.VideoPaused, video,
		nullSource(p.PresentationSource), presentation, p.PresentationPaused, p.CallId, p.PeerType, p.PeerId)
	if isDuplicate(err) {
		return ErrSourceInUse
	}
	return err
}

// SetPresentation starts (media not nil, with the screen's sound source)
// or stops the shared screen of the account in a call.
func (s *Store) SetPresentation(ctx context.Context, callId, userId int64, source uint32, media *Media) (*Call, *Participant, bool, error) {
	p, err := s.ParticipantOf(ctx, callId, userId)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, false, ErrNotJoined
	}
	if err != nil {
		return nil, nil, false, err
	}
	return s.Edit(ctx, callId, p.Key(), func(p *Participant) (bool, error) {
		if p.UserId != userId {
			return false, ErrNotJoined
		}
		if media == nil {
			if p.Presentation == nil && p.PresentationSource == 0 {
				return false, nil
			}
			p.Presentation, p.PresentationSource, p.PresentationPaused = nil, 0, false
			return true, nil
		}
		if !p.CanSelfUnmute {
			// An admin took the word: the screen's sound would speak for
			// him. tdesktop shows MutedNoScreen on this error
			// (calls_group_call.cpp:2030-2036).
			return false, mtproto.ErrChatAdminRequired
		}
		p.Presentation, p.PresentationSource, p.PresentationPaused = media, source, false
		return true, nil
	})
}

// Prefs returns what viewer set for the participants of a call.
func (s *Store) Prefs(ctx context.Context, callId, viewer int64) (map[PeerKey]Pref, error) {
	var rows []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
		Muted    bool  `db:"muted"`
		Volume   int32 `db:"volume"`
	}
	err := s.db.QueryRowsPartial(ctx, &rows, "select peer_type, peer_id, muted, volume from group_call_viewer_prefs where call_id = ? and viewer_id = ?", callId, viewer)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make(map[PeerKey]Pref, len(rows))
	for _, r := range rows {
		out[PeerKey{r.PeerType, r.PeerId}] = Pref{Muted: r.Muted, Volume: r.Volume}
	}
	return out, nil
}

// SetPref stores what viewer set for a participant of a call.
func (s *Store) SetPref(ctx context.Context, callId, viewer int64, peer PeerKey, pref Pref) error {
	_, err := s.db.Exec(ctx, "insert into group_call_viewer_prefs (call_id, viewer_id, peer_type, peer_id, muted, volume) values (?, ?, ?, ?, ?, ?)"+
		" on duplicate key update muted = values(muted), volume = values(volume)",
		callId, viewer, peer.Type, peer.Id, pref.Muted, pref.Volume)
	return err
}

// MarkSeen records that the SFU holds the connections of the accounts
// userIds in a call at now.
func (s *Store) MarkSeen(ctx context.Context, callId int64, userIds []int64, now time.Time) error {
	if len(userIds) == 0 {
		return nil
	}
	args := []any{now.UnixMilli(), callId}
	for _, id := range userIds {
		args = append(args, id)
	}
	_, err := s.db.Exec(ctx, "update group_call_participants set seen_at = ? where call_id = ? and in_call = 1 and user_id in ("+placeholders(len(userIds))+")", args...)
	return err
}

// MarkActive records that the accounts userIds spoke in a call at now
// (groupCallParticipant.active_date).
func (s *Store) MarkActive(ctx context.Context, callId int64, userIds []int64, now time.Time) error {
	if len(userIds) == 0 {
		return nil
	}
	args := []any{int32(now.Unix()), callId}
	for _, id := range userIds {
		args = append(args, id)
	}
	_, err := s.db.Exec(ctx, "update group_call_participants set active_date = ? where call_id = ? and in_call = 1 and user_id in ("+placeholders(len(userIds))+")", args...)
	return err
}

// Users returns the accounts in a call.
func (s *Store) Users(ctx context.Context, callId int64) ([]int64, error) {
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids, "select user_id from group_call_participants where call_id = ? and in_call = 1", callId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}
