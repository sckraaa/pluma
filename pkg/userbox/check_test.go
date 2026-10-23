package userbox

import (
	"testing"
	"time"
)

func TestCheckRPCTimeout(t *testing.T) {
	need := 26 * time.Second
	for _, c := range []struct {
		ms int64
		ok bool
	}{
		{0, false},      // no deadline: refused
		{26_000, true},  // exactly the operation's limit
		{30_000, true},  // more
		{2_000, false},  // go-zero's default
		{25_999, false}, // just short
	} {
		if err := CheckRPCTimeout("Timeout", c.ms, need); (err == nil) != c.ok {
			t.Errorf("%d ms: %v, want ok %v", c.ms, err, c.ok)
		}
	}
}
