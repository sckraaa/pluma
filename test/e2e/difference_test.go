//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// updates.getDifference as Telegram composes it (task 010, ADR 0019): the
// messages new in the difference in their last state, deleted ones out,
// updateMessageID of the account's own new messages.

// differenceOnce returns u's updates.getDifference from st (one answer).
func differenceOnce(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState) *mtproto.Updates_Difference {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: u.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         st.GetPts(),
		Date:        st.GetDate(),
		Qts:         st.GetQts(),
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	return o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference()
}

// sendMessageID sends text from u to peer with randomId and returns the id
// of the message.
func sendMessageID(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string, randomId int64) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer, Message: text, RandomId: randomId,
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	for _, up := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateMessageID {
			return up.GetId_INT32()
		}
	}
	t.Fatal("messages.sendMessage reply without updateMessageID")
	return 0
}

func editText(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, id int32, text string) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        peer, Id: id, Message: wrapperspb.String(text),
	}); err != nil {
		t.Fatalf("messages.editMessage: %v", err)
	}
}

// updatesOfType returns the updates of d with the predicate.
func updatesOfType(d *mtproto.Updates_Difference, predicate string) []*mtproto.Update {
	var out []*mtproto.Update
	for _, u := range d.GetOtherUpdates() {
		if u.GetPredicateName() == predicate {
			out = append(out, u)
		}
	}
	return out
}

// TestDifferenceFoldsEditsAndDeletes: Alice sends Bob three messages, edits
// the second and deletes the third for both. Bob's difference from before
// has the first two, the second edited, an updateDeleteMessages for the
// third and no updateEditMessage; the seq is the one of updates.getState.
func TestDifferenceFoldsEditsAndDeletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	st := getState(ctx, t, bob)

	tag := fmt.Sprintf("fold %d", randomID())
	_ = sendMessageID(ctx, t, alice, toBob, tag+" 1", randomID())
	second := sendMessageID(ctx, t, alice, toBob, tag+" 2", randomID())
	third := sendMessageID(ctx, t, alice, toBob, tag+" 3", randomID())
	editText(ctx, t, alice, toBob, second, tag+" 2 edited")
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteMessages), Revoke: true, Id: []int32{third},
	}); err != nil {
		t.Fatalf("messages.deleteMessages: %v", err)
	}

	// Bob's box gets the events asynchronously: wait for the deletion.
	var d *mtproto.Updates_Difference
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		d = differenceOnce(ctx, t, bob, st)
		if len(updatesOfType(d, mtproto.Predicate_updateDeleteMessages)) > 0 || time.Now().After(deadline) {
			break
		}
	}
	var texts []string
	for _, m := range d.GetNewMessages() {
		texts = append(texts, m.GetMessage())
		if m.GetMessage() == tag+" 2 edited" && m.GetEditDate() == nil {
			t.Error("the edited message has no edit_date")
		}
	}
	if !slices.Equal(texts, []string{tag + " 1", tag + " 2 edited"}) {
		t.Errorf("new messages %q, want the first and the edited second", texts)
	}
	if n := len(updatesOfType(d, mtproto.Predicate_updateEditMessage)); n != 0 {
		t.Errorf("%d updateEditMessage of messages new in the difference", n)
	}
	if n := len(updatesOfType(d, mtproto.Predicate_updateDeleteMessages)); n != 1 {
		t.Errorf("%d updateDeleteMessages, want 1", n)
	}
	if n := len(updatesOfType(d, mtproto.Predicate_updateMessageID)); n != 0 {
		t.Errorf("%d updateMessageID for messages Bob did not send", n)
	}
	if got, want := d.GetState().GetSeq(), getState(ctx, t, bob).GetSeq(); got != want {
		t.Errorf("seq of the difference %d, of updates.getState %d", got, want)
	}
}

// TestDifferenceMessageIDForOwnMessages: a message Alice sends from one
// device is in the difference of her other device with updateMessageID
// first; an older message edited twice comes as one updateEditMessage with
// the last text.
func TestDifferenceMessageIDForOwnMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	old := sendMessageID(ctx, t, alice, toBob, "old", randomID())
	other := login(ctx, t, phones[0], "Alice")
	st := getState(ctx, t, other)

	randomId := randomID()
	sent := sendMessageID(ctx, t, alice, toBob, "new", randomId)
	editText(ctx, t, alice, toBob, old, "old edited once")
	editText(ctx, t, alice, toBob, old, "old edited twice")

	d := differenceOnce(ctx, t, other, st)
	ups := d.GetOtherUpdates()
	if len(ups) == 0 || ups[0].GetPredicateName() != mtproto.Predicate_updateMessageID || ups[0].GetId_INT32() != sent || ups[0].GetRandomId() != randomId {
		t.Fatalf("other updates %v, want updateMessageID of %d first", ups, sent)
	}
	// Only Alice's own: a notice of the new login may come in meanwhile.
	var own []*mtproto.Message
	for _, m := range d.GetNewMessages() {
		if m.GetOut() {
			own = append(own, m)
		}
	}
	if len(own) != 1 || own[0].GetId() != sent {
		t.Errorf("own new messages %v, want the one sent", own)
	}
	edits := updatesOfType(d, mtproto.Predicate_updateEditMessage)
	if len(edits) != 1 || edits[0].GetMessage_MESSAGE().GetMessage() != "old edited twice" {
		t.Errorf("edits of the older message %v, want one with the last text", edits)
	}
}
