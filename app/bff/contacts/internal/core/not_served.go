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

// The methods of contacts the server does not serve: each answers
// ERR_ENTERPRISE_IS_BLOCKED (400), which the clients do not retry.

// ContactsGetLocated is not served: contacts.getLocated#d348bc44, the people
// and geo groups nearby. Pluma has none (ADR 0050): none of the pinned
// clients calls it since Android dropped PeopleNearbyActivity in 11.1.1, and
// the point it carries is neither kept nor logged.
func (c *ContactsCore) ContactsGetLocated(in *mtproto.TLContactsGetLocated) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
