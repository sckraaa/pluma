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

// Premium granted by the admin (task 014, ADR 0016): what a user without it
// is refused, what pluma-admin premium grant gives, and what stays after
// revoke.

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

// waitSelfPremium waits on u for updateUser of its own user pushed with
// Premium premium (other pushes carry the user too, as it was then).
func waitSelfPremium(ctx context.Context, t *testing.T, u *user, premium bool) {
	t.Helper()
	_, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		if !slices.ContainsFunc(us.GetUpdates(), func(x *mtproto.Update) bool {
			return x.GetPredicateName() == mtproto.Predicate_updateUser && x.GetUserId() == u.self.GetId()
		}) {
			return false
		}
		for _, x := range us.GetUsers() {
			if x.GetId() == u.self.GetId() && x.GetSelf() && x.GetPremium() == premium {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("no push of the own user with Premium %v: %v", premium, err)
	}
}

func TestPremium(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	getState(ctx, t, laptop)
	bob := login(ctx, t, phones[1], "Bob")

	// Without Premium: 10 folders, no tags, no "contacts and Premium only".
	for id := int32(2); id < 12; id++ {
		updateDialogFilter(ctx, t, alice, id, folderOf(id, fmt.Sprint("F", id)))
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFilter{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateDialogFilter),
		Id:          12,
		Filter:      folderOf(12, "F12"),
	}); !rpcErrorIs(err, "DIALOG_FILTERS_TOO_MUCH") {
		t.Fatalf("an 11th folder without Premium: %v, want DIALOG_FILTERS_TOO_MUCH", err)
	}
	if err := toggleTags(ctx, alice, true); !rpcErrorIs(err, "PREMIUM_ACCOUNT_REQUIRED") {
		t.Fatalf("tags without Premium: %v, want PREMIUM_ACCOUNT_REQUIRED", err)
	}
	if err := setPremiumOnly(ctx, alice, true); !rpcErrorIs(err, "PREMIUM_ACCOUNT_REQUIRED") {
		t.Fatalf("contacts and Premium only, without Premium: %v, want PREMIUM_ACCOUNT_REQUIRED", err)
	}
	if _, me := fullUser(ctx, t, alice, inputSelf(alice)); me.GetPremium() {
		t.Fatal("a new user has Premium")
	}

	// The admin grants Premium; Alice's devices learn it at once.
	if out := admin(t, "premium", "grant", alice.phone); !strings.Contains(out, "has Premium for good") {
		t.Fatalf("premium grant: %q", out)
	}
	waitSelfPremium(ctx, t, laptop, true)
	if _, me := fullUser(ctx, t, alice, inputSelf(alice)); !me.GetPremium() {
		t.Fatal("users.getFullUser: no Premium after the grant")
	}
	if out := admin(t, "premium", "list"); !strings.Contains(out, alice.phone) {
		t.Fatalf("premium list without Alice: %q", out)
	}

	// With Premium: more folders, tags, All chats anywhere.
	updateDialogFilter(ctx, t, alice, 12, folderOf(12, "F12"))
	if err := toggleTags(ctx, alice, true); err != nil {
		t.Fatalf("tags with Premium: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDialogFilters, func(*mtproto.Update) bool { return true })
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFiltersOrder{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateDialogFiltersOrder),
		Order:       []int32{2, 0, 3},
	}); err != nil {
		t.Fatalf("messages.updateDialogFiltersOrder: %v", err)
	}
	ids, tags := folders(ctx, t, laptop)
	if len(ids) != 12 || !slices.Equal(ids[:3], []int32{2, 0, 3}) || !tags {
		t.Fatalf("folders with Premium %v, tags %v; want 11 folders and All chats second, tags on", ids, tags)
	}

	// Contacts and Premium only: Bob, a stranger without Premium, is refused.
	if err := setPremiumOnly(ctx, alice, true); err != nil {
		t.Fatalf("contacts and Premium only, with Premium: %v", err)
	}
	aliceForBob := resolve(ctx, t, bob, alice)
	if !aliceForBob.GetContactRequirePremium() {
		t.Error("Bob does not see contact_require_premium on Alice")
	}
	if full, _ := fullUser(ctx, t, bob, &mtproto.InputUser{
		Constructor: bob.c.Constructor(mtproto.Predicate_inputUser),
		UserId:      aliceForBob.GetId(),
		AccessHash:  aliceForBob.GetAccessHash().GetValue(),
	}); !full.GetContactRequirePremium() {
		t.Error("users.getFullUser of Alice for Bob: no contact_require_premium")
	}
	if r := requirementsToContact(ctx, t, bob, aliceForBob); !slices.Equal(r, []string{mtproto.Predicate_requirementToContactPremium}) {
		t.Errorf("users.getRequirementsToContact of Alice for Bob: %v", r)
	}
	if err := sendText(ctx, bob, aliceForBob, "Hi"); !rpcErrorIs(err, "PRIVACY_PREMIUM_REQUIRED") {
		t.Fatalf("Bob writes to Alice: %v, want PRIVACY_PREMIUM_REQUIRED", err)
	}

	// Once Alice writes to Bob, there is a chat and Bob may answer.
	if err := sendText(ctx, alice, resolve(ctx, t, alice, bob), "Hello Bob"); err != nil {
		t.Fatalf("Alice writes to Bob: %v", err)
	}
	if r := requirementsToContact(ctx, t, bob, aliceForBob); !slices.Equal(r, []string{mtproto.Predicate_requirementToContactEmpty}) {
		t.Errorf("after Alice wrote, users.getRequirementsToContact: %v", r)
	}
	if err := sendText(ctx, bob, aliceForBob, "Hi Alice"); err != nil {
		t.Fatalf("Bob answers Alice: %v", err)
	}

	// A contact of Alice may write without a chat, and sees no requirement
	// in userFull either.
	carol := login(ctx, t, phones[2], "Carol")
	aliceForCarol := resolve(ctx, t, carol, alice)
	inputAlice := &mtproto.InputUser{
		Constructor: carol.c.Constructor(mtproto.Predicate_inputUser),
		UserId:      aliceForCarol.GetId(),
		AccessHash:  aliceForCarol.GetAccessHash().GetValue(),
	}
	if full, _ := fullUser(ctx, t, carol, inputAlice); !full.GetContactRequirePremium() {
		t.Error("users.getFullUser of Alice for Carol, a stranger: no contact_require_premium")
	}
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, carol.phone, "Carol", ""))
	if full, _ := fullUser(ctx, t, carol, inputAlice); full.GetContactRequirePremium() {
		t.Error("users.getFullUser of Alice for Carol, her contact: contact_require_premium")
	}
	if r := requirementsToContact(ctx, t, carol, aliceForCarol); !slices.Equal(r, []string{mtproto.Predicate_requirementToContactEmpty}) {
		t.Errorf("users.getRequirementsToContact of Alice for Carol, her contact: %v", r)
	}
	if err := sendText(ctx, carol, aliceForCarol, "Hi from a contact"); err != nil {
		t.Fatalf("Carol, a contact, writes to Alice: %v", err)
	}

	// The admin takes Premium away: the folders stay, nothing more is made,
	// All chats is first again, tags and the setting are off.
	if out := admin(t, "premium", "revoke", alice.phone); !strings.Contains(out, "has no Premium") {
		t.Fatalf("premium revoke: %q", out)
	}
	waitSelfPremium(ctx, t, laptop, false)
	ids, tags = folders(ctx, t, laptop)
	if len(ids) != 12 || ids[0] != 0 || tags {
		t.Fatalf("folders after the revoke %v, tags %v; want all 11 kept, All chats first, tags off", ids, tags)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFilter{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateDialogFilter),
		Id:          13,
		Filter:      folderOf(13, "F13"),
	}); !rpcErrorIs(err, "DIALOG_FILTERS_TOO_MUCH") {
		t.Fatalf("a new folder after the revoke: %v, want DIALOG_FILTERS_TOO_MUCH", err)
	}
	if aliceForBob = resolve(ctx, t, bob, alice); aliceForBob.GetContactRequirePremium() || aliceForBob.GetPremium() {
		t.Error("after the revoke Bob still sees Premium or contact_require_premium on Alice")
	}

	// A grant for some months has an end.
	if out := admin(t, "premium", "grant", alice.phone, "-months", "1"); !strings.Contains(out, "has Premium until") {
		t.Fatalf("premium grant -months 1: %q", out)
	}
	waitSelfPremium(ctx, t, laptop, true)
	admin(t, "premium", "revoke", alice.phone)
}
