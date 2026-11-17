// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gnet

import "sync"

// timeoutWheel manages connection timeouts using a bucketed time wheel.
// Instead of iterating all connections every tick, only the current expired
// bucket is scanned, reducing CPU overhead from O(N) to O(N/numSlots) per tick.
//
// Each slot covers `slotDuration` seconds. Connections are placed into the
// slot corresponding to their closeDate. On each tick, the wheel advances
// and expires all connections in the current slot.

const (
	wheelSlotDuration = 10 // seconds per slot
	wheelNumSlots     = 64 // covers ~640 seconds, enough for 300+10s timeout

	// The ping wheel of the Edge entry: one-second slots over more than
	// the longest ping interval (config.MaxEdgePingInterval).
	pingWheelSlotDuration = 1
	pingWheelNumSlots     = 64
)

type timeoutWheel struct {
	mu           sync.Mutex
	slotDuration int64
	slots        []map[int64]struct{} // connId sets per slot
	// lastExpired is the time ExpireThrough expired last, 0 before.
	lastExpired int64
}

func newTimeoutWheel() *timeoutWheel {
	return newTimeoutWheelOf(wheelSlotDuration, wheelNumSlots)
}

// newPingWheel returns the wheel of the Edge pings (Server.pingWheel).
func newPingWheel() *timeoutWheel {
	return newTimeoutWheelOf(pingWheelSlotDuration, pingWheelNumSlots)
}

func newTimeoutWheelOf(slotDuration int64, numSlots int) *timeoutWheel {
	tw := &timeoutWheel{slotDuration: slotDuration, slots: make([]map[int64]struct{}, numSlots)}
	for i := range tw.slots {
		tw.slots[i] = make(map[int64]struct{})
	}
	return tw
}

// slotIndex returns the slot index for a given unix timestamp.
func (tw *timeoutWheel) slotIndex(unixSec int64) int {
	return int((unixSec / tw.slotDuration) % int64(len(tw.slots)))
}

// Add registers a connection in the slot for the given deadline.
func (tw *timeoutWheel) Add(connId int64, closeDate int64) {
	idx := tw.slotIndex(closeDate)
	tw.mu.Lock()
	tw.slots[idx][connId] = struct{}{}
	tw.mu.Unlock()
}

// Remove removes a connection from the slot for the given deadline.
func (tw *timeoutWheel) Remove(connId int64, closeDate int64) {
	idx := tw.slotIndex(closeDate)
	tw.mu.Lock()
	delete(tw.slots[idx], connId)
	tw.mu.Unlock()
}

// Move moves a connection from oldDeadline slot to newDeadline slot.
// If both map to the same slot, this is a no-op.
func (tw *timeoutWheel) Move(connId int64, oldDeadline, newDeadline int64) {
	oldIdx := tw.slotIndex(oldDeadline)
	newIdx := tw.slotIndex(newDeadline)
	if oldIdx == newIdx {
		return
	}
	tw.mu.Lock()
	delete(tw.slots[oldIdx], connId)
	tw.slots[newIdx][connId] = struct{}{}
	tw.mu.Unlock()
}

// ExpireSlot returns all connection IDs in the slot for the given time and clears that slot.
// Returns nil if the slot is empty.
func (tw *timeoutWheel) ExpireSlot(now int64) []int64 {
	idx := tw.slotIndex(now)
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.takeLocked(idx, nil)
}

// ExpireThrough is ExpireSlot for every second from the one after the
// previous call through now, so that a tick that comes late (the ticker
// fires about once a second, and unix seconds can skip one between two
// ticks) loses no slot; each second is expired once. The first call
// expires the slot of now alone, and so does a call after the clock went
// back: waiting for it to catch up would leave every slot unexpired until
// then.
func (tw *timeoutWheel) ExpireThrough(now int64) []int64 {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if now == tw.lastExpired {
		return nil
	}
	from := now
	if tw.lastExpired > 0 && now > tw.lastExpired {
		from = max(tw.lastExpired+1, now-int64(len(tw.slots))*tw.slotDuration+1)
	}
	tw.lastExpired = now
	var connIds []int64
	lastIdx := -1
	for t := from; t <= now; t++ {
		if idx := tw.slotIndex(t); idx != lastIdx {
			connIds = tw.takeLocked(idx, connIds)
			lastIdx = idx
		}
	}
	return connIds
}

// takeLocked appends the connection IDs of slot idx to connIds and clears
// the slot.
func (tw *timeoutWheel) takeLocked(idx int, connIds []int64) []int64 {
	slot := tw.slots[idx]
	if len(slot) == 0 {
		return connIds
	}
	for id := range slot {
		connIds = append(connIds, id)
	}
	tw.slots[idx] = make(map[int64]struct{})
	return connIds
}
