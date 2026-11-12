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

//go:build integration

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

// The supergroups of the tests, one a test: 1 the creator, 2 and 3
// members, 4 a stranger.
const testHash = int64(7)

var testChannels atomic.Int64

type recorder struct {
	mu     sync.Mutex
	pushes map[int64][]*mtproto.Updates
	sent   []*msg.TLMsgSendMessageV2
}

func (r *recorder) push(userId int64, u *mtproto.Updates) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes[userId] = append(r.pushes[userId], u)
}

// taken returns the pushes of userId since the last call and forgets them.
func (r *recorder) taken(userId int64) []*mtproto.Updates {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pushes[userId]
	delete(r.pushes, userId)
	return out
}

type fakeChannels struct {
	channel_client.ChannelClient
	id int64
	// removed is a member removed from the channel, 0 for none.
	removed *atomic.Int64
}

func (f fakeChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if in.GetChannelId() != f.id {
		return nil, mtproto.ErrChannelInvalid
	}
	part := func(id int64, role int32) *channelpb.ImmutableChannelParticipant {
		return &channelpb.ImmutableChannelParticipant{ChannelId: f.id, UserId: id, Role: role, State: channelpb.StateIn, Date: 1}
	}
	var parts []*channelpb.ImmutableChannelParticipant
	for _, id := range f.members() {
		role := channelpb.RoleMember
		if id == 1 {
			role = channelpb.RoleCreator
		}
		parts = append(parts, part(id, role))
	}
	return &channelpb.MutableChannel{
		Channel:      &channelpb.ImmutableChannel{Id: f.id, AccessHash: testHash, Title: "g", Megagroup: true, Date: 1},
		Participants: parts,
	}, nil
}

func (f fakeChannels) members() []int64 {
	var out []int64
	for _, id := range []int64{1, 2, 3} {
		if f.removed == nil || f.removed.Load() != id {
			out = append(out, id)
		}
	}
	return out
}

func (f fakeChannels) ChannelGetMemberIdList(context.Context, *channelpb.TLChannelGetMemberIdList) (*channelpb.Vector_Long, error) {
	return &channelpb.Vector_Long{Datas: f.members()}, nil
}

type fakeUsers struct{ user_client.UserClient }

func (fakeUsers) UserGetMutableUsers(context.Context, *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	return &userpb.Vector_ImmutableUser{}, nil
}

type fakeMsg struct {
	msg_client.MsgClient
	r *recorder
}

func (f fakeMsg) MsgSendMessageV2(_ context.Context, in *msg.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	f.r.mu.Lock()
	defer f.r.mu.Unlock()
	f.r.sent = append(f.r.sent, in)
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

type fakeSync struct {
	sync_client.SyncClient
	r *recorder
}

func (f fakeSync) SyncUpdatesNotMe(_ context.Context, in *syncpb.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	f.r.push(in.GetUserId(), in.GetUpdates())
	return &mtproto.Void{}, nil
}

func (f fakeSync) SyncPushUpdates(_ context.Context, in *syncpb.TLSyncPushUpdates) (*mtproto.Void, error) {
	f.r.push(in.GetUserId(), in.GetUpdates())
	return &mtproto.Void{}, nil
}

type world struct {
	channel int64
	svcCtx  *svc.ServiceContext
	sfu     *fakeSFU
	rec     *recorder
	removed *atomic.Int64
}

func newWorld(t *testing.T) *world {
	c := itMySQL.Config()
	rec := &recorder{pushes: map[int64][]*mtproto.Updates{}}
	sfu := newFakeSFU("main")
	channel := 1000 + testChannels.Add(1)
	removed := &atomic.Int64{}
	return &world{
		removed: removed,
		channel: channel,
		svcCtx: &svc.ServiceContext{
			Dao: &dao.Dao{
				Store:         groupcalls.NewStore(sqlx.NewMySQL(&c)),
				UserClient:    fakeUsers{},
				ChannelClient: fakeChannels{id: channel, removed: removed},
				MsgClient:     fakeMsg{r: rec},
				SyncClient:    fakeSync{r: rec},
			},
			SFU:           sfu,
			VideoLimit:    30,
			AbsentTimeout: 30 * time.Second,
		},
		sfu: sfu,
		rec: rec,
	}
}

func (w *world) as(userId int64) *GroupCallsCore {
	ctx := context.Background()
	return &GroupCallsCore{ctx: ctx, svcCtx: w.svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: userId, PermAuthKeyId: userId * 10}}
}

func (w *world) inputChannel() *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: w.channel, AccessHash: testHash}).To_InputPeer()
}

func self() *mtproto.InputPeer { return mtproto.MakeTLInputPeerSelf(nil).To_InputPeer() }

func inputUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
}

func params(ssrc uint32, video bool) *mtproto.DataJSON {
	return mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(testParams(ssrc, video))}).To_DataJSON()
}

func updatesOf(u *mtproto.Updates, predicate string) []*mtproto.Update {
	var out []*mtproto.Update
	for _, up := range u.GetUpdates() {
		if up.GetPredicateName() == predicate {
			out = append(out, up)
		}
	}
	return out
}

// rowOf is the row of peer userId in the participant updates of u.
func rowOf(u *mtproto.Updates, userId int64) (*mtproto.GroupCallParticipant, int32) {
	for _, up := range updatesOf(u, mtproto.Predicate_updateGroupCallParticipants) {
		for _, p := range up.GetParticipants_VECTORGROUPCALLPARTICIPANT() {
			if p.GetPeer().GetUserId() == userId {
				return p, up.GetVersion()
			}
		}
	}
	return nil, 0
}

func callOf(t *testing.T, u *mtproto.Updates) *mtproto.GroupCall {
	t.Helper()
	ups := updatesOf(u, mtproto.Predicate_updateGroupCall)
	if len(ups) != 1 {
		t.Fatalf("updateGroupCall %d times in %v", len(ups), u.GetUpdates())
	}
	return ups[0].GetCall_GROUPCALL()
}

// A call of a supergroup from its start to its end, as the clients see
// it (task 093, «Контракт клиентов»).
func TestGroupCallFlow(t *testing.T) {
	w := newWorld(t)
	create := &mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano()), Title: wrapperspb.String(" Planning ")}

	// Only one who manages the chat starts a call; RTMP is never served.
	if _, err := w.as(2).PhoneCreateGroupCall(create); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("a member creates: %v", err)
	}
	if _, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RtmpStream: true}); !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Fatalf("rtmp: %v", err)
	}

	// createGroupCall answers updateGroupCall (tdesktop calls_group_call.cpp:1306).
	u, err := w.as(1).PhoneCreateGroupCall(create)
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, u)
	if gc.GetPredicateName() != mtproto.Predicate_groupCall || gc.GetTitle().GetValue() != "Planning" || gc.GetVersion() != 1 || !gc.GetCanChangeJoinMuted() {
		t.Fatalf("created call: %v", gc)
	}
	call := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	if again, err := w.as(1).PhoneCreateGroupCall(create); err != nil || callOf(t, again).GetId() != gc.GetId() {
		t.Fatalf("the same random_id again: %v", err)
	}
	if len(w.rec.sent) != 1 || w.rec.sent[0].GetMessage()[0].GetMessage().GetAction().GetPredicateName() != mtproto.Predicate_messageActionGroupCall {
		t.Fatalf("the start in the chat: %v", w.rec.sent)
	}
	for _, m := range []int64{2, 3} {
		if ps := w.rec.taken(m); len(ps) != 1 || callOf(t, ps[0]).GetCanChangeJoinMuted() {
			t.Fatalf("member %d told of the call: %v", m, ps)
		}
	}
	w.rec.taken(1)

	// Join as a channel is not offered (ADR 0060); end-to-end joins are a
	// later stage; a stranger to the chat may not join.
	join := func(userId int64, ssrc uint32, video bool) (*mtproto.Updates, error) {
		return w.as(userId).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, video)})
	}
	if _, err = w.as(1).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: w.inputChannel(), Params: params(100, false)}); !errors.Is(err, mtproto.ErrJoinAsPeerInvalid) {
		t.Fatalf("join as a channel: %v", err)
	}
	if _, err = w.as(1).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(100, false), PublicKey: make([]byte, 32)}); !errors.Is(err, mtproto.ErrGroupcallInvalid) {
		t.Fatalf("join with a key: %v", err)
	}
	if _, err = join(4, 400, false); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("a stranger joins: %v", err)
	}
	if _, err = w.as(1).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: "{}"}).To_DataJSON()}); !errors.Is(err, mtproto.ErrDataJsonInvalid) {
		t.Fatalf("bad params: %v", err)
	}

	// The answer of a join: the connection, the call and the own row
	// (iOS GroupCalls.swift:786-829, Android VoIPService.java:2145-2160).
	u, err = join(1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatesOf(u, mtproto.Predicate_updateGroupCallConnection)) != 1 {
		t.Fatalf("no connection in %v", u.GetUpdates())
	}
	if gc = callOf(t, u); gc.GetParticipantsCount() != 1 || gc.GetVersion() != 2 {
		t.Fatalf("call after the join: %v", gc)
	}
	me, version := rowOf(u, 1)
	if me == nil || !me.GetSelf() || me.GetMin() || !me.GetJustJoined() || !me.GetVersioned() || me.GetSource() != 100 || !me.GetCanSelfUnmute() || version != 2 {
		t.Fatalf("own row %v at %d", me, version)
	}
	// The members learn that someone is in the call: the chat goes with it.
	if ps := w.rec.taken(2); len(ps) != 1 || len(ps[0].GetChats()) != 1 {
		t.Fatalf("member told of the first join: %v", ps)
	}

	// A source of another is GROUPCALL_SSRC_DUPLICATE_MUCH, and the
	// connection the SFU took is closed again.
	if _, err = join(2, 100, false); !errors.Is(err, mtproto.ErrGroupcallSsrcDuplicateMuch) {
		t.Fatalf("a taken source: %v", err)
	}
	if _, ok := w.sfu.rooms["main"][gc.GetId()][fakeConn{gc.GetId(), 2, 0}]; ok {
		t.Fatal("the refused connection stays on the SFU")
	}

	// The second joins with a camera: the first gets the min row with the
	// next version and the video's source groups.
	w.rec.taken(1)
	if u, err = join(2, 200, true); err != nil {
		t.Fatal(err)
	}
	// The other devices of 2 learn of the join too.
	if ps := w.rec.taken(2); len(ps) != 1 {
		t.Fatalf("pushes to the other devices of 2: %v", ps)
	}
	if mine, v := rowOf(u, 2); mine.GetVideo().GetEndpoint() == "" || v != 3 {
		t.Fatalf("own row with a camera %v at %d", mine, v)
	}
	ps := w.rec.taken(1)
	if len(ps) != 1 {
		t.Fatalf("pushes to 1: %v", ps)
	}
	other, v := rowOf(ps[0], 2)
	if other == nil || !other.GetMin() || other.GetSelf() || !other.GetJustJoined() || v != 3 || len(other.GetVideo().GetSourceGroups()) != 4 {
		t.Fatalf("row of 2 as 1 sees it: %v at %d", other, v)
	}

	// checkGroupCall answers what the SFU holds: nothing once the room is
	// lost (tdesktop calls_group_call.cpp:3746-3776).
	if got, err := w.as(2).PhoneCheckGroupCall(&mtproto.TLPhoneCheckGroupCall{Call: call, Sources: []int32{200, 999}}); err != nil || !slices.Equal(got.GetDatas(), []int32{200}) {
		t.Fatalf("check: %v %v", got, err)
	}
	if _, err = w.as(3).PhoneCheckGroupCall(&mtproto.TLPhoneCheckGroupCall{Call: call, Sources: []int32{200}}); !errors.Is(err, mtproto.ErrGroupcallJoinMissing) {
		t.Fatalf("check by one not in the call: %v", err)
	}

	// A member mutes another for himself only: muted_by_you in his answer,
	// nothing to the others.
	u, err = w.as(2).PhoneEditGroupCallParticipant(&mtproto.TLPhoneEditGroupCallParticipant{Call: call, Participant: inputUser(1), Muted: mtproto.ToBool(true), Volume: wrapperspb.Int32(5000)})
	if err != nil {
		t.Fatal(err)
	}
	if row, _ := rowOf(u, 1); !row.GetMutedByYou() || row.GetMuted() || row.GetVolume().GetValue() != 5000 || row.GetVolumeByAdmin() {
		t.Fatalf("personal mute: %v", row)
	}
	if ps = w.rec.taken(1); len(ps) != 0 {
		t.Fatalf("a personal mute went to 1: %v", ps)
	}

	// The creator takes the word of 2: can_self_unmute goes, the SFU stops
	// the sound, and 2 cannot unmute himself.
	if _, err = w.as(1).PhoneEditGroupCallParticipant(&mtproto.TLPhoneEditGroupCallParticipant{Call: call, Participant: inputUser(2), Muted: mtproto.ToBool(true)}); err != nil {
		t.Fatal(err)
	}
	if !w.sfu.muted[fakeConn{gc.GetId(), 2, 0}] {
		t.Fatal("the SFU forwards the sound of a participant without the word")
	}
	ps = w.rec.taken(2)
	if len(ps) != 1 {
		t.Fatalf("pushes to 2: %v", ps)
	}
	if row, v := rowOf(ps[0], 2); !row.GetSelf() || !row.GetMuted() || row.GetCanSelfUnmute() || row.GetVersioned() || v != 3 {
		t.Fatalf("2 after the admin's mute: %v at %d", row, v)
	}
	if _, err = w.as(2).PhoneEditGroupCallParticipant(&mtproto.TLPhoneEditGroupCallParticipant{Call: call, Participant: self(), Muted: mtproto.ToBool(false)}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("unmute without the word: %v", err)
	}
	if u, err = w.as(2).PhoneEditGroupCallParticipant(&mtproto.TLPhoneEditGroupCallParticipant{Call: call, Participant: self(), RaiseHand: mtproto.ToBool(true)}); err != nil {
		t.Fatal(err)
	}
	if row, _ := rowOf(u, 2); row.GetRaiseHandRating().GetValue() <= 0 {
		t.Fatalf("raised hand: %v", row)
	}

	// One not in the call resolves a source and pages through the list.
	got, err := w.as(3).PhoneGetGroupParticipants(&mtproto.TLPhoneGetGroupParticipants{Call: call, Sources: []int32{200}, Limit: 10})
	if err != nil || len(got.GetParticipants()) != 1 || got.GetParticipants()[0].GetPeer().GetUserId() != 2 || got.GetCount() != 2 || got.GetVersion() != 3 {
		t.Fatalf("by source: %v %v", got, err)
	}
	first, err := w.as(3).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: call, Limit: 1})
	if err != nil || len(first.GetParticipants()) != 1 || first.GetParticipantsNextOffset() == "" {
		t.Fatalf("first page: %v %v", first, err)
	}
	rest, err := w.as(3).PhoneGetGroupParticipants(&mtproto.TLPhoneGetGroupParticipants{Call: call, Offset: first.GetParticipantsNextOffset(), Limit: 10})
	if err != nil || len(rest.GetParticipants()) != 1 || rest.GetNextOffset() != "" {
		t.Fatalf("next page: %v %v", rest, err)
	}

	// Only the account itself to join as.
	as, err := w.as(3).PhoneGetGroupCallJoinAs(&mtproto.TLPhoneGetGroupCallJoinAs{Peer: w.inputChannel()})
	if err != nil || len(as.GetPeers()) != 1 || as.GetPeers()[0].GetUserId() != 3 {
		t.Fatalf("join as: %v %v", as, err)
	}
	if _, err = w.as(3).PhoneSaveDefaultGroupCallJoinAs(&mtproto.TLPhoneSaveDefaultGroupCallJoinAs{Peer: w.inputChannel(), JoinAs: w.inputChannel()}); !errors.Is(err, mtproto.ErrJoinAsPeerInvalid) {
		t.Fatalf("save a channel to join as: %v", err)
	}

	// A leave of a replaced connection changes nothing; the leave of the
	// own one is versioned.
	if u, err = w.as(2).PhoneLeaveGroupCall(&mtproto.TLPhoneLeaveGroupCall{Call: call, Source: 999}); err != nil || callOf(t, u).GetParticipantsCount() != 2 {
		t.Fatalf("stale leave: %v", err)
	}
	if u, err = w.as(2).PhoneLeaveGroupCall(&mtproto.TLPhoneLeaveGroupCall{Call: call, Source: 200}); err != nil {
		t.Fatal(err)
	}
	if row, v := rowOf(u, 2); !row.GetLeft() || !row.GetVersioned() || v != 4 {
		t.Fatalf("leave: %v at %d", row, v)
	}

	// The end: groupCallDiscarded with the duration, the room closed, the
	// end with its duration in the chat; no join after it.
	w.rec.taken(3)
	if _, err = w.as(2).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: call}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("a member ends the call: %v", err)
	}
	if u, err = w.as(1).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: call}); err != nil {
		t.Fatal(err)
	}
	if gc = callOf(t, u); gc.GetPredicateName() != mtproto.Predicate_groupCallDiscarded {
		t.Fatalf("discarded: %v", gc)
	}
	if _, open := w.sfu.rooms["main"][gc.GetId()]; open {
		t.Fatal("the room of an ended call is open")
	}
	last := w.rec.sent[len(w.rec.sent)-1].GetMessage()[0].GetMessage().GetAction()
	if last.GetPredicateName() != mtproto.Predicate_messageActionGroupCall || last.GetDuration() == nil {
		t.Fatalf("the end in the chat: %v", last)
	}
	if ps = w.rec.taken(3); len(ps) != 1 || callOf(t, ps[0]).GetPredicateName() != mtproto.Predicate_groupCallDiscarded {
		t.Fatalf("member told of the end: %v", ps)
	}
	if _, err = join(1, 101, false); !errors.Is(err, mtproto.ErrGroupcallAlreadyDiscarded) {
		t.Fatalf("join after the end: %v", err)
	}
}

// A lost room (an SFU restart): checkGroupCall finds nothing, the client
// joins again, the room opens again and the version moves on; the
// collector takes out who did not come back.
func TestGroupCallSfuRestart(t *testing.T) {
	w := newWorld(t)
	u, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, u)
	call := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	for _, id := range []int64{1, 2} {
		if _, err = w.as(id).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(uint32(id*100), false)}); err != nil {
			t.Fatal(err)
		}
	}
	delete(w.sfu.rooms["main"], gc.GetId())
	if got, err := w.as(1).PhoneCheckGroupCall(&mtproto.TLPhoneCheckGroupCall{Call: call, Sources: []int32{100}}); err != nil || len(got.GetDatas()) != 0 {
		t.Fatalf("check after the restart: %v %v", got, err)
	}
	u, err = w.as(1).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(101, false)})
	if err != nil {
		t.Fatal(err)
	}
	if me, v := rowOf(u, 1); me.GetJustJoined() || me.GetSource() != 101 || v != 4 {
		t.Fatalf("join again: %v at %d", me, v)
	}

	// 2 never comes back: past the absence limit the collector takes him
	// out, versioned.
	w.svcCtx.AbsentTimeout = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	col := &Collector{svcCtx: w.svcCtx, empty: map[int64]time.Time{}}
	w.rec.taken(1)
	col.pass(context.Background())
	after, err := w.svcCtx.Dao.Store.Get(context.Background(), gc.GetId())
	if err != nil || after.ParticipantsCount != 1 || after.Version != 5 {
		t.Fatalf("after the collector: %+v %v", after, err)
	}
	ps := w.rec.taken(1)
	if len(ps) != 1 {
		t.Fatalf("pushes to 1: %v", ps)
	}
	if row, v := rowOf(ps[0], 2); !row.GetLeft() || v != 5 {
		t.Fatalf("2 taken out: %v at %d", row, v)
	}
}

// The collector's repairs and the races of a join: a SetMuted the SFU did
// not take is sent again; one removed from the chat leaves the call; a join
// that races the end of the call leaves no room open; a join that races
// the collector's close of an empty room places the call again. An
// invitation leaves out who is not a member.
func TestGroupCallRepairsAndRaces(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	create := func() (*mtproto.InputGroupCall, int64) {
		t.Helper()
		u, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano())})
		if err != nil {
			t.Fatal(err)
		}
		gc := callOf(t, u)
		return mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall(), gc.GetId()
	}
	join := func(call *mtproto.InputGroupCall, userId int64, ssrc uint32) (*mtproto.Updates, error) {
		return w.as(userId).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, false)})
	}
	call, id := create()
	for _, u := range []int64{1, 2, 3} {
		if _, err := join(call, u, uint32(u*100)); err != nil {
			t.Fatal(err)
		}
	}
	col := &Collector{svcCtx: w.svcCtx, empty: map[int64]time.Time{}}

	// The invitation of a member and of one who is not: the member is
	// invited (tdesktop sends both in one request after trying to add the
	// other, calls_group_invite_controller.cpp:914-925).
	if _, err := w.as(1).PhoneInviteToGroupCall(&mtproto.TLPhoneInviteToGroupCall{Call: call, Users: []*mtproto.InputUser{
		mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 2}).To_InputUser(), mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 4}).To_InputUser(),
	}}); err != nil {
		t.Fatalf("invite a member and a stranger: %v", err)
	}
	if users := w.rec.sent[len(w.rec.sent)-1].GetMessage()[0].GetMessage().GetAction().GetUsers(); !slices.Equal(users, []int64{2}) {
		t.Fatalf("invited %v", users)
	}
	if _, err := w.as(1).PhoneInviteToGroupCall(&mtproto.TLPhoneInviteToGroupCall{Call: call, Users: []*mtproto.InputUser{
		mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 4}).To_InputUser(),
	}}); !errors.Is(err, mtproto.ErrUserNotParticipant) {
		t.Fatalf("invite only a stranger: %v", err)
	}

	// The SFU does not take the mute: the collector sends it again.
	w.sfu.mu.Lock()
	w.sfu.setMutedErr = []error{errors.New("unavailable")}
	w.sfu.mu.Unlock()
	if _, err := w.as(1).PhoneEditGroupCallParticipant(&mtproto.TLPhoneEditGroupCallParticipant{Call: call, Participant: inputUser(2), Muted: mtproto.ToBool(true)}); err != nil {
		t.Fatal(err)
	}
	if w.sfu.muted[fakeConn{id, 2, 0}] {
		t.Fatal("the fake took the mute")
	}
	col.pass(ctx)
	if !w.sfu.muted[fakeConn{id, 2, 0}] {
		t.Fatal("the collector did not mute 2 on the SFU")
	}

	// 3 is removed from the channel: the collector takes him out, and the
	// call is closed to him.
	w.removed.Store(3)
	w.rec.taken(3)
	col.pass(ctx)
	if p, err := w.svcCtx.Dao.Store.ParticipantOf(ctx, id, 3); err != nil || p.InCall {
		t.Fatalf("3 still in the call: %+v %v", p, err)
	}
	if _, ok := w.sfu.rooms["main"][id][fakeConn{id, 3, 0}]; ok {
		t.Fatal("the connection of 3 stays on the SFU")
	}
	ps := w.rec.taken(3)
	if len(ps) == 0 {
		t.Fatal("3 not told he left")
	}
	if row, _ := rowOf(ps[0], 3); !row.GetLeft() {
		t.Fatalf("3 told %v", ps[0])
	}
	if _, err := w.as(3).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: call, Limit: 3}); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("3 reads the call: %v", err)
	}
	if _, err := join(call, 3, 301); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("3 joins again: %v", err)
	}
	w.removed.Store(0)

	// A join connects while the call ends: the room it opened again is
	// closed.
	w.sfu.mu.Lock()
	w.sfu.beforeJoin = func() {
		if _, err := w.as(1).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: call}); err != nil {
			t.Errorf("discard: %v", err)
		}
	}
	w.sfu.mu.Unlock()
	if _, err := join(call, 3, 302); !errors.Is(err, mtproto.ErrGroupcallAlreadyDiscarded) {
		t.Fatalf("a join that races the end: %v", err)
	}
	if _, open := w.sfu.rooms["main"][id]; open {
		t.Fatal("the room opened by a join that raced the end stays open")
	}

	// A join connects while the collector closes the room it found empty
	// and forgets its node: the join places the call on the node again.
	call, id = create()
	w.sfu.mu.Lock()
	w.sfu.beforeJoin = func() {
		if err := w.sfu.CloseRoom(ctx, "main", id); err != nil {
			t.Error(err)
		}
		if _, err := w.svcCtx.Dao.Store.ForgetEmpty(ctx, id, "main"); err != nil {
			t.Error(err)
		}
	}
	w.sfu.mu.Unlock()
	if _, _, err := w.svcCtx.Dao.Store.SetNode(ctx, id, "", "main"); err != nil {
		t.Fatal(err)
	}
	if err := w.sfu.CreateRoom(ctx, "main", id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := join(call, 2, 210); err != nil {
		t.Fatal(err)
	}
	after, err := w.svcCtx.Dao.Store.Get(ctx, id)
	if err != nil || after.SfuNode != "main" {
		t.Fatalf("node after a join that raced the collector: %+v %v", after, err)
	}
	if _, ok := w.sfu.rooms["main"][id][fakeConn{id, 2, 0}]; !ok {
		t.Fatal("2 is not in the room")
	}
}
