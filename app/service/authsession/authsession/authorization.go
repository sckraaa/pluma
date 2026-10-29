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

package authsession

// AuthorizationAutoconfirmPeriod is how long a new login stays unconfirmed
// when nobody confirms or resets it (task 019): seconds after its
// date_created. Clients get it as authorization_autoconfirm_period of
// help.getAppConfig and drop their review banner after it; 604800 is the
// value of Telegram's example config and the fallback of its clients
// (https://core.telegram.org/api/config#authorization-autoconfirm-period).
const AuthorizationAutoconfirmPeriod = 7 * 24 * 60 * 60

// IsUnconfirmed says whether an authorization stored as unconfirmed, created
// at dateCreated, is still unconfirmed at now.
func IsUnconfirmed(stored bool, dateCreated, now int64) bool {
	return stored && now-dateCreated < AuthorizationAutoconfirmPeriod
}
