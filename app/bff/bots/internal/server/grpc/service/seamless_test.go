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

// The service is the server of RPCSeamless; declining a login and checking
// a match code, which belong to the logins of sites through
// oauth.pluma.chat, answer ERR_ENTERPRISE_IS_BLOCKED, a 400, until they are
// served.
func TestSeamlessNotServed(t *testing.T) {
	var _ mtproto.RPCSeamlessServer = (*Service)(nil)
	s := New(&svc.ServiceContext{})
	ctx := context.Background()
	if _, err := s.MessagesDeclineUrlAuth(ctx, &mtproto.TLMessagesDeclineUrlAuth{}); err != mtproto.ErrEnterpriseIsBlocked {
		t.Errorf("declineUrlAuth: %v", err)
	}
	if _, err := s.MessagesCheckUrlAuthMatchCode(ctx, &mtproto.TLMessagesCheckUrlAuthMatchCode{}); err != mtproto.ErrEnterpriseIsBlocked {
		t.Errorf("checkUrlAuthMatchCode: %v", err)
	}
}
