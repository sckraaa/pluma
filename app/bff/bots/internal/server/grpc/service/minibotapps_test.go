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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
)

// The service is the server of RPCMiniBotApps, RPCMainMiniBotApps and
// RPCPreparedInlineMessages; the
// methods it does not serve answer ERR_ENTERPRISE_IS_BLOCKED, a 400 the
// clients do not repeat, not gRPC's Unimplemented, a 500 they do.
func TestMiniBotAppsNotServed(t *testing.T) {
	var _ mtproto.RPCMiniBotAppsServer = (*Service)(nil)
	var _ mtproto.RPCMainMiniBotAppsServer = (*Service)(nil)
	var _ mtproto.RPCPreparedInlineMessagesServer = (*Service)(nil)
	s := New(&svc.ServiceContext{})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"requestChatJoinWebView":    func() error { _, err := s.MessagesRequestChatJoinWebView(ctx, nil); return err },
		"invokeWebViewCustomMethod": func() error { _, err := s.BotsInvokeWebViewCustomMethod(ctx, nil); return err },
		"checkDownloadFileParams":   func() error { _, err := s.BotsCheckDownloadFileParams(ctx, nil); return err },
		"requestSimpleWebView old":  func() error { _, err := s.MessagesRequestSimpleWebView1A46500A(ctx, nil); return err },
		"requestAppWebView old":     func() error { _, err := s.MessagesRequestAppWebView8C5A3B3C(ctx, nil); return err },
		"getPopularAppBots":         func() error { _, err := s.BotsGetPopularAppBots(ctx, nil); return err },
		"getPreviewMedias":          func() error { _, err := s.BotsGetPreviewMedias(ctx, nil); return err },
	} {
		if err := call(); err != mtproto.ErrEnterpriseIsBlocked {
			t.Errorf("%s: %v", name, err)
		}
	}
}
