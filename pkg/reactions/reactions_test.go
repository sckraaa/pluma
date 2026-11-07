package reactions

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func emoji(e string) *mtproto.Reaction {
	return mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: e}).To_Reaction()
}

func custom(id int64) *mtproto.Reaction {
	return mtproto.MakeTLReactionCustomEmoji(&mtproto.Reaction{DocumentId: id}).To_Reaction()
}

func TestKeys(t *testing.T) {
	keys, err := Keys([]*mtproto.Reaction{emoji("👍"), custom(42)})
	if err != nil || len(keys) != 2 || keys[0] != "e👍" || keys[1] != "c42" {
		t.Fatalf("keys %v, %v", keys, err)
	}
	for _, k := range keys {
		r := FromKey(k)
		if back, err := Key(r); err != nil || back != k {
			t.Errorf("round trip of %q: %q, %v", k, back, err)
		}
	}
	if keys, err := Keys(nil); err != nil || len(keys) != 0 {
		t.Errorf("none: %v, %v", keys, err)
	}
	for name, c := range map[string]struct {
		list []*mtproto.Reaction
		want error
	}{
		"too many": {[]*mtproto.Reaction{emoji("1"), emoji("2"), emoji("3"), emoji("4")}, mtproto.ErrReactionsTooMany},
		"repeated": {[]*mtproto.Reaction{emoji("👍"), emoji("👍")}, mtproto.ErrReactionInvalid},
		"paid":     {[]*mtproto.Reaction{mtproto.MakeTLReactionPaid(nil).To_Reaction()}, mtproto.ErrReactionInvalid},
		"empty":    {[]*mtproto.Reaction{emoji("")}, mtproto.ErrReactionInvalid},
	} {
		if _, err := Keys(c.list); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if FromKey("x1") != nil || FromKey("cabc") != nil || FromKey("e") != nil {
		t.Error("a key Key never made decoded")
	}
}

// The viewer's own reactions carry its chosen_order and my; results go by
// count, ties by the earliest; unread shows to the author only.
func TestView(t *testing.T) {
	const author, alice, bob = 1, 2, 3
	rows := []Row{
		{UserId: alice, Reaction: "e👍", ChosenOrder: 0, Date: 10, Unread: true},
		{UserId: alice, Reaction: "e🔥", ChosenOrder: 1, Date: 11, Unread: true},
		{UserId: bob, Reaction: "e🔥", ChosenOrder: 0, Date: 12, Unread: true, Big: true},
		{UserId: author, Reaction: "e😂", ChosenOrder: 0, Date: 9},
	}
	if View(nil, alice, author, Group) != nil {
		t.Error("a view without reactions")
	}
	if v := ViewOrEmpty(nil, alice, author, Group); v == nil || len(v.Results) != 0 || v.Min {
		t.Errorf("empty view %v", v)
	}

	v := View(rows, alice, author, Group)
	if v.Min || !v.CanSeeList || v.ReactionsAsTags {
		t.Errorf("flags %v", v)
	}
	order := []string{"e🔥", "e😂", "e👍"} // 2; then 1 at 9, 1 at 10
	for i, r := range v.Results {
		k, _ := Key(r.Reaction_REACTION)
		if k != order[i] {
			t.Fatalf("result %d is %s, want %s", i, k, order[i])
		}
	}
	if v.Results[0].Count != 2 || v.Results[0].ChosenOrder.GetValue() != 1 || v.Results[2].ChosenOrder == nil || v.Results[2].ChosenOrder.GetValue() != 0 || v.Results[1].ChosenOrder != nil {
		t.Errorf("counts and chosen_order of alice: %v", v.Results)
	}
	if len(v.RecentReactions) != 4 || v.RecentReactions[0].PeerId.GetUserId() != bob || !v.RecentReactions[0].Big {
		t.Fatalf("recent %v", v.RecentReactions)
	}
	for _, r := range v.RecentReactions {
		if r.Unread {
			t.Error("unread shown to a viewer who is not the author")
		}
		if r.My != (r.PeerId.GetUserId() == alice) {
			t.Errorf("my of %d", r.PeerId.GetUserId())
		}
	}

	v = View(rows, author, author, Private)
	for _, r := range v.RecentReactions {
		if want := r.PeerId.GetUserId() != author; r.Unread != want {
			t.Errorf("author's view: unread of %d is %v", r.PeerId.GetUserId(), r.Unread)
		}
	}
}

func TestViewSaved(t *testing.T) {
	v := View([]Row{{UserId: 1, Reaction: "e⭐", Date: 1}}, 1, 1, Saved)
	if !v.ReactionsAsTags || v.CanSeeList || len(v.RecentReactions) != 0 || v.Results[0].ChosenOrder == nil {
		t.Errorf("tags %v", v)
	}
}

func TestRecentMax(t *testing.T) {
	var rows []Row
	for u := int64(1); u <= RecentMax+3; u++ {
		rows = append(rows, Row{UserId: u, Reaction: "e👍", Date: u})
	}
	v := View(rows, 1, 1, Group)
	if len(v.RecentReactions) != RecentMax || v.RecentReactions[0].PeerId.GetUserId() != RecentMax+3 || v.Results[0].Count != RecentMax+3 {
		t.Errorf("recent %d, first %d", len(v.RecentReactions), v.RecentReactions[0].PeerId.GetUserId())
	}
}

// A channel's posts show the counts and the viewer's own choice, not who
// reacted.
func TestViewBroadcast(t *testing.T) {
	rows := []Row{{UserId: 1, Reaction: "e👍", Date: 10}, {UserId: 2, Reaction: "e👍", Date: 11}}
	v := View(rows, 2, 0, Broadcast)
	if v.GetCanSeeList() || len(v.GetRecentReactions()) != 0 {
		t.Errorf("who reacted is shown: %v", v)
	}
	if r := v.GetResults(); len(r) != 1 || r[0].GetCount() != 2 || r[0].GetChosenOrder() == nil {
		t.Errorf("results %v", r)
	}
	if g := View(rows, 2, 0, Group); !g.GetCanSeeList() || len(g.GetRecentReactions()) != 2 {
		t.Errorf("a supergroup shows who reacted: %v", g)
	}
}
