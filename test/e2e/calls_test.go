//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// callProtocol is what the clients send (VoIPService.java:1108-1114).
func callProtocol(u *user) *mtproto.PhoneCallProtocol {
	return &mtproto.PhoneCallProtocol{
		Constructor:     u.c.Constructor(mtproto.Predicate_phoneCallProtocol),
		UdpP2P:          true,
		UdpReflector:    true,
		MinLayer:        65,
		MaxLayer:        92,
		LibraryVersions: []string{"9.0.0"},
	}
}

func requestCall(ctx context.Context, t *testing.T, from, to *user) *mtproto.PhoneCall {
	t.Helper()
	o, err := from.c.Invoke(ctx, &mtproto.TLPhoneRequestCall{
		Constructor: from.c.Constructor(mtproto.Predicate_phone_requestCall),
		UserId:      inputUserOf(ctx, t, from, to),
		RandomId:    int32(randomID() & 0x7fffffff),
		GAHash:      make([]byte, 32),
		Protocol:    callProtocol(from),
	})
	if err != nil {
		t.Fatalf("phone.requestCall: %v", err)
	}
	return o.(interface {
		To_Phone_PhoneCall() *mtproto.Phone_PhoneCall
	}).To_Phone_PhoneCall().GetPhoneCall()
}

func inputCall(u *user, c *mtproto.PhoneCall) *mtproto.InputPhoneCall {
	return &mtproto.InputPhoneCall{Constructor: u.c.Constructor(mtproto.Predicate_inputPhoneCall), Id: c.GetId(), AccessHash: c.GetAccessHash()}
}

// callOf waits on u for updatePhoneCall of a call in the given state.
func callOf(ctx context.Context, t *testing.T, u *user, id int64, predicate string) *mtproto.PhoneCall {
	t.Helper()
	var found *mtproto.PhoneCall
	_, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() != mtproto.Predicate_updatePhoneCall {
				continue
			}
			if c := x.GetPhoneCall(); c.GetId() == id && c.GetPredicateName() == predicate {
				found = c
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("no %s: %v", predicate, err)
	}
	return found
}

func discard(ctx context.Context, u *user, c *mtproto.PhoneCall, reason string) (*mtproto.Updates, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneDiscardCall{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_discardCall),
		Peer:        inputCall(u, c),
		Reason:      &mtproto.PhoneCallDiscardReason{Constructor: u.c.Constructor(reason)},
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

// historyCalls returns the phoneCall service messages of u with peer.
func historyCalls(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) []*mtproto.MessageAction {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	var out []*mtproto.MessageAction
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		if a := m.GetAction(); a.GetPredicateName() == mtproto.Predicate_messageActionPhoneCall {
			out = append(out, a)
		}
	}
	return out
}

func TestCallRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobLaptop := login(ctx, t, phones[1], "Bob")
	defer closeChecked(t, "alice", alice.c)
	defer closeChecked(t, "bob", bob.c)
	defer closeChecked(t, "bob laptop", bobLaptop.c)
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	getState(ctx, t, bobLaptop)

	// Every client asks for the group first.
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetDhConfig{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_getDhConfig),
		RandomLength: 256,
	})
	if err != nil {
		t.Fatalf("messages.getDhConfig: %v", err)
	}
	dh := o.(interface {
		To_Messages_DhConfig() *mtproto.Messages_DhConfig
	}).To_Messages_DhConfig()
	if dh.GetPredicateName() != mtproto.Predicate_messages_dhConfig || len(dh.GetP()) != 256 || dh.GetG() == 0 || len(dh.GetRandom()) != 256 {
		t.Fatalf("messages.getDhConfig: %v", dh)
	}

	call := requestCall(ctx, t, alice, bob)
	if call.GetPredicateName() != mtproto.Predicate_phoneCallWaiting {
		t.Fatalf("requestCall answered %s", call.GetPredicateName())
	}
	incoming := callOf(ctx, t, bob, call.GetId(), mtproto.Predicate_phoneCallRequested)
	callOf(ctx, t, bobLaptop, call.GetId(), mtproto.Predicate_phoneCallRequested)
	if len(incoming.GetGAHash()) != 32 || incoming.GetAdminId() != alice.self.GetId() {
		t.Fatalf("requested: %v", incoming)
	}

	// The caller's phone rings back when the first device received the call.
	if _, err := bob.c.Invoke(ctx, &mtproto.TLPhoneReceivedCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_receivedCall), Peer: inputCall(bob, call)}); err != nil {
		t.Fatalf("phone.receivedCall: %v", err)
	}
	if got := callOf(ctx, t, alice, call.GetId(), mtproto.Predicate_phoneCallWaiting); got.GetReceiveDate() == nil {
		t.Fatalf("waiting without receive_date: %v", got)
	}

	// The first device to accept wins, the other one is told.
	gB := make([]byte, 256)
	gB[0] = 7
	if _, err := bob.c.Invoke(ctx, &mtproto.TLPhoneAcceptCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_acceptCall), Peer: inputCall(bob, call), GB: gB, Protocol: callProtocol(bob)}); err != nil {
		t.Fatalf("phone.acceptCall: %v", err)
	}
	if _, err := bobLaptop.c.Invoke(ctx, &mtproto.TLPhoneAcceptCall{Constructor: bobLaptop.c.Constructor(mtproto.Predicate_phone_acceptCall), Peer: inputCall(bobLaptop, call), GB: gB, Protocol: callProtocol(bobLaptop)}); err == nil || !strings.Contains(err.Error(), "CALL_ALREADY_ACCEPTED") {
		t.Fatalf("second accept: %v", err)
	}
	callOf(ctx, t, bobLaptop, call.GetId(), mtproto.Predicate_phoneCallDiscarded)
	accepted := callOf(ctx, t, alice, call.GetId(), mtproto.Predicate_phoneCallAccepted)
	if string(accepted.GetGB()) != string(gB) {
		t.Fatal("g_b not relayed")
	}

	// The caller reveals g_a; the other side gets it with the connections.
	gA := make([]byte, 256)
	gA[0] = 9
	o, err = alice.c.Invoke(ctx, &mtproto.TLPhoneConfirmCall{Constructor: alice.c.Constructor(mtproto.Predicate_phone_confirmCall), Peer: inputCall(alice, call), GA: gA, KeyFingerprint: 1234, Protocol: callProtocol(alice)})
	if err != nil {
		t.Fatalf("phone.confirmCall: %v", err)
	}
	active := o.(interface {
		To_Phone_PhoneCall() *mtproto.Phone_PhoneCall
	}).To_Phone_PhoneCall().GetPhoneCall()
	if active.GetPredicateName() != mtproto.Predicate_phoneCall || len(active.GetConnections()) == 0 {
		t.Fatalf("confirm answered %v", active)
	}
	if c := active.GetConnections()[0]; c.GetPredicateName() != mtproto.Predicate_phoneConnectionWebrtc || !c.GetTurn() || c.GetUsername() == "" || c.GetPassword() == "" {
		t.Fatalf("connection: %v", c)
	}
	bobCall := callOf(ctx, t, bob, call.GetId(), mtproto.Predicate_phoneCall)
	if string(bobCall.GetGAOrB()) != string(gA) || bobCall.GetKeyFingerprint() != 1234 {
		t.Fatalf("phoneCall to the participant: %v", bobCall)
	}

	// Signaling goes to the bound device.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLPhoneSendSignalingData{Constructor: alice.c.Constructor(mtproto.Predicate_phone_sendSignalingData), Peer: inputCall(alice, call), Data: []byte("sdp")}); err != nil {
		t.Fatalf("phone.sendSignalingData: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updatePhoneCallSignalingData, func(u *mtproto.Update) bool {
		return u.GetPhoneCallId() == call.GetId() && string(u.GetData_FLAGBYTES()) == "sdp"
	})

	// End of the call: both sides and both histories.
	r, err := discard(ctx, bob, call, mtproto.Predicate_phoneCallDiscardReasonHangup)
	if err != nil {
		t.Fatalf("phone.discardCall: %v", err)
	}
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPhoneCall().GetPredicateName() != mtproto.Predicate_phoneCallDiscarded {
		t.Fatalf("discard answered %v", r)
	}
	callOf(ctx, t, alice, call.GetId(), mtproto.Predicate_phoneCallDiscarded)
	isCallMessage := func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionPhoneCall && a.GetCallId() == call.GetId()
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, isCallMessage)
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, isCallMessage)
	if calls := historyCalls(ctx, t, alice, inputPeerOf(ctx, t, alice, bob)); len(calls) != 1 ||
		calls[0].GetReason().GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonHangup {
		t.Fatalf("history: %v", calls)
	}

	// A repeated discard answers the same and tells nobody again.
	again, err := discard(ctx, bob, call, mtproto.Predicate_phoneCallDiscardReasonHangup)
	if err != nil {
		t.Fatalf("second discard: %v", err)
	}
	if u := again.GetUpdates(); len(u) != 1 || u[0].GetPhoneCall().GetPredicateName() != mtproto.Predicate_phoneCallDiscarded ||
		u[0].GetPhoneCall().GetReason().GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonHangup {
		t.Fatalf("second discard answered %v", again)
	}
	quiet, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if _, err := alice.c.WaitUpdate(quiet, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if c := x.GetPhoneCall(); c.GetPredicateName() == mtproto.Predicate_phoneCallDiscarded && c.GetId() == call.GetId() {
				return true
			}
			if x.GetPredicateName() == mtproto.Predicate_updateNewMessage && isCallMessage(x) {
				return true
			}
		}
		return false
	}); err == nil {
		t.Fatal("the repeated discard reached the other side")
	}
	// Rating, on either side.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLPhoneSetCallRating{Constructor: bob.c.Constructor(mtproto.Predicate_phone_setCallRating), Peer: inputCall(bob, call), Rating: 5, Comment: "ok"}); err != nil {
		t.Fatalf("phone.setCallRating: %v", err)
	}
}

func TestCallRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	defer closeChecked(t, "alice", alice.c)
	defer closeChecked(t, "bob", bob.c)

	mustSetPrivacy(ctx, t, bob, mtproto.Predicate_inputPrivacyKeyPhoneCall, ruleOf(bob, mtproto.Predicate_inputPrivacyValueDisallowAll))
	req := &mtproto.TLPhoneRequestCall{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_requestCall),
		UserId:      inputUserOf(ctx, t, alice, bob),
		RandomId:    int32(randomID() & 0x7fffffff),
		GAHash:      make([]byte, 32),
		Protocol:    callProtocol(alice),
	}
	if _, err := alice.c.Invoke(ctx, req); err == nil || !strings.Contains(err.Error(), "USER_PRIVACY_RESTRICTED") {
		t.Fatalf("privacy: %v", err)
	}
	mustSetPrivacy(ctx, t, bob, mtproto.Predicate_inputPrivacyKeyPhoneCall, ruleOf(bob, mtproto.Predicate_inputPrivacyValueAllowAll))
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsBlock{Constructor: bob.c.Constructor(mtproto.Predicate_contacts_block), Id: inputPeerOf(ctx, t, bob, alice)}); err != nil {
		t.Fatalf("contacts.block: %v", err)
	}
	req.RandomId++
	if _, err := alice.c.Invoke(ctx, req); err == nil || !strings.Contains(err.Error(), "USER_IS_BLOCKED") {
		t.Fatalf("blocked: %v", err)
	}
}

// A device offline when the call started learns of it from the difference,
// once. A call nobody answers ends by the server's timer, as missed, with
// the message in both histories.
func TestCallMissedAndDifference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	earlier := login(ctx, t, phones[1], "Bob")
	defer closeChecked(t, "alice", alice.c)
	getState(ctx, t, alice)

	call := requestCall(ctx, t, alice, earlier)
	closeChecked(t, "earlier device", earlier.c)

	// The device that was away when the call started: a new authorization of
	// the callee, which no live update reached.
	bob := login(ctx, t, phones[1], "Bob")
	defer closeChecked(t, "bob", bob.c)
	st := getState(ctx, t, bob)
	d := differenceOnce(ctx, t, bob, st)
	if len(updatesOfType(d, mtproto.Predicate_updatePhoneCall)) != 1 {
		t.Fatalf("difference of the device that was offline: %v", d)
	}
	if again := differenceOnce(ctx, t, bob, st); len(updatesOfType(again, mtproto.Predicate_updatePhoneCall)) != 0 {
		t.Fatal("a second difference brought the call again")
	}

	// receive timeout + grace, with room for the sweep.
	wait, stop := context.WithTimeout(ctx, 40*time.Second)
	defer stop()
	got := callOf(wait, t, alice, call.GetId(), mtproto.Predicate_phoneCallDiscarded)
	if got.GetReason().GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonMissed {
		t.Fatalf("reason: %v", got.GetReason())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionPhoneCall && a.GetCallId() == call.GetId()
	})
}
