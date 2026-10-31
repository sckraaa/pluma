//go:build integration

package dao_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	dialog_helper "github.com/teamgram/teamgram-server/app/service/biz/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mqoutbox "github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/readmarks"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

// The write path of msg on MySQL and Redis (ADR 0011, p. 14): the RPC
// handlers and the inbox handlers over one Dao, the other services faked.
// The fake publisher hands inbox events to the inbox handlers in the order
// of each recipient, as one Kafka partition does, and checks that every
// pushed pts is committed.

var (
	itDB    *testinfra.MySQL
	itRedis *testinfra.Redis
	itKafka *testinfra.Kafka
	// itCheck reads what the tests check, outside msg's pools.
	itCheck    *sql.DB
	itDelivery *delivery
	itTopics   = dao.Topics{Inbox: "Inbox-T", Sync: "Sync-T"}
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 3)
	)
	wg.Add(3)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itRedis, errs[1] = testinfra.StartRedis(ctx) }()
	go func() { defer wg.Done(); itKafka, errs[2] = testinfra.StartKafka(ctx) }()
	wg.Wait()

	code := 1
	err := errors.Join(errs...)
	if err == nil {
		err = dao.RegisterTopics(itTopics)
	}
	if err == nil {
		itCheck, err = sql.Open("mysql", itDB.DSN())
	}
	if err == nil {
		itDelivery, err = startDelivery(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		itDelivery.stop()
	}
	_ = itDB.Terminate(ctx)
	_ = itRedis.Terminate(ctx)
	_ = itKafka.Terminate(ctx)
	os.Exit(code)
}

var (
	itUserSeq atomic.Int64
	itChatSeq atomic.Int64
	itIdSeq   atomic.Int64
)

func newUser() int64 { return 20_000_000 + itUserSeq.Add(1) }
func newChat() int64 { return 30_000_000 + itChatSeq.Add(1) }

// boxDSN is the DSN of msg: the test server's with the session lock wait
// timeout box transactions require. extra makes another pool.
func boxDSN(extra string) string {
	return itDB.DSN() + "&innodb_lock_wait_timeout=2" + extra
}

// snowflake issues ids like idgen.
type snowflake struct{}

func (snowflake) NextId(context.Context) (int64, error) { return 1_000_000 + itIdSeq.Add(1), nil }

func (s snowflake) NextIds(ctx context.Context, n int) ([]int64, error) {
	ids := make([]int64, n)
	for i := range ids {
		ids[i], _ = s.NextId(ctx)
	}
	return ids, nil
}

// fakeUsers serves the users msg asks biz for; blocked[{a, b}]: a blocked b;
// premium: users with Premium; premiumOnly: users who let only contacts,
// chats there already and Premium users write; bots: bot accounts;
// missing: users biz leaves out of a look-up, as after a failed load.
type fakeUsers struct {
	user_client.UserClient
	mu          sync.Mutex
	blocked     map[[2]int64]bool
	premium     map[int64]bool
	premiumOnly map[int64]bool
	bots        map[int64]bool
	missing     map[int64]bool
}

// mark gives users their Premium and settings.
func (f *fakeUsers) mark(users []*mtproto.ImmutableUser) []*mtproto.ImmutableUser {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range users {
		u.User.Premium = f.premium[u.Id()]
		u.User.NewNoncontactPeersRequirePremium = f.premiumOnly[u.Id()]
		if f.bots[u.Id()] {
			u.User.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
		}
	}
	return users
}

func immutableUser(id int64) *mtproto.ImmutableUser {
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{
			Id:         id,
			AccessHash: id * 7,
			FirstName:  "U" + strconv.FormatInt(id, 10),
		}).To_UserData(),
	}).To_ImmutableUser()
}

func immutableUsers(ids ...[]int64) []*mtproto.ImmutableUser {
	var (
		users []*mtproto.ImmutableUser
		seen  []int64
	)
	for _, list := range ids {
		for _, id := range list {
			if !slices.Contains(seen, id) {
				seen = append(seen, id)
				users = append(users, immutableUser(id))
			}
		}
	}
	return users
}

func (f *fakeUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	users := f.mark(immutableUsers(in.Id, in.To))
	f.mu.Lock()
	defer f.mu.Unlock()
	return &userpb.Vector_ImmutableUser{Datas: slices.DeleteFunc(users, func(u *mtproto.ImmutableUser) bool { return f.missing[u.Id()] })}, nil
}

func (f *fakeUsers) UserGetMutableUsersV2(_ context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	return mtproto.MakeTLMutableUsers(&mtproto.MutableUsers{Users: f.mark(immutableUsers(in.Id, in.To))}).To_MutableUsers(), nil
}

func (f *fakeUsers) UserBlockedByUser(_ context.Context, in *userpb.TLUserBlockedByUser) (*mtproto.Bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mtproto.ToBool(f.blocked[[2]int64{in.UserId, in.PeerUserId}]), nil
}

// fakeChats serves basic groups: their members; states[{chat, user}]: a
// participant no longer in the chat (kicked or left), normal otherwise.
type fakeChats struct {
	chat_client.ChatClient
	mu      sync.Mutex
	members map[int64][]int64
	states  map[[2]int64]int32
}

func (f *fakeChats) ChatGetMutableChat(_ context.Context, in *chatpb.TLChatGetMutableChat) (*mtproto.MutableChat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chat(in.ChatId)
}

// ChatGetChatListByIdList returns the chats of the list that exist, as
// biz chat does.
func (f *fakeChats) ChatGetChatListByIdList(_ context.Context, in *chatpb.TLChatGetChatListByIdList) (*chatpb.Vector_MutableChat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &chatpb.Vector_MutableChat{}
	for _, id := range in.IdList {
		if c, err := f.chat(id); err == nil {
			out.Datas = append(out.Datas, c)
		}
	}
	return out, nil
}

// chat is the basic group chatId with its members; f.mu is held.
func (f *fakeChats) chat(chatId int64) (*mtproto.MutableChat, error) {
	members, ok := f.members[chatId]
	if !ok {
		return nil, mtproto.ErrChatIdInvalid
	}
	var ps []*mtproto.ImmutableChatParticipant
	for _, u := range members {
		ps = append(ps, mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{
			ChatId: chatId,
			UserId: u,
			State:  f.states[[2]int64{chatId, u}],
		}).To_ImmutableChatParticipant())
	}
	return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{
			Id:                chatId,
			Creator:           members[0],
			Title:             "chat",
			ParticipantsCount: int32(len(members)),
		}).To_ImmutableChat(),
		ChatParticipants: ps,
	}).To_MutableChat(), nil
}

// push is an update pushed to a user's devices.
type push struct {
	notMe   bool // to the devices other than the requesting one
	updates *mtproto.Updates
}

// world is msg over the test MySQL and Redis, with the delivery of
// production: the relay publishes mq_outbox to Kafka, the inbox consumer
// applies the inbox events, and the push reader reads Sync-T as sync does.
type world struct {
	t     *testing.T
	d     *dao.Dao
	rpc   msg.RPCMsgServer
	pub   *worldDelivery
	check *sql.DB
	users *fakeUsers
	chats *fakeChats
}

// newWorld builds msg on the pool of dsn; maxConns limits the pool when > 0.
// The inbox consumer of the package applies the events with this world.
func newWorld(t *testing.T, dsn string, maxConns int) *world {
	t.Helper()
	db := sqlx.NewMySQL(&sqlx.Config{DSN: dsn})
	boxDB, err := userbox.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if maxConns > 0 {
		boxDB.SetMaxOpenConns(maxConns)
	}
	if err = userbox.Check(context.Background(), boxDB); err != nil {
		t.Fatal(err)
	}
	w := &world{
		t:     t,
		check: itCheck,
		users: &fakeUsers{blocked: map[[2]int64]bool{}, premium: map[int64]bool{}, premiumOnly: map[int64]bool{}, bots: map[int64]bool{}, missing: map[int64]bool{}},
		chats: &fakeChats{members: map[int64][]int64{}, states: map[[2]int64]int32{}},
		pub:   &worldDelivery{delivery: itDelivery, t: t},
	}
	w.d = &dao.Dao{
		Mysql:       dao.NewMysqlDao(db, 1),
		CachedConn:  sqlc.NewConn(db, itRedis.CacheConf()),
		IdGenerator: snowflake{},
		UserClient:  w.users,
		ChatClient:  w.chats,
		BoxDB:       boxDB,
		Topics:      itTopics,
	}
	w.rpc = msg_helper.New(w.d, nil)
	itDelivery.use(w.d)
	t.Cleanup(w.pub.drain)
	return w
}

func randomId() int64 { return itIdSeq.Add(1) * 7919 }

func textMessage(from int64, peer *mtproto.Peer, text string) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Out:     true,
		FromId:  mtproto.MakePeerUser(from),
		PeerId:  peer,
		Date:    int32(time.Now().Unix()),
		Message: text,
	}).To_Message()
}

func outbox(m *mtproto.Message) *msg.OutboxMessage {
	return msg.MakeTLOutboxMessage(&msg.OutboxMessage{RandomId: randomId(), Message: m}).To_OutboxMessage()
}

// send sends texts from from to the peer in one request and returns the
// reply.
func (w *world) send(from int64, peerType int32, peerId int64, texts ...string) *mtproto.Updates {
	w.t.Helper()
	peer := mtproto.MakePeer(peerType, peerId)
	var list []*msg.OutboxMessage
	for _, s := range texts {
		list = append(list, outbox(textMessage(from, peer, s)))
	}
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: from, AuthKeyId: from * 10, PeerType: peerType, PeerId: peerId, Message: list,
	})
	if err != nil {
		w.t.Fatalf("send from %d to %d/%d: %v", from, peerType, peerId, err)
	}
	return r
}

// sentIds are the ids of the new messages of a send reply.
func sentIds(r *mtproto.Updates) []int32 {
	var ids []int32
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			ids = append(ids, u.GetMessage_MESSAGE().GetId())
		}
	}
	return ids
}

func (w *world) state(userId int64) userbox.State {
	w.t.Helper()
	st, err := userbox.Read(context.Background(), w.check, userId)
	if err != nil {
		w.t.Fatal(err)
	}
	return st
}

type journalRow struct {
	Id       int64
	Pts      int32
	PtsCount int32
	Type     int32
	Data     string
}

// journal returns the user's journal rows by pts.
func (w *world) journal(userId int64) []journalRow {
	w.t.Helper()
	rows, err := w.check.Query("SELECT id, pts, pts_count, update_type, update_data FROM user_pts_updates WHERE user_id = ? ORDER BY pts", userId)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []journalRow
	for rows.Next() {
		var r journalRow
		if err = rows.Scan(&r.Id, &r.Pts, &r.PtsCount, &r.Type, &r.Data); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (r journalRow) update(t *testing.T) *mtproto.Update {
	t.Helper()
	u := &mtproto.Update{}
	if err := json.Unmarshal([]byte(r.Data), u); err != nil {
		t.Fatal(err)
	}
	return u
}

// checkBox checks the invariants of a box: the journal covers (base, pts]
// once, in the order of its rows' ids, and the box ids issued match the
// messages rows.
func (w *world) checkBox(userId int64) {
	w.t.Helper()
	rep, err := userbox.CheckJournal(context.Background(), w.check, userId, userbox.BasePts)
	if err != nil {
		w.t.Fatal(err)
	}
	if !rep.OK() {
		w.t.Errorf("user %d: journal gaps %v, overlaps %v", userId, rep.Gaps, rep.Overlaps)
	}
	rows := w.journal(userId)
	if !slices.IsSortedFunc(rows, func(a, b journalRow) int { return int(a.Id - b.Id) }) {
		w.t.Errorf("user %d: journal rows are not committed in pts order", userId)
	}
	if n := len(rows); n > 0 && rows[n-1].Pts != rep.State.Pts {
		w.t.Errorf("user %d: state pts %d, last journal row %d", userId, rep.State.Pts, rows[n-1].Pts)
	}
	var maxId int32
	if err = w.check.QueryRow("SELECT COALESCE(MAX(user_message_box_id), 0) FROM messages WHERE user_id = ?", userId).Scan(&maxId); err != nil {
		w.t.Fatal(err)
	}
	if maxId != rep.State.LastMessageBoxId {
		w.t.Errorf("user %d: last box id %d, newest message %d", userId, rep.State.LastMessageBoxId, maxId)
	}
}

type messageRow struct {
	Id              int32
	DialogMessageId int64
	Sender          int64
	Text            string
	Data            string
	Deleted         bool
	Pinned          bool
	Mentioned       bool
	MediaUnread     bool
}

// messages returns the user's messages rows by id.
func (w *world) messages(userId int64) []messageRow {
	w.t.Helper()
	rows, err := w.check.Query("SELECT user_message_box_id, dialog_message_id, sender_user_id, message, message_data, deleted, pinned, mentioned, media_unread FROM messages WHERE user_id = ? ORDER BY user_message_box_id", userId)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []messageRow
	for rows.Next() {
		var r messageRow
		if err = rows.Scan(&r.Id, &r.DialogMessageId, &r.Sender, &r.Text, &r.Data, &r.Deleted, &r.Pinned, &r.Mentioned, &r.MediaUnread); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func alive(rows []messageRow) []messageRow {
	return slices.DeleteFunc(slices.Clone(rows), func(r messageRow) bool { return r.Deleted })
}

type dialogRow struct {
	TopMessage, ReadInboxMaxId, ReadOutboxMaxId, UnreadCount, UnreadMentions, PinnedMsgId int32
}

func (w *world) dialog(userId int64, peerType int32, peerId int64) dialogRow {
	w.t.Helper()
	var r dialogRow
	err := w.check.QueryRow("SELECT top_message, read_inbox_max_id, read_outbox_max_id, unread_count, unread_mentions_count, pinned_msg_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		userId, peerType, peerId).Scan(&r.TopMessage, &r.ReadInboxMaxId, &r.ReadOutboxMaxId, &r.UnreadCount, &r.UnreadMentions, &r.PinnedMsgId)
	if err != nil {
		w.t.Fatalf("dialog %d -> %d/%d: %v", userId, peerType, peerId, err)
	}
	return r
}

// unreadIncoming counts the user's incoming messages of the dialog after
// maxId that are not deleted.
func (w *world) unreadIncoming(userId int64, peerType int32, peerId int64, maxId int32) int32 {
	w.t.Helper()
	did := mtproto.MakeDialogId(userId, peerType, peerId)
	var n int32
	if err := w.check.QueryRow("SELECT COUNT(*) FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND sender_user_id <> ? AND user_message_box_id > ? AND deleted = 0",
		userId, did.A, did.B, userId, maxId).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func journalTypes(rows []journalRow) []int32 {
	out := make([]int32, len(rows))
	for i, r := range rows {
		out[i] = r.Type
	}
	return out
}

// pools runs f on a pool like msg's and on a pool of one connection: on the
// latter a statement through the pool inside a box transaction would wait
// for a connection until the deadline.
func pools(t *testing.T, f func(t *testing.T, w *world)) {
	t.Run("pool", func(t *testing.T) { f(t, newWorld(t, boxDSN(""), 0)) })
	t.Run("one connection", func(t *testing.T) { f(t, newWorld(t, boxDSN("&timeout=31s"), 1)) })
}

// TestWritePath: send, reply, edit, read and delete between two users give
// consistent messages, dialogs and journals on both sides.
func TestWritePath(t *testing.T) {
	pools(t, func(t *testing.T, w *world) {
		ctx := context.Background()
		a, b := newUser(), newUser()

		// A sends to B.
		r := w.send(a, mtproto.PEER_USER, b, "hello")
		ids := sentIds(r)
		if len(ids) != 1 || ids[0] != 1 {
			t.Fatalf("sent ids %v", ids)
		}
		if st := w.state(a); r.GetUpdates()[1].GetPts_INT32() != st.Pts || st.Pts != userbox.BasePts+1 {
			t.Fatalf("reply pts %d, state %+v", r.GetUpdates()[1].GetPts_INT32(), st)
		}
		w.pub.drain()
		bMsgs := w.messages(b)
		if len(bMsgs) != 1 || bMsgs[0].Sender != a || bMsgs[0].Text != "hello" {
			t.Fatalf("B's messages %+v", bMsgs)
		}
		if d := w.dialog(b, mtproto.PEER_USER, a); d.UnreadCount != 1 || d.TopMessage != bMsgs[0].Id {
			t.Errorf("B's dialog %+v", d)
		}

		// B replies to the message.
		reply := textMessage(b, mtproto.MakePeerUser(a), "hi")
		reply.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
			ReplyToMsgId:           bMsgs[0].Id,
			ReplyToMsgId_INT32:     bMsgs[0].Id,
			ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(bMsgs[0].Id),
		}).To_MessageReplyHeader()
		if _, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{
			UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_USER, PeerId: a, Message: []*msg.OutboxMessage{outbox(reply)},
		}); err != nil {
			t.Fatal(err)
		}
		w.pub.drain()
		aMsgs := w.messages(a)
		if len(aMsgs) != 2 {
			t.Fatalf("A's messages %+v", aMsgs)
		}
		var got mtproto.Message
		if err := json.Unmarshal([]byte(aMsgs[1].Data), &got); err != nil {
			t.Fatal(err)
		}
		if got.GetReplyTo().GetReplyToMsgId() != aMsgs[0].Id {
			t.Errorf("reply in A's box points to %d, want A's copy %d", got.GetReplyTo().GetReplyToMsgId(), aMsgs[0].Id)
		}

		// A edits its message.
		edit := textMessage(a, mtproto.MakePeerUser(b), "hello, edited")
		edit.EditDate = mtproto.MakeFlagsInt32(int32(time.Now().Unix()))
		if _, err := w.rpc.MsgEditMessageV2(ctx, &msg.TLMsgEditMessageV2{
			UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: b,
			NewMessage: outbox(edit),
			DstMessage: &mtproto.MessageBox{MessageId: ids[0]},
		}); err != nil {
			t.Fatal(err)
		}
		// Nobody but the author edits a message.
		if _, err := w.rpc.MsgEditMessageV2(ctx, &msg.TLMsgEditMessageV2{
			UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: b,
			NewMessage: outbox(textMessage(a, mtproto.MakePeerUser(b), "not mine")),
			DstMessage: &mtproto.MessageBox{MessageId: aMsgs[1].Id},
		}); !errors.Is(err, mtproto.ErrMessageAuthorRequired) {
			t.Errorf("editing B's message: %v", err)
		}
		w.pub.drain()
		if m := w.messages(b)[0]; m.Text != "hello, edited" {
			t.Errorf("B's copy after the edit: %q", m.Text)
		}

		// B reads the dialog; A learns it.
		if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_USER, PeerId: a}); err != nil {
			t.Fatal(err)
		}
		w.pub.drain()
		if d := w.dialog(b, mtproto.PEER_USER, a); d.UnreadCount != 0 || d.ReadInboxMaxId != d.TopMessage {
			t.Errorf("B's dialog after reading %+v", d)
		}
		if d := w.dialog(a, mtproto.PEER_USER, b); d.ReadOutboxMaxId != ids[0] {
			t.Errorf("A's dialog after B read %+v", d)
		}

		// A deletes its message for both.
		if _, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: a, AuthKeyId: a * 10, Revoke: true, Id: []int32{ids[0]}}); err != nil {
			t.Fatal(err)
		}
		w.pub.drain()
		if left := alive(w.messages(a)); len(left) != 1 || left[0].Text != "hi" {
			t.Errorf("A's messages after the deletion %+v", left)
		}
		if left := alive(w.messages(b)); len(left) != 1 || left[0].Text != "hi" {
			t.Errorf("B's messages after the deletion %+v", left)
		}
		if d := w.dialog(a, mtproto.PEER_USER, b); d.TopMessage != aMsgs[1].Id {
			t.Errorf("A's top message %d, want %d", d.TopMessage, aMsgs[1].Id)
		}

		want := map[int64][]int32{
			a: {mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_EDIT_MESSAGE, mtproto.PTS_UPDATE_READ_HISTORY_OUTBOX, mtproto.PTS_UPDATE_DELETE_MESSAGES},
			b: {mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_EDIT_MESSAGE, mtproto.PTS_UPDATE_READ_HISTORY_INBOX, mtproto.PTS_UPDATE_DELETE_MESSAGES},
		}
		for u, types := range want {
			w.checkBox(u)
			if got := journalTypes(w.journal(u)); !slices.Equal(got, types) {
				t.Errorf("user %d journal %v, want %v", u, got, types)
			}
		}
		// Every update of the journal reached the user's devices.
		for _, u := range []int64{a, b} {
			var pushed []int32
			for _, p := range w.pub.pushesTo(u) {
				for _, up := range p.updates.GetUpdates() {
					if up.GetPts_INT32() > 0 {
						pushed = append(pushed, up.GetPts_INT32())
					}
				}
			}
			slices.Sort(pushed)
			if want := int(w.state(u).Pts - userbox.BasePts); len(pushed) != want {
				t.Errorf("user %d: %d updates pushed, %d journaled", u, len(pushed), want)
			}
		}
	})
}

// TestConcurrentBox: senders write to one user while the user sends and
// reads from two devices: the user's journal has no gaps, rows commit in pts
// order, the unread counters match the messages, and no transaction was
// retried after a deadlock or a lock wait timeout.
func TestConcurrentBox(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	b := newUser()
	senders := []int64{newUser(), newUser(), newUser(), newUser()}
	const perSender = 25

	before := lockRetriesNow(t)
	var wg sync.WaitGroup
	for _, s := range senders {
		wg.Go(func() {
			for i := range perSender {
				w.send(s, mtproto.PEER_USER, b, fmt.Sprintf("m%d", i))
			}
		})
	}
	for device := range 2 {
		wg.Go(func() {
			for i := range 15 {
				w.send(b, mtproto.PEER_USER, senders[(i+device)%len(senders)], "own")
				for _, s := range senders {
					if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: int64(device), PeerType: mtproto.PEER_USER, PeerId: s}); err != nil &&
						!errors.Is(err, mtproto.ErrPeerIdInvalid) { // no dialog yet
						t.Error(err)
					}
				}
			}
		})
	}
	wg.Wait()
	w.pub.drain()
	noLockRetries(t, before)

	w.checkBox(b)
	incoming := 0
	for _, m := range w.messages(b) {
		if m.Sender != b {
			incoming++
		}
	}
	if incoming != len(senders)*perSender {
		t.Errorf("B received %d messages, %d sent", incoming, len(senders)*perSender)
	}
	for _, s := range senders {
		w.checkBox(s)
		d := w.dialog(b, mtproto.PEER_USER, s)
		if actual := w.unreadIncoming(b, mtproto.PEER_USER, s, d.ReadInboxMaxId); d.UnreadCount != actual {
			t.Errorf("dialog with %d: unread_count %d, %d unread messages", s, d.UnreadCount, actual)
		}
	}
	// read_inbox_max_id of each dialog never went down.
	last := map[int64]int32{}
	for _, r := range w.journal(b) {
		if r.Type != mtproto.PTS_UPDATE_READ_HISTORY_INBOX {
			continue
		}
		u := r.update(t)
		peer := u.GetPeer_PEER().GetUserId()
		if u.GetMaxId() < last[peer] {
			t.Errorf("read_inbox_max_id with %d went from %d to %d", peer, last[peer], u.GetMaxId())
		}
		last[peer] = u.GetMaxId()
	}
}

// TestRedisLoss: FLUSHALL between sends does not touch pts and box ids:
// they keep growing and no message is lost.
func TestRedisLoss(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	for i := range 5 {
		w.send(a, mtproto.PEER_USER, b, fmt.Sprintf("before %d", i))
	}
	w.pub.drain()
	code, _, err := itRedis.Container.Exec(context.Background(),
		[]string{"redis-cli", "--no-auth-warning", "-a", itRedis.Password, "FLUSHALL"}, tcexec.Multiplexed())
	if err != nil || code != 0 {
		t.Fatalf("FLUSHALL: exit %d, %v", code, err)
	}
	for i := range 5 {
		w.send(a, mtproto.PEER_USER, b, fmt.Sprintf("after %d", i))
	}
	w.pub.drain()
	for _, u := range []int64{a, b} {
		w.checkBox(u)
		if st := w.state(u); st.Pts != userbox.BasePts+10 || st.LastMessageBoxId != 10 {
			t.Errorf("user %d: state %+v", u, st)
		}
		if n := len(w.messages(u)); n != 10 {
			t.Errorf("user %d: %d messages", u, n)
		}
	}
}

// TestDeleteHistoryPortions: deleting a history of 2500 messages takes three
// calls; each deletes a portion with a journal row of its size, the peer's
// copies all go, and both dialog rows with the last portions.
func TestDeleteHistoryPortions(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = "m"
	}
	for range 50 {
		w.send(a, mtproto.PEER_USER, b, texts...)
	}
	w.pub.drain()

	var offsets []int32
	for {
		r, err := w.rpc.MsgDeleteHistory(ctx, &msg.TLMsgDeleteHistory{UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_USER, PeerId: b, Revoke: true})
		if err != nil {
			t.Fatal(err)
		}
		offsets = append(offsets, r.GetOffset())
		if r.GetOffset() == 0 || len(offsets) > 5 {
			break
		}
	}
	w.pub.drain()
	if len(offsets) != 3 || offsets[0] != 1500 || offsets[1] != 500 || offsets[2] != 0 {
		t.Errorf("offsets %v, want [1500 500 0]", offsets)
	}
	for _, u := range []int64{a, b} {
		w.checkBox(u)
		if left := alive(w.messages(u)); len(left) != 0 {
			t.Errorf("user %d keeps %d messages", u, len(left))
		}
		if peer := a + b - u; w.hasDialog(u, mtproto.PEER_USER, peer) {
			t.Errorf("user %d keeps the dialog with %d", u, peer)
		}
		deleted := 0
		for _, r := range w.journal(u) {
			if r.Type != mtproto.PTS_UPDATE_DELETE_MESSAGES {
				continue
			}
			if n := len(r.update(t).GetMessages()); int32(n) != r.PtsCount || n > 1000 {
				t.Errorf("user %d: updateDeleteMessages of %d messages with pts_count %d", u, n, r.PtsCount)
			}
			deleted += int(r.PtsCount)
		}
		if deleted != 2500 {
			t.Errorf("user %d: %d messages journaled as deleted", u, deleted)
		}
	}
}

// TestBasicGroupAlbum: an album of three to a basic group of three lands
// once in every box, each recipient's in one transaction.
func TestBasicGroupAlbum(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}

	w.send(a, mtproto.PEER_CHAT, chat, "1", "2", "3")
	w.pub.drain()
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
		msgs := w.messages(u)
		if len(msgs) != 3 {
			t.Fatalf("user %d has %d messages", u, len(msgs))
		}
		dids := map[int64]bool{}
		for _, m := range msgs {
			dids[m.DialogMessageId] = true
		}
		if len(dids) != 3 {
			t.Errorf("user %d: dialog_message_ids %v", u, dids)
		}
		rows := w.journal(u)
		if len(rows) != 3 {
			t.Errorf("user %d: %d journal rows", u, len(rows))
		}
	}
	// One transaction per recipient: B's three updates came in one push.
	pushes := w.pub.pushesTo(b)
	if len(pushes) != 1 || len(pushes[0].updates.GetUpdates()) != 3 {
		t.Errorf("B got %d pushes", len(pushes))
	}
	if d := w.dialog(b, mtproto.PEER_CHAT, chat); d.UnreadCount != 3 {
		t.Errorf("B's dialog %+v", d)
	}
}

// TestRecipientOrder: a message sent and at once deleted for all, or sent
// and pinned, reaches each member in that order.
func TestRecipientOrder(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}

	ids := sentIds(w.send(a, mtproto.PEER_CHAT, chat, "oops"))
	if _, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: a, AuthKeyId: a * 10, Revoke: true, Id: ids}); err != nil {
		t.Fatal(err)
	}
	pinned := sentIds(w.send(a, mtproto.PEER_CHAT, chat, "rules"))
	r, err := w.rpc.MsgUpdatePinnedMessage(ctx, &msg.TLMsgUpdatePinnedMessage{UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Id: pinned[0]})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range r.GetUpdates() {
		names = append(names, u.GetPredicateName())
	}
	if !slices.Equal(names, []string{mtproto.Predicate_updatePinnedMessages, mtproto.Predicate_updateMessageID, mtproto.Predicate_updateNewMessage}) {
		t.Errorf("pin reply %v", names)
	} else if p0, p1 := r.GetUpdates()[0].GetPts_INT32(), r.GetUpdates()[2].GetPts_INT32(); p1 != p0+1 {
		t.Errorf("pin pts %d, service message pts %d", p0, p1)
	}
	w.pub.drain()

	for _, u := range []int64{b, c} {
		w.checkBox(u)
		msgs := alive(w.messages(u))
		if len(msgs) != 2 || msgs[0].Text != "rules" || !msgs[0].Pinned {
			t.Fatalf("user %d messages %+v", u, msgs)
		}
		var service mtproto.Message
		if err = json.Unmarshal([]byte(msgs[1].Data), &service); err != nil {
			t.Fatal(err)
		}
		if service.GetAction().GetPredicateName() != mtproto.Predicate_messageActionPinMessage || service.GetReplyTo().GetReplyToMsgId() != msgs[0].Id {
			t.Errorf("user %d: service message %v", u, service.GetAction())
		}
		if d := w.dialog(u, mtproto.PEER_CHAT, chat); d.PinnedMsgId != msgs[0].Id {
			t.Errorf("user %d: pinned_msg_id %d", u, d.PinnedMsgId)
		}
		types := journalTypes(w.journal(u))
		want := []int32{mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_DELETE_MESSAGES, mtproto.PTS_UPDATE_NEW_MESSAGE, mtproto.PTS_UPDATE_PINNED_MESSAGES, mtproto.PTS_UPDATE_NEW_MESSAGE}
		if !slices.Equal(types, want) {
			t.Errorf("user %d journal %v, want %v", u, types, want)
		}
	}
	w.checkBox(a)
}

// TestReadInGroup: reading a basic group tells the senders of the messages
// read, and only them; reading message contents journals the ids it
// changed once.
func TestReadInGroup(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c, d := newUser(), newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c, d}

	w.send(a, mtproto.PEER_CHAT, chat, "from A")
	mention := textMessage(c, mtproto.MakePeerChat(chat), "hey B")
	mention.Entities = []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 4, Length: 1, UserId_INT64: b}).To_MessageEntity()}
	if _, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{UserId: c, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(mention)}}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, PeerType: mtproto.PEER_CHAT, PeerId: chat}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	for u, want := range map[int64]int{a: 1, c: 1, d: 0} {
		n := 0
		for _, r := range w.journal(u) {
			if r.Type == mtproto.PTS_UPDATE_READ_HISTORY_OUTBOX {
				n++
			}
		}
		if n != want {
			t.Errorf("user %d got %d updateReadHistoryOutbox, want %d", u, n, want)
		}
	}

	bMsgs := w.messages(b)
	if !bMsgs[1].Mentioned || w.dialog(b, mtproto.PEER_CHAT, chat).UnreadMentions != 1 {
		t.Fatalf("B's mention %+v", bMsgs[1])
	}
	for i := range 2 {
		r, err := w.rpc.MsgReadMessageContents(ctx, &msg.TLMsgReadMessageContents{UserId: b, Id: []*msg.ContentMessage{{Id: bMsgs[1].Id}}})
		if err != nil {
			t.Fatal(err)
		}
		if want := int32(1 - i); r.GetPtsCount() != want || r.GetPts() != w.state(b).Pts {
			t.Errorf("call %d: affected %d/%d, state %d", i, r.GetPts(), r.GetPtsCount(), w.state(b).Pts)
		}
	}
	if m := w.messages(b)[1]; m.Mentioned || m.MediaUnread || w.dialog(b, mtproto.PEER_CHAT, chat).UnreadMentions != 0 {
		t.Errorf("B's mention after reading %+v", m)
	}
	for _, u := range []int64{a, b, c, d} {
		w.checkBox(u)
	}
}

// TestReadDates: every reader of a group's message leaves its sender the
// date they read it, the first reader and the next ones alike, and so does
// the peer of a private chat; a reader's dates older than
// readmarks.KeepPeriod go with their next read.
func TestReadDates(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}
	chatDialog := mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chat)
	privateDialog := mtproto.MakePeerDialogId(mtproto.PEER_USER, b)

	groupId := sentIds(w.send(a, mtproto.PEER_CHAT, chat, "to the group"))[0]
	privateId := sentIds(w.send(a, mtproto.PEER_USER, b, "to B"))[0]
	w.pub.drain()
	if _, err := w.check.Exec("INSERT INTO message_read_outbox"+
		" (user_id, peer_dialog_id, read_user_id, read_outbox_max_id, read_outbox_max_date) VALUES (?, ?, ?, 0, ?)",
		a, chatDialog, b, time.Now().Unix()-readmarks.KeepPeriod-60); err != nil {
		t.Fatal(err)
	}

	before := time.Now().Unix()
	for _, r := range []struct {
		reader   int64
		peerType int32
		peerId   int64
	}{{b, mtproto.PEER_CHAT, chat}, {c, mtproto.PEER_CHAT, chat}, {b, mtproto.PEER_USER, a}} {
		if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{
			UserId: r.reader, AuthKeyId: r.reader * 10, PeerType: r.peerType, PeerId: r.peerId,
		}); err != nil {
			t.Fatal(err)
		}
		w.pub.drain()
	}
	after := time.Now().Unix()

	type readDate struct {
		dialog, reader int64
		maxId          int32
	}
	rows, err := w.check.Query("SELECT peer_dialog_id, read_user_id, read_outbox_max_id, read_outbox_max_date"+
		" FROM message_read_outbox WHERE user_id = ?", a)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[readDate]bool{}
	for rows.Next() {
		var (
			r    readDate
			date int64
		)
		if err = rows.Scan(&r.dialog, &r.reader, &r.maxId, &date); err != nil {
			t.Fatal(err)
		}
		if date < before || date > after {
			t.Errorf("%+v read at %d, not within [%d, %d]", r, date, before, after)
		}
		got[r] = true
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[readDate]bool{
		{chatDialog, b, groupId}:      true,
		{chatDialog, c, groupId}:      true,
		{privateDialog, b, privateId}: true,
	}
	if !maps.Equal(got, want) {
		t.Errorf("A's read dates %v, want %v", got, want)
	}
	if d := w.dialog(a, mtproto.PEER_CHAT, chat); d.ReadOutboxMaxId != groupId {
		t.Errorf("A's group read_outbox_max_id %d, want %d", d.ReadOutboxMaxId, groupId)
	}
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
	}
}

// TestReadMentions: reading all mentions of a basic group goes in portions
// of 1000, each journaled as one updateReadMessagesContents, and leaves no
// unread mention and the counter at 0. The pin of the user's own message
// mentions them without counting as unread, and a counter that went astray
// is corrected.
func TestReadMentions(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}

	w.send(b, mtproto.PEER_CHAT, chat, "mine")
	w.pub.drain()
	mine := alive(w.messages(a))[0]
	if _, err := w.rpc.MsgUpdatePinnedMessage(ctx, &msg.TLMsgUpdatePinnedMessage{UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Id: mine.Id}); err != nil {
		t.Fatal(err)
	}
	peer := mtproto.MakePeerChat(chat)
	for range 21 {
		list := make([]*msg.OutboxMessage, 50)
		for i := range list {
			m := textMessage(c, peer, "hey B")
			m.Entities = []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 4, Length: 1, UserId_INT64: b}).To_MessageEntity()}
			list[i] = outbox(m)
		}
		if _, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{UserId: c, AuthKeyId: c * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: list}); err != nil {
			t.Fatal(err)
		}
	}
	w.pub.drain()

	pin := alive(w.messages(b))[1]
	if !pin.Mentioned || pin.MediaUnread {
		t.Errorf("B's copy of the pin %+v", pin)
	}
	if n := w.dialog(b, mtproto.PEER_CHAT, chat).UnreadMentions; n != 1050 {
		t.Errorf("B has %d unread mentions, want 1050", n)
	}

	var offsets, counts []int32
	for {
		r, err := w.rpc.MsgReadMentions(ctx, &msg.TLMsgReadMentions{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat})
		if err != nil {
			t.Fatal(err)
		}
		if r.GetPts() != w.state(b).Pts {
			t.Errorf("call %d: pts %d, state %d", len(offsets), r.GetPts(), w.state(b).Pts)
		}
		offsets, counts = append(offsets, r.GetOffset()), append(counts, r.GetPtsCount())
		if r.GetOffset() == 0 || len(offsets) > 5 {
			break
		}
	}
	if !slices.Equal(offsets, []int32{50, 0}) || !slices.Equal(counts, []int32{1000, 50}) {
		t.Errorf("offsets %v, pts counts %v; want [50 0], [1000 50]", offsets, counts)
	}
	for _, m := range alive(w.messages(b)) {
		if m.Sender == c && (m.Mentioned || m.MediaUnread) {
			t.Fatalf("B's mention after reading %+v", m)
		}
	}
	if n := w.dialog(b, mtproto.PEER_CHAT, chat).UnreadMentions; n != 0 {
		t.Errorf("B has %d unread mentions after reading", n)
	}
	read := 0
	for _, r := range w.journal(b) {
		if r.Type != mtproto.PTS_UPDATE_READ_MESSAGE_CONENTS {
			continue
		}
		if n := len(r.update(t).GetMessages()); int32(n) != r.PtsCount || n > 1000 {
			t.Errorf("updateReadMessagesContents of %d messages with pts_count %d", n, r.PtsCount)
		}
		read += int(r.PtsCount)
	}
	if read != 1050 {
		t.Errorf("%d messages journaled as read", read)
	}

	// Nothing left to read; a counter that went astray goes back to 0.
	if _, err := w.check.Exec("UPDATE dialogs SET unread_mentions_count = 7 WHERE user_id = ? AND peer_type = ? AND peer_id = ?", b, mtproto.PEER_CHAT, chat); err != nil {
		t.Fatal(err)
	}
	r, err := w.rpc.MsgReadMentions(ctx, &msg.TLMsgReadMentions{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 0 || r.GetOffset() != 0 || r.GetPts() != w.state(b).Pts {
		t.Errorf("read with nothing left: %d/%d/%d", r.GetPts(), r.GetPtsCount(), r.GetOffset())
	}
	if n := w.dialog(b, mtproto.PEER_CHAT, chat).UnreadMentions; n != 0 {
		t.Errorf("the counter that went astray is %d", n)
	}
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
	}
}

// TestFirstMessageInvalidatesDialogLists: a dialog created by a message
// drops the cached dialog lists biz dialog serves.
func TestFirstMessageInvalidatesDialogLists(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	keys := []string{dialog.GetNotPinnedDialogIdListCacheKey(b), dialog.GetCacheKeyByPeerType(b, mtproto.PEER_USER)}
	for _, k := range keys {
		if err := w.d.CachedConn.SetCache(ctx, k, []int64{}); err != nil {
			t.Fatal(err)
		}
	}
	w.send(a, mtproto.PEER_USER, b, "first")
	w.pub.drain()
	for _, k := range keys {
		var v []int64
		if err := w.d.CachedConn.GetCache(ctx, k, &v); err == nil {
			t.Errorf("cache key %s survived the new dialog: %v", k, v)
		}
	}
}

// TestInboxEvents: an event to its own initiator is dead at once, applied
// to nobody; an event whose recipient is locked longer than a box
// transaction may wait is applied once the lock goes.
func TestInboxEvents(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()

	event := func(to, from int64) userbox.Event {
		data, err := json.Marshal(&inbox.TLInboxSendUserMessageToInboxV2{
			UserId: to, FromId: from, PeerType: mtproto.PEER_USER, PeerId: to,
			BoxList: []*mtproto.MessageBox{{
				UserId: from, SenderUserId: from, PeerType: mtproto.PEER_USER, PeerId: to,
				DialogMessageId: 1_000_000 + itIdSeq.Add(1),
				Message:         textMessage(from, mtproto.MakePeerUser(to), "direct"),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return userbox.Event{Topic: "Inbox-T", Recipient: to, Method: "inbox.TL_inbox_sendUserMessageToInboxV2", Data: data}
	}

	self, err := mqoutbox.Insert(ctx, w.d.BoxDB, event(a, a))
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, self, userbox.EventDead)
	if st := w.state(a); st.Pts != userbox.BasePts {
		t.Errorf("event to its initiator applied: %+v", st)
	}
	discard(t, self)

	// Hold B's box past the budget of a box transaction.
	lock, err := w.check.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec("INSERT INTO user_state (user_id) VALUES (?)", b); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	time.AfterFunc(userbox.Budget+2*time.Second, func() {
		_ = lock.Commit()
		close(released)
	})
	start := time.Now()
	if _, err = mqoutbox.Insert(ctx, w.d.BoxDB, event(b, a)); err != nil {
		t.Fatal(err)
	}
	<-released
	w.pub.drain()
	if n := len(w.messages(b)); n != 1 {
		t.Errorf("after %v, B has %d messages", time.Since(start).Round(time.Second), n)
	}
	w.checkBox(b)
}

// TestPinnedDialogsWithDeletes: biz dialog reorders the pinned dialogs of a
// user while msg deletes messages of several of its dialogs: both lock the
// user's box first, so no transaction meets a deadlock or a lock wait
// timeout, not even one that a retry would hide.
func TestPinnedDialogsWithDeletes(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	dialogs := dialog_helper.New(dialog_helper.Config{
		Mysql:  sqlx.Config{DSN: boxDSN("")},
		Cache:  itRedis.CacheConf(),
		Topics: dialog_helper.Topics{Sync: "Sync-T"},
	})
	u := newUser()
	peers := []int64{newUser(), newUser(), newUser(), newUser()}
	const rounds = 100
	var ids [][]int32
	for range rounds {
		var round []int32
		for _, p := range peers[:3] {
			round = append(round, sentIds(w.send(u, mtproto.PEER_USER, p, "x"))...)
		}
		ids = append(ids, round)
	}
	w.send(u, mtproto.PEER_USER, peers[3], "x")

	before := lockRetriesNow(t)
	var (
		wg   sync.WaitGroup
		errs atomic.Int64
	)
	wg.Go(func() {
		for i := range rounds {
			order := []int64{peers[i%4], peers[(i+1)%4], peers[(i+2)%4]}
			if _, err := dialogs.DialogReorderPinnedDialogs(ctx, &dialog.TLDialogReorderPinnedDialogs{
				UserId: u, Force: mtproto.BoolTrue, IdList: order,
			}); err != nil {
				errs.Add(1)
				t.Errorf("reorder: %v", err)
			}
		}
	})
	wg.Go(func() {
		for _, round := range ids {
			if _, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: u, AuthKeyId: u * 10, Id: round}); err != nil {
				errs.Add(1)
				t.Errorf("delete: %v", err)
			}
		}
	})
	wg.Wait()
	w.pub.drain()
	if errs.Load() != 0 {
		t.Fatalf("%d operations failed", errs.Load())
	}
	noLockRetries(t, before)
	w.checkBox(u)
	var pinned int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND pinned > 0", u).Scan(&pinned); err != nil {
		t.Fatal(err)
	}
	if pinned != 3 {
		t.Errorf("%d pinned dialogs, want 3", pinned)
	}
	for _, p := range peers[:3] {
		if d := w.dialog(u, mtproto.PEER_USER, p); d.TopMessage != 0 {
			t.Errorf("dialog with %d keeps top message %d after all its messages went", p, d.TopMessage)
		}
	}
}
