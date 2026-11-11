package adminlog

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
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

func protojsonRoundTrip(a *mtproto.ChannelAdminLogEventAction) (*mtproto.ChannelAdminLogEventAction, error) {
	data, err := marshal(a)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}
