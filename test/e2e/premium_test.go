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

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// rpcErrorIs tells whether err is the RPC error code.
func rpcErrorIs(err error, code string) bool {
	var rpcErr *mtclient.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Message == code
}

// fullUser returns users.getFullUser of input as u sees it.
func fullUser(ctx context.Context, t *testing.T, u *user, input *mtproto.InputUser) (*mtproto.UserFull, *mtproto.User) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
		Constructor: u.c.Constructor(mtproto.Predicate_users_getFullUser),
		Id:          input,
	})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	r, ok := o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	})
	if !ok {
		t.Fatalf("users.getFullUser returned %T", o)
	}
	full := r.To_Users_UserFull()
	for _, x := range full.GetUsers() {
		if x.GetId() == full.GetFullUser().GetId() {
			return full.GetFullUser(), x
		}
	}
	t.Fatalf("users.getFullUser without the user %d", full.GetFullUser().GetId())
	return nil, nil
}

func inputSelf(u *user) *mtproto.InputUser {
	return &mtproto.InputUser{Constructor: u.c.Constructor(mtproto.Predicate_inputUserSelf)}
}

// resolve returns target as asker finds it by phone (contacts.resolvePhone).
func resolve(ctx context.Context, t *testing.T, asker, target *user) *mtproto.User {
	t.Helper()
	o, err := asker.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
		Constructor: asker.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
		Phone:       strings.TrimPrefix(target.phone, "+"),
	})
	if err != nil {
		t.Fatalf("contacts.resolvePhone: %v", err)
	}
	r, ok := o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	})
	if !ok {
		t.Fatalf("contacts.resolvePhone returned %T", o)
	}
	for _, x := range r.To_Contacts_ResolvedPeer().GetUsers() {
		if x.GetId() == target.self.GetId() && x.GetAccessHash() != nil {
			return x
		}
	}
	t.Fatalf("contacts.resolvePhone did not return user %d with an access hash", target.self.GetId())
	return nil
}

func sendText(ctx context.Context, from *user, to *mtproto.User, text string) error {
	_, err := from.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: from.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer: &mtproto.InputPeer{
			Constructor: from.c.Constructor(mtproto.Predicate_inputPeerUser),
			UserId:      to.GetId(),
			AccessHash:  to.GetAccessHash().GetValue(),
		},
		Message:  text,
		RandomId: randomID(),
	})
	return err
}

// requirementsToContact returns the predicates users.getRequirementsToContact
// answers u for the users.
func requirementsToContact(ctx context.Context, t *testing.T, u *user, users ...*mtproto.User) []string {
	t.Helper()
	ids := make([]*mtproto.InputUser, 0, len(users))
	for _, x := range users {
		ids = append(ids, &mtproto.InputUser{
			Constructor: u.c.Constructor(mtproto.Predicate_inputUser),
			UserId:      x.GetId(),
			AccessHash:  x.GetAccessHash().GetValue(),
		})
	}
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUsersGetRequirementsToContact{
		Constructor: u.c.Constructor(mtproto.Predicate_users_getRequirementsToContact),
		Id:          ids,
	})
	if err != nil {
		t.Fatalf("users.getRequirementsToContact: %v", err)
	}
	d := mtproto.NewDecodeBuf(raw)
	if crc := d.Int(); mtproto.TLConstructor(crc) != mtproto.CRC32_vector {
		t.Fatalf("users.getRequirementsToContact returned constructor %#x", uint32(crc))
	}
	out := make([]string, d.Int())
	for i := range out {
		switch d.Object().(type) {
		case *mtproto.TLRequirementToContactEmpty:
			out[i] = mtproto.Predicate_requirementToContactEmpty
		case *mtproto.TLRequirementToContactPremium:
			out[i] = mtproto.Predicate_requirementToContactPremium
		default:
			out[i] = "other"
		}
	}
	if err := d.GetError(); err != nil {
		t.Fatal(err)
	}
	return out
}

// folders returns u's folder ids in their order and whether tags are on.
func folders(ctx context.Context, t *testing.T, u *user) ([]int32, bool) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogFiltersEFD48C89{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogFiltersEFD48C89),
	})
	if err != nil {
		t.Fatalf("messages.getDialogFilters: %v", err)
	}
	r, ok := o.(*mtproto.TLMessagesDialogFilters)
	if !ok {
		t.Fatalf("messages.getDialogFilters returned %T", o)
	}
	ids := make([]int32, 0, len(r.GetFilters()))
	for _, f := range r.GetFilters() {
		ids = append(ids, f.GetId())
	}
	return ids, r.GetTagsEnabled()
}

func toggleTags(ctx context.Context, u *user, on bool) error {
	_, err := u.c.Invoke(ctx, &mtproto.TLMessagesToggleDialogFilterTags{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_toggleDialogFilterTags),
		Enabled:     mtproto.ToBool(on),
	})
	return err
}

func setPremiumOnly(ctx context.Context, u *user, on bool) error {
	_, err := u.c.Invoke(ctx, &mtproto.TLAccountSetGlobalPrivacySettings{
		Constructor: u.c.Constructor(mtproto.Predicate_account_setGlobalPrivacySettings),
		Settings: &mtproto.GlobalPrivacySettings{
			Constructor:                      u.c.Constructor(mtproto.Predicate_globalPrivacySettings),
			NewNoncontactPeersRequirePremium: on,
		},
	})
	return err
}

// TestPremiumForEveryone: every user has Premium (ADR 0041): the flag on
// their user as themselves and others see it, the limits and features of
// Premium, and no refusal for what Premium used to allow.
func TestPremiumForEveryone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)

	aliceForBob := resolve(ctx, t, bob, alice)
	if _, me := fullUser(ctx, t, alice, inputSelf(alice)); !me.GetPremium() {
		t.Error("the user's own view has no Premium")
	}
	if !aliceForBob.GetPremium() {
		t.Error("another user's view has no Premium")
	}

	// 30 folders, tags, "All chats" anywhere.
	for id := int32(2); id < 32; id++ {
		updateDialogFilter(ctx, t, alice, id, folderOf(id, fmt.Sprint("F", id)))
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFilter{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateDialogFilter),
		Id:          32,
		Filter:      folderOf(32, "F32"),
	}); !rpcErrorIs(err, "DIALOG_FILTERS_TOO_MUCH") {
		t.Fatalf("a 31st folder: %v, want DIALOG_FILTERS_TOO_MUCH", err)
	}
	if err := toggleTags(ctx, alice, true); err != nil {
		t.Fatalf("tags: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFiltersOrder{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateDialogFiltersOrder),
		Order:       []int32{2, 0, 3},
	}); err != nil {
		t.Fatalf("messages.updateDialogFiltersOrder: %v", err)
	}
	if ids, tags := folders(ctx, t, alice); len(ids) < 3 || !slices.Equal(ids[:3], []int32{2, 0, 3}) || !tags {
		t.Fatalf("folders %v, tags %v; want All chats second and tags on", ids, tags)
	}

	// "Contacts and Premium only" can be set; nobody needs anything to write,
	// because everybody has Premium.
	if err := setPremiumOnly(ctx, alice, true); err != nil {
		t.Fatalf("contacts and Premium only: %v", err)
	}
	if r := requirementsToContact(ctx, t, bob, aliceForBob); !slices.Equal(r, []string{mtproto.Predicate_requirementToContactEmpty}) {
		t.Fatalf("users.getRequirementsToContact: %v", r)
	}
	if err := sendText(ctx, carol, resolve(ctx, t, carol, alice), "Hi"); err != nil {
		t.Fatalf("a stranger writes: %v", err)
	}
}
