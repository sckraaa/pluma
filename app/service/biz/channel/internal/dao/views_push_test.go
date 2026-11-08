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
	"slices"
	"sync"
	"testing"
	"time"
)

type sentCounters struct {
	channelId, userId int64
	views, forwards   []int32
}

type counterSink struct {
	mu   sync.Mutex
	sent []sentCounters
	got  chan struct{}
}

func newCounterSink() *counterSink { return &counterSink{got: make(chan struct{}, 100)} }

func (s *counterSink) send(channelId, userId int64, views, forwards []int32) {
	s.mu.Lock()
	s.sent = append(s.sent, sentCounters{channelId, userId, views, forwards})
	s.mu.Unlock()
	s.got <- struct{}{}
}

func (s *counterSink) wait(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.got:
		case <-time.After(5 * time.Second):
			t.Fatal("no push")
		}
	}
}

func (s *counterSink) all() []sentCounters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sent)
}

// TestCounterPushBounded: the first change of a quiet channel goes at once,
// the others of its window together when it ends, whoever made them.
func TestCounterPushBounded(t *testing.T) {
	const channelId, other = int64(10), int64(20)
	sink := newCounterSink()
	p := newCounterPush(100*time.Millisecond, sink.send)

	p.mark(channelId, 1, []int32{1}, nil)
	sink.wait(t, 1)
	for i := range 20 {
		p.mark(channelId, int64(100+i), []int32{2, 3}, nil)
		p.mark(channelId, int64(100+i), nil, []int32{3})
	}
	// Another channel has its own window.
	p.mark(other, 5, []int32{9}, nil)
	sink.wait(t, 2)

	var ofChannel []sentCounters
	for _, s := range sink.all() {
		if s.channelId == channelId {
			ofChannel = append(ofChannel, s)
		}
	}
	if len(ofChannel) != 2 {
		t.Fatalf("%d pushes of the channel, want 2: %v", len(ofChannel), ofChannel)
	}
	if !slices.Equal(ofChannel[0].views, []int32{1}) || ofChannel[0].userId != 1 {
		t.Errorf("the first push: %+v", ofChannel[0])
	}
	if s := ofChannel[1]; !slices.Equal(s.views, []int32{2, 3}) || !slices.Equal(s.forwards, []int32{3}) || s.userId != 119 {
		t.Errorf("the push of the window: %+v", s)
	}
}

// TestCounterPushQuietAgain: a window that held nothing ends the channel's
// turn, and the next change goes at once.
func TestCounterPushQuietAgain(t *testing.T) {
	sink := newCounterSink()
	p := newCounterPush(30*time.Millisecond, sink.send)

	p.mark(1, 1, []int32{1}, nil)
	sink.wait(t, 1)
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		quiet := len(p.channels) == 0
		p.mu.Unlock()
		if quiet {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the channel stays in its window")
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.mark(1, 2, []int32{2}, nil)
	sink.wait(t, 1)
	if got := sink.all(); len(got) != 2 || !slices.Equal(got[1].views, []int32{2}) {
		t.Errorf("pushes: %+v", got)
	}
}

func TestCountedIds(t *testing.T) {
	long := make([]int32, countersMax+50)
	for i := range long {
		long[i] = int32(len(long) - i)
	}
	if got := countedIds(long); len(got) != countersMax || !slices.IsSorted(got) {
		t.Errorf("a long list: %d ids", len(got))
	}
	if got := countedIds([]int32{5, 3, 5}); !slices.Equal(got, []int32{3, 5}) {
		t.Errorf("duplicates: %v", got)
	}
}
