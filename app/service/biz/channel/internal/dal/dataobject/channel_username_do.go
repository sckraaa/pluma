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

package dataobject

// ChannelUsernameDO is a username of a channel: a row of username
// (peer_type PEER_CHANNEL). The editable one is its public link
// (channels.updateUsername); the others, when it has them, are listed with
// it in channel.usernames.
type ChannelUsernameDO struct {
	PeerId   int64  `db:"peer_id" json:"peer_id"`
	Username string `db:"username" json:"username"`
	Editable bool   `db:"editable" json:"editable"`
	Active   bool   `db:"active" json:"active"`
}
