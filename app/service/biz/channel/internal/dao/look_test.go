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

package dao

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/peernames"
)

// The usernames of a channel answer as in task 078: an order that is not
// every shown username once is USERNAME_INVALID and the order they have
// CHAT_NOT_MODIFIED (core.telegram.org/method/channels.reorderUsernames),
// unlike account.reorderUsernames.
func TestChannelUsernamesError(t *testing.T) {
	other := errors.New("db down")
	for _, c := range []struct {
		err, want error
	}{
		{peernames.ErrNotFound, mtproto.ErrUsernameInvalid},
		{peernames.ErrEditable, mtproto.ErrUsernameInvalid},
		{peernames.ErrSame, mtproto.ErrUsernameNotModified},
		{peernames.ErrTooMany, mtproto.ErrUsernamesActiveTooMuch},
		{peernames.ErrBadOrder, mtproto.ErrUsernameInvalid},
		{peernames.ErrSameOrder, mtproto.ErrChatNotModified},
		{other, other},
	} {
		if got := channelUsernamesError(c.err); !errors.Is(got, c.want) {
			t.Errorf("%v: %v, want %v", c.err, got, c.want)
		}
	}
}
