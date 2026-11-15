//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The notifications of a supergroup and of a channel (task 098): clients
// mute them with inputNotifyPeer{inputPeerChannel} (Android
// NotificationsController.java:6120, iOS
// ManagedPendingPeerNotificationSettings.swift:166, tdesktop
// apiwrap.cpp:2126); the settings are kept, and every device of the account
// gets updateNotifySettings with seq (ADR 0020). Before, the server
// answered ERR_ENTERPRISE_IS_BLOCKED.
func TestNotifySettingsChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	stLaptop := getState(ctx, t, laptop)

	notifyPeer := func(u *user, ch *mtproto.Chat, accessHash int64) *mtproto.InputNotifyPeer {
		return &mtproto.InputNotifyPeer{
			Constructor: u.c.Constructor(mtproto.Predicate_inputNotifyPeer),
			Peer: &mtproto.InputPeer{
				Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel),
				ChannelId:   ch.GetId(),
				AccessHash:  accessHash,
			},
		}
	}
	mute := func(u *user, peer *mtproto.InputNotifyPeer, until int32) error {
		o, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
			Constructor: u.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
			Peer:        peer,
			Settings: &mtproto.InputPeerNotifySettings{
				Constructor: u.c.Constructor(mtproto.Predicate_inputPeerNotifySettings),
				MuteUntil:   wrapperspb.Int32(until),
			},
		})
		if err == nil {
			if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
				t.Errorf("account.updateNotifySettings: %v, want boolTrue", o)
			}
		}
		return err
	}

	seq := stLaptop.GetSeq()
	for i, megagroup := range []bool{true, false} {
		_, ch := createChannel(ctx, t, alice, "muted", megagroup)
		hash := ch.GetAccessHash_FLAGINT64().GetValue()
		until := int32(time.Now().Add(time.Hour).Unix()) + int32(i)
		if err := mute(alice, notifyPeer(alice, ch, hash), until); err != nil {
			t.Fatalf("mute (megagroup %v): %v", megagroup, err)
		}

		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetNotifySettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_getNotifySettings),
			Peer:        notifyPeer(alice, ch, hash),
		})
		if err != nil {
			t.Fatalf("account.getNotifySettings: %v", err)
		}
		if got := o.(interface {
			To_PeerNotifySettings() *mtproto.PeerNotifySettings
		}).To_PeerNotifySettings().GetMuteUntil().GetValue(); got != until {
			t.Errorf("megagroup %v: mute_until %d, want %d", megagroup, got, until)
		}

		// The other device learns it with the next seq, the channel with it.
		up, err := laptop.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, x := range us.GetUpdates() {
				if x.GetPredicateName() == mtproto.Predicate_updateNotifySettings &&
					x.GetPeer_NOTIFYPEER().GetPeer().GetChannelId() == ch.GetId() &&
					x.GetNotifySettings().GetMuteUntil().GetValue() == until {
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("megagroup %v: no updateNotifySettings on the other device: %v", megagroup, err)
		}
		if up.GetSeq() <= seq {
			t.Errorf("megagroup %v: seq %d, want above %d", megagroup, up.GetSeq(), seq)
		}
		seq = up.GetSeq()
		found := false
		for _, c := range up.GetChats() {
			found = found || c.GetId() == ch.GetId()
		}
		if !found {
			t.Errorf("megagroup %v: the push has no channel: %v", megagroup, up.GetChats())
		}

		// A wrong access_hash; a private channel Bob is not in.
		if err = mute(alice, notifyPeer(alice, ch, hash+1), until); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
			t.Errorf("megagroup %v, wrong access_hash: %v, want PEER_ID_INVALID", megagroup, err)
		}
		if err = mute(bob, notifyPeer(bob, ch, hash), until); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
			t.Errorf("megagroup %v, not a member: %v, want CHANNEL_PRIVATE", megagroup, err)
		}
	}
}
