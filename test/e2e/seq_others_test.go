//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Updates with seq from another user's change (task 010, ADR 0021): a
// rename, a change of a basic group, a request to join reach every device
// of the accounts they concern in a container with the next seq of each
// account, and a device that was offline finds them in
// updates.getDifference, folded to the last state, with the users and
// chats they name. Another user's change reaches an account through an
// inbox event applied asynchronously, so an offline device polls its
// difference until its seq has grown.

// pushedUpdates returns the updates of a push.
func pushedUpdates(us *mtproto.Updates) []*mtproto.Update {
	if us.GetPredicateName() == mtproto.Predicate_updateShort {
		return []*mtproto.Update{us.GetUpdate()}
	}
	return us.GetUpdates()
}

// seqPush waits on u for the push of an update of predicate for which
// match holds, and checks that it came alone in a container with seq: an
// update with seq is pushed in its own container, whose seq is the one
// before it plus one.
func seqPush(ctx context.Context, t *testing.T, u *user, name string, seq int32, predicate string, match func(*mtproto.Update) bool) *mtproto.Updates {
	t.Helper()
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	up, err := u.c.WaitUpdate(wctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			if x.GetPredicateName() == predicate && match(x) {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no push of %s: %v", name, predicate, err)
	}
	if up.GetPredicateName() != mtproto.Predicate_updates || len(up.GetUpdates()) != 1 {
		t.Errorf("%s: %s pushed in %s with %d updates, want updates with one", name, predicate, up.GetPredicateName(), len(pushedUpdates(up)))
	}
	if up.GetSeq() != seq {
		t.Errorf("%s: %s pushed with seq %d, want %d", name, predicate, up.GetSeq(), seq)
	}
	return up
}

// noPushOf checks that u gets no push of an update of predicate for which
// match holds within wait.
func noPushOf(ctx context.Context, t *testing.T, u *user, name string, wait time.Duration, predicate string, match func(*mtproto.Update) bool) {
	t.Helper()
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	up, err := u.c.WaitUpdate(wctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			if x.GetPredicateName() == predicate && match(x) {
				return true
			}
		}
		return false
	})
	if err == nil {
		t.Errorf("%s: one more push of %s, in %s with seq %d", name, predicate, up.GetPredicateName(), up.GetSeq())
	}
}

// differenceUntil polls u's difference from st until done holds for it or
// 30 seconds pass, and returns the last one.
func differenceUntil(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState, done func(*mtproto.Updates_Difference) bool) *mtproto.Updates_Difference {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		d := differenceOnce(ctx, t, u, st)
		if done(d) || time.Now().After(deadline) {
			return d
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// seqReached tells a difference whose state has the seq want or more.
func seqReached(want int32) func(*mtproto.Updates_Difference) bool {
	return func(d *mtproto.Updates_Difference) bool { return d.GetState().GetSeq() >= want }
}

func userIn(users []*mtproto.User, id int64) *mtproto.User {
	for _, u := range users {
		if u.GetId() == id {
			return u
		}
	}
	return nil
}

func chatIn(chats []*mtproto.Chat, id int64) *mtproto.Chat {
	for _, c := range chats {
		if c.GetId() == id {
			return c
		}
	}
	return nil
}

// waitText waits for the push of a new message with text on u.
func waitText(ctx context.Context, t *testing.T, u *user, text string) {
	t.Helper()
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, m := range newMessages(us) {
			if m.GetMessage() == text {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no message %q: %v", text, err)
	}
}

// waitAction waits for the push of a service message with action in the
// basic group chatId on u.
func waitAction(ctx context.Context, t *testing.T, u *user, chatId int64, action string) {
	t.Helper()
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		return actionIn(newMessages(us), chatId, action) != nil
	}); err != nil {
		t.Fatalf("no %s in chat %d: %v", action, chatId, err)
	}
}

// actionIn returns the first service message of msgs in the basic group
// chatId with action, nil if there is none.
func actionIn(msgs []*mtproto.Message, chatId int64, action string) *mtproto.Message {
	for _, m := range msgs {
		if m.GetPeerId().GetChatId() == chatId && m.GetAction().GetPredicateName() == action {
			return m
		}
	}
	return nil
}

// inputUserOf returns to as from finds it by phone.
func inputUserOf(ctx context.Context, t *testing.T, from, to *user) *mtproto.InputUser {
	t.Helper()
	p := peerOf(ctx, t, from, to)
	return &mtproto.InputUser{
		Constructor: from.c.Constructor(mtproto.Predicate_inputUser),
		UserId:      p.GetUserId(),
		AccessHash:  p.GetAccessHash(),
	}
}

func chatPeer(u *user, chatId int64) *mtproto.InputPeer {
	return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: chatId}
}

// createChat creates a basic group of creator with members and returns
// its id.
func createChat(ctx context.Context, t *testing.T, creator *user, title string, members ...*user) int64 {
	t.Helper()
	var users []*mtproto.InputUser
	for _, m := range members {
		users = append(users, inputUserOf(ctx, t, creator, m))
	}
	o, err := creator.c.Invoke(ctx, &mtproto.TLMessagesCreateChat92CEDDD4{
		Constructor: creator.c.Constructor(mtproto.Predicate_messages_createChat92CEDDD4),
		Users:       users,
		Title:       title,
	})
	if err != nil {
		t.Fatalf("messages.createChat: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_InvitedUsers() *mtproto.Messages_InvitedUsers
	})
	if !ok {
		t.Fatalf("messages.createChat returned %T", o)
	}
	if missing := r.To_Messages_InvitedUsers().GetMissingInvitees(); len(missing) != 0 {
		t.Fatalf("messages.createChat: missing invitees %v", missing)
	}
	for _, c := range r.To_Messages_InvitedUsers().GetUpdates().GetChats() {
		if c.GetTitle() == title {
			return c.GetId()
		}
	}
	t.Fatal("messages.createChat reply without the chat")
	return 0
}

func addChatUser(ctx context.Context, t *testing.T, u *user, chatId int64, who *mtproto.InputUser) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesAddChatUserCBC6D107{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_addChatUserCBC6D107),
		ChatId:      chatId,
		UserId:      who,
		FwdLimit:    100,
	}); err != nil {
		t.Fatalf("messages.addChatUser: %v", err)
	}
}

func editChatAdmin(ctx context.Context, t *testing.T, u *user, chatId int64, who *mtproto.InputUser, admin bool) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditChatAdmin{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_editChatAdmin),
		ChatId:      chatId,
		UserId:      who,
		IsAdmin:     mtproto.ToBool(admin),
	})
	if err != nil {
		t.Fatalf("messages.editChatAdmin: %v", err)
	}
	if _, ok := o.(*mtproto.TLBoolTrue); !ok {
		t.Fatalf("messages.editChatAdmin returned %T", o)
	}
}

func deleteChatUser(ctx context.Context, t *testing.T, u *user, chatId int64, who *mtproto.InputUser) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesDeleteChatUser{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_deleteChatUser),
		ChatId:      chatId,
		UserId:      who,
	}); err != nil {
		t.Fatalf("messages.deleteChatUser: %v", err)
	}
}

// chatOf returns the basic group chatId as u sees it (messages.getChats).
func chatOf(ctx context.Context, t *testing.T, u *user, chatId int64) *mtproto.Chat {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetChats{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getChats),
		Id:          []int64{chatId},
	})
	if err != nil {
		t.Fatalf("messages.getChats: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	})
	if !ok {
		t.Fatalf("messages.getChats returned %T", o)
	}
	c := chatIn(r.To_Messages_Chats().GetChats(), chatId)
	if c == nil {
		t.Fatalf("messages.getChats without chat %d", chatId)
	}
	return c
}

// peerDialogs returns u's messages.getPeerDialogs of peer.
func peerDialogs(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) *mtproto.Messages_PeerDialogs {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetPeerDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getPeerDialogs),
		Peers: []*mtproto.InputDialogPeer{{
			Constructor: u.c.Constructor(mtproto.Predicate_inputDialogPeer),
			Peer:        peer,
		}},
	})
	if err != nil {
		t.Fatalf("messages.getPeerDialogs: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_PeerDialogs() *mtproto.Messages_PeerDialogs
	})
	if !ok {
		t.Fatalf("messages.getPeerDialogs returned %T", o)
	}
	return r.To_Messages_PeerDialogs()
}

// hasDialog tells whether u's dialog list (messages.getDialogs) has the
// dialog of peer. messages.getPeerDialogs answers a peer without a dialog
// too, with an empty one.
func hasDialog(ctx context.Context, t *testing.T, u *user, peer *mtproto.Peer) bool {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getDialogs: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Dialogs() *mtproto.Messages_Dialogs
	})
	if !ok {
		t.Fatalf("messages.getDialogs returned %T", o)
	}
	for _, d := range r.To_Messages_Dialogs().GetDialogs() {
		p := d.GetPeer()
		if p.GetPredicateName() == peer.GetPredicateName() && p.GetUserId() == peer.GetUserId() && p.GetChatId() == peer.GetChatId() {
			return true
		}
	}
	return false
}

// deleteDialog deletes u's history with peer and the dialog (no
// just_clear), in as many calls as it takes.
func deleteDialog(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) {
	t.Helper()
	for range 10 {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesDeleteHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_deleteHistory),
			Peer:        peer,
		})
		if err != nil {
			t.Fatalf("messages.deleteHistory: %v", err)
		}
		h, ok := o.(interface {
			To_Messages_AffectedHistory() *mtproto.Messages_AffectedHistory
		})
		if !ok {
			t.Fatalf("messages.deleteHistory returned %T", o)
		}
		if h.To_Messages_AffectedHistory().GetOffset() == 0 {
			return
		}
	}
	t.Fatal("messages.deleteHistory: too many portions")
}

// participantsOf returns the updateChatParticipants of the basic group
// chatId in d.
func participantsOf(d *mtproto.Updates_Difference, chatId int64) []*mtproto.Update {
	var out []*mtproto.Update
	for _, u := range updatesOfType(d, mtproto.Predicate_updateChatParticipants) {
		if u.GetParticipants_CHATPARTICIPANTS().GetChatId() == chatId {
			out = append(out, u)
		}
	}
	return out
}

// members returns the participants of ps by user id, each with its
// predicate (chatParticipantCreator, chatParticipantAdmin,
// chatParticipant).
func members(ps *mtproto.ChatParticipants) map[int64]string {
	m := map[int64]string{}
	for _, p := range ps.GetParticipants() {
		m[p.GetUserId()] = p.GetPredicateName()
	}
	return m
}

// isParticipants tells an updateChatParticipants of chatId with the whole
// list (forbidden false) or the forbidden form (forbidden true).
func isParticipants(chatId int64, forbidden bool) func(*mtproto.Update) bool {
	want := mtproto.Predicate_chatParticipants
	if forbidden {
		want = mtproto.Predicate_chatParticipantsForbidden
	}
	return func(u *mtproto.Update) bool {
		ps := u.GetParticipants_CHATPARTICIPANTS()
		return ps.GetChatId() == chatId && ps.GetPredicateName() == want
	}
}

// checkMembers checks the whole list of the members of an
// updateChatParticipants: who is in it, in which role, and its version.
func checkMembers(t *testing.T, name string, u *mtproto.Update, want map[int64]string, version int32) {
	t.Helper()
	ps := u.GetParticipants_CHATPARTICIPANTS()
	if ps.GetPredicateName() != mtproto.Predicate_chatParticipants {
		t.Errorf("%s: %s, want the whole list", name, ps.GetPredicateName())
		return
	}
	if got := members(ps); !maps.Equal(got, want) {
		t.Errorf("%s: members %v, want %v", name, got, want)
	}
	if ps.GetVersion() != version {
		t.Errorf("%s: version %d, want %d", name, ps.GetVersion(), version)
	}
}

// TestSeqOthersNames: Bob has a private dialog with Alice; Alice renames
// herself twice and takes a username. Bob's phone gets each change pushed
// with the next seq and Alice as Bob sees her; his laptop, offline, finds
// one updateUserName with the last names in its difference.
func TestSeqOthersNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bobPhone := login(ctx, t, phones[1], "Bob")
	laptop := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bobPhone)

	// The dialog makes Bob one who sees Alice; it is in his box once the
	// message arrived.
	text := fmt.Sprintf("names %d", randomID())
	sendTo(ctx, t, alice, peerOf(ctx, t, alice, bobPhone), text)
	waitText(ctx, t, bobPhone, text)
	stPhone := getState(ctx, t, bobPhone)
	stLaptop := getState(ctx, t, laptop)

	tag := uint32(randomID())
	first1, first2 := fmt.Sprintf("Alice%d", tag), fmt.Sprintf("Alicia%d", tag)
	username := fmt.Sprintf("alice%d", tag)
	for _, first := range []string{first1, first2} {
		if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateProfile{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateProfile),
			FirstName:   wrapperspb.String(first),
		}); err != nil {
			t.Fatalf("account.updateProfile: %v", err)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateUsername),
		Username:    username,
	}); err != nil {
		t.Fatalf("account.updateUsername: %v", err)
	}

	usernameOf := func(u *mtproto.Update) string {
		if len(u.GetUsernames()) == 0 {
			return ""
		}
		return u.GetUsernames()[0].GetUsername()
	}
	steps := []struct{ first, username string }{{first1, ""}, {first2, ""}, {first2, username}}
	var last *mtproto.Updates
	for i, s := range steps {
		last = seqPush(ctx, t, bobPhone, "Bob's phone", stPhone.GetSeq()+int32(i)+1, mtproto.Predicate_updateUserName, func(u *mtproto.Update) bool {
			return u.GetUserId() == alice.self.GetId() && u.GetFirstName() == s.first && usernameOf(u) == s.username
		})
		// The users are as Bob sees them when his box applies the change:
		// a later change may be in them already.
		if a := userIn(last.GetUsers(), alice.self.GetId()); a == nil || a.GetAccessHash() == nil {
			t.Errorf("push %d: users %v, want Alice with an access hash", i+1, last.GetUsers())
		}
	}
	if a := userIn(last.GetUsers(), alice.self.GetId()); a.GetFirstName().GetValue() != first2 || a.GetUsername().GetValue() != username {
		t.Errorf("Alice with the last push: %q @%q, want %q @%q", a.GetFirstName().GetValue(), a.GetUsername().GetValue(), first2, username)
	}

	// The laptop missed the pushes: the difference has the last state.
	d := differenceOnce(ctx, t, laptop, stLaptop)
	var names []*mtproto.Update
	for _, u := range updatesOfType(d, mtproto.Predicate_updateUserName) {
		if u.GetUserId() == alice.self.GetId() {
			names = append(names, u)
		}
	}
	if len(names) != 1 || names[0].GetFirstName() != first2 || usernameOf(names[0]) != username {
		t.Errorf("updateUserName of Alice in the difference %v, want one with %q @%q", names, first2, username)
	}
	if a := userIn(d.GetUsers(), alice.self.GetId()); a == nil || a.GetFirstName().GetValue() != first2 || a.GetUsername().GetValue() != username {
		t.Errorf("Alice in the users of the difference: %v, want %q @%q", a, first2, username)
	}
	if got, want := d.GetState().GetSeq(), stLaptop.GetSeq()+3; got != want {
		t.Errorf("state seq %d, want %d", got, want)
	}
}

// TestSeqOthersChatParticipants: Alice creates a basic group with Bob and
// Carol, adds Dave, makes Bob an admin and removes Dave. Carol, offline,
// finds the members as they are now in one updateChatParticipants with the
// chat's version; Dave finds the forbidden form, keeps the dialog with the
// chat forbidden, and has the service message of his removal.
func TestSeqOthersChatParticipants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")

	chatId := createChat(ctx, t, alice, fmt.Sprintf("participants %d", randomID()), bob, carol)
	v0 := chatOf(ctx, t, alice, chatId).GetVersion()
	stCarol := getState(ctx, t, carol)
	stDave := getState(ctx, t, dave)

	daveIn := inputUserOf(ctx, t, alice, dave)
	addChatUser(ctx, t, alice, chatId, daveIn)
	editChatAdmin(ctx, t, alice, chatId, inputUserOf(ctx, t, alice, bob), true)
	deleteChatUser(ctx, t, alice, chatId, daveIn)
	version := v0 + 3
	if v := chatOf(ctx, t, alice, chatId).GetVersion(); v != version {
		t.Errorf("chat version %d after three changes of the members from %d", v, v0)
	}

	// Carol: three changes, folded into the last list.
	d := differenceUntil(ctx, t, carol, stCarol, seqReached(stCarol.GetSeq()+3))
	if got, want := d.GetState().GetSeq(), stCarol.GetSeq()+3; got != want {
		t.Fatalf("Carol: state seq %d, want %d", got, want)
	}
	ps := participantsOf(d, chatId)
	if len(ps) != 1 {
		t.Fatalf("Carol: %d updateChatParticipants of the chat, want 1", len(ps))
	}
	checkMembers(t, "Carol", ps[0], map[int64]string{
		alice.self.GetId(): mtproto.Predicate_chatParticipantCreator,
		bob.self.GetId():   mtproto.Predicate_chatParticipantAdmin,
		carol.self.GetId(): mtproto.Predicate_chatParticipant,
	}, version)
	if c := chatIn(d.GetChats(), chatId); c == nil || c.GetPredicateName() != mtproto.Predicate_chat || c.GetVersion() != version {
		t.Errorf("Carol: chat in the difference %v, want the chat of version %d", c, version)
	}
	for _, u := range []*user{alice, bob, carol} {
		if userIn(d.GetUsers(), u.self.GetId()) == nil {
			t.Errorf("Carol: user %d of the members is not in the difference", u.self.GetId())
		}
	}

	// Dave: added and removed while offline.
	d = differenceUntil(ctx, t, dave, stDave, func(d *mtproto.Updates_Difference) bool {
		return d.GetState().GetSeq() >= stDave.GetSeq()+3 &&
			actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatDeleteUser) != nil
	})
	if got, want := d.GetState().GetSeq(), stDave.GetSeq()+3; got != want {
		t.Errorf("Dave: state seq %d, want %d", got, want)
	}
	ps = participantsOf(d, chatId)
	if len(ps) != 1 || !isParticipants(chatId, true)(ps[0]) {
		t.Errorf("Dave: updateChatParticipants of the chat %v, want one chatParticipantsForbidden", ps)
	}
	if c := chatIn(d.GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chatForbidden {
		t.Errorf("Dave: chat in the difference %v, want chatForbidden", c)
	}
	if m := actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatAddUser); m == nil || len(m.GetAction().GetUsers()) != 1 || m.GetAction().GetUsers()[0] != dave.self.GetId() {
		t.Errorf("Dave: the message of his addition %v", m)
	}
	if m := actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatDeleteUser); m.GetAction().GetUserId() != dave.self.GetId() ||
		m.GetFromId().GetUserId() != alice.self.GetId() || m.GetOut() {
		t.Errorf("Dave: the message of his removal %v, want one from Alice about Dave", m)
	}
	if !hasDialog(ctx, t, dave, mtproto.MakePeerChat(chatId)) {
		t.Errorf("Dave: no dialog of the chat he was removed from")
	}
	if c := chatIn(peerDialogs(ctx, t, dave, chatPeer(dave, chatId)).GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chatForbidden {
		t.Errorf("Dave: chat of the dialog %v, want chatForbidden", c)
	}
}

// TestSeqOthersChatLeave: Bob leaves a basic group from his phone. His
// laptop gets the forbidden form with the next seq and the chat it left,
// Alice and Carol the members without him; the dialog stays until Bob
// deletes it with messages.deleteHistory.
func TestSeqOthersChatLeave(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bobPhone := login(ctx, t, phones[1], "Bob")
	laptop := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, bobPhone)

	chatId := createChat(ctx, t, alice, fmt.Sprintf("leave %d", randomID()), bobPhone, carol)
	// The dialog is in Bob's box once the message of the creation arrived.
	waitAction(ctx, t, bobPhone, chatId, mtproto.Predicate_messageActionChatCreate)
	v0 := chatOf(ctx, t, alice, chatId).GetVersion()
	stAlice := getState(ctx, t, alice)
	stCarol := getState(ctx, t, carol)
	stLaptop := getState(ctx, t, laptop)

	deleteChatUser(ctx, t, bobPhone, chatId, inputSelf(bobPhone))

	up := seqPush(ctx, t, laptop, "Bob's laptop", stLaptop.GetSeq()+1, mtproto.Predicate_updateChatParticipants, isParticipants(chatId, true))
	if c := chatIn(up.GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chat || !c.GetLeft() {
		t.Errorf("Bob's laptop: chat with the push %v, want the chat left", c)
	}
	want := map[int64]string{
		alice.self.GetId(): mtproto.Predicate_chatParticipantCreator,
		carol.self.GetId(): mtproto.Predicate_chatParticipant,
	}
	for _, m := range []struct {
		name string
		u    *user
		st   *mtproto.TLUpdatesState
	}{{"Alice", alice, stAlice}, {"Carol", carol, stCarol}} {
		up = seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+1, mtproto.Predicate_updateChatParticipants, isParticipants(chatId, false))
		checkMembers(t, m.name, up.GetUpdates()[0], want, v0+1)
		if c := chatIn(up.GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chat || c.GetVersion() != v0+1 {
			t.Errorf("%s: chat with the push %v, want the chat of version %d", m.name, c, v0+1)
		}
	}

	if !hasDialog(ctx, t, bobPhone, mtproto.MakePeerChat(chatId)) {
		t.Fatalf("Bob: no dialog of the chat he left")
	}
	if c := chatIn(peerDialogs(ctx, t, bobPhone, chatPeer(bobPhone, chatId)).GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chat || !c.GetLeft() {
		t.Errorf("Bob: chat of the dialog %v, want the chat left", c)
	}
	deleteDialog(ctx, t, bobPhone, chatPeer(bobPhone, chatId))
	if hasDialog(ctx, t, bobPhone, mtproto.MakePeerChat(chatId)) {
		t.Errorf("Bob: the dialog of the chat is left after messages.deleteHistory")
	}
}

// TestSeqOthersChatRights: Alice changes the default rights of a basic
// group and turns on the protection of its content. The members get
// updateChatDefaultBannedRights and updateChat with seq, not updateShort;
// Carol, offline, finds both in her difference with the chat as it is now.
func TestSeqOthersChatRights(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	chatId := createChat(ctx, t, alice, fmt.Sprintf("rights %d", randomID()), bob, carol)
	v0 := chatOf(ctx, t, alice, chatId).GetVersion()
	stAlice := getState(ctx, t, alice)
	stBob := getState(ctx, t, bob)
	stCarol := getState(ctx, t, carol)

	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditChatDefaultBannedRights{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editChatDefaultBannedRights),
		Peer:        chatPeer(alice, chatId),
		BannedRights: &mtproto.ChatBannedRights{
			Constructor: alice.c.Constructor(mtproto.Predicate_chatBannedRights),
			ChangeInfo:  true,
			PinMessages: true,
		},
	}); err != nil {
		t.Fatalf("messages.editChatDefaultBannedRights: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesToggleNoForwards{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_toggleNoForwards),
		Peer:        chatPeer(alice, chatId),
		Enabled:     mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("messages.toggleNoForwards: %v", err)
	}

	isRights := func(u *mtproto.Update) bool { return u.GetPeer_PEER().GetChatId() == chatId }
	isChat := func(u *mtproto.Update) bool { return u.GetChatId_INT64() == chatId }
	checkRights := func(name string, u *mtproto.Update) {
		t.Helper()
		r := u.GetDefaultBannedRights()
		if !r.GetChangeInfo() || !r.GetPinMessages() || r.GetSendMessages() || u.GetVersion() != v0+1 {
			t.Errorf("%s: updateChatDefaultBannedRights %v of version %d, want change_info and pin_messages of version %d", name, r, u.GetVersion(), v0+1)
		}
	}
	for _, m := range []struct {
		name string
		u    *user
		st   *mtproto.TLUpdatesState
	}{{"Alice", alice, stAlice}, {"Bob", bob, stBob}} {
		up := seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+1, mtproto.Predicate_updateChatDefaultBannedRights, isRights)
		checkRights(m.name, up.GetUpdates()[0])
		up = seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+2, mtproto.Predicate_updateChat, isChat)
		if c := chatIn(up.GetChats(), chatId); !c.GetNoforwards() || c.GetVersion() != v0+2 {
			t.Errorf("%s: chat with updateChat %v, want noforwards of version %d", m.name, c, v0+2)
		}
	}
	// Neither came once more, as updateShort or otherwise.
	noPushOf(ctx, t, bob, "Bob", time.Second, mtproto.Predicate_updateChatDefaultBannedRights, isRights)
	noPushOf(ctx, t, bob, "Bob", time.Second, mtproto.Predicate_updateChat, isChat)

	d := differenceUntil(ctx, t, carol, stCarol, seqReached(stCarol.GetSeq()+2))
	if got, want := d.GetState().GetSeq(), stCarol.GetSeq()+2; got != want {
		t.Fatalf("Carol: state seq %d, want %d", got, want)
	}
	var rights, chats []*mtproto.Update
	for _, u := range updatesOfType(d, mtproto.Predicate_updateChatDefaultBannedRights) {
		if isRights(u) {
			rights = append(rights, u)
		}
	}
	for _, u := range updatesOfType(d, mtproto.Predicate_updateChat) {
		if isChat(u) {
			chats = append(chats, u)
		}
	}
	if len(rights) != 1 || len(chats) != 1 {
		t.Fatalf("Carol: %d updateChatDefaultBannedRights and %d updateChat of the chat, want one of each", len(rights), len(chats))
	}
	checkRights("Carol", rights[0])
	c := chatIn(d.GetChats(), chatId)
	if !c.GetNoforwards() || !c.GetDefaultBannedRights().GetChangeInfo() || !c.GetDefaultBannedRights().GetPinMessages() || c.GetVersion() != v0+2 {
		t.Errorf("Carol: chat in the difference %v, want noforwards and the rights of version %d", c, v0+2)
	}
}

// inviteHash returns the hash of an invite link (https://t.me/+hash).
func inviteHash(link string) string {
	return link[strings.LastIndexAny(link, "/+")+1:]
}

// TestSeqOthersJoinRequests: Erin asks to join a basic group by a link
// that needs an approval. The creator Alice and the admin Bob get the
// requests pending with seq, Carol does not; when Alice approves, the
// admins get the requests left, every member the members with Erin, and
// Erin the service message and the chat.
func TestSeqOthersJoinRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	erin := login(ctx, t, phones[3], "Erin")

	chatId := createChat(ctx, t, alice, fmt.Sprintf("requests %d", randomID()), bob, carol)
	getState(ctx, t, bob)
	getState(ctx, t, carol)
	editChatAdmin(ctx, t, alice, chatId, inputUserOf(ctx, t, alice, bob), true)
	// The states below are taken after Bob's and Carol's boxes applied the
	// new admin.
	for _, u := range []*user{bob, carol} {
		waitUpdate(ctx, t, u, mtproto.Predicate_updateChatParticipants, func(up *mtproto.Update) bool {
			return isParticipants(chatId, false)(up) &&
				members(up.GetParticipants_CHATPARTICIPANTS())[bob.self.GetId()] == mtproto.Predicate_chatParticipantAdmin
		})
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesExportChatInvite{
		Constructor:   alice.c.Constructor(mtproto.Predicate_messages_exportChatInvite),
		RequestNeeded: true,
		Peer:          chatPeer(alice, chatId),
	})
	if err != nil {
		t.Fatalf("messages.exportChatInvite: %v", err)
	}
	invite, ok := o.(interface {
		To_ExportedChatInvite() *mtproto.ExportedChatInvite
	})
	if !ok || !invite.To_ExportedChatInvite().GetRequestNeeded() {
		t.Fatalf("messages.exportChatInvite returned %T %v, want a link that needs an approval", o, o)
	}
	stAlice := getState(ctx, t, alice)
	stBob := getState(ctx, t, bob)
	stCarol := getState(ctx, t, carol)
	stErin := getState(ctx, t, erin)

	_, err = erin.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: erin.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        inviteHash(invite.To_ExportedChatInvite().GetLink()),
	})
	if !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Fatalf("messages.importChatInvite: %v, want INVITE_REQUEST_SENT", err)
	}

	isRequests := func(u *mtproto.Update) bool { return u.GetPeer_PEER().GetChatId() == chatId }
	admins := []struct {
		name string
		u    *user
		st   *mtproto.TLUpdatesState
	}{{"Alice", alice, stAlice}, {"Bob", bob, stBob}}
	for _, m := range admins {
		up := seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+1, mtproto.Predicate_updatePendingJoinRequests, isRequests)
		r := up.GetUpdates()[0]
		if r.GetRequestsPending() != 1 || len(r.GetRecentRequesters()) != 1 || r.GetRecentRequesters()[0] != erin.self.GetId() {
			t.Errorf("%s: %d requests pending, recent %v; want Erin's (%d)", m.name, r.GetRequestsPending(), r.GetRecentRequesters(), erin.self.GetId())
		}
		if userIn(up.GetUsers(), erin.self.GetId()) == nil || chatIn(up.GetChats(), chatId) == nil {
			t.Errorf("%s: users %v and chats %v with the requests, want Erin and the chat", m.name, up.GetUsers(), up.GetChats())
		}
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesHideChatJoinRequest{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_hideChatJoinRequest),
		Approved:    true,
		Peer:        chatPeer(alice, chatId),
		UserId:      inputUserOf(ctx, t, alice, erin),
	})
	if err != nil {
		t.Fatalf("messages.hideChatJoinRequest: %v", err)
	}
	version := chatOf(ctx, t, alice, chatId).GetVersion()
	want := map[int64]string{
		alice.self.GetId(): mtproto.Predicate_chatParticipantCreator,
		bob.self.GetId():   mtproto.Predicate_chatParticipantAdmin,
		carol.self.GetId(): mtproto.Predicate_chatParticipant,
		erin.self.GetId():  mtproto.Predicate_chatParticipant,
	}
	for _, m := range admins {
		up := seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+2, mtproto.Predicate_updateChatParticipants, isParticipants(chatId, false))
		checkMembers(t, m.name, up.GetUpdates()[0], want, version)
		up = seqPush(ctx, t, m.u, m.name, m.st.GetSeq()+3, mtproto.Predicate_updatePendingJoinRequests, isRequests)
		if r := up.GetUpdates()[0]; r.GetRequestsPending() != 0 || len(r.GetRecentRequesters()) != 0 {
			t.Errorf("%s: after the approval %d requests pending, recent %v; want none", m.name, r.GetRequestsPending(), r.GetRecentRequesters())
		}
	}

	// Carol's updates come in the order of the changes: the members with
	// Erin are her next seq, the request gave her nothing.
	up := seqPush(ctx, t, carol, "Carol", stCarol.GetSeq()+1, mtproto.Predicate_updateChatParticipants, isParticipants(chatId, false))
	checkMembers(t, "Carol", up.GetUpdates()[0], want, version)
	waitAction(ctx, t, carol, chatId, mtproto.Predicate_messageActionChatJoinedByRequest)
	if d := differenceOnce(ctx, t, carol, stCarol); actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatJoinedByRequest).GetFromId().GetUserId() != erin.self.GetId() {
		t.Errorf("Carol: messageActionChatJoinedByRequest not from Erin")
	}
	noPushOf(ctx, t, carol, "Carol", time.Second, mtproto.Predicate_updatePendingJoinRequests, isRequests)
	if d := differenceOnce(ctx, t, carol, stCarol); len(updatesOfType(d, mtproto.Predicate_updatePendingJoinRequests)) != 0 {
		t.Errorf("Carol: requests to join in her difference %v", updatesOfType(d, mtproto.Predicate_updatePendingJoinRequests))
	}

	// Erin is a member: the members with her, the chat and the service
	// message.
	d := differenceUntil(ctx, t, erin, stErin, func(d *mtproto.Updates_Difference) bool {
		return d.GetState().GetSeq() >= stErin.GetSeq()+1 &&
			actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatJoinedByRequest) != nil
	})
	if got, want := d.GetState().GetSeq(), stErin.GetSeq()+1; got != want {
		t.Errorf("Erin: state seq %d, want %d", got, want)
	}
	if ps := participantsOf(d, chatId); len(ps) != 1 {
		t.Errorf("Erin: %d updateChatParticipants of the chat, want 1", len(ps))
	} else {
		checkMembers(t, "Erin", ps[0], want, version)
	}
	// The message is the new member's, as in Telegram: clients show "Your
	// request to join the group was approved" to her and "Erin was
	// accepted into the group" to the others by its sender.
	if m := actionIn(d.GetNewMessages(), chatId, mtproto.Predicate_messageActionChatJoinedByRequest); m == nil ||
		m.GetFromId().GetUserId() != erin.self.GetId() || !m.GetOut() {
		t.Errorf("Erin: messageActionChatJoinedByRequest in the difference %v, want her own", m)
	}
	if c := chatIn(d.GetChats(), chatId); c.GetPredicateName() != mtproto.Predicate_chat || c.GetLeft() {
		t.Errorf("Erin: chat in the difference %v, want the chat she is in", c)
	}
	if c := chatOf(ctx, t, erin, chatId); c.GetPredicateName() != mtproto.Predicate_chat || c.GetLeft() || c.GetParticipantsCount_INT32() != 4 {
		t.Errorf("Erin: messages.getChats %v, want the chat of 4 she is in", c)
	}
}

// TestSeqOthersUnpinOnDelete: Alice pins her dialog with Bob and deletes it
// from her phone (messages.deleteHistory without just_clear). The phone
// gets updateDialogPinned with pinned false and the next seq; the laptop,
// offline, finds it last among the pins of the dialog in its difference.
func TestSeqOthersUnpinOnDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	phoneA := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, phoneA, bob)
	sendTo(ctx, t, phoneA, toBob, fmt.Sprintf("unpin %d", randomID()))
	o, err := phoneA.c.Invoke(ctx, &mtproto.TLMessagesToggleDialogPin{
		Constructor: phoneA.c.Constructor(mtproto.Predicate_messages_toggleDialogPin),
		Pinned:      true,
		Peer:        &mtproto.InputDialogPeer{Constructor: phoneA.c.Constructor(mtproto.Predicate_inputDialogPeer), Peer: toBob},
	})
	if err != nil {
		t.Fatalf("messages.toggleDialogPin: %v", err)
	}
	if _, ok := o.(*mtproto.TLBoolTrue); !ok {
		t.Fatalf("messages.toggleDialogPin returned %T", o)
	}
	stPhone := getState(ctx, t, phoneA)
	stLaptop := getState(ctx, t, laptop)

	deleteDialog(ctx, t, phoneA, toBob)

	isBob := func(u *mtproto.Update) bool {
		return u.GetPeer_DIALOGPEER().GetPeer().GetUserId() == bob.self.GetId()
	}
	// The push of the pin may come after the state.
	seqPush(ctx, t, phoneA, "Alice's phone", stPhone.GetSeq()+1, mtproto.Predicate_updateDialogPinned, func(u *mtproto.Update) bool {
		return isBob(u) && !u.GetPinned()
	})

	d := differenceOnce(ctx, t, laptop, stLaptop)
	var pins []*mtproto.Update
	for _, u := range updatesOfType(d, mtproto.Predicate_updateDialogPinned) {
		if isBob(u) {
			pins = append(pins, u)
		}
	}
	if len(pins) == 0 || pins[len(pins)-1].GetPinned() {
		t.Errorf("laptop: the pins of the dialog in the difference %v, want the unpin last", pins)
	}
	if got, want := d.GetState().GetSeq(), stLaptop.GetSeq()+1; got != want {
		t.Errorf("laptop: state seq %d, want %d", got, want)
	}
	if hasDialog(ctx, t, laptop, mtproto.MakePeerUser(bob.self.GetId())) {
		t.Errorf("laptop: the dialog with Bob is left after the deletion")
	}
}
