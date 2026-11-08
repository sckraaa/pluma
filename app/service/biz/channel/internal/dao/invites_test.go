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
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

func TestCheckLimits(t *testing.T) {
	const now = 1000
	for _, c := range []struct {
		expire, limit int32
		want          error
	}{
		{0, 0, nil},
		{1001, 1, nil},
		{1000, 0, mtproto.ErrExpireDateInvalid},
		{5, 0, mtproto.ErrExpireDateInvalid},
		{0, -1, mtproto.ErrUsageLimitInvalid},
		{0, InviteUsageMax, nil},
		{0, InviteUsageMax + 1, mtproto.ErrUsageLimitInvalid},
	} {
		if err := checkLimits(c.expire, c.limit, now); !errors.Is(err, c.want) {
			t.Errorf("checkLimits(%d, %d) = %v, want %v", c.expire, c.limit, err, c.want)
		}
	}
}

func TestCutTitle(t *testing.T) {
	long := strings.Repeat("я", InviteTitleMax+10)
	got := cutTitle(long)
	if utf8.RuneCountInString(got) != InviteTitleMax {
		t.Errorf("%d characters", utf8.RuneCountInString(got))
	}
	if !strings.HasPrefix(long, got) {
		t.Errorf("cut %q", got)
	}
	if cutTitle("short") != "short" {
		t.Errorf("a short title changed")
	}
}

// A member of a supergroup may invite but makes no links; an admin needs
// invite_users.
func TestLinkAdmin(t *testing.T) {
	for _, c := range []struct {
		name string
		p    channelperm.Participant
		want error
	}{
		{"creator", channelperm.Participant{Role: channelperm.RoleCreator}, nil},
		{"admin", channelperm.Participant{Role: channelperm.RoleAdmin, AdminRights: mtproto.ADMIN_INVITE_USERS}, nil},
		{"admin without the right", channelperm.Participant{Role: channelperm.RoleAdmin, AdminRights: mtproto.ADMIN_PIN_MESSAGES}, mtproto.ErrChatAdminRequired},
		{"member", channelperm.Participant{Role: channelperm.RoleMember}, mtproto.ErrChatAdminRequired},
	} {
		a := &actor{p: c.p}
		if err := a.linkAdmin(1); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// The links of others are the creator's: an admin acts on its own.
func TestLinksOf(t *testing.T) {
	admin := &actor{p: channelperm.Participant{Role: channelperm.RoleAdmin}}
	creator := &actor{p: channelperm.Participant{Role: channelperm.RoleCreator}}
	if err := admin.linksOf(5, 5); err != nil {
		t.Errorf("its own: %v", err)
	}
	if err := admin.linksOf(5, 6); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Errorf("another's: %v", err)
	}
	if err := creator.linksOf(5, 6); err != nil {
		t.Errorf("the creator: %v", err)
	}
}
