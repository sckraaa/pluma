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
	"github.com/teamgram/proto/mtproto"
)

// The methods of Telegram Business not served yet (task 105, parts 3-8):
// each answers ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not
// retry, never an internal error (500), which Android's and Web K's
// transports retry without end. A method moves to its own handler file
// when its part serves it.

// AccountUpdateBusinessGreetingMessage is not served yet: account.updateBusinessGreetingMessage#66cdafc4.
func (c *BusinessCore) AccountUpdateBusinessGreetingMessage(in *mtproto.TLAccountUpdateBusinessGreetingMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountUpdateBusinessAwayMessage is not served yet: account.updateBusinessAwayMessage#a26a7fa5.
func (c *BusinessCore) AccountUpdateBusinessAwayMessage(in *mtproto.TLAccountUpdateBusinessAwayMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetBotBusinessConnection is not served yet: account.getBotBusinessConnection#76a86270.
func (c *BusinessCore) AccountGetBotBusinessConnection(in *mtproto.TLAccountGetBotBusinessConnection) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
