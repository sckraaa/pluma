package core

import (
	"testing"

	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

func TestCountOnline(t *testing.T) {
	seen := []*userpb.LastSeenData{{LastSeenAt: 101}, {LastSeenAt: 100}, {LastSeenAt: 50}, {LastSeenAt: 200}}
	if got := countOnline(seen, 100); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}
