//go:build integration

package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// user.newAuthorization (task 019, ADR 0022): updateNewAuthorization of a
// login is journaled with seq for every authorization of the account but the
// new one and pushed to the other devices, in one box transaction; the end
// of the review goes to every authorization.
func TestNewAuthorizationWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Anna")
	const (
		newKey = int64(7001)
		hash   = int64(555)
	)
	login := mtproto.MakeTLUpdateNewAuthorization(&mtproto.Update{
		Unconfirmed:    true,
		Hash:           hash,
		Date_FLAGINT32: wrapperspb.Int32(1000),
		Device:         wrapperspb.String("Safari 26"),
		Location:       wrapperspb.String("Prague, Czechia"),
	}).To_Update()
	if r, err := c.UserNewAuthorization(&user.TLUserNewAuthorization{UserId: u, ExceptAuthKeyId: newKey, Update: login}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("the login: %v %v", r, err)
	}
	reviewed := mtproto.MakeTLUpdateNewAuthorization(&mtproto.Update{Hash: hash}).To_Update()
	if _, err := c.UserNewAuthorization(&user.TLUserNewAuthorization{UserId: u, Update: reviewed}); err != nil {
		t.Fatalf("the end of the review: %v", err)
	}
	// Another update is refused and writes nothing.
	other := mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{}).To_Update()
	if _, err := c.UserNewAuthorization(&user.TLUserNewAuthorization{UserId: u, Update: other}); err == nil {
		t.Fatal("user.newAuthorization took updateNotifySettings")
	}

	type row struct {
		seq    int32
		kind   string
		except int64
	}
	rows, err := itRaw.Query("SELECT seq, update_type, except_auth_key_id FROM user_seq_updates WHERE user_id = ? ORDER BY seq", u)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.seq, &r.kind, &r.except); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []row{
		{userbox.BaseSeq + 1, mtproto.Predicate_updateNewAuthorization, newKey},
		{userbox.BaseSeq + 2, mtproto.Predicate_updateNewAuthorization, 0},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("journal %v, want %v", got, want)
	}
	if s := stateSeq(t, u); s != userbox.BaseSeq+2 {
		t.Fatalf("seq %d", s)
	}

	// The login goes to the devices but the new one, the end of the review to
	// all of them.
	events := eventsOf(t, u)
	if len(events) != 2 || events[0].method != "sync.TL_sync_updatesNotMe" || events[1].method != "sync.TL_sync_pushUpdates" {
		t.Fatalf("events %v", events)
	}
	notMe := &sync.TLSyncUpdatesNotMe{}
	if err = jsonx.Unmarshal(events[0].payload, notMe); err != nil {
		t.Fatal(err)
	}
	if up := notMe.GetUpdates(); notMe.GetUserId() != u || notMe.GetPermAuthKeyId() != newKey || up.GetSeq() != userbox.BaseSeq+1 ||
		len(up.GetUpdates()) != 1 || !up.GetUpdates()[0].GetUnconfirmed() || up.GetUpdates()[0].GetHash() != hash {
		t.Fatalf("push of the login %v", notMe)
	}
	push := &sync.TLSyncPushUpdates{}
	if err = jsonx.Unmarshal(events[1].payload, push); err != nil {
		t.Fatal(err)
	}
	if up := push.GetUpdates(); up.GetSeq() != userbox.BaseSeq+2 || up.GetUpdates()[0].GetUnconfirmed() {
		t.Fatalf("push of the end of the review %v", push)
	}
}
