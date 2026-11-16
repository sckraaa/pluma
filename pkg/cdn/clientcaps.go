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

package cdn

import (
	"math"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"
)

// PlumaCdnParam is the key of initConnection.params by which a patched
// client says it downloads from an HTTPS delivery (ADR 0071 §8): a
// jsonNumber, the version of the contract, 1 today. The official clients
// never send it; Android, tdesktop and iOS send other keys there (Android
// jni/tgnet/ConnectionsManager.cpp:3225-3278, tdesktop
// mtproto/session_private.cpp:549-570, iOS BuildConfig.m:157-158), Web K
// sends no params at all (src/lib/mtproto/networker.ts:429-440).
const PlumaCdnParam = "pluma_cdn"

// maxPlumaCdnVersion bounds the versions taken: a larger number is not one
// the server could have agreed with a client.
const maxPlumaCdnVersion = 1 << 16

// PlumaCdnVersion returns the version of the HTTPS CDN a client supports by
// its initConnection params as the session stores them (the JSONValue
// marshalled with jsonx, sess.MainAuthWrapper.onInitConnection; read as
// dao.ClientZone of the configuration service reads tz_offset): the value of
// pluma_cdn if it is a jsonNumber of at least 1, its whole part; 0 for none,
// for a string or any other JSON value, and for params that do not parse.
// The first pluma_cdn of the object counts.
func PlumaCdnVersion(params string) int {
	if params == "" || params == "null" {
		return 0
	}
	var v mtproto.JSONValue
	if err := jsonx.UnmarshalFromString(params, &v); err != nil {
		return 0
	}
	if v.GetPredicateName() != mtproto.Predicate_jsonObject {
		return 0
	}
	for _, kv := range v.GetValue_VECTORJSONOBJECTVALUE() {
		if kv.GetKey() != PlumaCdnParam {
			continue
		}
		if kv.GetValue().GetPredicateName() != mtproto.Predicate_jsonNumber {
			return 0
		}
		n := kv.GetValue().GetValue_FLOAT64()
		if math.IsNaN(n) || n < 1 || n > maxPlumaCdnVersion {
			return 0
		}
		return int(n)
	}
	return 0
}
