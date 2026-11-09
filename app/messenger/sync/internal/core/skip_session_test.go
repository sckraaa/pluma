package core

import "testing"

// TestSkipSession: sync.updatesMe reaches only its authorization, even with
// no gateway named; sync.updatesNotMe everyone else.
func TestSkipSession(t *testing.T) {
	for _, tc := range []struct {
		name          string
		typ           SyncType
		perm, session int64
		skip          bool
	}{
		{"me, the authorization", syncTypeUserMe, 7, 7, false},
		{"me, another authorization", syncTypeUserMe, 7, 8, true},
		{"not me, the authorization", syncTypeUserNotMe, 7, 7, true},
		{"not me, another", syncTypeUserNotMe, 7, 8, false},
		{"user", syncTypeUser, 0, 8, false},
	} {
		if got := skipSession(tc.typ, tc.perm, tc.session); got != tc.skip {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}
