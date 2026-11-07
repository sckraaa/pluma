package channelperm

import (
	"fmt"
	"math"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

const now = int32(1_800_000_000)

var (
	supergroup = Channel{}
	broadcast  = Channel{Broadcast: true}
	member     = Participant{Role: RoleMember, State: StateIn}
	creator    = Participant{Role: RoleCreator, State: StateIn}
)

func admin(rights int32) Participant {
	return Participant{Role: RoleAdmin, State: StateIn, AdminRights: rights}
}

func restricted(rights, until int32) Participant {
	return Participant{Role: RoleMember, State: StateIn, BannedRights: rights, UntilDate: until}
}

var allActions = []Action{SendPlain, SendPhotos, SendVideos, SendRoundVideos, SendAudios, SendVoices, SendDocs,
	SendStickers, SendGifs, SendGames, SendInline, SendPolls, EmbedLinks, EditOthers, DeleteOthers, PinMessages,
	InviteUsers, ChangeInfo, BanUsers, AddAdmins, ManageCall, ManageTopics}

// check compares the errors by their text: Can answers with the errors of
// mtproto, which the clients read by text.
func check(t *testing.T, name string, got, want error) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: %v, want %v", name, got, want)
	}
}

// The creator does everything, in a channel of either kind, whatever the
// default restrictions.
func TestCreator(t *testing.T) {
	for _, ch := range []Channel{supergroup, broadcast, {Default: math.MaxInt32}} {
		for _, a := range allActions {
			check(t, "creator", Can(ch, creator, a, now), nil)
		}
	}
}

// Out of the channel, nothing: left and kicked get CHANNEL_PRIVATE, whatever
// the role they had.
func TestOutOfTheChannel(t *testing.T) {
	for _, state := range []int32{StateLeft, StateKicked} {
		for _, p := range []Participant{member, creator, admin(math.MaxInt32)} {
			p.State = state
			check(t, "out", Can(supergroup, p, SendPlain, now), mtproto.ErrChannelPrivate)
		}
	}
}

// Each admin right allows its action and only it; an admin of a supergroup
// sends anything, of a broadcast channel with post_messages only, and the
// restrictions of members do not apply to admins.
func TestAdminRights(t *testing.T) {
	bits := map[Action]int32{
		EditOthers: mtproto.ADMIN_EDIT_MESSAGES, DeleteOthers: mtproto.ADMIN_DELETE_MESSAGES,
		PinMessages: mtproto.ADMIN_PIN_MESSAGES, InviteUsers: mtproto.ADMIN_INVITE_USERS,
		ChangeInfo: mtproto.ADMIN_CHANGE_INFO, BanUsers: mtproto.ADMIN_BAN_USERS,
		AddAdmins: mtproto.ADMIN_ADD_ADMINS, ManageCall: mtproto.ADMIN_MANAGE_CALL,
		ManageTopics: mtproto.ADMIN_MANAGE_TOPICS,
	}
	for a, bit := range bits {
		check(t, "with the right", Can(supergroup, admin(bit), a, now), nil)
		check(t, "with every other right", Can(supergroup, admin(math.MaxInt32&^bit), a, now), mtproto.ErrChatAdminRequired)
	}
	everything := Channel{Default: math.MaxInt32}
	for a := range sendBits {
		check(t, "supergroup admin sends", Can(everything, admin(mtproto.ADMIN_OTHER), a, now), nil)
		check(t, "broadcast admin without post_messages", Can(broadcast, admin(mtproto.ADMIN_EDIT_MESSAGES), a, now), mtproto.ErrChatWriteForbidden)
		check(t, "broadcast admin with post_messages", Can(broadcast, admin(mtproto.ADMIN_POST_MESSAGES), a, now), nil)
	}
	// Pinning in a broadcast channel: pin_messages, or edit_messages as the
	// clients allow it.
	check(t, "broadcast pin by an editor", Can(broadcast, admin(mtproto.ADMIN_EDIT_MESSAGES), PinMessages, now), nil)
	check(t, "supergroup pin by an editor", Can(supergroup, admin(mtproto.ADMIN_EDIT_MESSAGES), PinMessages, now), mtproto.ErrChatAdminRequired)
}

// A member of a broadcast channel only reads.
func TestBroadcastMember(t *testing.T) {
	for _, a := range allActions {
		want := mtproto.ErrChatAdminRequired
		if _, send := sendBits[a]; send {
			want = mtproto.ErrChatWriteForbidden
		}
		check(t, "broadcast member", Can(broadcast, member, a, now), want)
	}
}

// A member of a supergroup does what no restriction forbids; each flag of
// chatBannedRights forbids its action with its own error, send_messages
// every message, send_media every kind of media; the default restrictions
// and the member's own add up; admin-only actions stay forbidden.
func TestMemberRestrictions(t *testing.T) {
	for _, a := range []Action{SendPlain, SendPhotos, SendStickers, SendPolls, PinMessages, InviteUsers, ChangeInfo, ManageTopics} {
		check(t, "free member", Can(supergroup, member, a, now), nil)
	}
	for _, a := range []Action{EditOthers, DeleteOthers, BanUsers, AddAdmins, ManageCall} {
		check(t, "admin-only", Can(supergroup, member, a, now), mtproto.ErrChatAdminRequired)
	}
	for a, s := range sendBits {
		check(t, "own flag", Can(supergroup, restricted(s.banned, 0), a, now), s.err)
		check(t, "default flag", Can(Channel{Default: s.banned}, member, a, now), s.err)
		check(t, "send_messages", Can(supergroup, restricted(mtproto.BANNED_SEND_MESSAGES, 0), a, now), mtproto.ErrChatWriteForbidden)
		want := error(nil)
		if s.media {
			want = s.err
		}
		check(t, "send_media", Can(supergroup, restricted(mtproto.BANNED_SEND_MEDIA, 0), a, now), want)
	}
	for a, bit := range memberBits {
		check(t, "own member flag", Can(supergroup, restricted(bit, 0), a, now), mtproto.ErrChatAdminRequired)
		check(t, "default member flag", Can(Channel{Default: bit}, member, a, now), mtproto.ErrChatAdminRequired)
	}
	// Photos forbidden by default, stickers by the member's own restriction.
	both := restricted(mtproto.BANNED_SEND_STICKERS, 0)
	ch := Channel{Default: mtproto.BANNED_SEND_PHOTOS}
	check(t, "default part", Can(ch, both, SendPhotos, now), mtproto.ErrChatSendPhotosForbidden)
	check(t, "own part", Can(ch, both, SendStickers, now), mtproto.ErrChatSendStickersForbidden)
	check(t, "neither", Can(ch, both, SendPlain, now), nil)
}

// A restriction is over at its until_date; 0 and Int32.max are for good.
// The default restrictions have no date.
func TestUntilDate(t *testing.T) {
	stickers := mtproto.BANNED_SEND_STICKERS
	check(t, "running", Can(supergroup, restricted(int32(stickers), now+60), SendStickers, now), mtproto.ErrChatSendStickersForbidden)
	check(t, "over", Can(supergroup, restricted(int32(stickers), now-1), SendStickers, now), nil)
	check(t, "at its date", Can(supergroup, restricted(int32(stickers), now), SendStickers, now), nil)
	check(t, "for good (0)", Can(supergroup, restricted(int32(stickers), 0), SendStickers, now), mtproto.ErrChatSendStickersForbidden)
	check(t, "for good (max)", Can(supergroup, restricted(int32(stickers), math.MaxInt32), SendStickers, now), mtproto.ErrChatSendStickersForbidden)
	over := restricted(int32(stickers), now-1)
	check(t, "default after the own is over", Can(Channel{Default: int32(stickers)}, over, SendStickers, now), mtproto.ErrChatSendStickersForbidden)
}

// The until_date a client sends: under 30 seconds or over 366 days from
// now is for good.
func TestUntilDateOfARequest(t *testing.T) {
	for _, c := range []struct{ in, want int32 }{
		{0, math.MaxInt32},
		{now + 29, math.MaxInt32},
		{now + 30, now + 30},
		{now + 86400, now + 86400},
		{now + 366*86400, now + 366*86400},
		{now + 366*86400 + 1, math.MaxInt32},
		{now - 10, math.MaxInt32},
		{math.MaxInt32, math.MaxInt32},
	} {
		if got := UntilDate(c.in, now); got != c.want {
			t.Errorf("UntilDate(%d) = %d, want %d", c.in-now, got, c.want)
		}
	}
}
