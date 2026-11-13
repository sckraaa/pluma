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
	"errors"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// eventTimeout bounds the work of one event.
	eventTimeout = 10 * time.Second
	// activeEvery: active_date of a participant is written at most this
	// often; it orders the list (iOS GroupCalls.swift:1377-1423), seconds
	// do not matter.
	activeEvery = 10 * time.Second
)

// Events takes the events of the SFU nodes (ADR 0059 §3): a connection
// that ended without a leave takes its participant out of the call, and
// who speaks gets an active_date. Every bff instance follows every node;
// the leaves are conditional updates, so two instances do not leave one
// participant twice.
type Events struct {
	svcCtx *svc.ServiceContext

	mu     sync.Mutex
	active map[activeKey]time.Time
}

type activeKey struct {
	call, user int64
}

// NewEvents returns the intake over svcCtx.
func NewEvents(svcCtx *svc.ServiceContext) *Events {
	return &Events{svcCtx: svcCtx, active: map[activeKey]time.Time{}}
}

// Handle takes one event of node.
func (e *Events) Handle(node string, ev *pb.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
	defer cancel()
	c := background(ctx, e.svcCtx)
	switch ev.GetType() {
	case pb.Event_TYPE_CONNECTED:
		c.Logger.Infof("groupcalls: call %d: %d connected (%s) over %s as %s in %d ms",
			ev.GetCallId(), ev.GetParticipant(), ev.GetKind(), ev.GetNetwork(), ev.GetRole(), ev.GetConnectMs())
	case pb.Event_TYPE_DISCONNECTED:
		c.Logger.Infof("groupcalls: call %d: %d disconnected (%s): %s", ev.GetCallId(), ev.GetParticipant(), ev.GetKind(), ev.GetReason())
		c.gone(node, ev)
	case pb.Event_TYPE_SPEAKING:
		e.speaking(c, ev)
	case pb.Event_TYPE_E2E_STATUS:
		if ev.GetKind() == pb.ConnectionKind_CONNECTION_KIND_MAIN && c.hosts(node, ev) {
			e.svcCtx.E2E.Report(ev.GetCallId(), ev.GetParticipant(), ev.GetE2EStatus())
		}
	case pb.Event_TYPE_SEND_EPOCHS:
		if ev.GetKind() == pb.ConnectionKind_CONNECTION_KIND_MAIN && c.hosts(node, ev) {
			e.svcCtx.E2E.SendEpochs(ev.GetCallId(), ev.GetParticipant(), ev.GetSendEpochs())
		}
	}
}

// hosts reports whether node holds the room of the event's call and its
// participant is in the call. A node may stand on a machine outside the
// trusted zone (ADR 0060 §3): what it says of the encryption counts only
// for the calls placed on it, as its word on a connection that ended does
// (gone), so that it can neither fill the watch with calls it made up nor
// raise alarms in calls of other nodes.
func (c *GroupCallsCore) hosts(node string, ev *pb.Event) bool {
	call, err := c.svcCtx.Dao.Store.Get(c.ctx, ev.GetCallId())
	if err == nil && !call.Discarded && call.SfuNode == node {
		var p *groupcalls.Participant
		if p, err = c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, ev.GetParticipant()); err == nil && p.InCall {
			return true
		}
	}
	if err != nil && !errors.Is(err, groupcalls.ErrNotFound) {
		c.Logger.Errorf("groupcalls: the call of an event of %s: %v", node, err)
		return false
	}
	metricForeignEvents.Inc(ev.GetType().String())
	return false
}

// gone takes the participant of a connection that ended out of the call,
// or its screen. The SFU's word is checked against its room first: a
// participant who joined again since has a new connection there. A join
// writes the state before it connects the SFU, so in between the room
// holds nothing of a participant who joined again: the leave is then
// conditional on the source of the connection that ended, which the event
// names, and a join with a new source is not taken out.
func (c *GroupCallsCore) gone(node string, ev *pb.Event) {
	call, err := c.svcCtx.Dao.Store.Get(c.ctx, ev.GetCallId())
	if err != nil || call.Discarded || call.SfuNode != node {
		return
	}
	if ev.GetKind() == pb.ConnectionKind_CONNECTION_KIND_PRESENTATION {
		after, row, changed, err := c.svcCtx.Dao.Store.SetPresentation(c.ctx, call.Id, ev.GetParticipant(), 0, nil)
		if err != nil || !changed {
			return
		}
		if a, err := c.loadPeer(call.PeerType, call.PeerId); err == nil {
			c.deliver(a, after, fanOut{changes: []groupcalls.Change{{Participant: row}}})
		}
		return
	}
	p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, ev.GetParticipant())
	if err != nil || !p.InCall {
		return
	}
	sources, err := held(c.ctx, c.svcCtx.SFU, call, p.UserId)
	if err != nil || len(sources) > 0 {
		return
	}
	source := p.Source
	if ev.GetSource() != 0 {
		source = ev.GetSource()
	}
	c.leaveGone(call, p, groupcalls.LeaveCond{Source: source}, "disconnect")
}

// leaveGone takes one who is gone out of the call under cond and tells
// everyone.
func (c *GroupCallsCore) leaveGone(call *groupcalls.Call, p *groupcalls.Participant, cond groupcalls.LeaveCond, reason string) {
	after, change, err := c.svcCtx.Dao.Store.Leave(c.ctx, call.Id, p.UserId, cond)
	if err != nil {
		c.Logger.Errorf("groupcalls: leave of %d from call %d: %v", p.UserId, call.Id, err)
		return
	}
	if change == nil {
		return
	}
	metricLeaves.Inc(reason)
	c.svcCtx.E2E.Left(call.Id, p.UserId)
	c.disconnect(after, p.UserId)
	a, err := c.loadPeer(call.PeerType, call.PeerId)
	if err != nil {
		c.Logger.Errorf("groupcalls: the chat of call %d: %v", call.Id, err)
		return
	}
	flip := (call.ParticipantsCount == 0) != (after.ParticipantsCount == 0)
	c.deliver(a, after, fanOut{changes: []groupcalls.Change{*change}, call: true, chats: flip})
}

// speaking writes active_date of those who spoke, at most every
// activeEvery each.
func (e *Events) speaking(c *GroupCallsCore, ev *pb.Event) {
	now := time.Now()
	var ids []int64
	e.mu.Lock()
	for _, l := range ev.GetLevels() {
		k := activeKey{ev.GetCallId(), l.GetParticipant()}
		if now.Sub(e.active[k]) >= activeEvery {
			e.active[k] = now
			ids = append(ids, l.GetParticipant())
		}
	}
	if len(e.active) > 4096 {
		for k, t := range e.active {
			if now.Sub(t) > time.Minute {
				delete(e.active, k)
			}
		}
	}
	e.mu.Unlock()
	if err := c.svcCtx.Dao.Store.MarkActive(c.ctx, ev.GetCallId(), ids, now); err != nil {
		logx.WithContext(c.ctx).Errorf("groupcalls: who speaks in call %d: %v", ev.GetCallId(), err)
	}
}
