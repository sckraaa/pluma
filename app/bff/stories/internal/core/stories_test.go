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

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

const testSelf = int64(100)

func testCore() *StoriesCore {
	return &StoriesCore{ctx: context.Background(), Logger: logx.WithContext(context.Background()), MD: &metadata.RpcMetadata{UserId: testSelf}}
}

func inputRule(predicate string, users ...int64) *mtproto.InputPrivacyRule {
	r := &mtproto.InputPrivacyRule{PredicateName: predicate}
	for _, id := range users {
		r.Users = append(r.Users, mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id}).To_InputUser())
	}
	return r
}

func predicates(rules []*mtproto.PrivacyRule) string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.GetPredicateName())
	}
	return strings.Join(names, ",")
}

// The rules of a story: the ones stories have are kept, an empty list of
// names is dropped, and rules that let nobody in or that stories do not
// have are refused.
func TestStoryRules(t *testing.T) {
	for name, tc := range map[string]struct {
		in   []*mtproto.InputPrivacyRule
		want string
		err  error
	}{
		"everyone": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowAll)}, mtproto.Predicate_privacyValueAllowAll, nil},
		"everyone but one": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowAll), inputRule(mtproto.Predicate_inputPrivacyValueDisallowUsers, 7)},
			mtproto.Predicate_privacyValueAllowAll + "," + mtproto.Predicate_privacyValueDisallowUsers, nil},
		"contacts, an empty exception": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowContacts), inputRule(mtproto.Predicate_inputPrivacyValueDisallowUsers)},
			mtproto.Predicate_privacyValueAllowContacts, nil},
		"close friends": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowCloseFriends)}, mtproto.Predicate_privacyValueAllowCloseFriends, nil},
		"chosen users":  {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowUsers, 7, 8)}, mtproto.Predicate_privacyValueAllowUsers, nil},
		"a chat": {[]*mtproto.InputPrivacyRule{{PredicateName: mtproto.Predicate_inputPrivacyValueAllowChatParticipants, Chats: []int64{5}}},
			mtproto.Predicate_privacyValueAllowChatParticipants, nil},
		"nobody":            {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueDisallowAll)}, "", mtproto.ErrPrivacyValueInvalid},
		"no rules":          {nil, "", mtproto.ErrPrivacyValueInvalid},
		"only an exception": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueDisallowUsers, 7)}, "", mtproto.ErrPrivacyValueInvalid},
		"no chosen users":   {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowUsers)}, "", mtproto.ErrPrivacyValueInvalid},
		"a rule of the privacy keys": {[]*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowAll), inputRule(mtproto.Predicate_inputPrivacyValueAllowPremium)},
			"", mtproto.ErrPrivacyValueInvalid},
	} {
		got, err := storyRules(testSelf, tc.in)
		if !errors.Is(err, tc.err) || predicates(got) != tc.want {
			t.Errorf("%s: %q, %v; want %q, %v", name, predicates(got), err, tc.want, tc.err)
		}
	}
	many := make([]int64, maxPrivacyIds+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	if _, err := storyRules(testSelf, []*mtproto.InputPrivacyRule{inputRule(mtproto.Predicate_inputPrivacyValueAllowUsers, many...)}); !errors.Is(err, mtproto.ErrPrivacyTooLong) {
		t.Errorf("too many names: %v", err)
	}
	// inputUserSelf is the account itself: the way iOS says "nobody".
	self := &mtproto.InputPrivacyRule{PredicateName: mtproto.Predicate_inputPrivacyValueAllowUsers,
		Users: []*mtproto.InputUser{mtproto.MakeTLInputUserSelf(nil).To_InputUser()}}
	if got, err := storyRules(testSelf, []*mtproto.InputPrivacyRule{self}); err != nil || len(got) != 1 || len(got[0].GetUsers()) != 1 || got[0].GetUsers()[0] != testSelf {
		t.Errorf("only the account itself: %v, %v", got, err)
	}
}

// The caption of a story: its length and the bounds of its entities are
// counted in UTF-16 units, a mention by input user becomes one by id, and
// the users it mentions are found, the author aside.
func TestStoryCaption(t *testing.T) {
	c := testCore()
	mentionOf := func(id int64, offset, length int32) *mtproto.MessageEntity {
		return mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{
			Offset: offset, Length: length,
			UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id}).To_InputUser(),
		}).To_MessageEntity()
	}
	entities, mentioned, err := c.storyCaption("hi Bob and me", []*mtproto.MessageEntity{
		mentionOf(7, 3, 3),
		mentionOf(testSelf, 11, 2),
		mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 2}).To_MessageEntity(),
	})
	if err != nil || len(entities) != 3 || len(mentioned) != 1 || mentioned[0] != 7 {
		t.Fatalf("entities %v, mentioned %v, %v", entities, mentioned, err)
	}
	if e := entities[0]; e.GetPredicateName() != mtproto.Predicate_messageEntityMentionName || e.GetUserId_INT64() != 7 || e.GetOffset() != 3 || e.GetLength() != 3 {
		t.Errorf("the mention by id: %v", e)
	}
	// An emoji is two units: an entity over it fits, one unit further does
	// not.
	if _, _, err = c.storyCaption("\U0001F600", []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 2}).To_MessageEntity()}); err != nil {
		t.Errorf("an entity over an emoji: %v", err)
	}
	for name, e := range map[string]*mtproto.MessageEntity{
		"past the end":    mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity(),
		"negative offset": mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: -1, Length: 1}).To_MessageEntity(),
		"empty":           mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 0}).To_MessageEntity(),
	} {
		if _, _, err = c.storyCaption("\U0001F600", []*mtproto.MessageEntity{e}); !errors.Is(err, mtproto.ErrEntityBoundsInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err = c.storyCaption(strings.Repeat("a", stories.MaxCaption), nil); err != nil {
		t.Errorf("the longest caption: %v", err)
	}
	if _, _, err = c.storyCaption(strings.Repeat("a", stories.MaxCaption+1), nil); !errors.Is(err, mtproto.ErrMediaCaptionTooLong) {
		t.Errorf("a caption too long: %v", err)
	}
	// No more users are told than the bound.
	var crowd []*mtproto.MessageEntity
	for i := range maxMentions + 5 {
		crowd = append(crowd, mentionOf(int64(1000+i), 0, 1))
	}
	if _, mentioned, _ = c.storyCaption("x", crowd); len(mentioned) != maxMentions {
		t.Errorf("mentioned %d users, want %d", len(mentioned), maxMentions)
	}
}

func channelView(role, rights int32, state int32) *peerView {
	p := &channelpb.ImmutableChannelParticipant{UserId: testSelf, Role: role, AdminRights: rights, State: state}
	return &peerView{
		peer:    stories.ChannelPeer(5),
		channel: &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: 5, Broadcast: true}, Participants: []*channelpb.ImmutableChannelParticipant{p}},
		me:      p,
	}
}

// Who posts, edits and deletes the stories of a channel: the creator
// everything; an admin its own with post_stories, the others' with
// edit_stories and delete_stories; a member and one who left nothing.
func TestChannelRights(t *testing.T) {
	mine := &stories.Story{Peer: stories.ChannelPeer(5), Id: 1, SenderId: testSelf}
	theirs := &stories.Story{Peer: stories.ChannelPeer(5), Id: 2, SenderId: 7}
	for name, tc := range map[string]struct {
		v                                                 *peerView
		post, editMine, editTheirs, deleteTheirs, manages bool
	}{
		"creator":            {channelView(channelpb.RoleCreator, 0, channelpb.StateIn), true, true, true, true, true},
		"admin who posts":    {channelView(channelpb.RoleAdmin, mtproto.ADMIN_POST_STORIES, channelpb.StateIn), true, true, false, false, true},
		"admin who edits":    {channelView(channelpb.RoleAdmin, mtproto.ADMIN_EDIT_STORIES, channelpb.StateIn), false, true, true, false, true},
		"admin who deletes":  {channelView(channelpb.RoleAdmin, mtproto.ADMIN_DELETE_STORIES, channelpb.StateIn), false, false, false, true, true},
		"admin of messages":  {channelView(channelpb.RoleAdmin, mtproto.ADMIN_POST_MESSAGES, channelpb.StateIn), false, false, false, false, false},
		"member":             {channelView(channelpb.RoleMember, 0, channelpb.StateIn), false, false, false, false, false},
		"creator who left":   {channelView(channelpb.RoleCreator, 0, channelpb.StateLeft), false, false, false, false, false},
		"one who never came": {&peerView{peer: stories.ChannelPeer(5), channel: &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: 5}}}, false, false, false, false, false},
	} {
		got := []bool{tc.v.canPost(), tc.v.canEdit(mine, testSelf), tc.v.canEdit(theirs, testSelf), tc.v.canDelete(theirs, testSelf), tc.v.manages()}
		want := []bool{tc.post, tc.editMine, tc.editTheirs, tc.deleteTheirs, tc.manages}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: post, edit own, edit others', delete others', manages = %v, want %v", name, got, want)
				break
			}
		}
	}
	// A channel the owner of the server blocked takes no stories.
	blocked := channelView(channelpb.RoleCreator, 0, channelpb.StateIn)
	blocked.channel.Channel.Blocked = true
	if blocked.canPost() {
		t.Errorf("the creator posts to a blocked channel")
	}
	// A user's stories are its own.
	own := &peerView{peer: stories.UserPeer(testSelf), own: true}
	other := &peerView{peer: stories.UserPeer(7)}
	userStory := &stories.Story{Peer: stories.UserPeer(7), Id: 1, SenderId: 7}
	if !own.canPost() || !own.manages() || other.canPost() || other.manages() || other.canEdit(userStory, testSelf) || other.canDelete(userStory, testSelf) {
		t.Errorf("the stories of users: own %v %v, another's %v %v", own.canPost(), own.manages(), other.canPost(), other.manages())
	}
}

// The stories the caller is shown: all of its own, none of one who blocked
// it, and of another user those whose rules let it in, by the row of the
// owner's contacts.
func TestVisible(t *testing.T) {
	c := testCore()
	list := []*stories.Story{
		{Id: 1, Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll}}},
		{Id: 2, Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowContacts}}},
		{Id: 3, Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowCloseFriends}}},
		{Id: 4, Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll},
			{PredicateName: mtproto.Predicate_privacyValueDisallowUsers, Users: []int64{testSelf}}}},
	}
	owner := func(contact, friend bool) *mtproto.ImmutableUser {
		u := &mtproto.ImmutableUser{User: &mtproto.UserData{Id: 7}}
		if contact {
			u.Contacts = []*mtproto.ContactData{{UserId: 7, ContactUserId: testSelf, CloseFriend: friend}}
		}
		return mtproto.MakeTLImmutableUser(u).To_ImmutableUser()
	}
	for name, tc := range map[string]struct {
		v    *peerView
		want []int32
	}{
		"own":          {&peerView{peer: stories.UserPeer(testSelf), own: true}, []int32{1, 2, 3, 4}},
		"stranger":     {&peerView{peer: stories.UserPeer(7), owner: owner(false, false)}, []int32{1}},
		"contact":      {&peerView{peer: stories.UserPeer(7), owner: owner(true, false)}, []int32{1, 2}},
		"close friend": {&peerView{peer: stories.UserPeer(7), owner: owner(true, true)}, []int32{1, 2, 3}},
		"blocked":      {&peerView{peer: stories.UserPeer(7), owner: owner(true, true), blocked: true}, nil},
		"channel":      {&peerView{peer: stories.ChannelPeer(5)}, []int32{1, 2, 3, 4}},
	} {
		got, err := c.visible(tc.v, list)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ids := idsOf(got); len(ids) != len(tc.want) || (len(ids) > 0 && !equalIds(ids, tc.want)) {
			t.Errorf("%s: sees %v, want %v", name, ids, tc.want)
		}
	}
	// A bot is shown no stories of users.
	bot := testCore()
	bot.MD.IsBot = true
	if got, _ := bot.visible(&peerView{peer: stories.UserPeer(7), owner: owner(true, true)}, list); len(got) != 0 {
		t.Errorf("a bot sees %v", idsOf(got))
	}
}

func equalIds(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The pages of viewers and reactions: the offset is the number of rows
// before the page, and the last page names no next one.
func TestPages(t *testing.T) {
	for _, tc := range []struct {
		n, offset, limit int
		from, to         int
		next             string
	}{
		{0, 0, 20, 0, 0, ""},
		{5, 0, 20, 0, 5, ""},
		{20, 0, 20, 0, 20, ""},
		{21, 0, 20, 0, 20, "20"},
		{21, 20, 20, 20, 21, ""},
		{5, 9, 20, 5, 5, ""},
	} {
		page, next := pageOf(tc.n, tc.offset, tc.limit)
		if page != [2]int{tc.from, tc.to} || next != tc.next {
			t.Errorf("%d rows from %d by %d: %v, next %q", tc.n, tc.offset, tc.limit, page, next)
		}
	}
	if n, err := pageOffset(""); n != 0 || err != nil {
		t.Errorf("the first page: %d, %v", n, err)
	}
	if n, err := pageOffset("40"); n != 40 || err != nil {
		t.Errorf("offset 40: %d, %v", n, err)
	}
	for _, bad := range []string{"-1", "x", "1.5"} {
		if _, err := pageOffset(bad); !errors.Is(err, mtproto.ErrOffsetInvalid) {
			t.Errorf("offset %q: %v", bad, err)
		}
	}
	if clampLimit(0, 20, 100) != 20 || clampLimit(-3, 20, 100) != 20 || clampLimit(500, 20, 100) != 100 || clampLimit(7, 20, 100) != 7 {
		t.Errorf("limits are not bounded")
	}
}

// The limits of pkg/stories as the errors the clients read.
func TestLimitError(t *testing.T) {
	for want, err := range map[string]error{
		"STORIES_TOO_MUCH":                    stories.ErrTooMany,
		"STORY_SEND_FLOOD_WEEKLY_1800000000":  &stories.FloodError{Until: 1800000000},
		"STORY_SEND_FLOOD_MONTHLY_1800000001": &stories.FloodError{Monthly: true, Until: 1800000001},
	} {
		s, ok := status.FromError(limitError(err))
		if !ok || s.Code() != mtproto.ErrBadRequest || s.Message() != want {
			t.Errorf("%v: %v, want 400 %s", err, limitError(err), want)
		}
	}
	other := errors.New("db is down")
	if limitError(other) != other {
		t.Errorf("another error was changed")
	}
	if !refused(mtproto.ErrPeerIdInvalid) || refused(other) || refused(status.Error(500, "INTERNAL")) {
		t.Errorf("refused: 400 %v, a plain error %v, 500 %v", refused(mtproto.ErrPeerIdInvalid), refused(other), refused(status.Error(500, "INTERNAL")))
	}
}

// The updates and the small objects the methods answer with.
func TestUpdates(t *testing.T) {
	u := updateStory(stories.UserPeer(7), stories.Deleted(3))
	if u.GetPredicateName() != mtproto.Predicate_updateStory || u.GetPeer_PEER().GetUserId() != 7 || u.GetUserId() != 7 || u.GetStory().GetId() != 3 {
		t.Errorf("updateStory of a user: %v", u)
	}
	u = updateReadStories(stories.ChannelPeer(5), 9)
	if u.GetPredicateName() != mtproto.Predicate_updateReadStories || u.GetPeer_PEER().GetChannelId() != 5 || u.GetUserId() != 0 || u.GetMaxId() != 9 {
		t.Errorf("updateReadStories of a channel: %v", u)
	}
	us := updatesOf([]*mtproto.Update{u}, nil, nil)
	if us.GetPredicateName() != mtproto.Predicate_updates || us.GetSeq() != 0 || us.GetUsers() == nil || us.GetChats() == nil || us.GetDate() == 0 {
		t.Errorf("the container: %v", us)
	}
	// A date that has passed is left out of the stealth mode.
	m := stealthMode(stories.Stealth{ActiveUntil: 100, CooldownUntil: 200}, 150)
	if m.GetActiveUntilDate() != nil || m.GetCooldownUntilDate().GetValue() != 200 {
		t.Errorf("the stealth mode at 150: %v", m)
	}
	if m = stealthMode(stories.Stealth{}, 150); m.GetPredicateName() != mtproto.Predicate_storiesStealthMode || m.GetActiveUntilDate() != nil || m.GetCooldownUntilDate() != nil {
		t.Errorf("no stealth mode: %v", m)
	}
}

// The message of a mention has one random_id per story and user, for every
// try.
func TestMentionRandomId(t *testing.T) {
	st := &stories.Story{Peer: stories.UserPeer(7), Id: 3}
	a := mentionRandomId(st, 9)
	if a == 0 || a != mentionRandomId(st, 9) {
		t.Errorf("the id changes between tries: %d", a)
	}
	seen := map[int64]bool{a: true}
	for _, other := range []int64{
		mentionRandomId(st, 10),
		mentionRandomId(&stories.Story{Peer: stories.UserPeer(7), Id: 4}, 9),
		mentionRandomId(&stories.Story{Peer: stories.UserPeer(8), Id: 3}, 9),
	} {
		if seen[other] {
			t.Errorf("two mentions share the id %d", other)
		}
		seen[other] = true
	}
}

// What an answer refers to: the peer of a story, the author of a repost,
// the channel of a post it shows, and the users of its rules for its owner
// only.
func TestRefs(t *testing.T) {
	st := &stories.Story{
		Peer:    stories.UserPeer(7),
		FwdFrom: mtproto.MakeTLStoryFwdHeader(&mtproto.StoryFwdHeader{From: mtproto.MakePeerChannel(50)}).To_StoryFwdHeader(),
		MediaAreas: []*mtproto.MediaArea{
			mtproto.MakeTLMediaAreaChannelPost(&mtproto.MediaArea{ChannelId: 60, MsgId: 1}).To_MediaArea(),
		},
		Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowUsers, Users: []int64{8, 9}}},
	}
	r := &refs{}
	r.story(st, false)
	r.story(st, false)
	if len(r.users) != 1 || r.users[0] != 7 || len(r.channels) != 2 {
		t.Errorf("a viewer's answer: users %v, channels %v", r.users, r.channels)
	}
	r = &refs{}
	r.story(st, true)
	if len(r.users) != 3 {
		t.Errorf("the owner's answer: users %v", r.users)
	}
}
