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
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/svc"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

const me = int64(1)

// fakeUsers is the user service of the handlers. Methods a test does not
// set up panic through the nil embedded interface: the handler must not
// call them.
type fakeUsers struct {
	user_client.UserClient
	users      map[int64]*mtproto.ImmutableUser
	contacts   []int64
	blocked    []int64
	imported   *userpb.UserImportedContacts
	topPeers   *mtproto.Contacts_TopPeers
	saved      []*mtproto.SavedContact
	usernames  map[string]int64
	phones     map[string]int64
	signUpOn   bool
	added      []*userpb.TLUserAddContact
	blockCalls []string
}

func (f *fakeUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if u, ok := f.users[id]; ok {
			out.Datas = append(out.Datas, u)
		}
	}
	return out, nil
}

func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return f.users[in.Id], nil
}

func (f *fakeUsers) UserImportContacts(context.Context, *userpb.TLUserImportContacts) (*userpb.UserImportedContacts, error) {
	return f.imported, nil
}

func (f *fakeUsers) UserGetContactSignUpNotification(context.Context, *userpb.TLUserGetContactSignUpNotification) (*mtproto.Bool, error) {
	return mtproto.ToBool(f.signUpOn), nil
}

func (f *fakeUsers) UserGetSavedContacts(context.Context, *userpb.TLUserGetSavedContacts) (*userpb.Vector_SavedContact, error) {
	return &userpb.Vector_SavedContact{Datas: f.saved}, nil
}

func (f *fakeUsers) UserGetBlockedList(context.Context, *userpb.TLUserGetBlockedList) (*userpb.Vector_PeerBlocked, error) {
	out := &userpb.Vector_PeerBlocked{}
	for _, id := range f.blocked {
		out.Datas = append(out.Datas, mtproto.MakeTLPeerBlocked(&mtproto.PeerBlocked{PeerId: mtproto.MakePeerUser(id)}).To_PeerBlocked())
	}
	return out, nil
}

func (f *fakeUsers) UserBlockPeer(_ context.Context, in *userpb.TLUserBlockPeer) (*mtproto.Bool, error) {
	f.blockCalls = append(f.blockCalls, "block "+itoa(in.PeerId))
	return mtproto.BoolTrue, nil
}

func (f *fakeUsers) UserUnBlockPeer(_ context.Context, in *userpb.TLUserUnBlockPeer) (*mtproto.Bool, error) {
	f.blockCalls = append(f.blockCalls, "unblock "+itoa(in.PeerId))
	return mtproto.BoolTrue, nil
}

func (f *fakeUsers) UserGetTopPeers(context.Context, *userpb.TLUserGetTopPeers) (*mtproto.Contacts_TopPeers, error) {
	return f.topPeers, nil
}

func (f *fakeUsers) UserGetContactIdList(context.Context, *userpb.TLUserGetContactIdList) (*userpb.Vector_Long, error) {
	return &userpb.Vector_Long{Datas: f.contacts}, nil
}

func (f *fakeUsers) UserSearchUsername(_ context.Context, in *userpb.TLUserSearchUsername) (*userpb.Vector_UsernameData, error) {
	out := &userpb.Vector_UsernameData{}
	for name, id := range f.usernames {
		if len(name) >= len(in.Q) && name[:len(in.Q)] == in.Q && !slices.Contains(in.ExcludedContacts, id) {
			out.Datas = append(out.Datas, userpb.MakeTLUsernameData(&userpb.UsernameData{Username: name, Peer: mtproto.MakePeerUser(id)}).To_UsernameData())
		}
	}
	return out, nil
}

func (f *fakeUsers) UserGetUserIdByPhone(_ context.Context, in *userpb.TLUserGetUserIdByPhone) (*mtproto.Int64, error) {
	id, ok := f.phones[in.Phone]
	if !ok {
		return nil, mtproto.ErrPhoneNotOccupied
	}
	return &mtproto.Int64{V: id}, nil
}

func (f *fakeUsers) UserAddContact(_ context.Context, in *userpb.TLUserAddContact) (*mtproto.Bool, error) {
	f.added = append(f.added, in)
	owner := f.users[in.UserId]
	owner.Contacts = append(owner.Contacts, contactData(in.UserId, in.Id, in.FirstName, in.LastName))
	return mtproto.BoolFalse, nil
}

type fakeSync struct {
	sync_client.SyncClient
	pushed []*sync.TLSyncUpdatesNotMe
}

func (f *fakeSync) SyncUpdatesNotMe(_ context.Context, in *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	f.pushed = append(f.pushed, in)
	return mtproto.EmptyVoid, nil
}

type fakeChats struct {
	chat_client.ChatClient
	chats []*mtproto.MutableChat
}

func (f *fakeChats) ChatGetChatListByIdList(context.Context, *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error) {
	return &chatpb.Vector_MutableChat{Datas: f.chats}, nil
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func newCore(users *fakeUsers, push *fakeSync, chats *fakeChats, md *metadata.RpcMetadata) *ContactsCore {
	ctx := context.Background()
	if md == nil {
		md = &metadata.RpcMetadata{UserId: me, PermAuthKeyId: 77}
	}
	return &ContactsCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{
			UserClient: users,
			SyncClient: push,
			ChatClient: chats,
		}},
		Logger: logx.WithContext(ctx),
		MD:     md,
	}
}

var everybody = []*mtproto.PrivacyKeyRules{
	mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{
		Key:   mtproto.PHONE_NUMBER,
		Rules: []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()},
	}).To_PrivacyKeyRules(),
}

var nobody = []*mtproto.PrivacyKeyRules{
	mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{
		Key:   mtproto.PHONE_NUMBER,
		Rules: []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()},
	}).To_PrivacyKeyRules(),
}

func immutableUser(id int64, first, last, username, phone string, rules []*mtproto.PrivacyKeyRules) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{
			Id:          id,
			AccessHash:  id * 10,
			FirstName:   first,
			LastName:    last,
			Username:    username,
			Phone:       phone,
			CountryCode: "US",
			UserType:    userpb.UserTypeRegular,
		}).To_UserData(),
		KeysPrivacyRules: rules,
	}).To_ImmutableUser()
}

func contactData(owner, contact int64, first, last string) *mtproto.ContactData {
	return mtproto.MakeTLContactData(&mtproto.ContactData{
		UserId:        owner,
		ContactUserId: contact,
		FirstName:     mtproto.MakeFlagsString(first),
		LastName:      mtproto.MakeFlagsString(last),
	}).To_ContactData()
}

func rpcError(err error) (int, string) {
	st, _ := status.FromError(err)
	return int(st.Code()), st.Message()
}

func TestImportContactsAnswersAndTellsTheOtherDevices(t *testing.T) {
	bob := immutableUser(2, "Bob", "", "", "", nil)
	bobUser := bob.ToUnsafeUser(immutableUser(me, "Me", "", "", "", nil))
	users := &fakeUsers{imported: userpb.MakeTLUserImportedContacts(&userpb.UserImportedContacts{
		Imported:       []*mtproto.ImportedContact{mtproto.MakeTLImportedContact(&mtproto.ImportedContact{UserId: 2, ClientId: 7}).To_ImportedContact()},
		PopularInvites: []*mtproto.PopularContact{mtproto.MakeTLPopularContact(&mtproto.PopularContact{ClientId: 8, Importers: 3}).To_PopularContact()},
		RetryContacts:  []int64{9},
		Users:          []*mtproto.User{bobUser},
		UpdateIdList:   []int64{2},
	}).To_UserImportedContacts()}
	push := &fakeSync{}
	c := newCore(users, push, nil, nil)

	r, err := c.ContactsImportContacts(&mtproto.TLContactsImportContacts{Contacts: []*mtproto.InputContact{
		mtproto.MakeTLInputPhoneContact(&mtproto.InputContact{ClientId: 7, Phone: "+12015550102", FirstName: "Bob"}).To_InputContact(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetImported()) != 1 || r.GetImported()[0].GetClientId() != 7 || len(r.GetPopularInvites()) != 1 ||
		!reflect.DeepEqual(r.GetRetryContacts(), []int64{9}) || len(r.GetUsers()) != 1 {
		t.Errorf("answer %v", r)
	}
	if len(push.pushed) != 1 {
		t.Fatalf("%d pushes", len(push.pushed))
	}
	p := push.pushed[0]
	ups := p.GetUpdates().GetUpdates()
	if p.GetPermAuthKeyId() != 77 || len(ups) != 1 || ups[0].GetPredicateName() != mtproto.Predicate_updatePeerSettings ||
		ups[0].GetPeer_PEER().GetUserId() != 2 || len(p.GetUpdates().GetUsers()) != 1 {
		t.Errorf("push %v", p)
	}

	// Nothing changed: nothing to tell.
	users.imported.UpdateIdList = nil
	push.pushed = nil
	if _, err = c.ContactsImportContacts(&mtproto.TLContactsImportContacts{Contacts: []*mtproto.InputContact{
		mtproto.MakeTLInputPhoneContact(&mtproto.InputContact{ClientId: 7, Phone: "+12015550102"}).To_InputContact(),
	}}); err != nil || len(push.pushed) != 0 {
		t.Errorf("repeated import: %v, %d pushes", err, len(push.pushed))
	}

	// An empty phone book does not reach the user service (the fake would
	// not be asked).
	c = newCore(&fakeUsers{}, push, nil, nil)
	if r, err = c.ContactsImportContacts(&mtproto.TLContactsImportContacts{}); err != nil || len(r.GetImported()) != 0 {
		t.Errorf("empty: %v, %v", r, err)
	}
}

// account.getContactSignUpNotification tells whether the notifications are
// silent: Android reads boolFalse as "on".
func TestGetContactSignUpNotificationIsSilent(t *testing.T) {
	for _, on := range []bool{true, false} {
		c := newCore(&fakeUsers{signUpOn: on}, nil, nil, nil)
		r, err := c.AccountGetContactSignUpNotification(&mtproto.TLAccountGetContactSignUpNotification{})
		if err != nil || mtproto.FromBool(r) == on {
			t.Errorf("notifications on %v: %v, %v", on, r, err)
		}
	}
}

func TestGetSavedNeedsTakeout(t *testing.T) {
	saved := []*mtproto.SavedContact{mtproto.MakeTLSavedPhoneContact(&mtproto.SavedContact{Phone: "12015550103"}).To_SavedContact()}
	c := newCore(&fakeUsers{saved: saved}, nil, nil, nil)
	_, err := c.ContactsGetSaved(&mtproto.TLContactsGetSaved{})
	if code, msg := rpcError(err); code != 403 || msg != "TAKEOUT_REQUIRED" {
		t.Errorf("without takeout: %d %s", code, msg)
	}

	c = newCore(&fakeUsers{saved: saved}, nil, nil, &metadata.RpcMetadata{UserId: me, Takeout: &metadata.Takeout{Id: 5}})
	r, err := c.ContactsGetSaved(&mtproto.TLContactsGetSaved{})
	if err != nil || len(r.GetDatas()) != 1 || r.GetDatas()[0].GetPhone() != "12015550103" {
		t.Errorf("in a takeout: %v, %v", r, err)
	}
}

func inputPeerUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: id * 10}).To_InputPeer()
}

func TestSetBlockedReplacesTheList(t *testing.T) {
	users := &fakeUsers{
		blocked: []int64{3, 4},
		users: map[int64]*mtproto.ImmutableUser{
			me: immutableUser(me, "Me", "", "", "", nil),
			3:  immutableUser(3, "C", "", "", "", nil),
			4:  immutableUser(4, "D", "", "", "", nil),
			5:  immutableUser(5, "E", "", "", "", nil),
		},
	}
	push := &fakeSync{}
	c := newCore(users, push, nil, nil)
	r, err := c.ContactsSetBlocked(&mtproto.TLContactsSetBlocked{Id: []*mtproto.InputPeer{inputPeerUser(4), inputPeerUser(5)}, Limit: 2})
	if err != nil || !mtproto.FromBool(r) {
		t.Fatalf("%v, %v", r, err)
	}
	if want := []string{"unblock " + itoa(3), "block " + itoa(5)}; !reflect.DeepEqual(users.blockCalls, want) {
		t.Errorf("calls %v, want %v", users.blockCalls, want)
	}
	if len(push.pushed) != 1 || len(push.pushed[0].GetUpdates().GetUpdates()) != 2 {
		t.Fatalf("pushes %v", push.pushed)
	}
	ups := push.pushed[0].GetUpdates().GetUpdates()
	if ups[0].GetPeerId().GetUserId() != 3 || ups[0].GetBlocked_FLAGBOOLEAN() || ups[1].GetPeerId().GetUserId() != 5 || !ups[1].GetBlocked_FLAGBOOLEAN() {
		t.Errorf("updates %v", ups)
	}

	// The same list again: nothing to do.
	users.blocked, users.blockCalls, push.pushed = []int64{4, 5}, nil, nil
	if _, err = c.ContactsSetBlocked(&mtproto.TLContactsSetBlocked{Id: []*mtproto.InputPeer{inputPeerUser(5), inputPeerUser(4)}, Limit: 2}); err != nil || len(users.blockCalls) != 0 || len(push.pushed) != 0 {
		t.Errorf("same list: %v, %v, %v", err, users.blockCalls, push.pushed)
	}

	// The story blocklist does not exist here; the main one is not touched.
	_, err = c.ContactsSetBlocked(&mtproto.TLContactsSetBlocked{MyStoriesFrom: true, Id: []*mtproto.InputPeer{inputPeerUser(3)}, Limit: 1})
	if _, msg := rpcError(err); msg != "STORIES_UNAVAILABLE" || len(users.blockCalls) != 0 {
		t.Errorf("story blocklist: %v, %v", err, users.blockCalls)
	}
	_, err = c.ContactsSetBlocked(&mtproto.TLContactsSetBlocked{Id: []*mtproto.InputPeer{mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()}, Limit: 1})
	if _, msg := rpcError(err); msg != "PEER_ID_INVALID" {
		t.Errorf("blocking oneself: %v", err)
	}
}

func TestGetTopPeers(t *testing.T) {
	deleted := immutableUser(3, "Gone", "", "", "", nil)
	deleted.User.Deleted = true
	users := &fakeUsers{
		users: map[int64]*mtproto.ImmutableUser{
			me: immutableUser(me, "Me", "", "", "", nil),
			2:  immutableUser(2, "B", "", "", "", nil),
			3:  deleted,
			4:  immutableUser(4, "D", "", "", "", nil),
		},
		topPeers: mtproto.MakeTLContactsTopPeers(&mtproto.Contacts_TopPeers{Categories: []*mtproto.TopPeerCategoryPeers{
			mtproto.MakeTLTopPeerCategoryPeers(&mtproto.TopPeerCategoryPeers{
				Category: mtproto.MakeTLTopPeerCategoryCorrespondents(nil).To_TopPeerCategory(),
				Peers: []*mtproto.TopPeer{
					mtproto.MakeTLTopPeer(&mtproto.TopPeer{Peer: mtproto.MakePeerUser(2), Rating: 3}).To_TopPeer(),
					mtproto.MakeTLTopPeer(&mtproto.TopPeer{Peer: mtproto.MakePeerUser(3), Rating: 2}).To_TopPeer(),
					mtproto.MakeTLTopPeer(&mtproto.TopPeer{Peer: mtproto.MakePeerUser(4), Rating: 1}).To_TopPeer(),
				},
			}).To_TopPeerCategoryPeers(),
			mtproto.MakeTLTopPeerCategoryPeers(&mtproto.TopPeerCategoryPeers{
				Category: mtproto.MakeTLTopPeerCategoryGroups(nil).To_TopPeerCategory(),
				Peers: []*mtproto.TopPeer{
					mtproto.MakeTLTopPeer(&mtproto.TopPeer{Peer: mtproto.MakePeerChat(10), Rating: 2}).To_TopPeer(),
					mtproto.MakeTLTopPeer(&mtproto.TopPeer{Peer: mtproto.MakePeerChat(11), Rating: 1}).To_TopPeer(),
				},
			}).To_TopPeerCategoryPeers(),
		}}).To_Contacts_TopPeers(),
	}
	chat := func(id int64, state int32) *mtproto.MutableChat {
		return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
			Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: id, Title: "G"}).To_ImmutableChat(),
			ChatParticipants: []*mtproto.ImmutableChatParticipant{
				mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{ChatId: id, UserId: me, State: state}).To_ImmutableChatParticipant(),
			},
		}).To_MutableChat()
	}
	chats := &fakeChats{chats: []*mtproto.MutableChat{chat(10, mtproto.ChatMemberStateNormal), chat(11, mtproto.ChatMemberStateLeft)}}
	c := newCore(users, nil, chats, nil)

	req := &mtproto.TLContactsGetTopPeers{Correspondents: true, Groups: true, Limit: 20}
	r, err := c.ContactsGetTopPeers(req)
	if err != nil {
		t.Fatal(err)
	}
	cats := r.GetCategories()
	if len(cats) != 2 {
		t.Fatalf("categories %v", cats)
	}
	var people []int64
	for _, p := range cats[0].GetPeers() {
		people = append(people, p.GetPeer().GetUserId())
	}
	if !reflect.DeepEqual(people, []int64{2, 4}) || cats[0].GetCount() != 2 {
		t.Errorf("correspondents %v, count %d (a gone user is left out)", people, cats[0].GetCount())
	}
	if len(cats[1].GetPeers()) != 1 || cats[1].GetPeers()[0].GetPeer().GetChatId() != 10 {
		t.Errorf("groups %v (a group the user left is left out)", cats[1].GetPeers())
	}
	var order []int64
	for _, u := range r.GetUsers() {
		order = append(order, u.GetId())
	}
	if !reflect.DeepEqual(order, []int64{2, 4}) || len(r.GetChats()) != 1 {
		t.Errorf("users %v (in rating order), chats %d", order, len(r.GetChats()))
	}

	// The same list again with its hash: not modified.
	var hash uint64
	for _, id := range []int64{2, 4, 10} {
		hash = hashUpdate(hash, id)
	}
	req.Hash = int64(hash)
	if r, err = c.ContactsGetTopPeers(req); err != nil || r.GetPredicateName() != mtproto.Predicate_contacts_topPeersNotModified {
		t.Errorf("with the hash: %v, %v", r.GetPredicateName(), err)
	}

	// A page.
	req.Hash, req.Offset, req.Limit = 0, 1, 1
	if r, err = c.ContactsGetTopPeers(req); err != nil || len(r.GetCategories()[0].GetPeers()) != 1 ||
		r.GetCategories()[0].GetPeers()[0].GetPeer().GetUserId() != 4 || r.GetCategories()[0].GetCount() != 2 {
		t.Errorf("offset 1, limit 1: %v, %v", r, err)
	}
}

// The hash of a list of ids is the one tdesktop computes (api_hash.h):
// for 1, 2 — 1, then 1 ^ 1<<35 = 34359738369, ^ itself>>4 = 36507222017,
// + 2.
func TestHashUpdate(t *testing.T) {
	if h := hashUpdate(hashUpdate(0, 1), 2); h != 36507222019 {
		t.Errorf("hash of 1, 2: %d", h)
	}
}

func TestSearch(t *testing.T) {
	alice := immutableUser(2, "Alice", "Smith", "", "", nil)
	bob := immutableUser(3, "Robert", "Jones", "bobby", "", nil)
	carol := immutableUser(5, "Carol", "", "carolina", "", nil)
	visible := immutableUser(6, "Dan", "", "", "12015550106", everybody)
	hidden := immutableUser(7, "Eve", "", "", "12015550107", nobody)
	self := immutableUser(me, "Me", "", "", "12015550101", nil)
	self.Contacts = []*mtproto.ContactData{contactData(me, 2, "Ally", ""), contactData(me, 3, "Bob", "")}
	users := &fakeUsers{
		users:     map[int64]*mtproto.ImmutableUser{me: self, 2: alice, 3: bob, 5: carol, 6: visible, 7: hidden},
		contacts:  []int64{2, 3},
		usernames: map[string]int64{"bobby": 3, "carolina": 5},
		phones:    map[string]int64{"12015550106": 6, "12015550107": 7},
	}
	c := newCore(users, nil, nil, nil)
	search := func(q string) (mine, others []int64) {
		t.Helper()
		r, err := c.ContactsSearch(&mtproto.TLContactsSearch{Q: q, Limit: 20})
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		for _, p := range r.GetMyResults() {
			mine = append(mine, p.GetUserId())
		}
		for _, p := range r.GetResults() {
			others = append(others, p.GetUserId())
		}
		if len(r.GetUsers()) != len(mine)+len(others) {
			t.Errorf("%q: %d users for %d peers", q, len(r.GetUsers()), len(mine)+len(others))
		}
		return mine, others
	}
	for _, c := range []struct {
		q            string
		mine, others []int64
	}{
		{"smi", []int64{2}, nil},             // the contact's own last name
		{"ALL", []int64{2}, nil},             // the name the user saved
		{"bob", []int64{3}, nil},             // saved name and username
		{"@rob", nil, nil},                   // "@": usernames only
		{"car", nil, []int64{5}},             // by username, not a contact
		{"+1 201 555 0106", nil, []int64{6}}, // a visible number
		{"+1 201 555 0107", nil, nil},        // a hidden one
		{"Carol", nil, nil},                  // no global search by name
	} {
		mine, others := search(c.q)
		if !reflect.DeepEqual(mine, c.mine) || !reflect.DeepEqual(others, c.others) {
			t.Errorf("%q: my_results %v, results %v; want %v, %v", c.q, mine, others, c.mine, c.others)
		}
	}
	_, err := c.ContactsSearch(&mtproto.TLContactsSearch{Q: " @ "})
	if _, msg := rpcError(err); msg != "SEARCH_QUERY_EMPTY" {
		t.Errorf("empty query: %v", err)
	}
}

func inputUser(id int64) *mtproto.InputUser {
	return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: id * 10}).To_InputUser()
}

func TestAcceptContact(t *testing.T) {
	peer := immutableUser(2, "Bob", "B", "", "", nil)
	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{me: immutableUser(me, "Me", "", "", "", nil), 2: peer}}
	push := &fakeSync{}
	c := newCore(users, push, nil, nil)

	// Bob did not add us: there is no request to accept.
	_, err := c.ContactsAcceptContact(&mtproto.TLContactsAcceptContact{Id: inputUser(2)})
	if _, msg := rpcError(err); msg != "CONTACT_REQ_MISSING" || len(users.added) != 0 {
		t.Errorf("without a request: %v", err)
	}

	peer.Contacts = []*mtproto.ContactData{contactData(2, me, "Me", "")}
	r, err := c.ContactsAcceptContact(&mtproto.TLContactsAcceptContact{Id: inputUser(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(users.added) != 1 || !mtproto.FromBool(users.added[0].AddPhonePrivacyException) || users.added[0].FirstName != "Bob" || users.added[0].LastName != "B" {
		t.Errorf("added %v: the user becomes a contact under their name and sees our number", users.added)
	}
	ups := r.GetUpdates()
	if len(ups) != 1 || ups[0].GetPredicateName() != mtproto.Predicate_updatePeerSettings || len(r.GetUsers()) != 1 || !r.GetUsers()[0].GetContact() {
		t.Errorf("updates %v", r)
	}
	if len(push.pushed) != 1 {
		t.Errorf("%d pushes to the other devices", len(push.pushed))
	}

	_, err = c.ContactsAcceptContact(&mtproto.TLContactsAcceptContact{Id: mtproto.MakeTLInputUserSelf(nil).To_InputUser()})
	if _, msg := rpcError(err); msg != "CONTACT_ID_INVALID" {
		t.Errorf("oneself: %v", err)
	}
}
