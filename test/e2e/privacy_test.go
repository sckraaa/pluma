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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func setPrivacy(ctx context.Context, u *user, key string, rules ...*mtproto.InputPrivacyRule) (*mtproto.Account_PrivacyRules, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLAccountSetPrivacy{
		Constructor: u.c.Constructor(mtproto.Predicate_account_setPrivacy),
		Key:         &mtproto.InputPrivacyKey{Constructor: u.c.Constructor(key)},
		Rules:       rules,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Account_PrivacyRules() *mtproto.Account_PrivacyRules
	})
	if !ok {
		return nil, fmt.Errorf("account.setPrivacy returned %T", o)
	}
	return r.To_Account_PrivacyRules(), nil
}

func mustSetPrivacy(ctx context.Context, t *testing.T, u *user, key string, rules ...*mtproto.InputPrivacyRule) {
	t.Helper()
	if _, err := setPrivacy(ctx, u, key, rules...); err != nil {
		t.Fatalf("account.setPrivacy %s: %v", key, err)
	}
}

func ruleOf(u *user, predicate string) *mtproto.InputPrivacyRule {
	return &mtproto.InputPrivacyRule{Constructor: u.c.Constructor(predicate)}
}

func allowUsersRule(u *user, users ...*mtproto.InputUser) *mtproto.InputPrivacyRule {
	r := ruleOf(u, mtproto.Predicate_inputPrivacyValueAllowUsers)
	r.Users = users
	if r.Users == nil {
		r.Users = []*mtproto.InputUser{}
	}
	return r
}

func chatsRule(u *user, predicate string, chats ...int64) *mtproto.InputPrivacyRule {
	r := ruleOf(u, predicate)
	r.Chats = chats
	if r.Chats == nil {
		r.Chats = []int64{}
	}
	return r
}

func setAbout(ctx context.Context, t *testing.T, u *user, about string) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateProfile{
		Constructor: u.c.Constructor(mtproto.Predicate_account_updateProfile),
		About:       wrapperspb.String(about),
	}); err != nil {
		t.Fatalf("account.updateProfile: %v", err)
	}
}

// aboutAs returns the bio of owner as viewer sees it in the full profile.
func aboutAs(ctx context.Context, t *testing.T, viewer, owner *user) string {
	t.Helper()
	full, _ := fullUser(ctx, t, viewer, inputUserOf(ctx, t, viewer, owner))
	return full.GetAbout().GetValue()
}

// The privacy rules of every key are decided by one evaluator, the same way
// whatever the order the client sent them in (task 034): the exceptions
// Android sends with an empty chat list no longer open a key to everybody,
// "everybody except the members of a group" hides from them, and "my
// contacts" counts the owner's contacts.
func TestPrivacyRules(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")     // the exception
	carol := login(ctx, t, phones[2], "Carol") // a member of Alice's group
	dave := login(ctx, t, phones[3], "Dave")   // a stranger
	const about = "my bio"
	setAbout(ctx, t, alice, about)
	group := createChat(ctx, t, alice, "Privacy", carol)

	// Android: "Nobody, always share with Bob" with an empty chat rule.
	mustSetPrivacy(ctx, t, alice, mtproto.Predicate_inputPrivacyKeyAbout,
		allowUsersRule(alice, inputUserOf(ctx, t, alice, bob)),
		chatsRule(alice, mtproto.Predicate_inputPrivacyValueAllowChatParticipants),
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueDisallowAll))
	for _, c := range []struct {
		name string
		u    *user
		want string
	}{{"Bob", bob, about}, {"Carol", carol, ""}, {"Dave", dave, ""}} {
		if got := aboutAs(ctx, t, c.u, alice); got != c.want {
			t.Errorf("nobody except Bob: %s reads the bio %q, want %q", c.name, got, c.want)
		}
	}

	// Web K order: base rule first, then the group.
	mustSetPrivacy(ctx, t, alice, mtproto.Predicate_inputPrivacyKeyAbout,
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowAll),
		chatsRule(alice, mtproto.Predicate_inputPrivacyValueDisallowChatParticipants, group))
	for _, c := range []struct {
		name string
		u    *user
		want string
	}{{"Bob", bob, about}, {"Carol", carol, ""}, {"Dave", dave, about}} {
		if got := aboutAs(ctx, t, c.u, alice); got != c.want {
			t.Errorf("everybody except the group: %s reads the bio %q, want %q", c.name, got, c.want)
		}
	}

	// My contacts: Alice has Bob.
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, phones[1], "Bob", ""))
	mustSetPrivacy(ctx, t, alice, mtproto.Predicate_inputPrivacyKeyPhoneNumber,
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowContacts))
	for _, c := range []struct {
		name  string
		u     *user
		phone bool
	}{{"Bob", bob, true}, {"Dave", dave, false}} {
		_, u := fullUser(ctx, t, c.u, inputUserOf(ctx, t, c.u, alice))
		if got := u.GetPhone().GetValue() != ""; got != c.phone {
			t.Errorf("phone to contacts: %s sees the number: %v, want %v", c.name, got, c.phone)
		}
	}

	// The rules are stored as one base rule and the exceptions that name
	// someone; a rule for stories only is refused.
	r, err := setPrivacy(ctx, alice, mtproto.Predicate_inputPrivacyKeyForwards,
		chatsRule(alice, mtproto.Predicate_inputPrivacyValueAllowChatParticipants),
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowContacts),
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueDisallowAll))
	if err != nil {
		t.Fatalf("account.setPrivacy: %v", err)
	}
	if n := len(r.GetRules()); n != 1 || r.GetRules()[0].GetPredicateName() != mtproto.Predicate_privacyValueAllowContacts {
		t.Errorf("stored rules %v, want only privacyValueAllowContacts", r.GetRules())
	}
	if _, err := setPrivacy(ctx, alice, mtproto.Predicate_inputPrivacyKeyProfilePhoto,
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowCloseFriends)); !mtclient.IsRPCError(err, "PRIVACY_VALUE_INVALID") {
		t.Errorf("close friends on the profile photo: %v, want PRIVACY_VALUE_INVALID", err)
	}
	if _, err := setPrivacy(ctx, alice, mtproto.Predicate_inputPrivacyKeyVoiceMessages,
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowContacts)); err != nil {
		t.Errorf("voice messages from contacts: %v", err)
	}
}

// "Who can find me by my number" is the owner's rule for the caller, and
// "who can add me to groups" makes createChat list the user as a missing
// invitee instead of dropping them silently (task 034).
func TestPrivacyFindAndInvite(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	toBob := inputUserOf(ctx, t, alice, bob)
	toCarol := inputUserOf(ctx, t, alice, carol)

	// Alice has Bob as a contact, not Carol.
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, phones[1], "Bob", ""))
	mustSetPrivacy(ctx, t, alice, mtproto.Predicate_inputPrivacyKeyAddedByPhone,
		ruleOf(alice, mtproto.Predicate_inputPrivacyValueAllowContacts))
	resolve := func(u *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
			Constructor: u.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
			Phone:       strings.TrimPrefix(alice.phone, "+"),
		})
		return err
	}
	if err := resolve(bob); err != nil {
		t.Errorf("Alice's contact finds her by number: %v", err)
	}
	if err := resolve(carol); !mtclient.IsRPCError(err, "PHONE_NOT_OCCUPIED") {
		t.Errorf("a stranger finds Alice by number: %v, want PHONE_NOT_OCCUPIED", err)
	}

	mustSetPrivacy(ctx, t, carol, mtproto.Predicate_inputPrivacyKeyChatInvite,
		ruleOf(carol, mtproto.Predicate_inputPrivacyValueDisallowAll))
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCreateChat92CEDDD4{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_createChat92CEDDD4),
		Users:       []*mtproto.InputUser{toBob, toCarol},
		Title:       "Invites",
	})
	if err != nil {
		t.Fatalf("messages.createChat: %v", err)
	}
	invited, ok := o.(interface {
		To_Messages_InvitedUsers() *mtproto.Messages_InvitedUsers
	})
	if !ok {
		t.Fatalf("messages.createChat returned %T", o)
	}
	missing := invited.To_Messages_InvitedUsers().GetMissingInvitees()
	if len(missing) != 1 || missing[0].GetUserId() != carol.self.GetId() {
		t.Errorf("missing invitees %v, want Carol %d", missing, carol.self.GetId())
	}
}

// Hide read time: the last seen rule of a user then guards its read dates
// too (globalPrivacySettings.hide_read_marks, task 034).
func TestPrivacyReadDates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	text := fmt.Sprintf("read date %d", randomID())
	toBob := inputUserOf(ctx, t, alice, bob)
	sent := sendMessageID(ctx, t, alice, &mtproto.InputPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser),
		UserId:      toBob.GetUserId(),
		AccessHash:  toBob.GetAccessHash(),
	}, text, randomID())
	waitMessageId(ctx, t, bob, text)
	toAlice := inputUserOf(ctx, t, bob, alice)
	readHistory(ctx, t, bob, &mtproto.InputPeer{
		Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerUser),
		UserId:      toAlice.GetUserId(),
		AccessHash:  toAlice.GetAccessHash(),
	})
	readDate := func() error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetOutboxReadDate{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getOutboxReadDate),
			Peer: &mtproto.InputPeer{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser),
				UserId:      toBob.GetUserId(),
				AccessHash:  toBob.GetAccessHash(),
			},
			MsgId: sent,
		})
		return err
	}
	var err error
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if err = readDate(); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the read date before any privacy: %v", err)
	}

	mustSetPrivacy(ctx, t, bob, mtproto.Predicate_inputPrivacyKeyStatusTimestamp,
		ruleOf(bob, mtproto.Predicate_inputPrivacyValueDisallowAll))
	if err := readDate(); err != nil {
		t.Errorf("last seen hidden, read time not: %v", err)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountSetGlobalPrivacySettings{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_setGlobalPrivacySettings),
		Settings: &mtproto.GlobalPrivacySettings{
			Constructor:   bob.c.Constructor(mtproto.Predicate_globalPrivacySettings),
			HideReadMarks: true,
		},
	}); err != nil {
		t.Fatalf("account.setGlobalPrivacySettings: %v", err)
	}
	if err := readDate(); !mtclient.IsRPCError(err, "USER_PRIVACY_RESTRICTED") {
		t.Errorf("read time hidden: %v, want USER_PRIVACY_RESTRICTED", err)
	}
	if full, _ := fullUser(ctx, t, alice, toBob); !full.GetReadDatesPrivate() {
		t.Error("read_dates_private not set")
	}
}
