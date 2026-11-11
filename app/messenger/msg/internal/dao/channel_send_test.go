package dao

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// A message is checked by each kind it is, and a preview of one who may
// not embed links is dropped, not refused.
func TestCheckSend(t *testing.T) {
	const now = 1_800_000_000
	member := func(banned int32) *sender {
		return &sender{p: channelperm.Participant{Role: channelperm.RoleMember, State: channelperm.StateIn, BannedRights: banned}}
	}
	photo := func() *mtproto.Message {
		return &mtproto.Message{Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()}
	}
	preview := func() *mtproto.Message {
		return &mtproto.Message{Message: "x", Media: mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{}).To_MessageMedia()}
	}
	for _, c := range []struct {
		name string
		s    *sender
		m    *mtproto.Message
		want error
	}{
		{"free photo", member(0), photo(), nil},
		{"photos forbidden", member(mtproto.BANNED_SEND_PHOTOS), photo(), mtproto.ErrChatSendPhotosForbidden},
		{"media forbidden", member(mtproto.BANNED_SEND_MEDIA), photo(), mtproto.ErrChatSendPhotosForbidden},
		{"writing forbidden", member(mtproto.BANNED_SEND_MESSAGES), &mtproto.Message{Message: "x"}, mtproto.ErrChatWriteForbidden},
		{"broadcast member", &sender{ch: channelperm.Channel{Broadcast: true}, p: member(0).p}, &mtproto.Message{Message: "x"}, mtproto.ErrChatWriteForbidden},
		{"links forbidden", member(mtproto.BANNED_EMBED_LINKS), preview(), nil},
	} {
		if err := checkSend(c.s, c.m, now); fmt.Sprint(err) != fmt.Sprint(c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	m := preview()
	if checkSend(member(mtproto.BANNED_EMBED_LINKS), m, now); m.Media != nil {
		t.Errorf("preview kept: %v", m.Media)
	}
	m = preview()
	if checkSend(member(0), m, now); m.Media == nil {
		t.Error("preview dropped for a member who may embed links")
	}
}

// A reply with no message of this chat is kept only if it says what it
// replies to.
func TestReplyStandsAlone(t *testing.T) {
	for _, c := range []struct {
		name string
		h    *mtproto.MessageReplyHeader
		want bool
	}{
		{"none", nil, false},
		{"only a topic", mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{}).To_MessageReplyHeader(), false},
		{"another chat", mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyFrom: &mtproto.MessageFwdHeader{}}).To_MessageReplyHeader(), true},
		{"a story", mtproto.MakeTLMessageReplyStoryHeader(&mtproto.MessageReplyHeader{}).To_MessageReplyHeader(), true},
	} {
		if got := replyStandsAlone(c.h); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// An anonymous admin writes as the group whatever user it names; anyone
// else writes as itself without a channel.
func TestSendAsAnonymous(t *testing.T) {
	const group = int64(100)
	admin := func(rights int32, guest bool) *sender {
		return &sender{p: channelperm.Participant{Role: channelperm.RoleAdmin, State: channelperm.StateIn, AdminRights: rights}, guest: guest}
	}
	for _, c := range []struct {
		name string
		s    *sender
		as   *mtproto.Peer
		want int64
	}{
		{"anonymous admin, no send_as", admin(mtproto.ADMIN_ANONYMOUS, false), nil, group},
		{"anonymous admin as itself", admin(mtproto.ADMIN_ANONYMOUS, false), mtproto.MakePeerUser(7), group},
		{"admin", admin(mtproto.ADMIN_CHANGE_INFO, false), nil, 0},
		{"member", &sender{p: channelperm.Participant{Role: channelperm.RoleMember, State: channelperm.StateIn, AdminRights: mtproto.ADMIN_ANONYMOUS}}, nil, 0},
		{"guest", admin(mtproto.ADMIN_ANONYMOUS, true), mtproto.MakePeerUser(7), 0},
	} {
		if got, err := c.s.sendAs(nil, group, c.as); err != nil || got != c.want {
			t.Errorf("%s: %d, %v; want %d", c.name, got, err, c.want)
		}
	}
}
