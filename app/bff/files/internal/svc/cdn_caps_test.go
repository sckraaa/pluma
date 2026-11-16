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

package svc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/zeromicro/go-zero/core/jsonx"
)

// fakeAuthStates keeps the stored initConnection params of each key and
// counts the reads; err fails them.
type fakeAuthStates struct {
	mu     sync.Mutex
	params map[int64]string
	err    error
	reads  int
}

func (f *fakeAuthStates) AuthsessionGetAuthStateData(_ context.Context, in *authsession.TLAuthsessionGetAuthStateData) (*authsession.AuthKeyStateData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	return authsession.MakeTLAuthKeyStateData(&authsession.AuthKeyStateData{
		AuthKeyId: in.GetAuthKeyId(),
		Client:    authsession.MakeTLClientSession(&authsession.ClientSession{AuthKeyId: in.GetAuthKeyId(), Params: f.params[in.GetAuthKeyId()]}).To_ClientSession(),
	}).To_AuthKeyStateData(), nil
}

// storedParams are params with pluma_cdn v as the session stores them.
func storedParams(t *testing.T, v float64) string {
	t.Helper()
	s, err := jsonx.MarshalToString(mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: []*mtproto.JSONObjectValue{
		mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
			Key:   "pluma_cdn",
			Value: mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: v}).To_JSONValue(),
		}).To_JSONObjectValue(),
	}}).To_JSONValue())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestCdnCaps: the pluma_cdn of the stored params of each key, read from
// authsession once while cached; a failed read is not cached.
func TestCdnCaps(t *testing.T) {
	ctx := context.Background()
	auth := &fakeAuthStates{params: map[int64]string{1: storedParams(t, 1), 2: ""}}
	caps, err := newCdnCaps(auth)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if v, err := caps.PlumaCdn(ctx, 1); err != nil || v != 1 {
			t.Fatalf("key 1: %d, %v", v, err)
		}
	}
	if v, err := caps.PlumaCdn(ctx, 2); err != nil || v != 0 {
		t.Fatalf("key 2: %d, %v", v, err)
	}
	if auth.reads != 2 {
		t.Fatalf("%d reads, want one per key", auth.reads)
	}

	auth.err = errors.New("authsession is down")
	if _, err = caps.PlumaCdn(ctx, 3); err == nil {
		t.Fatal("no error from a failed read")
	}
	auth.err = nil
	auth.params[3] = storedParams(t, 1)
	if v, err := caps.PlumaCdn(ctx, 3); err != nil || v != 1 {
		t.Fatalf("key 3 after the failure: %d, %v", v, err)
	}
}
