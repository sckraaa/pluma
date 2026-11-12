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

package forward

import (
	"sync"

	"github.com/pion/rtp"
)

// PacketCache keeps the last packets of one incoming video stream by
// sequence number, for retransmissions to receivers that lost them. One
// cache serves every receiver of the stream.
type PacketCache struct {
	mu    sync.Mutex
	slots []*rtp.Packet
}

// NewPacketCache keeps size packets (rounded up to a power of two).
func NewPacketCache(size int) *PacketCache {
	n := 1
	for n < size {
		n <<= 1
	}
	return &PacketCache{slots: make([]*rtp.Packet, n)}
}

// Put stores a packet. The cache keeps it as it is; callers do not change
// it afterwards.
func (c *PacketCache) Put(pkt *rtp.Packet) {
	c.mu.Lock()
	c.slots[int(pkt.SequenceNumber)&(len(c.slots)-1)] = pkt
	c.mu.Unlock()
}

// Get returns the packet with the sequence number, nil if it is gone.
func (c *PacketCache) Get(seq uint16) *rtp.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	pkt := c.slots[int(seq)&(len(c.slots)-1)]
	if pkt == nil || pkt.SequenceNumber != seq {
		return nil
	}
	return pkt
}
