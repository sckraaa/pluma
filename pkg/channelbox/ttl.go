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

package channelbox

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
)

// TTL is the auto-delete period of the channel's new messages, 0 for none
// (task 063), read in the transaction, which has the channel locked.
func (c *Channel) TTL() (int32, error) {
	if c.ttlRead {
		return c.ttl, nil
	}
	if err := c.b.Tx().QueryRow(&c.ttl, "SELECT ttl_period FROM channels WHERE id = ?", c.id); err != nil {
		return 0, err
	}
	c.ttlRead = true
	return c.ttl, nil
}

// SetTTL gives the channel the period for its new messages.
func (c *Channel) SetTTL(period int32) error {
	if _, err := c.b.Tx().Exec("UPDATE channels SET ttl_period = ? WHERE id = ?", period, c.id); err != nil {
		return err
	}
	c.ttl, c.ttlRead = period, true
	return nil
}

// ApplyTTL gives message m of the channel, with its id and date set, the
// channel's period and schedules its deletion: the message and the row that
// deletes it commit together. A message that came with another period (a
// forward) gets the channel's. The service message of a change of the
// period stays.
func (c *Channel) ApplyTTL(m *mtproto.Message) error {
	m.TtlPeriod = nil
	if historyttl.IsChange(m) {
		return nil
	}
	ttl, err := c.TTL()
	if err != nil || ttl == 0 {
		return err
	}
	m.TtlPeriod = mtproto.MakeFlagsInt32(ttl)
	_, err = c.b.Tx().Exec("INSERT INTO channel_message_expiry (channel_id, message_id, expire_at) VALUES (?, ?, ?)",
		c.id, m.GetId(), int64(m.GetDate())+int64(ttl))
	return err
}
