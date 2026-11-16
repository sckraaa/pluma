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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"
)

// params are initConnection params as the session stores them: the
// JSONValue marshalled with jsonx (sess.MainAuthWrapper.onInitConnection).
func params(t *testing.T, fields ...*mtproto.JSONObjectValue) string {
	t.Helper()
	s, err := jsonx.MarshalToString(mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: fields}).To_JSONValue())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func field(key string, value *mtproto.JSONValue) *mtproto.JSONObjectValue {
	return mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: key, Value: value}).To_JSONObjectValue()
}

func number(v float64) *mtproto.JSONValue {
	return mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: v}).To_JSONValue()
}

func TestPlumaCdnVersion(t *testing.T) {
	str := mtproto.MakeTLJsonString(&mtproto.JSONValue{Value_STRING: "1"}).To_JSONValue()
	obj := mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: []*mtproto.JSONObjectValue{field("v", number(1))}}).To_JSONValue()
	null := mtproto.MakeTLJsonNull(nil).To_JSONValue()
	for name, c := range map[string]struct {
		params string
		want   int
	}{
		"1":                {params(t, field("tz_offset", number(10800)), field("pluma_cdn", number(1))), 1},
		"2":                {params(t, field("pluma_cdn", number(2))), 2},
		"1.5":              {params(t, field("pluma_cdn", number(1.5))), 1},
		"the first counts": {params(t, field("pluma_cdn", number(1)), field("pluma_cdn", number(0))), 1},
		"a string":         {params(t, field("pluma_cdn", str)), 0},
		"0":                {params(t, field("pluma_cdn", number(0))), 0},
		"negative":         {params(t, field("pluma_cdn", number(-1))), 0},
		"huge":             {params(t, field("pluma_cdn", number(1e300))), 0},
		"an object":        {params(t, field("pluma_cdn", obj)), 0},
		"null value":       {params(t, field("pluma_cdn", null)), 0},
		"missing":          {params(t, field("tz_offset", number(1))), 0},
		"empty":            {"", 0},
		"null":             {"null", 0},
		"not a JSON":       {"{", 0},
		"not an object":    {`{"predicate_name":"jsonNumber","value_FLOAT64":1}`, 0},
	} {
		if got := PlumaCdnVersion(c.params); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}
