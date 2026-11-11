package adminlog

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

func TestKindsAndMask(t *testing.T) {
	title := mtproto.MakeTLChannelAdminLogEventActionChangeTitle(&mtproto.ChannelAdminLogEventAction{PrevValue_STRING: "a", NewValue_STRING: "b"}).To_ChannelAdminLogEventAction()
	if kindOf(title) != Info {
		t.Errorf("title is kind %d", kindOf(title))
	}
	if Mask(nil) != -1 || Mask(&mtproto.ChannelAdminLogEventsFilter{}) != -1 {
		t.Error("no filter must ask for every kind")
	}
	if m := Mask(&mtproto.ChannelAdminLogEventsFilter{Info: true, Pinned: true}); m != Info|Pinned {
		t.Errorf("mask %b", m)
	}
	data, err := protojsonRoundTrip(title)
	if err != nil || data.GetNewValue_STRING() != "b" || data.GetPredicateName() != mtproto.Predicate_channelAdminLogEventActionChangeTitle {
		t.Errorf("round trip: %v %v", data, err)
	}
}

// TestKindsOfTheFilter: each action goes under the flag of
// channelAdminLogEventsFilter that names it, so that a client asking for
// that flag only finds it.
func TestKindsOfTheFilter(t *testing.T) {
	// Each constructor sets the predicate of the object it is given.
	a := func() *mtproto.ChannelAdminLogEventAction { return &mtproto.ChannelAdminLogEventAction{} }
	for _, c := range []struct {
		action *mtproto.ChannelAdminLogEventAction
		kind   int64
	}{
		{mtproto.MakeTLChannelAdminLogEventActionParticipantJoinByInvite(a()).To_ChannelAdminLogEventAction(), Join},
		{mtproto.MakeTLChannelAdminLogEventActionParticipantJoinByRequest(a()).To_ChannelAdminLogEventAction(), Join},
		{mtproto.MakeTLChannelAdminLogEventActionToggleSlowMode(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionChangeHistoryTTL(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionChangeLinkedChat(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionChangeStickerSet(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionChangeWallpaper(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionChangeUsernames(a()).To_ChannelAdminLogEventAction(), Info},
		{mtproto.MakeTLChannelAdminLogEventActionToggleSignatures(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionToggleSignatureProfiles(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionTogglePreHistoryHidden(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionToggleNoForwards(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionToggleForum(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionChangeAvailableReactions(a()).To_ChannelAdminLogEventAction(), Settings},
		{mtproto.MakeTLChannelAdminLogEventActionExportedInviteEdit(a()).To_ChannelAdminLogEventAction(), Invites},
		{mtproto.MakeTLChannelAdminLogEventActionExportedInviteRevoke(a()).To_ChannelAdminLogEventAction(), Invites},
		{mtproto.MakeTLChannelAdminLogEventActionExportedInviteDelete(a()).To_ChannelAdminLogEventAction(), Invites},
		{mtproto.MakeTLChannelAdminLogEventActionCreateTopic(a()).To_ChannelAdminLogEventAction(), Forums},
		{mtproto.MakeTLChannelAdminLogEventActionEditTopic(a()).To_ChannelAdminLogEventAction(), Forums},
		{mtproto.MakeTLChannelAdminLogEventActionDeleteTopic(a()).To_ChannelAdminLogEventAction(), Forums},
		{mtproto.MakeTLChannelAdminLogEventActionPinTopic(a()).To_ChannelAdminLogEventAction(), Forums},
		{mtproto.MakeTLChannelAdminLogEventActionStopPoll(a()).To_ChannelAdminLogEventAction(), Edit},
	} {
		if got := kindOf(c.action); got != c.kind {
			t.Errorf("%s: kind %b, want %b", c.action.GetPredicateName(), got, c.kind)
		}
	}
}

// TestRoundTripOfNewActions: what Write stores of an action reads back the
// same, the flags and the nested objects with it.
func TestRoundTripOfNewActions(t *testing.T) {
	invite := mtproto.MakeTLChatInviteExported(&mtproto.ExportedChatInvite{Link: "https://t.me/+abc", AdminId: 7, Title: mtproto.MakeFlagsString("x")}).To_ExportedChatInvite()
	topic := mtproto.MakeTLForumTopic(&mtproto.ForumTopic{
		Id: 5, Title: "t", Pinned: true, Peer: mtproto.MakePeerChannel(3), FromId: mtproto.MakePeerUser(7),
		NotifySettings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(),
	}).To_ForumTopic()
	for _, a := range []*mtproto.ChannelAdminLogEventAction{
		mtproto.MakeTLChannelAdminLogEventActionParticipantJoinByInvite(&mtproto.ChannelAdminLogEventAction{ViaChatlist: true, Invite: invite}).To_ChannelAdminLogEventAction(),
		mtproto.MakeTLChannelAdminLogEventActionParticipantJoinByRequest(&mtproto.ChannelAdminLogEventAction{
			Invite: mtproto.MakeTLChatInvitePublicJoinRequests(nil).To_ExportedChatInvite(), ApprovedBy: 9,
		}).To_ChannelAdminLogEventAction(),
		mtproto.MakeTLChannelAdminLogEventActionPinTopic(&mtproto.ChannelAdminLogEventAction{NewTopic: topic}).To_ChannelAdminLogEventAction(),
		mtproto.MakeTLChannelAdminLogEventActionToggleSlowMode(&mtproto.ChannelAdminLogEventAction{PrevValue_INT32: 0, NewValue_INT32: 60}).To_ChannelAdminLogEventAction(),
		mtproto.MakeTLChannelAdminLogEventActionChangeLinkedChat(&mtproto.ChannelAdminLogEventAction{PrevValue_INT64: 3, NewValue_INT64: 4}).To_ChannelAdminLogEventAction(),
	} {
		got, err := protojsonRoundTrip(a)
		if err != nil || !proto.Equal(got, a) {
			t.Errorf("%s: %v %v", a.GetPredicateName(), got, err)
		}
		// The clients are on layers 228 and 233: both encode it.
		for _, layer := range []int32{228, 233} {
			if err = got.Encode(mtproto.NewEncodeBuf(512), layer); err != nil {
				t.Errorf("%s at layer %d: %v", a.GetPredicateName(), layer, err)
			}
		}
	}
}

func protojsonRoundTrip(a *mtproto.ChannelAdminLogEventAction) (*mtproto.ChannelAdminLogEventAction, error) {
	data, err := marshal(a)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}
