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
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func selfPeer() *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
}

// invokeInts invokes a method that answers Vector<int>.
func invokeInts(ctx context.Context, t *testing.T, u *user, method string, req mtproto.TLObject) []int32 {
	t.Helper()
	raw, err := u.c.InvokeRaw(ctx, req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if len(raw) < 8 || mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(raw))) != mtproto.CRC32_vector {
		t.Fatalf("%s: not a vector (%d bytes)", method, len(raw))
	}
	n := int(binary.LittleEndian.Uint32(raw[4:]))
	if len(raw) != 8+4*n {
		t.Fatalf("%s: a vector of %d ints in %d bytes", method, n, len(raw))
	}
	out := make([]int32, n)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32(raw[8+4*i:]))
	}
	return out
}

func storyRule(predicate string, users ...*mtproto.InputUser) *mtproto.InputPrivacyRule {
	return &mtproto.InputPrivacyRule{PredicateName: predicate, Users: users}
}

// sendStory posts a photo story of u to peer and returns its id with the
// story the answer carries.
func sendStory(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, randomId int64, caption string, rules ...*mtproto.InputPrivacyRule) (int32, *mtproto.StoryItem) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor:  u.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:         peer,
		Media:        uploadPhoto(ctx, t, u),
		Caption:      mtproto.MakeFlagsString(caption),
		PrivacyRules: rules,
		RandomId:     randomId,
	})
	if err != nil {
		t.Fatalf("stories.sendStory: %v", err)
	}
	var (
		id    int32
		story *mtproto.StoryItem
	)
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateStoryID:
			if x.GetRandomId() != randomId {
				t.Errorf("updateStoryID of random_id %d, want %d", x.GetRandomId(), randomId)
			}
			id = x.GetId_INT32()
		case mtproto.Predicate_updateStory:
			story = x.GetStory()
		}
	}
	if id == 0 || story.GetId() != id || story.GetPredicateName() != mtproto.Predicate_storyItem {
		t.Fatalf("stories.sendStory: id %d, story %v", id, story)
	}
	return id, story
}

func peerStories(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) *mtproto.PeerStories {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLStoriesGetPeerStories{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_getPeerStories),
		Peer:        peer,
	})
	if err != nil {
		t.Fatalf("stories.getPeerStories: %v", err)
	}
	return o.(interface {
		To_Stories_PeerStories() *mtproto.Stories_PeerStories
	}).To_Stories_PeerStories().GetStories()
}

func allStories(ctx context.Context, t *testing.T, u *user, hidden bool, state string) *mtproto.Stories_AllStories {
	t.Helper()
	req := &mtproto.TLStoriesGetAllStories{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_getAllStories),
		Hidden:      hidden,
	}
	if state != "" {
		req.State = mtproto.MakeFlagsString(state)
	}
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("stories.getAllStories: %v", err)
	}
	r, ok := o.(interface {
		To_Stories_AllStories() *mtproto.Stories_AllStories
	})
	if !ok {
		t.Fatalf("stories.getAllStories returned %T", o)
	}
	return r.To_Stories_AllStories()
}

func storiesOf(ctx context.Context, t *testing.T, u *user, method string, req mtproto.TLObject) *mtproto.Stories_Stories {
	t.Helper()
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return o.(interface {
		To_Stories_Stories() *mtproto.Stories_Stories
	}).To_Stories_Stories()
}

func storyIds(items []*mtproto.StoryItem) []int32 {
	ids := make([]int32, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.GetId())
	}
	return ids
}

// storiesMaxId is user.stories_max_id of of as u sees it, 0 for none.
func storiesMaxId(ctx context.Context, t *testing.T, u, of *user) int32 {
	t.Helper()
	_, shown := fullUser(ctx, t, u, inputUserOf(ctx, t, u, of))
	return shown.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue()
}

// An account without stories: the lists are empty and complete (Android
// showed "1 Story" in the archive row while it had no answer), and nothing
// may be posted without the media.
func TestStoriesEmpty(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	for _, hidden := range []bool{false, true} {
		if all := allStories(ctx, t, alice, hidden, ""); all.GetPredicateName() != mtproto.Predicate_stories_allStories || all.GetHasMore() || all.GetCount() != 0 || all.GetStealthMode() == nil {
			t.Errorf("stories.getAllStories, hidden %v: %v", hidden, all)
		}
	}
	archive := storiesOf(ctx, t, alice, "stories.getStoriesArchive", &mtproto.TLStoriesGetStoriesArchive{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoriesArchive),
		Peer:        selfPeer(),
		Limit:       10,
	})
	if archive.GetCount() != 0 || len(archive.GetStories()) != 0 {
		t.Errorf("the archive of a new account: %v", archive)
	}
	if ps := peerStories(ctx, t, alice, selfPeer()); len(ps.GetStories()) != 0 {
		t.Errorf("the stories of a new account: %v", ps)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesCanSendStory30EB63F0{
		Constructor: mtproto.CRC32_stories_canSendStory30EB63F0,
		Peer:        selfPeer(),
	})
	if err != nil {
		t.Fatalf("stories.canSendStory: %v", err)
	}
	if n := o.(interface {
		To_Stories_CanSendStoryCount() *mtproto.Stories_CanSendStoryCount
	}).To_Stories_CanSendStoryCount().GetCountRemains(); n != 100 {
		t.Errorf("stories.canSendStory: %d stories remain, want 100", n)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor:  alice.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:         selfPeer(),
		Media:        mtproto.MakeTLInputMediaEmpty(nil).To_InputMedia(),
		PrivacyRules: []*mtproto.InputPrivacyRule{storyRule(mtproto.Predicate_inputPrivacyValueAllowAll)},
		RandomId:     randomID(),
	}); !mtclient.IsRPCError(err, "MEDIA_EMPTY") {
		t.Errorf("a story without media: %v, want MEDIA_EMPTY", err)
	}
	// What is not served answers 400, which a client gives up on.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLStoriesGetAlbums{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getAlbums),
		Peer:        selfPeer(),
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("stories.getAlbums: %v, want ERR_ENTERPRISE_IS_BLOCKED", err)
	}
}

// The life of a user's story (task 084): posted for the contacts, it
// reaches a contact who is online and its feeds, shows as stories_max_id
// to those it is for and to nobody else, counts who watched it and their
// reactions, stays in the profile when pinned, leaves those an edit of its
// rules shuts out, and is deleted.
func TestStoriesOfUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, laptop)
	getState(ctx, t, bob)
	getState(ctx, t, alice)

	// Bob is Alice's contact (her stories are for him) and Alice is Bob's
	// (they are in his feeds); Carol is nobody's.
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, bob.phone, "Bob", "E2E"))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, alice.phone, "Alice", "E2E"))
	aliceForBob := peerOf(ctx, t, bob, alice)
	aliceForCarol := peerOf(ctx, t, carol, alice)

	randomId := randomID()
	id, story := sendStory(ctx, t, alice, selfPeer(), randomId, "hello", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts))
	if !story.GetOut() || !story.GetContacts() || story.GetPublic() || story.GetCaption().GetValue() != "hello" || len(story.GetPrivacy()) != 1 ||
		story.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto || story.GetExpireDate()-story.GetDate() != 86400 {
		t.Errorf("the story its author gets: %v", story)
	}
	// The same request again answers the same story.
	if again, _ := sendStory(ctx, t, alice, selfPeer(), randomId, "hello", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts)); again != id {
		t.Errorf("a repeated stories.sendStory made story %d, the first was %d", again, id)
	}
	inFeed := func(u *mtproto.Update) bool {
		return u.GetPeer_PEER().GetUserId() == alice.self.GetId() && u.GetStory().GetId() == id
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return inFeed(u) && u.GetStory().GetPredicateName() == mtproto.Predicate_storyItem && !u.GetStory().GetOut() && u.GetStory().GetPrivacy() == nil
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool { return inFeed(u) && u.GetStory().GetOut() })

	// The feeds of Bob, and their state.
	all := allStories(ctx, t, bob, false, "")
	if all.GetCount() != 1 || len(all.GetPeerStories()) != 1 || all.GetPeerStories()[0].GetPeer().GetUserId() != alice.self.GetId() ||
		!slices.Equal(storyIds(all.GetPeerStories()[0].GetStories()), []int32{id}) || all.GetState() == "" || all.GetHasMore() {
		t.Fatalf("the feeds of a contact: %v", all)
	}
	if same := allStories(ctx, t, bob, false, all.GetState()); same.GetPredicateName() != mtproto.Predicate_stories_allStoriesNotModified || same.GetState() != all.GetState() {
		t.Errorf("the feeds by their state: %v", same)
	}
	if hidden := allStories(ctx, t, bob, true, ""); hidden.GetCount() != 0 {
		t.Errorf("the archive row of a contact: %v", hidden)
	}
	if own := allStories(ctx, t, alice, false, ""); own.GetCount() != 1 || !own.GetPeerStories()[0].GetStories()[0].GetOut() {
		t.Errorf("the author's own feeds: %v", own)
	}
	if strangers := allStories(ctx, t, carol, false, ""); strangers.GetCount() != 0 {
		t.Errorf("the feeds of a stranger: %v", strangers)
	}

	// The ring of the story: for the contact and the author, not for a
	// stranger, who is shown no story either.
	if got := storiesMaxId(ctx, t, bob, alice); got != id {
		t.Errorf("user.stories_max_id for a contact: %d, want %d", got, id)
	}
	if got := storiesMaxId(ctx, t, carol, alice); got != 0 {
		t.Errorf("user.stories_max_id for a stranger: %d", got)
	}
	if ps := peerStories(ctx, t, carol, aliceForCarol); len(ps.GetStories()) != 0 {
		t.Errorf("a stranger reads the story: %v", ps)
	}
	full, _ := fullUser(ctx, t, bob, inputUserOf(ctx, t, bob, alice))
	if !slices.Equal(storyIds(full.GetStories_FLAGPEERSTORIES().GetStories()), []int32{id}) {
		t.Errorf("userFull.stories for a contact: %v", full.GetStories_FLAGPEERSTORIES())
	}
	if ids := invokeInts(ctx, t, bob, "stories.getPeerMaxIDs", &mtproto.TLStoriesGetPeerMaxIDs535983C3{
		Constructor: mtproto.CRC32_stories_getPeerMaxIDs535983C3,
		Id:          []*mtproto.InputPeer{aliceForBob, selfPeer()},
	}); !slices.Equal(ids, []int32{id, 0}) {
		t.Errorf("stories.getPeerMaxIDs: %v", ids)
	}

	// Bob watches it: read on his side, a viewer on Alice's.
	if read := invokeInts(ctx, t, bob, "stories.readStories", &mtproto.TLStoriesReadStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_readStories),
		Peer:        aliceForBob,
		MaxId:       id,
	}); !slices.Equal(read, []int32{id}) {
		t.Errorf("stories.readStories: %v", read)
	}
	if ps := peerStories(ctx, t, bob, aliceForBob); ps.GetMaxReadId().GetValue() != id {
		t.Errorf("max_read_id after the read: %v", ps.GetMaxReadId())
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLStoriesSendReaction{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_sendReaction),
		Peer:        aliceForBob,
		StoryId:     id,
		Reaction:    mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: "❤"}).To_Reaction(),
	}); err != nil {
		t.Fatalf("stories.sendReaction: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewStoryReaction, func(u *mtproto.Update) bool {
		return u.GetStoryId() == id && u.GetPeer_PEER().GetUserId() == bob.self.GetId() && u.GetReaction().GetEmoticon() == "❤"
	})
	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesGetStoryViewsList{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoryViewsList),
		Peer:        selfPeer(),
		Id:          id,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("stories.getStoryViewsList: %v", err)
	}
	viewers := o.(interface {
		To_Stories_StoryViewsList() *mtproto.Stories_StoryViewsList
	}).To_Stories_StoryViewsList()
	if viewers.GetCount() != 1 || viewers.GetViewsCount() != 1 || viewers.GetReactionsCount() != 1 || len(viewers.GetViews()) != 1 ||
		viewers.GetViews()[0].GetUserId() != bob.self.GetId() || viewers.GetViews()[0].GetReaction().GetEmoticon() != "❤" || viewers.GetNextOffset() != nil {
		t.Errorf("the viewers of the story: %v", viewers)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLStoriesGetStoryViewsList{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_getStoryViewsList),
		Peer:        aliceForBob,
		Id:          id,
		Limit:       20,
	}); err == nil {
		t.Errorf("a viewer reads who else watched the story")
	}
	if ps := peerStories(ctx, t, bob, aliceForBob); ps.GetStories()[0].GetSentReaction().GetEmoticon() != "❤" || ps.GetStories()[0].GetViews() != nil {
		t.Errorf("the story for the one who reacted: %v", ps.GetStories()[0])
	}

	// A report of the story walks the tree of reasons into the queue.
	o, err = bob.c.Invoke(ctx, &mtproto.TLStoriesReport19D8EB45{
		Constructor: mtproto.CRC32_stories_report19D8EB45,
		Peer:        aliceForBob,
		Id:          []int32{id},
	})
	if err != nil {
		t.Fatalf("stories.report: %v", err)
	}
	if root := o.(interface{ To_ReportResult() *mtproto.ReportResult }).To_ReportResult(); root.GetPredicateName() != mtproto.Predicate_reportResultChooseOption || len(root.GetOptions()) == 0 {
		t.Errorf("stories.report without an option: %v", root)
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLStoriesReport19D8EB45{
		Constructor: mtproto.CRC32_stories_report19D8EB45,
		Peer:        aliceForBob,
		Id:          []int32{id},
		Option:      []byte("spam:c"),
	})
	if err != nil {
		t.Fatalf("stories.report: %v", err)
	}
	if done := o.(interface{ To_ReportResult() *mtproto.ReportResult }).To_ReportResult(); done.GetPredicateName() != mtproto.Predicate_reportResultReported {
		t.Errorf("stories.report with a reason: %v", done)
	}

	// Pinned, it is in the profile; a link needs a public username.
	if pinned := invokeInts(ctx, t, alice, "stories.togglePinned", &mtproto.TLStoriesTogglePinned{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_togglePinned),
		Peer:        selfPeer(),
		Id:          []int32{id},
		Pinned:      mtproto.BoolTrue,
	}); !slices.Equal(pinned, []int32{id}) {
		t.Errorf("stories.togglePinned: %v", pinned)
	}
	profile := storiesOf(ctx, t, bob, "stories.getPinnedStories", &mtproto.TLStoriesGetPinnedStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_getPinnedStories),
		Peer:        aliceForBob,
		Limit:       10,
	})
	if profile.GetCount() != 1 || !slices.Equal(storyIds(profile.GetStories()), []int32{id}) || !profile.GetStories()[0].GetPinned() {
		t.Errorf("the profile for a contact: %v", profile)
	}
	if profile = storiesOf(ctx, t, carol, "stories.getPinnedStories", &mtproto.TLStoriesGetPinnedStories{
		Constructor: carol.c.Constructor(mtproto.Predicate_stories_getPinnedStories),
		Peer:        aliceForCarol,
		Limit:       10,
	}); profile.GetCount() != 0 || len(profile.GetStories()) != 0 {
		t.Errorf("the profile for a stranger: %v", profile)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLStoriesExportStoryLink{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_exportStoryLink),
		Peer:        selfPeer(),
		Id:          id,
	}); !mtclient.IsRPCError(err, "USER_PUBLIC_MISSING") {
		t.Errorf("the link of a story without a username: %v", err)
	}

	// Bob shares the story to Carol and replies to it; a story that is not
	// for Carol is not shared by her.
	carolForBob := peerOf(ctx, t, bob, carol)
	share := &mtproto.TLMessagesSendMedia{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        carolForBob,
		Media:       mtproto.MakeTLInputMediaStory(&mtproto.InputMedia{Peer: aliceForBob, Id_INT32: id}).To_InputMedia(),
		RandomId:    randomID(),
	}
	o, err = bob.c.Invoke(ctx, share)
	if err != nil {
		t.Fatalf("a story shared to a chat: %v", err)
	}
	// The same request again (a resend of the client) is the same message.
	if _, err = bob.c.Invoke(ctx, share); err != nil {
		t.Fatalf("a story shared to a chat again: %v", err)
	}
	shared := newMessages(o.(interface{ To_Updates() *mtproto.Updates }).To_Updates())
	if len(shared) != 1 || shared[0].GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaStory ||
		shared[0].GetMedia().GetPeer().GetUserId() != alice.self.GetId() || shared[0].GetMedia().GetId() != id || shared[0].GetMedia().GetViaMention() {
		t.Errorf("the message of a shared story: %v", shared)
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        aliceForBob,
		Message:     "nice",
		RandomId:    randomID(),
		ReplyTo:     mtproto.MakeTLInputReplyToStory(&mtproto.InputReplyTo{Peer: aliceForBob, StoryId: id}).To_InputReplyTo(),
	})
	if err != nil {
		t.Fatalf("a reply to a story: %v", err)
	}
	if reply := newMessages(o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()); len(reply) != 1 ||
		reply[0].GetReplyTo().GetPredicateName() != mtproto.Predicate_messageReplyStoryHeader || reply[0].GetReplyTo().GetStoryId() != id ||
		reply[0].GetReplyTo().GetPeer().GetUserId() != alice.self.GetId() {
		t.Errorf("the reply to a story: %v", reply)
	}
	if _, err = carol.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: carol.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peerOf(ctx, t, carol, bob),
		Media:       mtproto.MakeTLInputMediaStory(&mtproto.InputMedia{Peer: aliceForCarol, Id_INT32: id}).To_InputMedia(),
		RandomId:    randomID(),
	}); !mtclient.IsRPCError(err, "STORY_ID_INVALID") {
		t.Errorf("a stranger shares the story: %v, want STORY_ID_INVALID", err)
	}
	// One message shared it: the resend and the refused share count
	// nothing.
	o, err = alice.c.Invoke(ctx, &mtproto.TLStoriesGetStoryViewsList{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoryViewsList),
		Peer:        selfPeer(),
		Id:          id,
		Offset:      "",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("stories.getStoryViewsList: %v", err)
	}
	if n := o.(interface {
		To_Stories_StoryViewsList() *mtproto.Stories_StoryViewsList
	}).To_Stories_StoryViewsList().GetForwardsCount(); n != 1 {
		t.Errorf("the shares of the story: %d, want 1", n)
	}

	// An edit of the rules to "only myself" takes the story from Bob. The
	// caption goes with its entities, as the clients send them (Android
	// tgnet/tl/TL_stories.java:472-477).
	if _, err = alice.c.Invoke(ctx, &mtproto.TLStoriesEditStory{
		Constructor:  alice.c.Constructor(mtproto.Predicate_stories_editStory),
		Peer:         selfPeer(),
		Id:           id,
		Caption:      mtproto.MakeFlagsString("edited"),
		Entities:     []*mtproto.MessageEntity{}, // one flag carries the caption and its entities
		PrivacyRules: []*mtproto.InputPrivacyRule{storyRule(mtproto.Predicate_inputPrivacyValueAllowUsers, mtproto.MakeTLInputUserSelf(nil).To_InputUser())},
	}); err != nil {
		t.Fatalf("stories.editStory: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return inFeed(u) && u.GetStory().GetPredicateName() == mtproto.Predicate_storyItemDeleted
	})
	if ps := peerStories(ctx, t, bob, aliceForBob); len(ps.GetStories()) != 0 {
		t.Errorf("the story after its rules shut the contact out: %v", ps)
	}
	if got := storiesMaxId(ctx, t, bob, alice); got != 0 {
		t.Errorf("user.stories_max_id after the rules shut the contact out: %d", got)
	}
	if own := peerStories(ctx, t, alice, selfPeer()); len(own.GetStories()) != 1 || !own.GetStories()[0].GetEdited() || own.GetStories()[0].GetCaption().GetValue() != "edited" || !own.GetStories()[0].GetSelectedContacts() {
		t.Errorf("the edited story for its author: %v", own)
	}

	// Deleted, it is gone from the archive too.
	if deleted := invokeInts(ctx, t, alice, "stories.deleteStories", &mtproto.TLStoriesDeleteStories{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_deleteStories),
		Peer:        selfPeer(),
		Id:          []int32{id, 999},
	}); !slices.Equal(deleted, []int32{id}) {
		t.Errorf("stories.deleteStories: %v", deleted)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return inFeed(u) && u.GetStory().GetPredicateName() == mtproto.Predicate_storyItemDeleted
	})
	if archive := storiesOf(ctx, t, alice, "stories.getStoriesArchive", &mtproto.TLStoriesGetStoriesArchive{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoriesArchive),
		Peer:        selfPeer(),
		Limit:       10,
	}); archive.GetCount() != 0 {
		t.Errorf("the archive after the deletion: %v", archive)
	}
	if got := storiesMaxId(ctx, t, alice, alice); got != 0 {
		t.Errorf("the author's own stories_max_id after the deletion: %d", got)
	}
}

// The stealth mode and the archive row: the mode answers its dates and
// refuses a second use while it cools down; a contact moved to the archive
// row is in the hidden list and comes with stories_hidden.
func TestStoriesStealthAndHidden(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, bob.phone, "Bob", "E2E"))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, alice.phone, "Alice", "E2E"))
	aliceForBob := peerOf(ctx, t, bob, alice)
	id, _ := sendStory(ctx, t, alice, selfPeer(), randomID(), "", storyRule(mtproto.Predicate_inputPrivacyValueAllowAll))

	activate := &mtproto.TLStoriesActivateStealthMode{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_activateStealthMode),
		Past:        true,
		Future:      true,
	}
	o, err := bob.c.Invoke(ctx, activate)
	if err != nil {
		t.Fatalf("stories.activateStealthMode: %v", err)
	}
	var mode *mtproto.StoriesStealthMode
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateStoriesStealthMode {
			mode = x.GetStealthMode()
		}
	}
	now := int32(time.Now().Unix())
	if active, cooldown := mode.GetActiveUntilDate().GetValue(), mode.GetCooldownUntilDate().GetValue(); active < now+24*60 || active > now+26*60 || cooldown < now+59*60 || cooldown > now+61*60 {
		t.Errorf("the stealth mode: active until %d, cooldown until %d, now %d", active, cooldown, now)
	}
	if _, err = bob.c.Invoke(ctx, activate); err == nil {
		t.Errorf("the stealth mode turned on twice")
	}
	if all := allStories(ctx, t, bob, false, ""); all.GetStealthMode().GetActiveUntilDate().GetValue() != mode.GetActiveUntilDate().GetValue() {
		t.Errorf("the stealth mode in the feeds: %v", all.GetStealthMode())
	}
	// Watched in stealth: read for Bob, no viewer for Alice.
	if read := invokeInts(ctx, t, bob, "stories.readStories", &mtproto.TLStoriesReadStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_readStories),
		Peer:        aliceForBob,
		MaxId:       id,
	}); !slices.Equal(read, []int32{id}) {
		t.Errorf("stories.readStories in stealth: %v", read)
	}
	if own := peerStories(ctx, t, alice, selfPeer()); own.GetStories()[0].GetViews().GetViewsCount() != 0 {
		t.Errorf("a view in stealth is counted: %v", own.GetStories()[0].GetViews())
	}

	if _, err = bob.c.Invoke(ctx, &mtproto.TLStoriesTogglePeerStoriesHidden{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_togglePeerStoriesHidden),
		Peer:        aliceForBob,
		Hidden:      mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("stories.togglePeerStoriesHidden: %v", err)
	}
	if shown := allStories(ctx, t, bob, false, ""); shown.GetCount() != 0 {
		t.Errorf("the feeds after the contact was hidden: %v", shown)
	}
	hidden := allStories(ctx, t, bob, true, "")
	if hidden.GetCount() != 1 || len(hidden.GetUsers()) == 0 {
		t.Fatalf("the archive row: %v", hidden)
	}
	for _, u := range hidden.GetUsers() {
		if u.GetId() == alice.self.GetId() && !u.GetStoriesHidden() {
			t.Errorf("the hidden contact comes without stories_hidden")
		}
	}
}

// The stories of a channel: an admin with the right posts them, a member
// does not; everyone in the channel has them in the feeds and in the full
// channel, with the counters; the channel's object carries stories_max_id.
func TestStoriesOfChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	_, ch := createChannel(ctx, t, alice, "Stories", false)
	peer := channelInputPeer(ch)
	input := mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}).To_InputChannel()
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     input,
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesGetChatsToSend{Constructor: alice.c.Constructor(mtproto.Predicate_stories_getChatsToSend)})
	if err != nil {
		t.Fatalf("stories.getChatsToSend: %v", err)
	}
	if chats := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	}).To_Messages_Chats().GetChats(); len(chats) != 1 || chats[0].GetId() != ch.GetId() {
		t.Errorf("stories.getChatsToSend of the creator: %v", chats)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:        peer,
		Media:       uploadPhoto(ctx, t, bob),
		RandomId:    randomID(),
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member posts a story of the channel: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	id, story := sendStory(ctx, t, alice, peer, randomID(), "news")
	if !story.GetOut() || !story.GetPublic() || story.GetPrivacy() != nil || story.GetViews() == nil {
		t.Errorf("the channel's story for its author: %v", story)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetPeer_PEER().GetChannelId() == ch.GetId() && u.GetStory().GetId() == id && !u.GetStory().GetOut()
	})
	all := allStories(ctx, t, bob, false, "")
	if all.GetCount() != 1 || all.GetPeerStories()[0].GetPeer().GetChannelId() != ch.GetId() || len(all.GetChats()) != 1 ||
		all.GetChats()[0].GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != id {
		t.Fatalf("the feeds of a member: %v", all)
	}
	if read := invokeInts(ctx, t, bob, "stories.readStories", &mtproto.TLStoriesReadStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_readStories),
		Peer:        peer,
		MaxId:       id,
	}); !slices.Equal(read, []int32{id}) {
		t.Errorf("stories.readStories of a channel: %v", read)
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input,
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat()
	if st := full.GetStories(); !slices.Equal(storyIds(st.GetStories()), []int32{id}) || st.GetMaxReadId().GetValue() != id || st.GetStories()[0].GetViews().GetViewsCount() != 1 {
		t.Errorf("channelFull.stories for a member: %v", st)
	}
	// The hidden channel moves to the archive row, with its flag.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLStoriesTogglePeerStoriesHidden{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_togglePeerStoriesHidden),
		Peer:        peer,
		Hidden:      mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("stories.togglePeerStoriesHidden: %v", err)
	}
	if hidden := allStories(ctx, t, bob, true, ""); hidden.GetCount() != 1 || len(hidden.GetChats()) != 1 || !hidden.GetChats()[0].GetStoriesHidden() {
		t.Errorf("the archive row with the channel: %v", hidden)
	}
	// A member deletes nothing; the creator does.
	if _, err = bob.c.InvokeRaw(ctx, &mtproto.TLStoriesDeleteStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_deleteStories),
		Peer:        peer,
		Id:          []int32{id},
	}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member deletes a story of the channel: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if deleted := invokeInts(ctx, t, alice, "stories.deleteStories", &mtproto.TLStoriesDeleteStories{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_deleteStories),
		Peer:        peer,
		Id:          []int32{id},
	}); !slices.Equal(deleted, []int32{id}) {
		t.Errorf("stories.deleteStories of a channel: %v", deleted)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetPeer_PEER().GetChannelId() == ch.GetId() && u.GetStory().GetPredicateName() == mtproto.Predicate_storyItemDeleted
	})
}

// A story that mentions a user leaves that user a message from the author
// with the story (messageMediaStory, via_mention).
func TestStoryMention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:        selfPeer(),
		Media:       uploadPhoto(ctx, t, alice),
		Caption:     mtproto.MakeFlagsString("with Bob"),
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{
			Offset:           5,
			Length:           3,
			UserId_INPUTUSER: inputUserOf(ctx, t, alice, bob),
		}).To_MessageEntity()},
		PrivacyRules: []*mtproto.InputPrivacyRule{storyRule(mtproto.Predicate_inputPrivacyValueAllowAll)},
		RandomId:     randomID(),
	})
	if err != nil {
		t.Fatalf("stories.sendStory: %v", err)
	}
	var id int32
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateStory {
			id = x.GetStory().GetId()
			if e := x.GetStory().GetEntities(); len(e) != 1 || e[0].GetPredicateName() != mtproto.Predicate_messageEntityMentionName || e[0].GetUserId_INT64() != bob.self.GetId() {
				t.Errorf("the entities of the story: %v", e)
			}
		}
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, m := range newMessages(us) {
			if media := m.GetMedia(); media.GetPredicateName() == mtproto.Predicate_messageMediaStory && media.GetViaMention() &&
				media.GetPeer().GetUserId() == alice.self.GetId() && media.GetId() == id {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no message of the mention: %v", err)
	}
}

// storiesSQL runs a statement in the stack's MySQL and returns its output
// without the column names. The password stays in the container's
// environment.
func storiesSQL(t *testing.T, query string) string {
	t.Helper()
	return strings.TrimSpace(stackOut(t, "exec", "-T", "mysql", "sh", "-c",
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -N -B -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "`+query+`"`))
}

// The rules and the period of a story (task 084): a contact the rules
// except sees nothing of it, another contact sees it; when its period is
// over it leaves the feeds and the ring, the worker takes it out of the
// active ones, and it stays in the author's archive and, pinned, in the
// profile. The period is moved into the past in the stack's MySQL: a test
// does not wait six hours.
func TestStoriesPrivacyAndExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	importContacts(ctx, t, alice,
		phoneContact(alice.c, 1, bob.phone, "Bob", "E2E"),
		phoneContact(alice.c, 2, carol.phone, "Carol", "E2E"))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, alice.phone, "Alice", "E2E"))
	importContacts(ctx, t, carol, phoneContact(carol.c, 1, alice.phone, "Alice", "E2E"))
	aliceForBob := peerOf(ctx, t, bob, alice)
	aliceForCarol := peerOf(ctx, t, carol, alice)

	// Both are Alice's contacts; the rules except Carol.
	exceptCarol := storyRule(mtproto.Predicate_inputPrivacyValueDisallowUsers, inputUserOf(ctx, t, alice, carol))
	pinned, _ := sendStory(ctx, t, alice, selfPeer(), randomID(), "", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts), exceptCarol)
	if ids := invokeInts(ctx, t, alice, "stories.togglePinned", &mtproto.TLStoriesTogglePinned{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_togglePinned),
		Peer:        selfPeer(),
		Id:          []int32{pinned},
		Pinned:      mtproto.BoolTrue,
	}); !slices.Equal(ids, []int32{pinned}) {
		t.Fatalf("stories.togglePinned: %v", ids)
	}
	plain, _ := sendStory(ctx, t, alice, selfPeer(), randomID(), "", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts), exceptCarol)

	if ps := peerStories(ctx, t, bob, aliceForBob); !slices.Equal(storyIds(ps.GetStories()), []int32{pinned, plain}) {
		t.Errorf("the stories for a contact: %v", ps)
	}
	if got := storiesMaxId(ctx, t, bob, alice); got != plain {
		t.Errorf("user.stories_max_id for a contact: %d, want %d", got, plain)
	}
	if ps := peerStories(ctx, t, carol, aliceForCarol); len(ps.GetStories()) != 0 {
		t.Errorf("the stories for the excepted contact: %v", ps)
	}
	if got := storiesMaxId(ctx, t, carol, alice); got != 0 {
		t.Errorf("user.stories_max_id for the excepted contact: %d", got)
	}
	if all := allStories(ctx, t, carol, false, ""); all.GetCount() != 0 {
		t.Errorf("the feeds of the excepted contact: %v", all)
	}
	if got := storiesOf(ctx, t, carol, "stories.getStoriesByID", &mtproto.TLStoriesGetStoriesByID{
		Constructor: carol.c.Constructor(mtproto.Predicate_stories_getStoriesByID),
		Peer:        aliceForCarol,
		Id:          []int32{pinned, plain},
	}); len(got.GetStories()) != 0 {
		t.Errorf("the excepted contact reads the stories by id: %v", got)
	}

	// The period is over.
	storiesSQL(t, fmt.Sprintf("UPDATE stories SET expire_date = UNIX_TIMESTAMP() - 1 WHERE peer_type = 2 AND peer_id = %d", alice.self.GetId()))
	if all := allStories(ctx, t, bob, false, ""); all.GetCount() != 0 {
		t.Errorf("the feeds of a contact after the period: %v", all)
	}
	if ps := peerStories(ctx, t, bob, aliceForBob); len(ps.GetStories()) != 0 {
		t.Errorf("the stories of the contact after the period: %v", ps)
	}
	active := fmt.Sprintf("SELECT COUNT(*) FROM stories WHERE peer_type = 2 AND peer_id = %d AND active = 1", alice.self.GetId())
	for storiesSQL(t, active) != "0" {
		select {
		case <-ctx.Done():
			t.Fatalf("the worker did not take the stories out of the active ones")
		case <-time.After(500 * time.Millisecond):
		}
	}
	// The ring follows once the worker has told the service of users (the
	// period moved in MySQL is not the one the cached user carries).
	// stories_unavailable takes the ring a client kept away.
	if _, shown := fullUser(ctx, t, bob, inputUserOf(ctx, t, bob, alice)); shown.GetStoriesMaxId_FLAGRECENTSTORY() != nil || !shown.GetStoriesUnavailable() {
		t.Errorf("the author after the period: stories_max_id %v, stories_unavailable %v", shown.GetStoriesMaxId_FLAGRECENTSTORY(), shown.GetStoriesUnavailable())
	}
	archive := storiesOf(ctx, t, alice, "stories.getStoriesArchive", &mtproto.TLStoriesGetStoriesArchive{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoriesArchive),
		Peer:        selfPeer(),
		Limit:       10,
	})
	if archive.GetCount() != 2 || !slices.Equal(storyIds(archive.GetStories()), []int32{plain, pinned}) {
		t.Errorf("the archive after the period: %v", archive)
	}
	profile := storiesOf(ctx, t, bob, "stories.getPinnedStories", &mtproto.TLStoriesGetPinnedStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_getPinnedStories),
		Peer:        aliceForBob,
		Limit:       10,
	})
	if !slices.Equal(storyIds(profile.GetStories()), []int32{pinned}) {
		t.Errorf("the profile of the author after the period: %v", profile)
	}
	if profile = storiesOf(ctx, t, carol, "stories.getPinnedStories", &mtproto.TLStoriesGetPinnedStories{
		Constructor: carol.c.Constructor(mtproto.Predicate_stories_getPinnedStories),
		Peer:        aliceForCarol,
		Limit:       10,
	}); len(profile.GetStories()) != 0 {
		t.Errorf("the profile for the excepted contact after the period: %v", profile)
	}
}

// getBlocked is contacts.getBlocked of u: the main blocklist, or with
// stories the blocklist of stories; the ids of its users.
func getBlocked(ctx context.Context, t *testing.T, u *user, stories bool) []int64 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLContactsGetBlocked{
		Constructor:   u.c.Constructor(mtproto.Predicate_contacts_getBlocked),
		MyStoriesFrom: stories,
		Limit:         100,
	})
	if err != nil {
		t.Fatalf("contacts.getBlocked: %v", err)
	}
	var ids []int64
	for _, b := range o.(interface {
		To_Contacts_Blocked() *mtproto.Contacts_Blocked
	}).To_Contacts_Blocked().GetBlocked() {
		ids = append(ids, b.GetPeerId().GetUserId())
	}
	return ids
}

// "Hide my stories from" (contacts.block with my_stories_from, sent from
// the list of viewers, Android StoriesController.java:4483-4513) hides the
// stories only: the user is not blocked. The main blocklist hides the
// stories too, ring included, and a supergroup a story leaves out counts
// its members.
func TestStoriesBlocklists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")
	getState(ctx, t, alice)
	importContacts(ctx, t, alice,
		phoneContact(alice.c, 1, bob.phone, "Bob", "E2E"),
		phoneContact(alice.c, 2, carol.phone, "Carol", "E2E"),
		phoneContact(alice.c, 3, dave.phone, "Dave", "E2E"))
	for _, u := range []*user{bob, carol, dave} {
		importContacts(ctx, t, u, phoneContact(u.c, 1, alice.phone, "Alice", "E2E"))
	}
	aliceForBob := peerOf(ctx, t, bob, alice)
	bobForAlice := inputPeerOf(ctx, t, alice, bob)
	carolForAlice := inputPeerOf(ctx, t, alice, carol)

	// The story is for the contacts but the members of a supergroup Dave
	// is in (iOS names supergroups so, TelegramCore Stories.swift:
	// 1023-1027).
	_, group := createChannel(ctx, t, alice, "stories", true)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: group.GetId(), AccessHash: group.GetAccessHash_FLAGINT64().GetValue()},
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, dave)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	id, _ := sendStory(ctx, t, alice, selfPeer(), randomID(), "",
		storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts),
		&mtproto.InputPrivacyRule{PredicateName: mtproto.Predicate_inputPrivacyValueDisallowChatParticipants, Chats: []int64{group.GetId()}})
	if got := storiesMaxId(ctx, t, dave, alice); got != 0 {
		t.Errorf("user.stories_max_id for a member of the supergroup left out: %d", got)
	}
	if ps := peerStories(ctx, t, dave, peerOf(ctx, t, dave, alice)); len(ps.GetStories()) != 0 {
		t.Errorf("a member of the supergroup left out reads the story: %v", ps)
	}
	if got := storiesMaxId(ctx, t, bob, alice); got != id {
		t.Fatalf("user.stories_max_id for a contact: %d, want %d", got, id)
	}

	// Hidden from Bob: no ring, no story, and Bob is not blocked.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLContactsBlock{
		Constructor:   alice.c.Constructor(mtproto.Predicate_contacts_block),
		MyStoriesFrom: true,
		Id:            bobForAlice,
	}); err != nil {
		t.Fatalf("contacts.block with my_stories_from: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updatePeerBlocked, func(u *mtproto.Update) bool {
		return u.GetPeerId().GetUserId() == bob.self.GetId() && u.GetBlockedMyStoriesFrom() && !u.GetBlocked_FLAGBOOLEAN()
	})
	if got := storiesMaxId(ctx, t, bob, alice); got != 0 {
		t.Errorf("user.stories_max_id for one the stories are hidden from: %d", got)
	}
	if ps := peerStories(ctx, t, bob, aliceForBob); len(ps.GetStories()) != 0 {
		t.Errorf("one the stories are hidden from reads them: %v", ps)
	}
	if main := getBlocked(ctx, t, alice, false); len(main) != 0 {
		t.Errorf("the main blocklist after hiding the stories: %v", main)
	}
	if hidden := getBlocked(ctx, t, alice, true); !slices.Equal(hidden, []int64{bob.self.GetId()}) {
		t.Errorf("the blocklist of stories: %v", hidden)
	}
	if full, _ := fullUser(ctx, t, alice, inputUserOf(ctx, t, alice, bob)); full.GetBlocked() || !full.GetBlockedMyStoriesFrom() {
		t.Errorf("userFull of Bob for Alice: blocked %v, blocked_my_stories_from %v", full.GetBlocked(), full.GetBlockedMyStoriesFrom())
	}
	sendMessageID(ctx, t, bob, aliceForBob, "still here", randomID())
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "still here"
	})

	// Shown again.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLContactsUnblock{
		Constructor:   alice.c.Constructor(mtproto.Predicate_contacts_unblock),
		MyStoriesFrom: true,
		Id:            bobForAlice,
	}); err != nil {
		t.Fatalf("contacts.unblock with my_stories_from: %v", err)
	}
	if got := storiesMaxId(ctx, t, bob, alice); got != id {
		t.Errorf("user.stories_max_id after the stories are shown again: %d, want %d", got, id)
	}
	if hidden := getBlocked(ctx, t, alice, true); len(hidden) != 0 {
		t.Errorf("the blocklist of stories after unhiding: %v", hidden)
	}

	// Blocked, Carol has no ring either; the update says her stories are
	// not hidden apart from that.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLContactsBlock{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_block),
		Id:          carolForAlice,
	}); err != nil {
		t.Fatalf("contacts.block: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updatePeerBlocked, func(u *mtproto.Update) bool {
		return u.GetPeerId().GetUserId() == carol.self.GetId() && u.GetBlocked_FLAGBOOLEAN() && !u.GetBlockedMyStoriesFrom()
	})
	if got := storiesMaxId(ctx, t, carol, alice); got != 0 {
		t.Errorf("user.stories_max_id for a blocked user: %d", got)
	}
	if ids := invokeInts(ctx, t, carol, "stories.getPeerMaxIDs", &mtproto.TLStoriesGetPeerMaxIDs535983C3{
		Constructor: mtproto.CRC32_stories_getPeerMaxIDs535983C3,
		Id:          []*mtproto.InputPeer{peerOf(ctx, t, carol, alice)},
	}); !slices.Equal(ids, []int32{0}) {
		t.Errorf("stories.getPeerMaxIDs for a blocked user: %v", ids)
	}

	// The privacy sheet saves the whole list (contacts.setBlocked); the
	// main blocklist stays as it is, and the update about Carol says she is
	// blocked as well.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLContactsSetBlocked{
		Constructor:   alice.c.Constructor(mtproto.Predicate_contacts_setBlocked),
		MyStoriesFrom: true,
		Id:            []*mtproto.InputPeer{carolForAlice},
		Limit:         1,
	}); err != nil {
		t.Fatalf("contacts.setBlocked with my_stories_from: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updatePeerBlocked, func(u *mtproto.Update) bool {
		return u.GetPeerId().GetUserId() == carol.self.GetId() && u.GetBlocked_FLAGBOOLEAN() && u.GetBlockedMyStoriesFrom()
	})
	if hidden := getBlocked(ctx, t, alice, true); !slices.Equal(hidden, []int64{carol.self.GetId()}) {
		t.Errorf("the blocklist of stories after setBlocked: %v", hidden)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLContactsUnblock{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_unblock),
		Id:          carolForAlice,
	}); err != nil {
		t.Fatalf("contacts.unblock: %v", err)
	}
	// Unblocked, Carol still has her stories hidden.
	if got := storiesMaxId(ctx, t, carol, alice); got != 0 {
		t.Errorf("user.stories_max_id for an unblocked user the stories are hidden from: %d", got)
	}
	if main := getBlocked(ctx, t, alice, false); len(main) != 0 {
		t.Errorf("the main blocklist after the unblock: %v", main)
	}
}
