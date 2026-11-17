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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// TestChatLinkErrors: the errors of the store become the errors of the
// methods, and each result of account.resolveBusinessChatLink has its
// label of business_chatlink_resolve_total.
func TestChatLinkErrors(t *testing.T) {
	other := errors.New("connection refused")
	for _, c := range []struct {
		err, want error
	}{
		{business.ErrChatLinkNotFound, mtproto.ErrChatlinkSlugExpired},
		{business.ErrChatLinksTooMuch, mtproto.ErrChatlinksTooMuch},
		{other, other},
	} {
		if got := chatLinkError(c.err); !errors.Is(got, c.want) {
			t.Errorf("chatLinkError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{mtproto.ErrChatlinkSlugEmpty, "empty"},
		{mtproto.ErrChatlinkSlugExpired, "expired"},
		{chatLinkError(business.ErrChatLinkNotFound), "expired"},
		{other, "error"},
	} {
		if got := resolveResult(c.err); got != c.want {
			t.Errorf("resolveResult(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
