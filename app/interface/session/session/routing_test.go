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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestRouteKey(t *testing.T) {
	const (
		keyId  = int64(0x1111)
		permId = int64(0x2222)
	)

	cases := []struct {
		name        string
		authKeyId   int64
		keyType     int32
		permId      int64
		wantKey     int64
		wantUnbound bool
	}{
		{"perm key", permId, mtproto.AuthKeyTypePerm, permId, permId, false},
		{"perm key without perm id", keyId, mtproto.AuthKeyTypePerm, 0, keyId, false},
		{"bound temp key", keyId, mtproto.AuthKeyTypeTemp, permId, permId, false},
		{"bound media temp key", keyId, mtproto.AuthKeyTypeMediaTemp, permId, permId, false},
		{"unbound temp key", keyId, mtproto.AuthKeyTypeTemp, 0, keyId, true},
		{"unbound media temp key", keyId, mtproto.AuthKeyTypeMediaTemp, 0, keyId, true},
		{"unknown key type", keyId, mtproto.AuthKeyTypeUnknown, 0, keyId, false},
		{"no key", 0, mtproto.AuthKeyTypeTemp, 0, 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotKey, gotUnbound := RouteKey(c.authKeyId, c.keyType, c.permId)
			if gotKey != c.wantKey || gotUnbound != c.wantUnbound {
				t.Fatalf("RouteKey(%d, %d, %d) = (%d, %v), want (%d, %v)",
					c.authKeyId, c.keyType, c.permId, gotKey, gotUnbound, c.wantKey, c.wantUnbound)
			}
		})
	}
}
