package dao

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A rank is up to 16 characters of text: no emoji, no control characters.
func TestCheckRank(t *testing.T) {
	for _, c := range []struct {
		rank string
		want error
	}{
		{"", nil},
		{"Модератор", nil},
		{"sixteen chars ok", nil},
		{"seventeen chars!!", mtproto.ErrAdminRankInvalid},
		{"кот 🐱", mtproto.ErrAdminRankEmojiNotAllowed},
		{"♥ fan", mtproto.ErrAdminRankEmojiNotAllowed},
		{"a\nb", mtproto.ErrAdminRankInvalid},
		{string([]byte{0xff}), mtproto.ErrAdminRankInvalid},
	} {
		if got := checkRank(c.rank); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q: %v, want %v", c.rank, got, c.want)
		}
	}
}
