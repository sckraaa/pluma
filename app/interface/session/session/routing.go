// Copyright 2024 Teamgram Authors
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

package session

import (
	"github.com/teamgram/proto/mtproto"
)

// RouteKey returns the key that the gateway shards client traffic by and that
// the session service groups it under (one MainAuthWrapper per key).
//
//   - A permanent key, or a temporary key bound to one, is routed by the
//     permanent auth key id. permAuthKeyId comes from authsession and is set
//     only after auth.bindTempAuthKey has been verified there.
//   - A temporary key that is not bound yet (permAuthKeyId == 0) is routed by
//     its own id and unboundTemp is true. The session serves it in a
//     restricted wrapper that has no user and allows only the methods
//     https://core.telegram.org/api/pfs lists for unbound keys.
//
// A permanent key with permAuthKeyId == 0 is its own permanent key.
// routeKey is 0 only when authKeyId is 0 and nothing can be routed.
func RouteKey(authKeyId int64, keyType int32, permAuthKeyId int64) (routeKey int64, unboundTemp bool) {
	if permAuthKeyId != 0 {
		return permAuthKeyId, false
	}

	switch keyType {
	case mtproto.AuthKeyTypeTemp, mtproto.AuthKeyTypeMediaTemp:
		return authKeyId, authKeyId != 0
	default:
		return authKeyId, false
	}
}
