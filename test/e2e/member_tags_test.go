
// A member of a supergroup sets its own tag, the next message carries it,
// and the group can forbid it.
func TestMemberTagSupergroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("tags %d", randomID()), true)
	input := &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   ch.GetId(),
		AccessHash:  ch.GetAccessHash().GetValue(),
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input,
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{
			Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash().GetValue(),
		}
	}
	getState(ctx, t, alice)

	if err := editRank(ctx, bob, peer(bob), userPeer(bob.c, bob), "Tester"); err != nil {
		t.Fatalf("own tag: %v", err)
	}
	if err := editRank(ctx, bob, peer(bob), userPeer(bob.c, alice), "No"); err == nil {
		t.Error("a member set the tag of the creator")
	}
	sendMessageID(ctx, t, bob, peer(bob), "with a tag", randomID())
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "with a tag" {
			return false
		}
		if got := m.GetFromRank().GetValue(); got != "Tester" {
			t.Errorf("from_rank %q, want Tester", got)
		}
		return true
	})
}
