//go:build integration

package reactions

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

// itSeq gives each test its own users and messages.
var itSeq atomic.Int64

func itId() int64 { return 40_000_000 + itSeq.Add(1) }

type fixture struct {
	t  *testing.T
	db *sqlx.DB
}

func newFixture(t *testing.T) *fixture {
	conf := itDB.Config()
	return &fixture{t: t, db: sqlx.NewMySQL(&conf)}
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(context.Background(), query, args...); err != nil {
		f.t.Fatal(err)
	}
}

// message writes owner's copy boxId of the message dialogMessageId in the
// dialog with peer, with the text and the saved dialog given.
func (f *fixture) message(owner int64, boxId int32, dialogMessageId, peer int64, text string, savedPeer int64) {
	did := mtproto.MakeDialogId(owner, mtproto.PEER_USER, peer)
	f.exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message, saved_peer_type, saved_peer_id, date2) VALUES (?, ?, ?, ?, ?, ?, 2, ?, ?, ?, ?, 0)",
		owner, boxId, did.A, did.B, dialogMessageId, owner, peer, text, mtproto.PEER_USER, savedPeer)
}

func (f *fixture) react(dialogMessageId, user int64, key string, order int32, date int64) {
	f.exec("INSERT INTO message_reactions (dialog_message_id, user_id, reaction, chosen_order, `date`) VALUES (?, ?, ?, ?, ?)",
		dialogMessageId, user, key, order, date)
}

func TestTagsAndSearch(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	me, friend := itId(), itId()
	m1, m2, m3 := itId(), itId(), itId()
	f.message(me, 1, m1, me, "buy milk", me)
	f.message(me, 2, m2, me, "buy bread", friend) // saved from the friend's chat
	f.message(me, 3, m3, me, "read a book", me)
	f.react(m1, me, "e⭐", 0, 10)
	f.react(m1, me, "e🔥", 1, 11)
	f.react(m2, me, "e⭐", 0, 12)
	f.react(m3, me, "e🔥", 0, 13)
	f.exec("INSERT INTO saved_reaction_tags (user_id, reaction, title) VALUES (?, 'e⭐', 'todo')", me)

	tags, err := Tags(ctx, f.db, me, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Two each: the newer first.
	if len(tags) != 2 || tags[0] != (Tag{Reaction: "e🔥", Count: 2}) || tags[1] != (Tag{Reaction: "e⭐", Title: "todo", Count: 2}) {
		t.Fatalf("tags %+v", tags)
	}
	if tags, err = Tags(ctx, f.db, me, mtproto.MakePeerUtil(mtproto.PEER_USER, friend)); err != nil || len(tags) != 1 || tags[0].Reaction != "e⭐" {
		t.Fatalf("tags of one saved dialog %+v, %v", tags, err)
	}

	for name, c := range map[string]struct {
		keys   []string
		text   string
		offset int32
		want   []int32
	}{
		"one tag":         {[]string{"e⭐"}, "", 1 << 30, []int32{2, 1}},
		"both tags":       {[]string{"e⭐", "e🔥"}, "", 1 << 30, []int32{1}},
		"with text":       {[]string{"e🔥"}, "book", 1 << 30, []int32{3}},
		"below offset":    {[]string{"e⭐"}, "", 2, []int32{1}},
		"a like wildcard": {[]string{"e⭐"}, "%", 1 << 30, nil},
	} {
		ids, err := Tagged(ctx, f.db, me, c.keys, nil, c.text, c.offset, 10)
		if err != nil || !slices.Equal(ids, c.want) {
			t.Errorf("%s: %v, %v; want %v", name, ids, err, c.want)
		}
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	me, friend := itId(), itId()
	if key, err := Default(ctx, f.db, me); err != nil || key != "" {
		t.Fatalf("default of a new user %q, %v", key, err)
	}
	if err := SetDefault(ctx, f.db, me, "e👍"); err != nil {
		t.Fatal(err)
	}
	if key, err := Default(ctx, f.db, me); err != nil || key != "e👍" {
		t.Fatalf("default %q, %v", key, err)
	}

	m1, m2, tag := itId(), itId(), itId()
	f.message(me, 1, m1, friend, "hi", 0)
	f.message(me, 2, m2, friend, "hello", 0)
	f.message(me, 3, tag, me, "note", me)
	f.react(m1, me, "e👍", 0, 10)
	f.react(m2, me, "e🔥", 0, 20)
	f.react(m2, me, "e👍", 1, 21)
	f.react(tag, me, "e⭐", 0, 30) // a tag, not a recent reaction
	if keys, err := Recent(ctx, f.db, me, 10); err != nil || !slices.Equal(keys, []string{"e👍", "e🔥"}) {
		t.Fatalf("recent %v, %v", keys, err)
	}
	if err := ClearRecent(ctx, f.db, me, 20); err != nil {
		t.Fatal(err)
	}
	if keys, err := Recent(ctx, f.db, me, 10); err != nil || !slices.Equal(keys, []string{"e👍"}) {
		t.Fatalf("recent after clearing %v, %v", keys, err)
	}
	if key, _ := Default(ctx, f.db, me); key != "e👍" {
		t.Errorf("clearing the recent ones changed the default: %q", key)
	}
}

func TestAttach(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	author, reader := itId(), itId()
	m := itId()
	f.react(m, reader, "e❤", 0, 5)
	box := func(owner int64) *mtproto.MessageBox {
		return &mtproto.MessageBox{UserId: owner, SenderUserId: author, PeerType: mtproto.PEER_USER, PeerId: author + reader - owner, DialogMessageId: m, Message: &mtproto.Message{}}
	}
	plain := &mtproto.MessageBox{UserId: author, SenderUserId: author, PeerType: mtproto.PEER_USER, PeerId: reader, DialogMessageId: itId(), Message: &mtproto.Message{}}
	mine, theirs := box(author), box(reader)
	if err := Attach(ctx, f.db, mine, theirs, plain); err != nil {
		t.Fatal(err)
	}
	if r := mine.Message.Reactions; len(r.GetResults()) != 1 || r.GetResults()[0].ChosenOrder != nil {
		t.Errorf("the author's view %v", r)
	}
	if r := theirs.Message.Reactions; len(r.GetResults()) != 1 || r.GetResults()[0].ChosenOrder.GetValue() != 0 {
		t.Errorf("the reader's view %v", r)
	}
	if plain.Message.Reactions != nil {
		t.Errorf("a message without reactions got %v", plain.Message.Reactions)
	}
}
