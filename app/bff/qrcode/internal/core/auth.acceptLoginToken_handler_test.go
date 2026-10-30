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

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/qrcode/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// A token that is not of the login token size is AUTH_TOKEN_INVALID before
// any lookup: the core has no service context to look it up in. The 24-byte
// tokens of the older format are among them.
func TestAcceptLoginTokenSize(t *testing.T) {
	ctx := context.Background()
	c := &QrCodeCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 42, PermAuthKeyId: 7}}
	for _, size := range []int{0, 8, 24, model.LoginTokenSize - 1, model.LoginTokenSize + 1} {
		_, err := c.AuthAcceptLoginToken(&mtproto.TLAuthAcceptLoginToken{Token: make([]byte, size)})
		if !errors.Is(err, mtproto.ErrAuthTokenInvalid) {
			t.Errorf("token of %d bytes: %v, want AUTH_TOKEN_INVALID", size, err)
		}
	}
}
