//go:build integration

package dao_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/grpc/status"
)

// The slow mode and the signatures of channels on the send path (task 045),
// on MySQL.

const slowmodeSeconds = 3600

// newChannelRow makes a channel of creator with the creator's row.
func (w *world) newChannelRow(creator int64, broadcast bool) int64 {
	w.t.Helper()
	id := newChat()
	_, err := w.d.RunBox(context.Background(), creator, nil, func(b *userbox.Box) error {
		if _, err := channelbox.Create(b, channelbox.NewChannel{
			Id: id, AccessHash: id * 3, CreatorUserId: creator, Title: "t", Broadcast: broadcast, Megagroup: !broadcast, Date: b.Date(),
		}); err != nil {
			return err
		}
		_, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, ?)",
			id, creator, channelperm.RoleCreator, b.Date())
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) member(channelId, userId int64, role int32) {
	w.t.Helper()
	w.exec("INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, 1)", channelId, userId, role)
}

func (w *world) lastSend(channelId, userId int64) (last int64, state int32) {
	w.t.Helper()
	if err := w.check.QueryRow("SELECT last_send_date, state FROM channel_participants WHERE channel_id = ? AND user_id = ?",
		channelId, userId).Scan(&last, &state); err != nil {
		w.t.Fatal(err)
	}
	return last, state
}

func channelText(from, channelId int64, grouped int64) dao.Outgoing {
	m := textMessage(from, mtproto.MakePeerChannel(channelId), "text")
	if grouped != 0 {
		m.GroupedId = mtproto.MakeFlagsInt64(grouped)
	}
	return dao.Outgoing{RandomId: randomId(), Message: m}
}

func (w *world) sendOutgoing(from, channelId int64, out ...dao.Outgoing) ([]dao.ChannelSent, error) {
	var sent []dao.ChannelSent
	_, err := w.d.RunBox(context.Background(), from, nil, func(b *userbox.Box) error {
		var err error
		sent, _, _, err = w.d.SendChannel(b, channelId, out, "Post Author")
		return err
	})
	return sent, err
}

func isSlowmodeWait(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Code() == mtproto.ErrFlood && strings.HasPrefix(s.Message(), "SLOWMODE_WAIT_")
}

// TestChannelSlowmodeParallel: of sends of a member at once, within the
// window, one passes and the others are SLOWMODE_WAIT_X; its time is written
// with it.
func TestChannelSlowmodeParallel(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, member := newUser(), newUser()
	id := w.newChannelRow(creator, false)
	w.member(id, member, channelperm.RoleMember)
	w.exec("UPDATE channels SET slowmode_seconds = ? WHERE id = ?", slowmodeSeconds, id)

	const sends = 8
	var (
		wg   sync.WaitGroup
		errs = make([]error, sends)
	)
	for i := range sends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.sendOutgoing(member, id, channelText(member, id, 0))
		}()
	}
	wg.Wait()
	passed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			passed++
		case !isSlowmodeWait(err):
			t.Errorf("a send at once: %v", err)
		}
	}
	if passed != 1 {
		t.Fatalf("%d sends passed, want 1: %v", passed, errs)
	}
	if last, _ := w.lastSend(id, member); last == 0 {
		t.Error("the send's time is not written")
	}
	var count int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND sender_user_id = ?", id, member).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("%d messages stored, want 1", count)
	}
}

// TestChannelSlowmodeRules: an admin and the creator are not limited; an
// album is one send, other messages together are not; a send that is
// refused writes nothing; when the window ends the member sends again.
func TestChannelSlowmodeRules(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, admin, member := newUser(), newUser(), newUser()
	id := w.newChannelRow(creator, false)
	w.member(id, admin, channelperm.RoleAdmin)
	w.member(id, member, channelperm.RoleMember)
	w.exec("UPDATE channels SET slowmode_seconds = ? WHERE id = ?", slowmodeSeconds, id)

	for _, who := range []int64{creator, admin} {
		for range 3 {
			if _, err := w.sendOutgoing(who, id, channelText(who, id, 0)); err != nil {
				t.Fatalf("user %d is limited: %v", who, err)
			}
		}
		if last, _ := w.lastSend(id, who); last != 0 {
			t.Errorf("user %d: last send %d under no limit", who, last)
		}
	}

	if _, err := w.sendOutgoing(member, id, channelText(member, id, 0), channelText(member, id, 0)); !errors.Is(err, mtproto.ErrSlowmodeMultiMsgsDisabled) {
		t.Errorf("two messages at once: %v, want SLOWMODE_MULTI_MSGS_DISABLED", err)
	}
	if _, err := w.sendOutgoing(member, id, channelText(member, id, 11), channelText(member, id, 12)); !errors.Is(err, mtproto.ErrSlowmodeMultiMsgsDisabled) {
		t.Errorf("two albums at once: %v, want SLOWMODE_MULTI_MSGS_DISABLED", err)
	}
	if last, _ := w.lastSend(id, member); last != 0 {
		t.Errorf("a refused send wrote %d", last)
	}
	sent, err := w.sendOutgoing(member, id, channelText(member, id, 21), channelText(member, id, 21), channelText(member, id, 21))
	if err != nil || len(sent) != 3 {
		t.Fatalf("an album: %d sent, %v", len(sent), err)
	}
	if _, err = w.sendOutgoing(member, id, channelText(member, id, 0)); !isSlowmodeWait(err) {
		t.Errorf("after the album: %v, want SLOWMODE_WAIT_X", err)
	}

	w.exec("UPDATE channel_participants SET last_send_date = last_send_date - ? WHERE channel_id = ? AND user_id = ?", slowmodeSeconds, id, member)
	if _, err = w.sendOutgoing(member, id, channelText(member, id, 0)); err != nil {
		t.Errorf("after the window: %v", err)
	}
	w.exec("UPDATE channels SET slowmode_seconds = 0 WHERE id = ?", id)
	for range 2 {
		if _, err = w.sendOutgoing(member, id, channelText(member, id, 0)); err != nil {
			t.Errorf("slow mode off: %v", err)
		}
	}
}

// TestChannelSlowmodeGuest: one who comments in a discussion group without
// being in it is limited as a member, the send kept on a row that reads as
// out of the group.
func TestChannelSlowmodeGuest(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, guest := newUser(), newUser()
	post := w.newChannelRow(creator, true)
	group := w.newChannelRow(creator, false)
	w.member(post, guest, channelperm.RoleMember)
	w.exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", group, post)
	w.exec("UPDATE channels SET linked_chat_id = ?, slowmode_seconds = ? WHERE id = ?", post, slowmodeSeconds, group)

	if _, err := w.sendOutgoing(guest, group, channelText(guest, group, 0)); err != nil {
		t.Fatalf("the guest's comment: %v", err)
	}
	if last, state := w.lastSend(group, guest); last == 0 || state != channelperm.StateLeft {
		t.Errorf("the guest's row: last %d, state %d", last, state)
	}
	if _, err := w.sendOutgoing(guest, group, channelText(guest, group, 0)); !isSlowmodeWait(err) {
		t.Errorf("the guest again: %v, want SLOWMODE_WAIT_X", err)
	}
}

// TestChannelSignatureProfiles: a signed post names its author; with the
// links it also comes from the author; unsigned it comes from neither.
func TestChannelSignatureProfiles(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator := newUser()
	id := w.newChannelRow(creator, true)
	post := func() *mtproto.Message {
		t.Helper()
		sent, err := w.sendOutgoing(creator, id, channelText(creator, id, 0))
		if err != nil {
			t.Fatal(err)
		}
		return sent[0].Message
	}

	if m := post(); m.GetFromId() != nil || m.GetPostAuthor() != nil {
		t.Errorf("unsigned: from %v, author %v", m.GetFromId(), m.GetPostAuthor())
	}
	w.exec("UPDATE channels SET signatures = 1 WHERE id = ?", id)
	if m := post(); m.GetFromId() != nil || m.GetPostAuthor().GetValue() != "Post Author" {
		t.Errorf("signed: from %v, author %v", m.GetFromId(), m.GetPostAuthor())
	}
	w.exec("UPDATE channels SET signature_profiles = 1 WHERE id = ?", id)
	if m := post(); m.GetFromId().GetUserId() != creator || m.GetPostAuthor().GetValue() != "Post Author" || !m.GetPost() {
		t.Errorf("with links: from %v, author %v, post %v", m.GetFromId(), m.GetPostAuthor(), m.GetPost())
	}
}
