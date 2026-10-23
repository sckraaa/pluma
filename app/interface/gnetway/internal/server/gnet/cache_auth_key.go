// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
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

package gnet

import (
	"fmt"
)

// cachedAuthKeySize approximates the memory one cached key takes (the
// 256-byte key, AuthKeyInfo, authKeyUtil and the LRU entry), so that
// Gnetway.AuthKeyCacheM really bounds the cache in megabytes.
const cachedAuthKeySize = 640

type CacheV struct {
	V *authKeyUtil
}

func (c CacheV) Size() int {
	return cachedAuthKeySize
}

func (s *Server) GetAuthKey(authKeyId int64) *authKeyUtil {
	var (
		cacheK = fmt.Sprintf("%d", authKeyId)
		value  *CacheV
	)

	if v, ok := s.cache.Get(cacheK); ok {
		value = v.(*CacheV)
	}

	if value == nil {
		return nil
	} else {
		return value.V
	}
}

// PutAuthKey caches a key read from authsession or created by a handshake.
// Its binding is whatever authsession returned: the gateway never derives it
// from client messages.
func (s *Server) PutAuthKey(key *authKeyUtil) {
	var (
		cacheK = fmt.Sprintf("%d", key.AuthKeyId())
	)

	s.cache.Set(cacheK, &CacheV{V: key})
}

// RemoveAuthKey drops a key that authsession no longer knows (or that expired).
func (s *Server) RemoveAuthKey(authKeyId int64) {
	s.cache.Delete(fmt.Sprintf("%d", authKeyId))
}
