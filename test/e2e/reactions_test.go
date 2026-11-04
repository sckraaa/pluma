// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// sendForReactions sends text to peer and returns the id of the sender's copy.
func sendForReactions(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			return x.GetMessage_MESSAGE().GetId()
		}
	}
	t.Fatal("messages.sendMessage reply without updateNewMessage")
	return 0
}

func emojiReaction(u *user, e string) *mtproto.Reaction {
	return &mtproto.Reaction{Constructor: u.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: e}
}

func sendReaction(ctx context.Context, u *user, peer *mtproto.InputPeer, msgId int32, emoji ...string) (*mtproto.Updates, error) {
	list := make([]*mtproto.Reaction, 0, len(emoji))
	for _, e := range emoji {
		list = append(list, emojiReaction(u, e))
	}
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor:                 u.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:                        peer,
		MsgId:                       msgId,
		Reaction_FLAGVECTORREACTION: list,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		return nil, nil
	}
	return r.To_Updates(), nil
}

// reactionsOf finds the updateMessageReactions of msgId in updates.
func reactionsOf(updates []*mtproto.Update, msgId int32) *mtproto.MessageReactions {
	var last *mtproto.MessageReactions
	for _, u := range updates {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageReactions && u.GetMsgId_INT32() == msgId {
			last = u.GetReactions_MESSAGEREACTIONS()
		}
	}
	return last
}

// counts are the reactions of r by emoji, and the viewer's own in order.
func counts(r *mtproto.MessageReactions) (map[string]int32, []string) {
	n := map[string]int32{}
	mine := map[int32]string{}
	for _, c := range r.GetResults() {
		n[c.GetReaction_REACTION().GetEmoticon()] = c.GetCount()
		if c.GetChosenOrder() != nil {
			mine[c.GetChosenOrder().GetValue()] = c.GetReaction_REACTION().GetEmoticon()
		}
	}
	var own []string
	for i := range int32(len(mine)) {
		own = append(own, mine[i])
	}
	return n, own
}

// otherUpdates are the other_updates of u's updates.getDifference from st,
// following slices.
func otherUpdates(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState) []*mtproto.Update {
	t.Helper()
	var all []*mtproto.Update
	pts, date := st.GetPts(), st.GetDate()
	for range 100 {
		o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
			Constructor: u.c.Constructor(mtproto.Predicate_updates_getDifference),
			Pts:         pts,
			Date:        date,
			Qts:         st.GetQts(),
		})
		if err != nil {
			t.Fatalf("updates.getDifference: %v", err)
		}
		d, ok := o.(interface {
			To_Updates_Difference() *mtproto.Updates_Difference
		})
		if !ok {
			t.Fatalf("updates.getDifference returned %T", o)
		}
		diff := d.To_Updates_Difference()
		all = append(all, diff.GetOtherUpdates()...)
		switch diff.GetPredicateName() {
		case mtproto.Predicate_updates_differenceSlice:
			pts, date = diff.GetIntermediateState().GetPts(), diff.GetIntermediateState().GetDate()
		default:
			return all
		}
	}
	t.Fatal("updates.getDifference: too many slices")
	return nil
}

// Reactions of a private chat (task 053): the author's devices and the
// other side's get them in their view, pushed and through getDifference;
// the author sees them unread until readMessageContents; a user puts up to
// three, in its order; getMessagesReactions answers every message asked.
func TestReactionsPrivate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	before := getState(ctx, t, laptop)

	toBob := inputPeerOf(ctx, t, alice, bob)
	toAlice := inputPeerOf(ctx, t, bob, alice)
	text := "react to me"
	sentId := sendForReactions(ctx, t, alice, toBob, text)
	bobsId := waitMessageId(ctx, t, bob, text)

	// Bob reacts with two, in his order.
	r, err := sendReaction(ctx, bob, toAlice, bobsId, "🔥", "👍")
	if err != nil {
		t.Fatalf("messages.sendReaction: %v", err)
	}
	if n, own := counts(reactionsOf(r.GetUpdates(), bobsId)); n["🔥"] != 1 || n["👍"] != 1 || len(own) != 2 || own[0] != "🔥" || own[1] != "👍" {
		t.Fatalf("bob's answer: %v, own %v", n, own)
	}

	// Alice's phone gets them pushed, unread, none her own.
	var pushed *mtproto.MessageReactions
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateMessageReactions, func(u *mtproto.Update) bool {
		if u.GetMsgId_INT32() == sentId && len(u.GetReactions_MESSAGEREACTIONS().GetResults()) == 2 {
			pushed = u.GetReactions_MESSAGEREACTIONS()
			return true
		}
		return false
	})
	if _, own := counts(pushed); len(own) != 0 || pushed.GetMin() {
		t.Errorf("alice's view: own %v, min %v", own, pushed.GetMin())
	}
	unread := 0
	for _, p := range pushed.GetRecentReactions() {
		if p.GetUnread() {
			unread++
		}
	}
	if unread != 2 {
		t.Errorf("%d unread reactions for the author, want 2", unread)
	}
	if n := peerDialogs(ctx, t, alice, toBob).GetDialogs()[0].GetUnreadReactionsCount(); n != 1 {
		t.Errorf("unread_reactions_count %d, want 1", n)
	}

	// The laptop learns them from getDifference.
	if got := reactionsOf(otherUpdates(ctx, t, laptop, before), sentId); len(got.GetResults()) != 2 {
		t.Errorf("getDifference: reactions %v", got)
	}

	// A fourth reaction is too many; the same list changes nothing.
	if _, err = sendReaction(ctx, bob, toAlice, bobsId, "🔥", "👍", "❤", "😂"); !mtclient.IsRPCError(err, "REACTIONS_TOO_MANY") {
		t.Errorf("four reactions: %v, want REACTIONS_TOO_MANY", err)
	}

	// Reading the message's contents marks the reactions seen, and
	// journals nothing: the message is Alice's own, and an
	// updateReadMessagesContents would mark its media listened to.
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesReadMessageContents{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_readMessageContents),
		Id:          []int32{sentId},
	})
	if err != nil {
		t.Fatalf("messages.readMessageContents: %v", err)
	}
	if n := o.(interface {
		To_Messages_AffectedMessages() *mtproto.Messages_AffectedMessages
	}).To_Messages_AffectedMessages().GetPtsCount(); n != 0 {
		t.Errorf("reading the reactions to an own message journaled %d pts", n)
	}
	if n := peerDialogs(ctx, t, alice, toBob).GetDialogs()[0].GetUnreadReactionsCount(); n != 0 {
		t.Errorf("unread_reactions_count %d after reading, want 0", n)
	}

	// getMessagesReactions: every message asked, an empty object for one
	// without reactions.
	plain := sendForReactions(ctx, t, alice, toBob, "no reactions here")
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessagesReactions),
		Peer:        toBob,
		Id:          []int32{sentId, plain},
	})
	if err != nil {
		t.Fatalf("messages.getMessagesReactions: %v", err)
	}
	ups := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates()
	if got := reactionsOf(ups, sentId); len(got.GetResults()) != 2 {
		t.Errorf("reactions of the message: %v", got)
	}
	if got := reactionsOf(ups, plain); got == nil || len(got.GetResults()) != 0 {
		t.Errorf("reactions of the plain message: %v, want an empty object", got)
	}

	// Taking all back leaves an empty object with the other side.
	if _, err = sendReaction(ctx, bob, toAlice, bobsId); err != nil {
		t.Fatalf("messages.sendReaction with none: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateMessageReactions, func(u *mtproto.Update) bool {
		return u.GetMsgId_INT32() == sentId && len(u.GetReactions_MESSAGEREACTIONS().GetResults()) == 0
	})
}

// Tags of Saved Messages (task 053): reactions in the chat with oneself are
// tags, for everyone; a title and a search by tag.
func TestSavedReactionTags(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	getState(ctx, t, alice)
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	tagged := sendForReactions(ctx, t, alice, self, "a tagged note")
	sendForReactions(ctx, t, alice, self, "an untagged note")

	r, err := sendReaction(ctx, alice, self, tagged, "👍")
	if err != nil {
		t.Fatalf("messages.sendReaction in Saved Messages: %v", err)
	}
	if got := reactionsOf(r.GetUpdates(), tagged); !got.GetReactionsAsTags() {
		t.Errorf("tags: %v, want reactions_as_tags", got)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesUpdateSavedReactionTag{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_updateSavedReactionTag),
		Reaction:    emojiReaction(alice, "👍"),
		Title:       mtproto.MakeFlagsString("todo"),
	}); err != nil {
		t.Fatalf("messages.updateSavedReactionTag: %v", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetSavedReactionTags{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getSavedReactionTags),
	})
	if err != nil {
		t.Fatalf("messages.getSavedReactionTags: %v", err)
	}
	tags := o.(interface {
		To_Messages_SavedReactionTags() *mtproto.Messages_SavedReactionTags
	}).To_Messages_SavedReactionTags()
	if len(tags.GetTags()) != 1 || tags.GetTags()[0].GetCount() != 1 || tags.GetTags()[0].GetTitle().GetValue() != "todo" {
		t.Fatalf("tags %v", tags.GetTags())
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSearch{
		Constructor:   alice.c.Constructor(mtproto.Predicate_messages_search),
		Peer:          self,
		SavedReaction: []*mtproto.Reaction{emojiReaction(alice, "👍")},
		Filter:        &mtproto.MessagesFilter{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
		Limit:         20,
	})
	if err != nil {
		t.Fatalf("messages.search by tag: %v", err)
	}
	found := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
	if len(found) != 1 || found[0].GetId() != tagged {
		t.Errorf("found %d messages, want the tagged one", len(found))
	}
}
