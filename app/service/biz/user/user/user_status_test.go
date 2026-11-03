package user

import (
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestMakeUserStatus(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name      string
		seenUntil int64
		allow     bool
		want      string
		expires   int32 // online: expires; offline: was_online
	}{
		{"inside the online window", now + 200, true, mtproto.Predicate_userStatusOnline, int32(now + 200)},
		{"window over", now - 5, true, mtproto.Predicate_userStatusOffline, int32(now - 5)},
		{"went offline now", now, true, mtproto.Predicate_userStatusOffline, int32(now)},
		{"hidden while online", now + 200, false, mtproto.Predicate_userStatusRecently, 0},
		{"hidden, four days ago", now - 4*24*3600, false, mtproto.Predicate_userStatusLastWeek, 0},
		{"hidden, ten days ago", now - 10*24*3600, false, mtproto.Predicate_userStatusLastMonth, 0},
	} {
		s := MakeUserStatus(tc.seenUntil, tc.allow)
		if s.GetPredicateName() != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, s.GetPredicateName(), tc.want)
			continue
		}
		got := s.GetExpires()
		if tc.want == mtproto.Predicate_userStatusOffline {
			got = s.GetWasOnline()
		}
		if got != tc.expires {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.expires)
		}
		// The copy in the proto module answers the same.
		if p := mtproto.MakeUserStatus(tc.seenUntil, tc.allow); p.GetPredicateName() != s.GetPredicateName() ||
			p.GetExpires() != s.GetExpires() || p.GetWasOnline() != s.GetWasOnline() {
			t.Errorf("%s: mtproto.MakeUserStatus gives %v", tc.name, p)
		}
	}
}
