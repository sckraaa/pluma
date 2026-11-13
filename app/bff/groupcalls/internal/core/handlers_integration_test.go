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
	"crypto/ed25519"
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
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/e2emon"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	store := groupcalls.NewStore(sqlx.NewMySQL(&c))
	return &world{
		removed: removed,
		channel: channel,
		svcCtx: &svc.ServiceContext{
			E2E:     e2emon.New(store),
			Members: svc.NewChainMembers(),
			Dao: &dao.Dao{
				Store:         store,
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

// keyOf is the key userId joins with: one per user, so that a join again is
// with the same key, as iOS does to restore its transport (ADR 0060 §2).
func keyOf(userId int64) ed25519.PrivateKey { return tde2etest.Key(byte(userId)) }

// sealed fills the key of userId and his join block on top of the chain of
// the call of req, as a client builds it from the last block.
func (w *world) sealed(t *testing.T, userId int64, req *mtproto.TLPhoneJoinGroupCall) *mtproto.TLPhoneJoinGroupCall {
	t.Helper()
	ch, err := w.svcCtx.Dao.Store.Chain(context.Background(), req.GetCall().GetId())
	if err != nil {
		t.Fatalf("the chain of the call: %v", err)
	}
	pk := tde2etest.Pub(keyOf(userId))
	req.PublicKey, req.Block = pk[:], tde2etest.JoinBlock(ch, userId, keyOf(userId))
	return req
}

// chainOf is the chain of a call as stored.
func (w *world) chainOf(t *testing.T, callId int64) *tde2e.Chain {
	t.Helper()
	ch, err := w.svcCtx.Dao.Store.Chain(context.Background(), callId)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// chainUpdates are the updateGroupCallChainBlocks of u by sub-chain.
func chainUpdates(u *mtproto.Updates) map[int32]*mtproto.Update {
	out := map[int32]*mtproto.Update{}
	for _, up := range updatesOf(u, mtproto.Predicate_updateGroupCallChainBlocks) {
		out[up.GetSubChainId()] = up
	}
	return out
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
		return w.as(userId).PhoneJoinGroupCall(w.sealed(t, userId, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, video)}))
	}
	if _, err = w.as(1).PhoneJoinGroupCall(w.sealed(t, 1, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: w.inputChannel(), Params: params(100, false)})); !errors.Is(err, mtproto.ErrJoinAsPeerInvalid) {
		t.Fatalf("join as a channel: %v", err)
	}
	// Every call is end-to-end encrypted (ADR 0060): a client without the
	// key and the block is refused.
	if _, err = w.as(1).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(100, false)}); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("join without the encryption: %v", err)
	}
	if _, err = join(4, 400, false); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("a stranger joins: %v", err)
	}
	if _, err = w.as(1).PhoneJoinGroupCall(w.sealed(t, 1, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: "{}"}).To_DataJSON()})); !errors.Is(err, mtproto.ErrDataJsonInvalid) {
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
	// Both sub-chains: the block of the join, in the server form, with the
	// height after it, and no broadcast (iOS GroupCalls.swift:922-944).
	subs := chainUpdates(u)
	if b := subs[0]; b == nil || len(b.GetBlocks()) != 1 || !tde2e.IsFromServer(b.GetBlocks()[0]) || b.GetNextOffset() != 1 {
		t.Fatalf("sub-chain 0 of the zero block: %v", b)
	}
	if b := subs[1]; b == nil || len(b.GetBlocks()) != 0 || b.GetNextOffset() != 0 {
		t.Fatalf("sub-chain 1 of the join: %v", b)
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

	// A source of another is GROUPCALL_SSRC_DUPLICATE_MUCH: neither the
	// block nor a connection is taken.
	if _, err = join(2, 100, false); !errors.Is(err, mtproto.ErrGroupcallSsrcDuplicateMuch) {
		t.Fatalf("a taken source: %v", err)
	}
	if _, ok := w.sfu.rooms["main"][gc.GetId()][fakeConn{gc.GetId(), 2, 0}]; ok {
		t.Fatal("the refused join is on the SFU")
	}
	if h := w.chainOf(t, gc.GetId()).Height(); h != 0 {
		t.Fatalf("the refused join moved the chain to %d", h)
	}

	// The second joins with a camera: the first gets the min row with the
	// next version and the video's source groups.
	w.rec.taken(1)
	if u, err = join(2, 200, true); err != nil {
		t.Fatal(err)
	}
	// The other devices of 2 learn of the join too, and of its block.
	if ps := w.rec.taken(2); len(ps) != 2 {
		t.Fatalf("pushes to the other devices of 2: %v", ps)
	}
	if mine, v := rowOf(u, 2); mine.GetVideo().GetEndpoint() == "" || v != 3 {
		t.Fatalf("own row with a camera %v at %d", mine, v)
	}
	// 1 gets the row, then the block of the join
	// (TestGroupCallChainDeliveryOrder).
	ps := w.rec.taken(1)
	if len(ps) != 2 {
		t.Fatalf("pushes to 1: %v", ps)
	}
	if b := chainUpdates(ps[1])[0]; b == nil || len(b.GetBlocks()) != 1 || b.GetNextOffset() != 2 {
		t.Fatalf("the block of the join of 2 sent to 1: %v", ps[1])
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
		if _, err = w.as(id).PhoneJoinGroupCall(w.sealed(t, id, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(uint32(id*100), false)})); err != nil {
			t.Fatal(err)
		}
	}
	delete(w.sfu.rooms["main"], gc.GetId())
	if got, err := w.as(1).PhoneCheckGroupCall(&mtproto.TLPhoneCheckGroupCall{Call: call, Sources: []int32{100}}); err != nil || len(got.GetDatas()) != 0 {
		t.Fatalf("check after the restart: %v %v", got, err)
	}
	// The join again is with the same key: the transport is restored.
	u, err = w.as(1).PhoneJoinGroupCall(w.sealed(t, 1, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(101, false)}))
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
		return w.as(userId).PhoneJoinGroupCall(w.sealed(t, userId, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, false)}))
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

	// A join connects while the collector closes the room it found empty:
	// the join is in the call already, so the node is not forgotten, and
	// the join opens the room again.
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
	if err := w.sfu.CreateRoom(ctx, "main", id); err != nil {
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

// The chain of a call's encryption as the clients drive it (ADR 0060 §2):
// joins at one height, reads by members and by others, the removal of one
// who left by the others, what the removed one may still do, a join again
// with the same key and with another's, a kick, the emoji verification.
func TestGroupCallChain(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	u, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, u)
	id := gc.GetId()
	call := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: id, AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	joinReq := func(userId int64, ssrc uint32) *mtproto.TLPhoneJoinGroupCall {
		return w.sealed(t, userId, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, false)})
	}
	read := func(userId int64, sub, offset, limit int32) *mtproto.Update {
		t.Helper()
		r, err := w.as(userId).PhoneGetGroupCallChainBlocks(&mtproto.TLPhoneGetGroupCallChainBlocks{Call: call, SubChainId: sub, Offset: offset, Limit: limit})
		if err != nil {
			t.Fatalf("read of %d: %v", userId, err)
		}
		ups := updatesOf(r, mtproto.Predicate_updateGroupCallChainBlocks)
		if len(ups) != 1 {
			t.Fatalf("read of %d: %v", userId, r)
		}
		return ups[0]
	}

	// Before any block, the last block is none; 2 and 3 build their joins
	// on the same height: the first is taken, the second is stale.
	if b := read(2, 0, -1, 1); len(b.GetBlocks()) != 0 || b.GetNextOffset() != 0 {
		t.Fatalf("the last block of an empty chain: %v", b)
	}
	first, second := joinReq(2, 200), joinReq(3, 300)
	if _, err = w.as(2).PhoneJoinGroupCall(first); err != nil {
		t.Fatal(err)
	}
	if _, err = w.as(3).PhoneJoinGroupCall(second); !errors.Is(err, errChainInvalid) {
		t.Fatalf("a join on a taken height: %v", err)
	}
	if _, err = w.as(3).PhoneJoinGroupCall(joinReq(3, 300)); err != nil {
		t.Fatalf("the join again on the last block: %v", err)
	}
	if _, err = w.as(1).PhoneJoinGroupCall(joinReq(1, 100)); err != nil {
		t.Fatal(err)
	}
	// A member reads from an offset; next_offset follows the last block.
	if b := read(2, 0, 1, 50); len(b.GetBlocks()) != 2 || b.GetNextOffset() != 3 {
		t.Fatalf("blocks 1.. of a member: %d, next %d", len(b.GetBlocks()), b.GetNextOffset())
	}
	if b := read(2, 0, 0, 1); len(b.GetBlocks()) != 1 || b.GetNextOffset() != 1 {
		t.Fatalf("block 0 of a member: %v", b)
	}

	// The key of 3 is his: 2 may not join with it.
	stolen := joinReq(2, 201)
	pk3 := tde2etest.Pub(keyOf(3))
	stolen.PublicKey = pk3[:]
	stolen.Block = tde2etest.JoinBlock(w.chainOf(t, id), 2, keyOf(3))
	if _, err = w.as(2).PhoneJoinGroupCall(stolen); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("a join with the key of another: %v", err)
	}

	// 3 leaves the call; a member that is still in the call may not be
	// removed as one who left, 3 may: of the two removals the first is
	// taken, the second is stale.
	if _, err = w.as(3).PhoneLeaveGroupCall(&mtproto.TLPhoneLeaveGroupCall{Call: call, Source: 300}); err != nil {
		t.Fatal(err)
	}
	ch := w.chainOf(t, id)
	if _, err = w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{
		OnlyLeft: true, Call: call, Ids: []int64{2}, Block: tde2etest.RemoveBlock(ch, keyOf(1), 2),
	}); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("only_left of one in the call: %v", err)
	}
	by1 := tde2etest.RemoveBlock(ch, keyOf(1), 3)
	by2 := tde2etest.RemoveBlock(ch, keyOf(2), 3)
	w.rec.taken(3)
	r, err := w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{OnlyLeft: true, Call: call, Ids: []int64{3}, Block: by1})
	if err != nil {
		t.Fatal(err)
	}
	if b := chainUpdates(r)[0]; b == nil || len(b.GetBlocks()) != 1 || b.GetNextOffset() != 4 {
		t.Fatalf("the answer of the removal: %v", r)
	}
	if _, err = w.as(2).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{OnlyLeft: true, Call: call, Ids: []int64{3}, Block: by2}); !errors.Is(err, errChainInvalid) {
		t.Fatalf("the second removal: %v", err)
	}
	// The removed one gets his removal, may not write, and reads only the
	// last block whatever he asks.
	if ps := w.rec.taken(3); len(ps) != 1 || chainUpdates(ps[0])[0] == nil {
		t.Fatalf("the removal sent to 3: %v", ps)
	}
	if b := read(3, 0, 0, 100); len(b.GetBlocks()) != 1 || b.GetNextOffset() != 4 {
		t.Fatalf("a read of the removed one: %d blocks, next %d", len(b.GetBlocks()), b.GetNextOffset())
	}
	if b := read(3, 1, 0, 100); len(b.GetBlocks()) != 0 {
		t.Fatalf("sub-chain 1 to the removed one: %v", b)
	}
	ch = w.chainOf(t, id)
	var nonce3 [32]byte
	if _, err = w.as(3).PhoneSendConferenceCallBroadcast(&mtproto.TLPhoneSendConferenceCallBroadcast{Call: call, Block: tde2etest.Commit(ch, 3, keyOf(3), nonce3)}); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("a broadcast of the removed one: %v", err)
	}
	if _, err = w.as(3).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{OnlyLeft: true, Call: call, Ids: []int64{2}, Block: tde2etest.RemoveBlock(ch, keyOf(3), 2)}); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("a removal by the removed one: %v", err)
	}

	// The emoji verification of the height: commits of both, a reveal
	// before all committed is refused, the reveals; next_offset counts the
	// broadcasts.
	n1, n2 := [32]byte{1}, [32]byte{2}
	send := func(userId int64, block []byte) (*mtproto.Updates, error) {
		return w.as(userId).PhoneSendConferenceCallBroadcast(&mtproto.TLPhoneSendConferenceCallBroadcast{Call: call, Block: block})
	}
	if r, err = send(1, tde2etest.Commit(ch, 1, keyOf(1), n1)); err != nil {
		t.Fatal(err)
	}
	if b := chainUpdates(r)[1]; b == nil || len(b.GetBlocks()) != 1 || b.GetNextOffset() != 1 || !tde2e.IsFromServer(b.GetBlocks()[0]) {
		t.Fatalf("the answer of a commit: %v", r)
	}
	if _, err = send(1, tde2etest.Reveal(ch, 1, keyOf(1), n1)); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("a reveal before all committed: %v", err)
	}
	if _, err = send(1, tde2etest.Commit(ch, 1, keyOf(1), n1)); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("a second commit: %v", err)
	}
	for _, step := range []struct {
		user  int64
		block []byte
	}{{2, tde2etest.Commit(ch, 2, keyOf(2), n2)}, {1, tde2etest.Reveal(ch, 1, keyOf(1), n1)}, {2, tde2etest.Reveal(ch, 2, keyOf(2), n2)}} {
		if _, err = send(step.user, step.block); err != nil {
			t.Fatalf("broadcast of %d: %v", step.user, err)
		}
	}
	if b := read(2, 1, 0, 100); len(b.GetBlocks()) != 4 || b.GetNextOffset() != 4 {
		t.Fatalf("sub-chain 1: %d blocks, next %d", len(b.GetBlocks()), b.GetNextOffset())
	}

	// 3 joins again with his own key: taken; a join answers sub-chain 1
	// with the count of broadcasts, which goes on over the heights.
	if r, err = w.as(3).PhoneJoinGroupCall(joinReq(3, 301)); err != nil {
		t.Fatalf("a join again with the same key after removal: %v", err)
	}
	if b := chainUpdates(r)[1]; b == nil || b.GetNextOffset() != 4 {
		t.Fatalf("sub-chain 1 in the join: %v", b)
	}
	// A broadcast for the height before is answered and not kept.
	if r, err = send(2, tde2etest.Commit(ch, 2, keyOf(2), n2)); err != nil {
		t.Fatalf("a broadcast of an earlier height: %v", err)
	}
	if b := chainUpdates(r)[1]; b == nil || len(b.GetBlocks()) != 0 || b.GetNextOffset() != 4 {
		t.Fatalf("the answer of an old broadcast: %v", r)
	}

	// Only an admin kicks; a kick takes the user out of the call too.
	ch = w.chainOf(t, id)
	if _, err = w.as(2).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{Kick: true, Call: call, Ids: []int64{3}, Block: tde2etest.RemoveBlock(ch, keyOf(2), 3)}); !errors.Is(err, mtproto.ErrGroupcallForbidden) {
		t.Fatalf("a kick by a member: %v", err)
	}
	if r, err = w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{Kick: true, Call: call, Ids: []int64{3}, Block: tde2etest.RemoveBlock(ch, keyOf(1), 3)}); err != nil {
		t.Fatal(err)
	}
	if row, _ := rowOf(r, 3); row == nil || !row.GetLeft() {
		t.Fatalf("the kicked one in the answer: %v", r)
	}
	if p, err := w.svcCtx.Dao.Store.ParticipantOf(ctx, id, 3); err != nil || p.InCall {
		t.Fatalf("the kicked one is in the call: %+v %v", p, err)
	}
	if _, ok := w.sfu.rooms["main"][id][fakeConn{id, 3, 0}]; ok {
		t.Fatal("the kicked one stays on the SFU")
	}
	if _, err = w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{OnlyLeft: true, Kick: true, Call: call, Ids: []int64{2}, Block: tde2etest.RemoveBlock(w.chainOf(t, id), keyOf(1), 2)}); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("only_left and kick at once: %v", err)
	}

	// The conferences as a product are not served.
	if _, err = w.as(1).PhoneCreateConferenceCall7D0444BB(&mtproto.TLPhoneCreateConferenceCall7D0444BB{RandomId: 1}); !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Fatalf("createConferenceCall: %v", err)
	}

	// After the end the chain is read no more, and the collector deletes it
	// once ChainKeep is over.
	if _, err = w.as(1).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: call}); err != nil {
		t.Fatal(err)
	}
	if _, err = w.as(2).PhoneGetGroupCallChainBlocks(&mtproto.TLPhoneGetGroupCallChainBlocks{Call: call, SubChainId: 0, Offset: -1, Limit: 1}); !errors.Is(err, mtproto.ErrGroupcallAlreadyDiscarded) {
		t.Fatalf("a read after the end: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	w.svcCtx.ChainKeep = time.Nanosecond
	(&Collector{svcCtx: w.svcCtx, empty: map[int64]time.Time{}}).pass(ctx)
	after, err := w.svcCtx.Dao.Store.Get(ctx, id)
	if err != nil || after.ChainHeight != -1 {
		t.Fatalf("the chain after the collector: %+v %v", after, err)
	}
	if left, err := w.svcCtx.Dao.Store.Blocks(ctx, id, 0, 0, 100); err != nil || len(left) != 0 {
		t.Fatalf("blocks after the collector: %d %v", len(left), err)
	}
}

// pushOf is the index of the first push of ps that has an update of
// predicate, -1 for none.
func pushOf(ps []*mtproto.Updates, predicate string) int {
	for i, u := range ps {
		if len(updatesOf(u, predicate)) > 0 {
			return i
		}
	}
	return -1
}

// inOrder fails unless the pushes ps have the participants' rows before the
// block of the chain (first true) or the other way round.
func inOrder(t *testing.T, what string, ps []*mtproto.Updates, rowsFirst bool) {
	t.Helper()
	rows, chain := pushOf(ps, mtproto.Predicate_updateGroupCallParticipants), pushOf(ps, mtproto.Predicate_updateGroupCallChainBlocks)
	if rows < 0 || chain < 0 || (rows < chain) != rowsFirst {
		t.Fatalf("%s: the rows in push %d, the block in push %d of %d", what, rows, chain, len(ps))
	}
}

// The order of what a write of the chain sends the others. A join: the
// joiner's row before his block, so that tdesktop has him among the
// participants when the block changes the chain (data_group_call.cpp:
// 85-93, 233-255) and a device of the same user hangs up on its row before
// the block replaces its key. A join the SFU refused: his leave before the
// block, so that the removal of the others is taken. A kick: the block
// before the row, so that the kicked is out of the chain when he leaves the
// call.
func TestGroupCallChainDeliveryOrder(t *testing.T) {
	w := newWorld(t)
	u, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, u)
	call := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	joinReq := func(userId int64, ssrc uint32) *mtproto.TLPhoneJoinGroupCall {
		return w.sealed(t, userId, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(ssrc, false)})
	}
	for _, id := range []int64{1, 2} {
		if _, err = w.as(id).PhoneJoinGroupCall(joinReq(id, uint32(id*100))); err != nil {
			t.Fatal(err)
		}
	}
	w.rec.taken(1)
	if _, err = w.as(3).PhoneJoinGroupCall(joinReq(3, 300)); err != nil {
		t.Fatal(err)
	}
	inOrder(t, "a join, to another participant", w.rec.taken(1), true)

	// A second device of 3, with its own key.
	w.rec.taken(3)
	second := w.as(3)
	second.MD = &metadata.RpcMetadata{UserId: 3, PermAuthKeyId: 31}
	key := tde2etest.Key(33)
	pk := tde2etest.Pub(key)
	req := &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(301, false), PublicKey: pk[:],
		Block: tde2etest.JoinBlock(w.chainOf(t, gc.GetId()), 3, key)}
	if _, err = second.PhoneJoinGroupCall(req); err != nil {
		t.Fatal(err)
	}
	inOrder(t, "a join from another device, to the first", w.rec.taken(3), true)

	// 2 leaves and joins again; the SFU refuses the connection.
	if _, err = w.as(2).PhoneLeaveGroupCall(&mtproto.TLPhoneLeaveGroupCall{Call: call, Source: 200}); err != nil {
		t.Fatal(err)
	}
	w.rec.taken(1)
	w.sfu.mu.Lock()
	w.sfu.joinErr = []error{status.Error(codes.Internal, "x")}
	w.sfu.mu.Unlock()
	if _, err = w.as(2).PhoneJoinGroupCall(joinReq(2, 201)); err == nil {
		t.Fatal("a join the SFU refused")
	}
	ps := w.rec.taken(1)
	inOrder(t, "a join the SFU refused, to another participant", ps, true)
	if row, _ := rowOf(ps[pushOf(ps, mtproto.Predicate_updateGroupCallParticipants)], 2); row == nil || !row.GetLeft() {
		t.Fatalf("the row of a join the SFU refused: %v", row)
	}
	// The others' removal of him is taken.
	if _, err = w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{
		OnlyLeft: true, Call: call, Ids: []int64{2}, Block: tde2etest.RemoveBlock(w.chainOf(t, gc.GetId()), keyOf(1), 2),
	}); err != nil {
		t.Fatalf("the removal of one the SFU refused: %v", err)
	}

	w.rec.taken(3)
	if _, err = w.as(1).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{
		Kick: true, Call: call, Ids: []int64{3}, Block: tde2etest.RemoveBlock(w.chainOf(t, gc.GetId()), keyOf(1), 3),
	}); err != nil {
		t.Fatal(err)
	}
	inOrder(t, "a kick, to the kicked", w.rec.taken(3), false)

	// A list of ids no group state can hold is refused before anything.
	ids := make([]int64, tde2e.MaxParticipants+1)
	for i := range ids {
		ids[i] = 3
	}
	if _, err = w.as(2).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{
		OnlyLeft: true, Call: call, Ids: ids, Block: tde2etest.RemoveBlock(w.chainOf(t, gc.GetId()), keyOf(1), 3),
	}); !errors.Is(err, errBlockInvalid) {
		t.Fatalf("a removal of %d ids: %v", len(ids), err)
	}
}

// What an SFU node says of the encryption counts only for a call placed on
// it and a participant in that call (ADR 0060 §3: a node may stand outside
// the trusted zone).
func TestGroupCallEventsOfOtherNodes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	u, err := w.as(1).PhoneCreateGroupCall(&mtproto.TLPhoneCreateGroupCall{Peer: w.inputChannel(), RandomId: int32(time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, u)
	id := gc.GetId()
	call := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: id, AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	for _, uid := range []int64{1, 2} {
		if _, err = w.as(uid).PhoneJoinGroupCall(w.sealed(t, uid, &mtproto.TLPhoneJoinGroupCall{Call: call, JoinAs: self(), Params: params(uint32(uid*100), false)})); err != nil {
			t.Fatal(err)
		}
	}
	report := func(callId, userId int64) *pb.Event {
		return &pb.Event{Type: pb.Event_TYPE_E2E_STATUS, CallId: callId, Participant: userId, Kind: pb.ConnectionKind_CONNECTION_KIND_MAIN,
			E2EStatus: &pb.E2EStatus{Seq: 1, Height: -1, Offset0: -1, Offset1: -1, Error: 300}}
	}
	epochs := func(callId, userId int64) *pb.Event {
		return &pb.Event{Type: pb.Event_TYPE_SEND_EPOCHS, CallId: callId, Participant: userId, Kind: pb.ConnectionKind_CONNECTION_KIND_MAIN,
			SendEpochs: []string{"0000000000000000"}}
	}
	ev := NewEvents(w.svcCtx)
	ev.Handle("other", report(id, 2))
	ev.Handle("other", epochs(id, 2))
	ev.Handle("main", report(id, 3))
	ev.Handle("main", report(id+1_000_000_007, 1))
	ev.Handle("main", report(id, 1))
	ev.Handle("main", epochs(id, 1))
	w.svcCtx.E2E.Tick(ctx)
	diags, err := w.svcCtx.Dao.Store.Diags(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]*groupcalls.E2EDiag{}
	for _, d := range diags {
		by[d.UserId] = d.Diag
	}
	if d := by[1]; d == nil || d.Report == nil || d.Report.Error != 300 || len(d.SendEpochs) != 1 {
		t.Fatalf("the word of the node of the call: %+v", d)
	}
	if d := by[2]; d == nil || d.Report != nil || len(d.SendEpochs) != 0 {
		t.Fatalf("the word of another node: %+v", d)
	}
	if by[3] != nil {
		t.Fatalf("a report of one not in the call: %+v", by[3])
	}
	if other, err := w.svcCtx.Dao.Store.Diags(ctx, id+1_000_000_007); err != nil || len(other) != 0 {
		t.Fatalf("a call made up: %v %v", other, err)
	}
}
