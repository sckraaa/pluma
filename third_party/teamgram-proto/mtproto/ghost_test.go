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

package mtproto

import "testing"

// pluma: TestGhostUser: the support ghost's phone goes to nobody but itself,
// whatever its privacy and even to one who keeps the number; nobody finds
// it by the number or adds it to a chat; its own rules close neither its
// private chat nor its voice messages. An ordinary account with the same
// rules is as its rules say.
func TestGhostUser(t *testing.T) {
	const viewerId, ownerId, phone = 2, 1, "77777777777"
	keep := MakeTLContactData(&ContactData{UserId: viewerId, ContactUserId: ownerId, Phone: MakeFlagsString(phone)}).To_ContactData()
	owner := func(ghost bool) *ImmutableUser {
		var keys []*PrivacyKeyRules
		for _, k := range []int{PHONE_NUMBER, ADDED_BY_PHONE, CHAT_INVITE} {
			keys = append(keys, MakeTLPrivacyKeyRules(&PrivacyKeyRules{Key: int32(k), Rules: []*PrivacyRule{allowAll()}}).To_PrivacyKeyRules())
		}
		keys = append(keys, MakeTLPrivacyKeyRules(&PrivacyKeyRules{Key: VOICE_MESSAGES, Rules: []*PrivacyRule{disallowAll()}}).To_PrivacyKeyRules())
		return MakeTLImmutableUser(&ImmutableUser{
			User:             MakeTLUserData(&UserData{Id: ownerId, Phone: phone, Ghost: ghost, NewNoncontactPeersRequirePremium: true}).To_UserData(),
			KeysPrivacyRules: keys,
			ReverseContacts:  []*ContactData{keep},
		}).To_ImmutableUser()
	}
	viewer := MakeTLImmutableUser(&ImmutableUser{
		User:     MakeTLUserData(&UserData{Id: viewerId}).To_UserData(),
		Contacts: []*ContactData{keep},
	}).To_ImmutableUser()

	for _, ghost := range []bool{false, true} {
		o := owner(ghost)
		for path, got := range map[string]string{
			"ToUnsafeUser": o.ToUnsafeUser(viewer).GetPhone().GetValue(),
			"ToUserFor":    o.ToUserFor(viewer).GetPhone().GetValue(),
			"ToUser":       o.ToUser(viewerId).GetPhone().GetValue(),
		} {
			if (got == phone) == ghost {
				t.Errorf("ghost %v, %s: phone %q", ghost, path, got)
			}
		}
		if got := o.ToSelfUser().GetPhone().GetValue(); got != phone {
			t.Errorf("ghost %v: its own phone %q", ghost, got)
		}
		for key, want := range map[int]bool{ADDED_BY_PHONE: !ghost, CHAT_INVITE: !ghost, VOICE_MESSAGES: ghost} {
			if got := o.CheckPrivacyFor(key, viewer); got != want {
				t.Errorf("ghost %v: key %d %v, want %v", ghost, key, got, want)
			}
			if got := o.CheckPrivacy(key, viewerId); got != want {
				t.Errorf("ghost %v: key %d by id %v, want %v", ghost, key, got, want)
			}
		}
		if got := o.ContactRequirePremium(); got == ghost {
			t.Errorf("ghost %v: contact_require_premium %v", ghost, got)
		}
		if got := o.ToUnsafeUser(viewer).GetContactRequirePremium(); got == ghost {
			t.Errorf("ghost %v: user contact_require_premium %v", ghost, got)
		}
	}
}

// pluma: TestGhostCodec: the ghost flag of userData and the hidden flag of a
// basic group's participant survive the TL codec.
func TestGhostCodec(t *testing.T) {
	for _, on := range []bool{false, true} {
		in := MakeTLUserData(&UserData{Id: 7, FirstName: "Support", Ghost: on}).To_UserData()
		x := NewEncodeBuf(256)
		if err := in.Encode(x, 0); err != nil {
			t.Fatal(err)
		}
		out := &UserData{}
		if err := out.Decode(NewDecodeBuf(x.GetBuf())); err != nil {
			t.Fatal(err)
		}
		if out.GetGhost() != on || out.GetFirstName() != "Support" {
			t.Errorf("userData ghost %v: decoded %+v", on, out)
		}

		p := MakeTLImmutableChatParticipant(&ImmutableChatParticipant{
			ChatId: 5, UserId: 7, Rank: "r", Hidden: on, IsBot: true, AdminRights: MakeDefaultChatAdminRights(),
		}).To_ImmutableChatParticipant()
		x = NewEncodeBuf(256)
		if err := p.Encode(x, 228); err != nil {
			t.Fatal(err)
		}
		q := &ImmutableChatParticipant{}
		if err := q.Decode(NewDecodeBuf(x.GetBuf())); err != nil {
			t.Fatal(err)
		}
		if q.GetHidden() != on || q.GetRank() != "r" || !q.GetIsBot() || q.GetUserId() != 7 {
			t.Errorf("participant hidden %v: decoded %+v", on, q)
		}
	}
}

// pluma: TestHiddenParticipants: the members of a basic group as a member
// sees them leave a hidden member (the ghost, ADR 0048) out, but for the
// ghost itself; the list everyone is told leaves it out.
func TestHiddenParticipants(t *testing.T) {
	member := func(id int64, hidden bool) *ImmutableChatParticipant {
		return MakeTLImmutableChatParticipant(&ImmutableChatParticipant{ChatId: 5, UserId: id, Hidden: hidden}).To_ImmutableChatParticipant()
	}
	c := MakeTLMutableChat(&MutableChat{
		Chat:             MakeTLImmutableChat(&ImmutableChat{Id: 5, Version: 2}).To_ImmutableChat(),
		ChatParticipants: []*ImmutableChatParticipant{member(1, false), member(2, true), member(3, false)},
	}).To_MutableChat()
	ids := func(p *ChatParticipants) []int64 {
		var out []int64
		for _, x := range p.GetParticipants() {
			out = append(out, x.GetUserId())
		}
		return out
	}
	for self, want := range map[int64][]int64{0: {1, 3}, 1: {1, 3}, 2: {1, 2, 3}} {
		got := ids(c.ToChatParticipants(self))
		if len(got) != len(want) {
			t.Errorf("as %d: %v, want %v", self, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("as %d: %v, want %v", self, got, want)
			}
		}
	}
	if v := c.ToUnsafeChat(2); v.GetLeft() || v.GetPredicateName() != Predicate_chat {
		t.Errorf("the ghost's chat %+v", v)
	}
}
