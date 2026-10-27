//go:build integration

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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/svc"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/zrpc"
)

// Basic groups on MySQL and Redis: each change in the operator's box
// transaction with the chat locked (ADR 0021).

var (
	itDB    *testinfra.MySQL
	itRedis *testinfra.Redis
	itSvc   *svc.ServiceContext
	itRaw   *sql.DB
	itUsers = &fakeUsers{}
	itUser  atomic.Int64
)

// fakeUsers stands for biz user: every user asked for exists.
type fakeUsers struct {
	mu    sync.Mutex
	asked [][]int64
}

func (f *fakeUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	f.mu.Lock()
	f.asked = append(f.asked, in.Id)
	f.mu.Unlock()
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{
				Id:         id,
				AccessHash: id * 7,
				FirstName:  fmt.Sprint("U", id),
				UserType:   userpb.UserTypeRegular,
			}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itRedis, errs[1] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else if err = setUp(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itRaw.Close()
	}
	if itDB != nil {
		_ = itDB.Terminate(ctx)
	}
	if itRedis != nil {
		_ = itRedis.Terminate(ctx)
	}
	os.Exit(code)
}

func setUp() error {
	mysql := itDB.Config()
	// The session lock wait of the services' box transactions.
	mysql.DSN += "&innodb_lock_wait_timeout=2"
	itSvc = svc.NewServiceContext(config.Config{
		Mysql: mysql,
		Cache: itRedis.CacheConf(),
		// No media server: the chats of the tests have no photos to read.
		MediaClient: zrpc.RpcClientConf{Endpoints: []string{"127.0.0.1:1"}, NonBlock: true},
		Topics:      config.Topics{Inbox: "Inbox-T", Sync: "Sync-T"},
	}, nil, itUsers)
	itUser.Store(time.Now().UnixNano() % 1_000_000_000 * 1000)
	var err error
	itRaw, err = sql.Open("mysql", itDB.DSN())
	return err
}

func newUsers(n int) []int64 {
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = itUser.Add(1)
	}
	return ids
}

func chatCore() *ChatCore { return New(context.Background(), itSvc) }

// newChat creates a chat of creator with members; a creator makes one chat
// in ten seconds.
func newChat(t *testing.T, creator int64, members ...int64) int64 {
	t.Helper()
	c, err := chatCore().ChatCreateChat2(&chat.TLChatCreateChat2{CreatorId: creator, UserIdList: members, Title: "group"})
	if err != nil {
		t.Fatal(err)
	}
	return c.Id()
}

type chatRow struct {
	count, version int32
	title, about   string
	photoId        int64
	rights         int64
	noforwards     bool
	deactivated    bool
	ttl            int32
}

func rowOf(t *testing.T, chatId int64) chatRow {
	t.Helper()
	var r chatRow
	if err := itRaw.QueryRow("SELECT participant_count, version, title, about, photo_id, default_banned_rights, noforwards, deactivated, ttl_period FROM chats WHERE id = ?", chatId).
		Scan(&r.count, &r.version, &r.title, &r.about, &r.photoId, &r.rights, &r.noforwards, &r.deactivated, &r.ttl); err != nil {
		t.Fatal(err)
	}
	return r
}

type participantRow struct {
	ptype, state int32
	inviter      int64
	link, rank   string
}

func participantOf(t *testing.T, chatId, userId int64) *participantRow {
	t.Helper()
	var r participantRow
	err := itRaw.QueryRow("SELECT participant_type, state, inviter_user_id, link, rank2 FROM chat_participants WHERE chat_id = ? AND user_id = ?", chatId, userId).
		Scan(&r.ptype, &r.state, &r.inviter, &r.link, &r.rank)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		t.Fatal(err)
	}
	return &r
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := itRaw.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// events counts the events of initiator's transactions.
func events(t *testing.T, initiator int64) int {
	t.Helper()
	return count(t, "SELECT COUNT(*) FROM mq_outbox WHERE initiator_id = ?", initiator)
}

// cached is the chat as biz chat serves it, through the cache.
func cached(t *testing.T, chatId int64) *mtproto.MutableChat {
	t.Helper()
	c, err := chatCore().ChatGetMutableChat(&chat.TLChatGetMutableChat{ChatId: chatId})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: %v, want %v", what, err, want)
	}
}

func TestCreateChat(t *testing.T) {
	u := newUsers(5)
	a, b, c, bot := u[0], u[1], u[2], u[3]
	created, err := chatCore().ChatCreateChat2(&chat.TLChatCreateChat2{
		CreatorId: a, UserIdList: []int64{b, c, b, a}, Bots: []int64{bot}, Title: "group",
		TtlPeriod: mtproto.MakeFlagsInt32(86400),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Id()
	if r := rowOf(t, id); r.count != 4 || r.version != 1 || r.title != "group" || r.ttl != 86400 {
		t.Errorf("chat %+v", r)
	}
	var got []string
	for _, p := range created.ChatParticipants {
		if p.Id == 0 {
			t.Errorf("participant %d without an id", p.UserId)
		}
		got = append(got, fmt.Sprintf("%d/%d/%v", p.UserId, p.ParticipantType, p.IsBot))
	}
	want := []string{fmt.Sprintf("%d/1/false", a), fmt.Sprintf("%d/0/false", b), fmt.Sprintf("%d/0/false", c), fmt.Sprintf("%d/0/true", bot)}
	if !slices.Equal(got, want) || created.ParticipantsCount() != 4 {
		t.Errorf("participants %v (%d), want %v", got, created.ParticipantsCount(), want)
	}
	if p := participantOf(t, id, a); p == nil || p.link == "" || count(t, "SELECT COUNT(*) FROM chat_invites WHERE chat_id = ? AND admin_id = ? AND link = ? AND permanent = 1", id, a, p.link) != 1 {
		t.Errorf("the creator's permanent link: %+v", p)
	}

	// One chat in ten seconds.
	_, err = chatCore().ChatCreateChat2(&chat.TLChatCreateChat2{CreatorId: a, UserIdList: []int64{b}, Title: "again"})
	if err == nil || !strings.Contains(err.Error(), "FLOOD_WAIT_") {
		t.Errorf("second chat at once: %v", err)
	}
	if n := count(t, "SELECT COUNT(*) FROM chats WHERE creator_user_id = ?", a); n != 1 {
		t.Errorf("%d chats of the creator", n)
	}
	// 200 members at most.
	_, err = chatCore().ChatCreateChat2(&chat.TLChatCreateChat2{CreatorId: u[4], UserIdList: newUsers(200), Title: "big"})
	wantErr(t, "201 members", err, mtproto.ErrUsersTooMuch)
	if events(t, a) != 0 {
		t.Errorf("creation wrote events")
	}
}

func TestAddAndRemoveMembers(t *testing.T) {
	u := newUsers(4)
	a, b, c, d := u[0], u[1], u[2], u[3]
	id := newChat(t, a, b)
	cc := chatCore()

	added, err := cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: b, UserId: c})
	if err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, id); r.count != 3 || r.version != 2 || added.Version() != 2 || added.ParticipantsCount() != 3 {
		t.Errorf("after the add: row %+v, reply version %d count %d", r, added.Version(), added.ParticipantsCount())
	}
	if p := participantOf(t, id, c); p == nil || p.state != mtproto.ChatMemberStateNormal || p.inviter != b {
		t.Errorf("added: %+v", p)
	}
	if got := cached(t, id); got.Version() != 2 || len(got.ParticipantIdList()) != 3 {
		t.Errorf("the cache after the add: version %d, members %v", got.Version(), got.ParticipantIdList())
	}
	_, err = cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: c})
	wantErr(t, "add a member again", err, mtproto.ErrUserAlreadyParticipant)

	// The creator removes, a member may not; the removed user stays a
	// participant, kicked.
	_, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: b, DeleteUserId: a})
	wantErr(t, "a member removes the creator", err, mtproto.ErrChatAdminRequired)
	removed, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: c})
	if err != nil {
		t.Fatal(err)
	}
	if p := participantOf(t, id, c); p == nil || p.state != mtproto.ChatMemberStateKicked {
		t.Errorf("removed: %+v", p)
	}
	if r := rowOf(t, id); r.count != 2 || r.version != 3 || removed.Version() != 3 {
		t.Errorf("after the removal: %+v, reply version %d", r, removed.Version())
	}
	if got := cached(t, id); slices.Contains(got.ParticipantIdList(), c) || got.Version() != 3 {
		t.Errorf("the cache after the removal: version %d, members %v", got.Version(), got.ParticipantIdList())
	}
	_, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: c, DeleteUserId: c})
	wantErr(t, "a removed user leaves", err, mtproto.ErrPeerIdInvalid)

	// Back in: the row of the participant is written again.
	if _, err = cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: c}); err != nil {
		t.Fatal(err)
	}
	if p := participantOf(t, id, c); p == nil || p.state != mtproto.ChatMemberStateNormal || p.inviter != a {
		t.Errorf("back: %+v", p)
	}
	// The server's admin adds as the creator.
	if _, err = cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, UserId: d}); err != nil {
		t.Fatal(err)
	}
	if p := participantOf(t, id, d); p == nil || p.inviter != a {
		t.Errorf("added by the admin: %+v", p)
	}
	// A member leaves.
	if _, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: b, DeleteUserId: b}); err != nil {
		t.Fatal(err)
	}
	if p := participantOf(t, id, b); p == nil || p.state != mtproto.ChatMemberStateLeft {
		t.Errorf("left: %+v", p)
	}
	if r := rowOf(t, id); r.count != 3 || r.version != 6 {
		t.Errorf("at the end: %+v", r)
	}
}

// Members adding users at once: every add counts, the version grows by
// one each.
func TestConcurrentAdds(t *testing.T) {
	const n = 10
	u := newUsers(1 + 2*n)
	inviters, added := u[1:1+n], u[1+n:]
	id := newChat(t, u[0], inviters...)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = chatCore().ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: inviters[i], UserId: added[i]})
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, id); r.count != 1+2*n || r.version != 1+n {
		t.Errorf("after %d adds at once: %+v", n, r)
	}
	if got := count(t, "SELECT COUNT(*) FROM chat_participants WHERE chat_id = ? AND state = 0", id); got != 1+2*n {
		t.Errorf("%d members", got)
	}
}

func TestEditChatAdmin(t *testing.T) {
	u := newUsers(4)
	a, b, c, stranger := u[0], u[1], u[2], u[3]
	id := newChat(t, a, b, c)
	cc := chatCore()
	admin := func(operator, user int64, isAdmin bool) (*mtproto.MutableChat, error) {
		return cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: operator, EditChatAdminId: user, IsAdmin: mtproto.ToBool(isAdmin)})
	}

	_, err := admin(b, c, true)
	wantErr(t, "a member promotes", err, mtproto.ErrChatAdminRequired)
	_, err = admin(a, stranger, true)
	wantErr(t, "promote a stranger", err, mtproto.ErrUserNotParticipant)
	_, err = admin(a, a, false)
	wantErr(t, "demote the creator", err, mtproto.ErrUserCreator)

	promoted, err := admin(a, b, true)
	if err != nil {
		t.Fatal(err)
	}
	p := participantOf(t, id, b)
	if p == nil || p.ptype != mtproto.ChatMemberAdmin || p.link == "" ||
		count(t, "SELECT COUNT(*) FROM chat_invites WHERE chat_id = ? AND admin_id = ? AND link = ?", id, b, p.link) != 1 {
		t.Errorf("promoted: %+v", p)
	}
	if r := rowOf(t, id); r.version != 2 || promoted.Version() != 2 {
		t.Errorf("version %d, reply %d", r.version, promoted.Version())
	}
	if me, _ := cached(t, id).GetImmutableChatParticipant(b); me == nil || !me.IsChatMemberAdmin() {
		t.Errorf("the cache after the promotion: %v", me)
	}
	// Already an admin: nothing changes.
	if _, err = admin(a, b, true); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, id); r.version != 2 {
		t.Errorf("version %d after a promotion that changes nothing", r.version)
	}
	// The demotion is written with the version.
	if _, err = admin(a, b, false); err != nil {
		t.Fatal(err)
	}
	if p = participantOf(t, id, b); p.ptype != mtproto.ChatMemberNormal || rowOf(t, id).version != 3 {
		t.Errorf("demoted: %+v, version %d", p, rowOf(t, id).version)
	}
	if _, err = admin(a, b, false); err != nil || rowOf(t, id).version != 3 {
		t.Errorf("demotion that changes nothing: %v, version %d", err, rowOf(t, id).version)
	}
}

func TestEditChatParticipantRank(t *testing.T) {
	u := newUsers(2)
	a, b := u[0], u[1]
	id := newChat(t, a, b)
	cc := chatCore()
	rank := func(self, user int64, r string) error {
		_, err := cc.ChatEditChatParticipantRank(&chat.TLChatEditChatParticipantRank{SelfId: self, ChatId: id, Participant: user, Rank: r})
		return err
	}
	wantErr(t, "a member ranks the creator", rank(b, a, "boss"), mtproto.ErrChatAdminRequired)
	if err := rank(a, b, "helper"); err != nil {
		t.Fatal(err)
	}
	if p := participantOf(t, id, b); p.rank != "helper" || rowOf(t, id).version != 2 {
		t.Errorf("ranked: %+v, version %d", p, rowOf(t, id).version)
	}
	wantErr(t, "the same rank", rank(a, b, "helper"), mtproto.ErrChatNotModified)
	if rowOf(t, id).version != 2 {
		t.Errorf("version moved without a change")
	}
}

func TestEditChatInfo(t *testing.T) {
	u := newUsers(3)
	a, b, c := u[0], u[1], u[2]
	id := newChat(t, a, b, c)
	cc := chatCore()
	if _, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: b, IsAdmin: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	version := rowOf(t, id).version
	step := func(what string, err error, grows bool) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		v := rowOf(t, id).version
		if grows {
			version++
		}
		if v != version {
			t.Errorf("%s: version %d, want %d", what, v, version)
		}
	}
	refused := func(what string, err, want error) {
		t.Helper()
		wantErr(t, what, err, want)
		if v := rowOf(t, id).version; v != version {
			t.Errorf("%s: version %d, want %d", what, v, version)
		}
	}

	_, err := cc.ChatEditChatTitle(&chat.TLChatEditChatTitle{ChatId: id, EditUserId: b, Title: "renamed"})
	step("title", err, true)
	_, err = cc.ChatEditChatTitle(&chat.TLChatEditChatTitle{ChatId: id, EditUserId: a, Title: "renamed"})
	refused("the same title", err, mtproto.ErrChatNotModified)
	_, err = cc.ChatEditChatTitle(&chat.TLChatEditChatTitle{ChatId: id, EditUserId: c, Title: "mine"})
	refused("a member renames", err, mtproto.ErrChatAdminRequired)
	if got := cached(t, id).Title(); got != "renamed" {
		t.Errorf("cached title %q", got)
	}

	_, err = cc.ChatEditChatAbout(&chat.TLChatEditChatAbout{ChatId: id, EditUserId: a, About: "about us"})
	step("about", err, false)
	_, err = cc.ChatEditChatAbout(&chat.TLChatEditChatAbout{ChatId: id, EditUserId: a, About: "about us"})
	refused("the same about", err, mtproto.ErrChatNotModified)

	photo := mtproto.MakeTLPhoto(&mtproto.Photo{Id: 7001}).To_Photo()
	edited, err := cc.ChatEditChatPhoto(&chat.TLChatEditChatPhoto{ChatId: id, EditUserId: a, ChatPhoto: photo})
	step("photo", err, true)
	if edited.Chat.GetPhoto().GetId() != 7001 || rowOf(t, id).photoId != 7001 {
		t.Errorf("photo of the reply %v, of the row %d", edited.Chat.GetPhoto(), rowOf(t, id).photoId)
	}
	_, err = cc.ChatEditChatPhoto(&chat.TLChatEditChatPhoto{ChatId: id, EditUserId: a, ChatPhoto: photo})
	refused("the same photo", err, mtproto.ErrChatNotModified)

	_, err = cc.ChatToggleNoForwards(&chat.TLChatToggleNoForwards{ChatId: id, OperatorId: b, Enabled: mtproto.BoolTrue})
	refused("an admin protects the content", err, mtproto.ErrChatAdminRequired)
	_, err = cc.ChatToggleNoForwards(&chat.TLChatToggleNoForwards{ChatId: id, OperatorId: a, Enabled: mtproto.BoolTrue})
	step("no forwards", err, true)
	_, err = cc.ChatToggleNoForwards(&chat.TLChatToggleNoForwards{ChatId: id, OperatorId: a, Enabled: mtproto.BoolTrue})
	refused("no forwards again", err, mtproto.ErrChatNotModified)

	rights := mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{SendMedia: true}).To_ChatBannedRights()
	_, err = cc.ChatEditChatDefaultBannedRights(&chat.TLChatEditChatDefaultBannedRights{ChatId: id, OperatorId: c, BannedRights: rights})
	refused("a member bans", err, mtproto.ErrChatAdminRequired)
	changed, err := cc.ChatEditChatDefaultBannedRights(&chat.TLChatEditChatDefaultBannedRights{ChatId: id, OperatorId: b, BannedRights: rights})
	step("default rights", err, true)
	if !changed.DefaultBannedRights().GetSendMedia() || rowOf(t, id).rights != int64(rights.ToBannedRights()) {
		t.Errorf("rights of the reply %v, of the row %d", changed.DefaultBannedRights(), rowOf(t, id).rights)
	}
	_, err = cc.ChatEditChatDefaultBannedRights(&chat.TLChatEditChatDefaultBannedRights{ChatId: id, OperatorId: a, BannedRights: rights})
	refused("the same rights", err, mtproto.ErrChatNotModified)

	if r := rowOf(t, id); r.title != "renamed" || r.about != "about us" || !r.noforwards {
		t.Errorf("chat %+v", r)
	}
}

func TestDeleteChat(t *testing.T) {
	u := newUsers(3)
	a, b, c := u[0], u[1], u[2]
	id := newChat(t, a, b, c)
	cc := chatCore()
	_, err := cc.ChatDeleteChat(&chat.TLChatDeleteChat{ChatId: id, OperatorId: b})
	wantErr(t, "a member deletes", err, mtproto.ErrChatAdminRequired)
	deleted, err := cc.ChatDeleteChat(&chat.TLChatDeleteChat{ChatId: id, OperatorId: a})
	if err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, id); !r.deactivated || r.count != 0 || r.version != 2 || deleted.Version() != 2 || !deleted.Deactivated() {
		t.Errorf("deleted: %+v, reply version %d", r, deleted.Version())
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_participants WHERE chat_id = ? AND state = ?", id, mtproto.ChatMemberStateKicked); n != 3 {
		t.Errorf("%d participants removed", n)
	}
	if got := cached(t, id); !got.Deactivated() || len(got.ParticipantIdList()) != 0 {
		t.Errorf("cached: deactivated %v, members %v", got.Deactivated(), got.ParticipantIdList())
	}
	// Deleted already: nothing changes, the chat comes back for bff to go
	// on with the histories.
	again, err := cc.ChatDeleteChat(&chat.TLChatDeleteChat{ChatId: id, OperatorId: a})
	if err != nil || len(again.ChatParticipants) != 3 || rowOf(t, id).version != 2 {
		t.Errorf("deleted again: %v, %d participants, version %d", err, len(again.GetChatParticipants()), rowOf(t, id).version)
	}
	_, err = cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: b})
	wantErr(t, "add to a deleted chat", err, mtproto.ErrChatIdInvalid)
}

// newLink adds a link of admin to chatId.
func newLink(t *testing.T, chatId, admin int64, requestNeeded bool, usageLimit int32, revoked bool) string {
	t.Helper()
	link := chat.GenChatInviteHash()
	if _, err := itRaw.Exec("INSERT INTO chat_invites (chat_id, admin_id, link, request_needed, usage_limit, revoked, date2) VALUES (?, ?, ?, ?, ?, ?, ?)",
		chatId, admin, link, requestNeeded, usageLimit, revoked, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return link
}

func join(userId int64, link string) (*chat.ChatInviteImported, error) {
	return chatCore().ChatImportChatInvite2(&chat.TLChatImportChatInvite2{SelfId: userId, Hash: link})
}

func TestJoinByLink(t *testing.T) {
	u := newUsers(3)
	a, b, c := u[0], u[1], u[2]
	id := newChat(t, a)
	once := newLink(t, id, a, false, 1, false)
	joined, err := join(b, once)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Requesters != nil || joined.Chat.ParticipantsCount() != 2 {
		t.Errorf("joined: %v", joined)
	}
	if p := participantOf(t, id, b); p == nil || p.inviter != a || p.state != mtproto.ChatMemberStateNormal {
		t.Errorf("joined: %+v", p)
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_invite_participants WHERE link = ? AND user_id = ? AND requested = 0", once, b); n != 1 {
		t.Errorf("%d uses of the link", n)
	}
	_, err = join(c, once)
	wantErr(t, "a link used up", err, mtproto.ErrInviteHashExpired)
	_, err = join(c, newLink(t, id, a, false, 0, true))
	wantErr(t, "a revoked link", err, mtproto.ErrInviteHashExpired)
	_, err = join(c, "nolinknolinknolinkno")
	wantErr(t, "no link", err, mtproto.ErrInviteHashInvalid)

	// The old method joins as well.
	any := newLink(t, id, a, false, 0, false)
	if _, err = chatCore().ChatImportChatInvite(&chat.TLChatImportChatInvite{SelfId: c, Hash: any}); err != nil {
		t.Fatal(err)
	}
	_, err = join(c, any)
	wantErr(t, "join again", err, mtproto.ErrUserAlreadyParticipant)
	// A member who left and comes back by the same link.
	if _, err = chatCore().ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: c, DeleteUserId: c}); err != nil {
		t.Fatal(err)
	}
	if _, err = join(c, any); err != nil {
		t.Fatal(err)
	}
	if r := rowOf(t, id); r.count != 3 {
		t.Errorf("chat %+v", r)
	}
}

func TestJoinRequests(t *testing.T) {
	u := newUsers(5)
	a, b, f, g, h := u[0], u[1], u[2], u[3], u[4]
	id := newChat(t, a, b)
	link := newLink(t, id, a, true, 0, false)
	requesters := func(r *chat.RecentChatInviteRequesters) string {
		return fmt.Sprintf("%d %v", r.GetRequestsPending(), r.GetRecentRequesters())
	}

	asked, err := join(f, link)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := requesters(asked.Requesters), fmt.Sprintf("1 [%d]", f); got != want {
		t.Errorf("requesters %s, want %s", got, want)
	}
	if participantOf(t, id, f) != nil {
		t.Errorf("a requester is a participant")
	}
	// Asked again: nothing changes.
	if asked, err = join(f, link); err != nil || requesters(asked.Requesters) != fmt.Sprintf("1 [%d]", f) {
		t.Errorf("asked again: %v, %v", asked, err)
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_invite_participants WHERE chat_id = ? AND user_id = ?", id, f); n != 1 {
		t.Errorf("%d requests of one user", n)
	}
	if _, err = chatCore().ChatImportChatInvite(&chat.TLChatImportChatInvite{SelfId: g, Hash: link}); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Errorf("the old method asks: %v", err)
	}
	if asked, err = join(h, link); err != nil {
		t.Fatal(err)
	}
	if got, want := requesters(asked.Requesters), fmt.Sprintf("3 [%d %d %d]", h, g, f); got != want {
		t.Errorf("requesters %s, want %s (newest first)", got, want)
	}

	hide := func(self, user int64, approved bool) (*chat.RecentChatInviteRequesters, error) {
		return chatCore().ChatHideChatJoinRequests(&chat.TLChatHideChatJoinRequests{SelfId: self, ChatId: id, Approved: approved, UserId: mtproto.MakeFlagsInt64(user)})
	}
	_, err = hide(b, f, true)
	wantErr(t, "a member approves", err, mtproto.ErrChatAdminRequired)
	left, err := hide(a, f, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := requesters(left), fmt.Sprintf("2 [%d %d]", h, g); got != want {
		t.Errorf("after the approval %s, want %s", got, want)
	}
	if p := participantOf(t, id, f); p == nil || p.state != mtproto.ChatMemberStateNormal || p.inviter != a {
		t.Errorf("approved: %+v", p)
	}
	if left, err = hide(a, g, false); err != nil || requesters(left) != fmt.Sprintf("1 [%d]", h) {
		t.Errorf("declined: %v, %v", left, err)
	}
	if participantOf(t, id, g) != nil {
		t.Errorf("a declined user is a participant")
	}
	_, err = hide(a, g, false)
	wantErr(t, "decline again", err, mtproto.ErrHideRequesterMissing)
	recent, err := chatCore().ChatGetRecentChatInviteRequesters(&chat.TLChatGetRecentChatInviteRequesters{SelfId: a, ChatId: id})
	if err != nil || requesters(recent) != fmt.Sprintf("1 [%d]", h) {
		t.Errorf("recent requesters %v, %v", recent, err)
	}
}

// The link of an admin who was removed, who left or who was demoted where
// the members may not invite is dead: it lets no one in, asks nothing and
// shows no chat. The links of the creator and of a demoted admin whom the
// members' rights let invite work.
func TestLinksOfAdmins(t *testing.T) {
	u := newUsers(7)
	a, kicked, gone, demoted, j1, j2, j3 := u[0], u[1], u[2], u[3], u[4], u[5], u[6]
	id := newChat(t, a, kicked, gone, demoted)
	cc := chatCore()
	step := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	for _, admin := range []int64{kicked, gone, demoted} {
		_, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: admin, IsAdmin: mtproto.BoolTrue})
		step("promote", err)
	}
	var (
		kickedJoin  = newLink(t, id, kicked, false, 0, false)
		kickedAsk   = newLink(t, id, kicked, true, 0, false)
		goneJoin    = newLink(t, id, gone, false, 0, false)
		demotedJoin = newLink(t, id, demoted, false, 0, false)
		creatorJoin = newLink(t, id, a, false, 0, false)
	)
	check := func(userId int64, link string) error {
		_, err := cc.ChatCheckChatInvite(&chat.TLChatCheckChatInvite{SelfId: userId, Hash: link})
		return err
	}
	for _, link := range []string{kickedJoin, kickedAsk, goneJoin, demotedJoin, creatorJoin} {
		step("the link of an admin", check(j1, link))
	}

	_, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: kicked})
	step("remove", err)
	_, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: gone, DeleteUserId: gone})
	step("leave", err)
	_, err = cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: demoted, IsAdmin: mtproto.BoolFalse})
	step("demote", err)

	// The members may invite: the demoted admin's link works.
	step("check the demoted admin's link", check(j2, demotedJoin))
	_, err = join(j2, demotedJoin)
	step("join by the demoted admin's link", err)

	_, err = cc.ChatEditChatDefaultBannedRights(&chat.TLChatEditChatDefaultBannedRights{ChatId: id, OperatorId: a,
		BannedRights: mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{InviteUsers: true}).To_ChatBannedRights()})
	step("ban inviting", err)
	version := rowOf(t, id).version
	for _, dead := range []struct{ what, link string }{
		{"the link of a removed admin", kickedJoin},
		{"the request link of a removed admin", kickedAsk},
		{"the link of an admin who left", goneJoin},
		{"the link of a demoted admin, the members may not invite", demotedJoin},
	} {
		_, err = join(j1, dead.link)
		wantErr(t, dead.what+": join", err, mtproto.ErrInviteHashExpired)
		wantErr(t, dead.what+": check", check(j1, dead.link), mtproto.ErrInviteHashExpired)
	}
	if p := participantOf(t, id, j1); p != nil {
		t.Errorf("joined by a dead link: %+v", p)
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_invite_participants WHERE chat_id = ? AND user_id = ?", id, j1); n != 0 {
		t.Errorf("%d uses or requests by dead links", n)
	}
	if r := rowOf(t, id); r.version != version {
		t.Errorf("version %d after the dead links, want %d", r.version, version)
	}

	// The creator's link works whatever the members may.
	step("check the creator's link", check(j3, creatorJoin))
	_, err = join(j3, creatorJoin)
	step("join by the creator's link", err)
	if p := participantOf(t, id, j3); p == nil || p.inviter != a || p.state != mtproto.ChatMemberStateNormal {
		t.Errorf("joined by the creator's link: %+v", p)
	}
}
