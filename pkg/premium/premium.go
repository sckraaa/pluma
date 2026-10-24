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

// Package premium holds the rules of Premium (granted by the admin, task
// 014, ADR 0016) that several services apply.
package premium

import (
	"github.com/teamgram/proto/mtproto"

	"google.golang.org/grpc/status"
)

// ErrPrivacyPremiumRequired: the user lets only contacts, users with a
// private chat there already and Premium users write.
var ErrPrivacyPremiumRequired = status.Error(mtproto.ErrForbidden, "PRIVACY_PREMIUM_REQUIRED")

// RequiredToContact tells whether sender needs Premium to write to target
// in a private chat, as Telegram decides it: target turned on
// globalPrivacySettings.new_noncontact_peers_require_premium and has Premium
// (ImmutableUser.ContactRequirePremium), and sender has no Premium, is not
// a contact of target and target has no private chat with sender.
//
// Whether sender is a contact of target is read from either shape the user
// service loads: target's contacts (user.getMutableUsers with sender among
// id or to) or sender's reverse contacts (user.getMutableUsersV2 with
// has_to and target among to). hasChat tells whether target has a private
// chat with sender; it is asked only when the rest does not decide.
func RequiredToContact(sender, target *mtproto.ImmutableUser, hasChat func() (bool, error)) (bool, error) {
	if sender.Id() == target.Id() || !target.ContactRequirePremium() || sender.Premium() {
		return false, nil
	}
	if contact, _ := target.CheckContact(sender.Id()); contact {
		return false, nil
	}
	if contact, _ := sender.CheckReverseContact(target.Id()); contact {
		return false, nil
	}
	chat, err := hasChat()
	if err != nil {
		return false, err
	}
	return !chat, nil
}
