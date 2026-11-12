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

package core

import (
	"context"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	collectTick = 5 * time.Second
	// collectBatch bounds one read; the pass goes on to the next batch.
	collectBatch = 64
	// overKeep: rooms of calls that ended this long ago are not tried any
	// more (a node that was removed).
	overKeep = 24 * time.Hour
	// postKeep: a service message is not retried after this long.
	postKeep = time.Hour
)

// Collector compares the calls with the rooms of the SFU (ADR 0059 §10):
// who the SFU has not held for longer than AbsentTimeout leaves the call,
// who may no longer see the chat (removed or banned) leaves it, the SFU's
// mute of each participant is brought back to his word when a SetMuted was
// lost, a room empty for as long is closed, the rooms of ended calls are
// closed, and the service messages that were lost are sent again. It holds
// no state that matters: a restart or a second instance changes nothing.
type Collector struct {
	svcCtx *svc.ServiceContext
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// empty is since when each room was seen empty.
	empty map[int64]time.Time
}

// StartCollector starts the collector over svcCtx.
func StartCollector(svcCtx *svc.ServiceContext) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Collector{svcCtx: svcCtx, cancel: cancel, empty: map[int64]time.Time{}}
	metricSFUConfigured.Set(1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(collectTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.pass(ctx)
			}
		}
	}()
	return s
}

// Stop stops the collector and waits for the pass in progress.
func (s *Collector) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Collector) pass(ctx context.Context) {
	c := background(ctx, s.svcCtx)
	seen := map[int64]bool{}
	for after := int64(0); ctx.Err() == nil; {
		calls, err := s.svcCtx.Dao.Store.Placed(ctx, after, collectBatch)
		if err != nil {
			c.Logger.Errorf("groupcalls: calls with rooms: %v", err)
			break
		}
		for _, call := range calls {
			seen[call.Id] = true
			s.collect(c, call)
			after = call.Id
		}
		if len(calls) < collectBatch {
			break
		}
	}
	for id := range s.empty {
		if !seen[id] {
			delete(s.empty, id)
		}
	}
	over, err := s.svcCtx.Dao.Store.Over(ctx, time.Now().Add(-overKeep), 0, collectBatch)
	if err != nil {
		c.Logger.Errorf("groupcalls: ended calls with rooms: %v", err)
	}
	for _, call := range over {
		c.closeRoom(call)
	}
	unposted, err := s.svcCtx.Dao.Store.Unposted(ctx, time.Now().Add(-postKeep), collectBatch)
	if err != nil {
		c.Logger.Errorf("groupcalls: unposted calls: %v", err)
	}
	for _, call := range unposted {
		// A message just being sent by its step is not raced.
		if time.Since(time.Unix(int64(max(call.Date, call.StartDate, call.EndDate)), 0)) > collectTick {
			c.post(call)
		}
	}
}

// collect compares one call with its room.
func (s *Collector) collect(c *GroupCallsCore, call *groupcalls.Call) {
	now := time.Now()
	in, err := s.svcCtx.Dao.Store.InCall(c.ctx, call.Id)
	if err != nil {
		c.Logger.Errorf("groupcalls: participants of call %d: %v", call.Id, err)
		return
	}
	room, answered := s.roomOf(c, call)
	seen, gone := groupcalls.Sweep(in, room.main, now, s.svcCtx.AbsentTimeout)
	if err = s.svcCtx.Dao.Store.MarkSeen(c.ctx, call.Id, seen, now); err != nil {
		c.Logger.Errorf("groupcalls: seen in call %d: %v", call.Id, err)
	}
	cond := groupcalls.LeaveCond{SeenBefore: now.Add(-s.svcCtx.AbsentTimeout).UnixMilli()}
	out := map[int64]bool{}
	for _, p := range gone {
		out[p.UserId] = true
		c.leaveGone(call, p, cond, "absent")
		call, err = s.svcCtx.Dao.Store.Get(c.ctx, call.Id)
		if err != nil {
			return
		}
	}
	for _, p := range s.strangers(c, call, in, out) {
		out[p.UserId] = true
		c.leaveGone(call, p, groupcalls.LeaveCond{Source: p.Source}, "access")
		call, err = s.svcCtx.Dao.Store.Get(c.ctx, call.Id)
		if err != nil {
			return
		}
	}
	if answered {
		for _, p := range in {
			if out[p.UserId] {
				continue
			}
			if p.Presentation != nil && room.main[p.UserId] && !room.screens[p.UserId] {
				c.gone(call.SfuNode, &pb.Event{CallId: call.Id, Participant: p.UserId, Kind: pb.ConnectionKind_CONNECTION_KIND_PRESENTATION})
			}
			if muted, held := room.muted[p.UserId]; held && muted == p.CanSelfUnmute {
				s.repairMute(c, call, p)
			}
		}
	}
	if len(in) > len(out) || len(room.main) > 0 {
		delete(s.empty, call.Id)
		return
	}
	since, ok := s.empty[call.Id]
	if !ok {
		s.empty[call.Id] = now
		return
	}
	if now.Sub(since) < s.svcCtx.AbsentTimeout {
		return
	}
	// An empty room goes; the next join places the call again. The call
	// itself stays until it is discarded (ADR 0059 §10).
	if s.svcCtx.SFU.Has(call.SfuNode) {
		if err = s.svcCtx.SFU.CloseRoom(c.ctx, call.SfuNode, call.Id); err != nil {
			metricSFUErrors.Inc("close_room")
			c.Logger.Errorf("groupcalls: close the empty room of call %d: %v", call.Id, err)
			return
		}
	}
	// Only while no one is in the call: a join that connected after the
	// room was found empty keeps the node, and one that comes after the
	// room was closed places it again (Store.Join).
	if _, err = s.svcCtx.Dao.Store.ForgetEmpty(c.ctx, call.Id, call.SfuNode); err != nil {
		c.Logger.Errorf("groupcalls: node of call %d: %v", call.Id, err)
	}
	delete(s.empty, call.Id)
}

// repairMute tells the SFU again the word of p, whose mute there differs
// from it: a SetMuted that failed (the SFU forwards the sound of one whose
// word is taken, or holds back one who has it). The row is read again, so
// that an admin's change since the pass began is not undone; one that
// races this send is caught by the next pass.
func (s *Collector) repairMute(c *GroupCallsCore, call *groupcalls.Call, p *groupcalls.Participant) {
	fresh, err := s.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, p.UserId)
	if err != nil || !fresh.InCall || fresh.CanSelfUnmute != p.CanSelfUnmute {
		return
	}
	metricMuteRepairs.Inc()
	c.setMuted(call, fresh)
}

// strangers are those in a call who may no longer see its chat: removed
// from the group or banned from the channel. They leave the call; their
// client, told it left with its own source, joins again (tdesktop
// calls_group_call.cpp:2620-2626) and is refused with GROUPCALL_FORBIDDEN,
// on which it hangs up (:1874-1886). Whether Telegram ends the call of one
// removed this way is not verified; keeping him in would let him hear and
// see a chat he is out of.
func (s *Collector) strangers(c *GroupCallsCore, call *groupcalls.Call, in []*groupcalls.Participant, out map[int64]bool) []*groupcalls.Participant {
	var ids []int64
	for _, p := range in {
		if !out[p.UserId] {
			ids = append(ids, p.UserId)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, ids...)
	if err != nil {
		c.Logger.Errorf("groupcalls: the chat of call %d: %v", call.Id, err)
		return nil
	}
	var list []*groupcalls.Participant
	for _, p := range in {
		if !out[p.UserId] && !a.canView(p.UserId) {
			list = append(list, p)
		}
	}
	return list
}

// heldRoom is what the room of a call holds: the accounts with a main
// connection, those with a screen, and whether the SFU mutes each main
// connection.
type heldRoom struct {
	main, screens, muted map[int64]bool
}

// roomOf is what the room of a call holds. answered is false when the node
// did not answer; then no one is held, and who stays unseen for
// AbsentTimeout leaves.
func (s *Collector) roomOf(c *GroupCallsCore, call *groupcalls.Call) (room heldRoom, answered bool) {
	room = heldRoom{main: map[int64]bool{}, screens: map[int64]bool{}, muted: map[int64]bool{}}
	if !s.svcCtx.SFU.Has(call.SfuNode) {
		// The node is no longer configured: its rooms are lost.
		return room, true
	}
	list, err := s.svcCtx.SFU.Sources(c.ctx, call.SfuNode, call.Id)
	switch {
	case status.Code(err) == codes.NotFound:
		return room, true
	case err != nil:
		metricSFUErrors.Inc("sources")
		logx.WithContext(c.ctx).Errorf("groupcalls: the room of call %d on %s: %v", call.Id, call.SfuNode, err)
		return room, false
	}
	for _, cs := range list {
		if cs.GetKind() == pb.ConnectionKind_CONNECTION_KIND_PRESENTATION {
			room.screens[cs.GetParticipant()] = true
		} else {
			room.main[cs.GetParticipant()] = true
			room.muted[cs.GetParticipant()] = cs.GetMuted()
		}
	}
	return room, true
}
