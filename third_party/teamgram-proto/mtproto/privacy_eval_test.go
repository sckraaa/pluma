// pluma: tests of the privacy rule evaluator (task 034, ADR 0031)

package mtproto

import (
	"testing"
)

func allowAll() *PrivacyRule { return &PrivacyRule{PredicateName: Predicate_privacyValueAllowAll} }
func allowContacts() *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueAllowContacts}
}
func disallowAll() *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueDisallowAll}
}
func allowUsers(ids ...int64) *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueAllowUsers, Users: ids}
}
func disallowUsers(ids ...int64) *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueDisallowUsers, Users: ids}
}
func allowChats(ids ...int64) *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueAllowChatParticipants, Chats: ids}
}
func disallowChats(ids ...int64) *PrivacyRule {
	return &PrivacyRule{PredicateName: Predicate_privacyValueDisallowChatParticipants, Chats: ids}
}
func rule(predicate string) *PrivacyRule { return &PrivacyRule{PredicateName: predicate} }

const (
	viewer   = 100
	other    = 200
	chatX    = 10
	chatY    = 20
	notMyBot = 300
)

// inChats makes a membership check for a viewer who is a member of chats.
func inChats(chats ...int64) func([]int64) bool {
	return func(list []int64) bool {
		for _, c := range list {
			for _, m := range chats {
				if c == m {
					return true
				}
			}
		}
		return false
	}
}

var evalCases = []struct {
	name  string
	rules []*PrivacyRule
	v     PrivacyViewer
	want  bool
}{
	{"everybody", []*PrivacyRule{allowAll()}, PrivacyViewer{Id: viewer}, true},
	{"nobody", []*PrivacyRule{disallowAll()}, PrivacyViewer{Id: viewer}, false},
	{"no rules mean nobody", nil, PrivacyViewer{Id: viewer}, false},
	{"contacts, a contact", []*PrivacyRule{allowContacts()}, PrivacyViewer{Id: viewer, Contact: true}, true},
	{"contacts, a stranger", []*PrivacyRule{allowContacts()}, PrivacyViewer{Id: viewer}, false},

	// Android sends both exception rules, one of them empty
	// (PrivacyControlActivity.java:1072-1088); the empty chat rule opened the
	// key to everybody.
	{"nobody except B (Android), a stranger",
		[]*PrivacyRule{allowUsers(other), allowChats(), disallowAll()}, PrivacyViewer{Id: viewer, InChats: inChats()}, false},
	{"nobody except B (Android), B",
		[]*PrivacyRule{allowUsers(viewer), allowChats(), disallowAll()}, PrivacyViewer{Id: viewer, InChats: inChats()}, true},
	{"contacts except B, a stranger",
		[]*PrivacyRule{allowContacts(), disallowUsers(other), disallowChats()}, PrivacyViewer{Id: viewer, InChats: inChats()}, false},
	{"contacts except B, B a contact",
		[]*PrivacyRule{allowContacts(), disallowUsers(viewer), disallowChats()}, PrivacyViewer{Id: viewer, Contact: true, InChats: inChats()}, false},

	// "Everybody except members of chat X" did nothing.
	{"everybody except chat X, a member",
		[]*PrivacyRule{allowAll(), disallowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX)}, false},
	{"everybody except chat X, not a member",
		[]*PrivacyRule{allowAll(), disallowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatY)}, true},
	{"everybody except chat X and B (Web K order), B",
		[]*PrivacyRule{allowAll(), disallowChats(chatX), disallowUsers(viewer)}, PrivacyViewer{Id: viewer, InChats: inChats()}, false},
	{"nobody except chat X, a member",
		[]*PrivacyRule{disallowAll(), allowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX)}, true},
	{"nobody except chat X, not a member",
		[]*PrivacyRule{disallowAll(), allowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatY)}, false},
	{"contacts and chat X, a member who is no contact",
		[]*PrivacyRule{allowContacts(), allowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX)}, true},

	// The more specific rule wins.
	{"allowed user in a disallowed chat",
		[]*PrivacyRule{allowAll(), allowUsers(viewer), disallowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX)}, true},
	{"disallowed user in an allowed chat",
		[]*PrivacyRule{disallowAll(), disallowUsers(viewer), allowChats(chatX)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX)}, false},
	{"member of an allowed and a disallowed chat",
		[]*PrivacyRule{allowContacts(), allowChats(chatX), disallowChats(chatY)}, PrivacyViewer{Id: viewer, InChats: inChats(chatX, chatY)}, false},

	// Unknown membership hides.
	{"everybody except chat X, membership unknown",
		[]*PrivacyRule{allowAll(), disallowChats(chatX)}, PrivacyViewer{Id: viewer}, false},
	{"nobody except chat X, membership unknown",
		[]*PrivacyRule{disallowAll(), allowChats(chatX)}, PrivacyViewer{Id: viewer}, false},
	{"everybody, an empty disallowed chat list, membership unknown",
		[]*PrivacyRule{allowAll(), disallowChats()}, PrivacyViewer{Id: viewer}, true},

	// Resolved chat rules (ResolvePrivacyChats) need no membership check.
	{"resolved: everybody except chat X, a member",
		[]*PrivacyRule{allowAll(), {PredicateName: PredicatePrivacyDisallowChatMembers, Chats: []int64{chatX}, Users: []int64{viewer}}},
		PrivacyViewer{Id: viewer}, false},
	{"resolved: everybody except chat X, not a member",
		[]*PrivacyRule{allowAll(), {PredicateName: PredicatePrivacyDisallowChatMembers, Chats: []int64{chatX}, Users: []int64{other}}},
		PrivacyViewer{Id: viewer}, true},
	{"resolved: nobody except chat X, a member",
		[]*PrivacyRule{disallowAll(), {PredicateName: PredicatePrivacyAllowChatMembers, Chats: []int64{chatX}, Users: []int64{viewer}}},
		PrivacyViewer{Id: viewer}, true},

	{"contacts and Premium, a Premium stranger",
		[]*PrivacyRule{allowContacts(), rule(Predicate_privacyValueAllowPremium)}, PrivacyViewer{Id: viewer, Premium: true}, true},
	{"contacts and Premium, a plain stranger",
		[]*PrivacyRule{allowContacts(), rule(Predicate_privacyValueAllowPremium)}, PrivacyViewer{Id: viewer}, false},
	{"everybody except contacts, a contact",
		[]*PrivacyRule{allowAll(), rule(Predicate_privacyValueDisallowContacts)}, PrivacyViewer{Id: viewer, Contact: true}, false},
	{"everybody except contacts, a stranger",
		[]*PrivacyRule{allowAll(), rule(Predicate_privacyValueDisallowContacts)}, PrivacyViewer{Id: viewer}, true},
	{"nobody but bots, a bot",
		[]*PrivacyRule{disallowAll(), rule(Predicate_privacyValueAllowBots)}, PrivacyViewer{Id: notMyBot, Bot: true}, true},
	{"everybody but bots, a bot",
		[]*PrivacyRule{allowAll(), rule(Predicate_privacyValueDisallowBots)}, PrivacyViewer{Id: notMyBot, Bot: true}, false},
	{"everybody but bots, a user",
		[]*PrivacyRule{allowAll(), rule(Predicate_privacyValueDisallowBots)}, PrivacyViewer{Id: viewer}, true},
	{"close friends only apply to stories",
		[]*PrivacyRule{disallowAll(), rule(Predicate_privacyValueAllowCloseFriends)}, PrivacyViewer{Id: viewer, Contact: true}, false},

	// Several base rules: the most open one, as Android and iOS show it.
	{"contacts and nobody, a contact", []*PrivacyRule{allowContacts(), disallowAll()}, PrivacyViewer{Id: viewer, Contact: true}, true},
	{"nobody and everybody", []*PrivacyRule{disallowAll(), allowAll()}, PrivacyViewer{Id: viewer}, true},
}

func TestEvaluatePrivacy(t *testing.T) {
	for _, tc := range evalCases {
		if got := EvaluatePrivacy(tc.rules, tc.v); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The answer does not depend on the order of the rules: every permutation
// of every case answers the same.
func TestEvaluatePrivacyOrder(t *testing.T) {
	for _, tc := range evalCases {
		permute(tc.rules, func(rules []*PrivacyRule) {
			if got := EvaluatePrivacy(rules, tc.v); got != tc.want {
				t.Errorf("%s, order %v: %v, want %v", tc.name, names(rules), got, tc.want)
			}
		})
	}
}

func permute(rules []*PrivacyRule, f func([]*PrivacyRule)) {
	a := append([]*PrivacyRule(nil), rules...)
	var rec func(int)
	rec = func(k int) {
		if k == len(a) {
			f(a)
			return
		}
		for i := k; i < len(a); i++ {
			a[k], a[i] = a[i], a[k]
			rec(k + 1)
			a[k], a[i] = a[i], a[k]
		}
	}
	rec(0)
}

func names(rules []*PrivacyRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.PredicateName
	}
	return out
}

func TestResolvePrivacyChats(t *testing.T) {
	rules := []*PrivacyRule{allowContacts(), allowChats(chatX), disallowChats(chatY), allowUsers(other)}
	got := ResolvePrivacyChats(rules, func(chats []int64) []int64 {
		if chats[0] == chatX {
			return []int64{viewer}
		}
		return nil
	})
	if rules[1].PredicateName != Predicate_privacyValueAllowChatParticipants || len(rules[1].Users) != 0 {
		t.Fatal("the rules given were changed")
	}
	if len(got) != 4 || got[1].PredicateName != PredicatePrivacyAllowChatMembers || got[2].PredicateName != PredicatePrivacyDisallowChatMembers {
		t.Fatalf("resolved %v", names(got))
	}
	if !EvaluatePrivacy(got, PrivacyViewer{Id: viewer}) || EvaluatePrivacy(got, PrivacyViewer{Id: notMyBot}) {
		t.Fatal("resolved rules answer wrong")
	}
	plain := []*PrivacyRule{allowAll()}
	if r := ResolvePrivacyChats(plain, nil); &r[0] != &plain[0] {
		t.Fatal("rules without chat rules were copied")
	}
}

func TestCheckPrivacyFailsClosed(t *testing.T) {
	u := MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{Id: 1}).To_UserData()}).To_ImmutableUser()
	if u.CheckPrivacy(PROFILE_PHOTO, viewer) {
		t.Fatal("no rules loaded, the photo is shown")
	}
	if !u.CheckPrivacy(PROFILE_PHOTO, 1) {
		t.Fatal("the account does not see its own photo")
	}
}

func TestStatusFor(t *testing.T) {
	owner := func(id int64, premium bool, rules ...*PrivacyRule) *ImmutableUser {
		u := MakeTLImmutableUser(&ImmutableUser{
			User:       MakeTLUserData(&UserData{Id: id, Premium: premium}).To_UserData(),
			LastSeenAt: 1 << 40, // online
			KeysPrivacyRules: []*PrivacyKeyRules{
				MakeTLPrivacyKeyRules(&PrivacyKeyRules{Key: STATUS_TIMESTAMP, Rules: rules}).To_PrivacyKeyRules(),
			},
		}).To_ImmutableUser()
		return u
	}
	for _, tc := range []struct {
		name          string
		owner, viewer *ImmutableUser
		want          string
		byMe          bool
	}{
		{"both show", owner(1, false, allowAll()), owner(2, false, allowAll()), Predicate_userStatusOnline, false},
		{"owner hides", owner(1, false, disallowAll()), owner(2, false, allowAll()), Predicate_userStatusRecently, false},
		{"viewer hides", owner(1, false, allowAll()), owner(2, false, disallowAll()), Predicate_userStatusRecently, true},
		{"viewer hides, has Premium", owner(1, false, allowAll()), owner(2, true, disallowAll()), Predicate_userStatusOnline, false},
	} {
		s := tc.owner.StatusFor(tc.viewer)
		if s.GetPredicateName() != tc.want || s.GetByMe() != tc.byMe {
			t.Errorf("%s: %s by_me %v, want %s by_me %v", tc.name, s.GetPredicateName(), s.GetByMe(), tc.want, tc.byMe)
		}
	}
}
