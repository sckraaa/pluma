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

package dao

import (
	"maps"
	"slices"
	"sync"
	"time"
)

// counterPush keeps the pushes of a channel's counters to at most one per
// window, whatever the number of viewers: the first change of a quiet
// channel goes at once, the changes during the window together when it
// ends, each post with its count of that moment. Its state is the
// process's: after a restart the changes it held are not pushed, and the
// clients read them with getMessagesViews and with the history. Each
// process of the service keeps its own window.
type counterPush struct {
	window time.Duration
	send   func(channelId, userId int64, views, forwards []int32)

	mu       sync.Mutex
	channels map[int64]*heldCounters
}

// heldCounters are the changes of a channel in the current window; its
// presence in channels is the window itself.
type heldCounters struct {
	userId   int64
	views    map[int32]struct{}
	forwards map[int32]struct{}
}

func newCounterPush(window time.Duration, send func(channelId, userId int64, views, forwards []int32)) *counterPush {
	return &counterPush{window: window, send: send, channels: map[int64]*heldCounters{}}
}

// mark tells that userId changed the views and the forwards of these posts
// of the channel. The slices are not written to afterwards.
func (p *counterPush) mark(channelId, userId int64, views, forwards []int32) {
	if len(views) == 0 && len(forwards) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	held, inWindow := p.channels[channelId]
	if !inWindow {
		p.channels[channelId] = &heldCounters{views: map[int32]struct{}{}, forwards: map[int32]struct{}{}}
		p.startWindow(channelId)
		go p.send(channelId, userId, views, forwards)
		return
	}
	held.userId = userId
	for _, id := range views {
		held.views[id] = struct{}{}
	}
	for _, id := range forwards {
		held.forwards[id] = struct{}{}
	}
}

func (p *counterPush) startWindow(channelId int64) {
	time.AfterFunc(p.window, func() { p.windowEnded(channelId) })
}

// windowEnded sends what the window held and opens the next one; a window
// that held nothing leaves the channel quiet.
func (p *counterPush) windowEnded(channelId int64) {
	p.mu.Lock()
	held := p.channels[channelId]
	if held == nil || len(held.views) == 0 && len(held.forwards) == 0 {
		delete(p.channels, channelId)
		p.mu.Unlock()
		return
	}
	p.channels[channelId] = &heldCounters{views: map[int32]struct{}{}, forwards: map[int32]struct{}{}}
	p.startWindow(channelId)
	p.mu.Unlock()
	p.send(channelId, held.userId, slices.Sorted(maps.Keys(held.views)), slices.Sorted(maps.Keys(held.forwards)))
}
