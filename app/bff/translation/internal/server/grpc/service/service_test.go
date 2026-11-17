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
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"

	"google.golang.org/grpc/status"
)

// The tones of the AI editor (RPCAiComposeTone, routed to bff by
// session.yaml) answer ERR_ENTERPRISE_IS_BLOCKED (400), which the clients
// do not retry, never gRPC Unimplemented (500).
func TestAiComposeTonesNotServed(t *testing.T) {
	s := New(&svc.ServiceContext{})
	v := reflect.ValueOf(s)
	iface := reflect.TypeOf((*mtproto.RPCAiComposeToneServer)(nil)).Elem()
	n := 0
	for i := range iface.NumMethod() {
		m := iface.Method(i)
		if !strings.HasPrefix(m.Name, "Aicompose") {
			continue
		}
		n++
		req := reflect.New(m.Type.In(1).Elem())
		out := v.MethodByName(m.Name).Call([]reflect.Value{reflect.ValueOf(context.Background()), req})
		err, _ := out[1].Interface().(error)
		want := status.Convert(mtproto.ErrEnterpriseIsBlocked)
		if got := status.Convert(err); got.Code() != want.Code() || got.Message() != want.Message() {
			t.Errorf("%s: %v, want %v", m.Name, err, mtproto.ErrEnterpriseIsBlocked)
		}
	}
	if n != 7 {
		t.Fatalf("%d methods of aicompose, want 7", n)
	}
}
