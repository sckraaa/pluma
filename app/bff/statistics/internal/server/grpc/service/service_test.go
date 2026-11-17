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

package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/status"
)

// Every method of RPCStatistics (routed to bff by session.yaml) answers
// ERR_ENTERPRISE_IS_BLOCKED (400) while the statistics are off and for the
// methods not served, which the clients do not retry, never gRPC
// Unimplemented (500).
func TestOffAndNotServed(t *testing.T) {
	s := New(&svc.ServiceContext{Flight: new(singleflight.Group)})
	v := reflect.ValueOf(s)
	iface := reflect.TypeOf((*mtproto.RPCStatisticsServer)(nil)).Elem()
	if iface.NumMethod() != 9 {
		t.Fatalf("%d methods of RPCStatistics, want 9", iface.NumMethod())
	}
	want := status.Convert(mtproto.ErrEnterpriseIsBlocked)
	for i := range iface.NumMethod() {
		m := iface.Method(i)
		req := reflect.New(m.Type.In(1).Elem())
		out := v.MethodByName(m.Name).Call([]reflect.Value{reflect.ValueOf(context.Background()), req})
		err, _ := out[1].Interface().(error)
		if got := status.Convert(err); got.Code() != want.Code() || got.Message() != want.Message() {
			t.Errorf("%s: %v, want %v", m.Name, err, mtproto.ErrEnterpriseIsBlocked)
		}
	}
}
