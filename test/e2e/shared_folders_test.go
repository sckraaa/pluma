//go:build e2e

package e2e

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Shared folders (task 064): a folder of channels is shared by a link; who
// opens it sees the channels, joins them and gets the folder.

func chatlistInput(id int32) *mtproto.InputChatlist {
	return mtproto.MakeTLInputChatlistDialogFilter(&mtproto.InputChatlist{FilterId: id}).To_InputChatlist()
}

func channelInputPeer(ch *mtproto.Chat) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}).To_InputPeer()
}

func channelFolder(id int32, title string, shared bool, peers ...*mtproto.InputPeer) *mtproto.DialogFilter {
	f := &mtproto.DialogFilter{
		Id:                     id,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: title}).To_TextWithEntities(),
		PinnedPeers:            []*mtproto.InputPeer{},
		IncludePeers:           peers,
	}
	if shared {
		return mtproto.MakeTLDialogFilterChatlist(f).To_DialogFilter()
	}
	return mtproto.MakeTLDialogFilter(f).To_DialogFilter()
}

func exportChatlist(ctx context.Context, t *testing.T, u *user, folder int32, title string, peers ...*mtproto.InputPeer) *mtproto.Chatlists_ExportedChatlistInvite {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChatlistsExportChatlistInvite{
		Constructor: u.c.Constructor(mtproto.Predicate_chatlists_exportChatlistInvite),
		Chatlist:    chatlistInput(folder),
		Title:       title,
		Peers:       peers,
	})
	if err != nil {
		t.Fatalf("chatlists.exportChatlistInvite: %v", err)
	}
	return o.(interface {
		To_Chatlists_ExportedChatlistInvite() *mtproto.Chatlists_ExportedChatlistInvite
	}).To_Chatlists_ExportedChatlistInvite()
}

func checkChatlist(ctx context.Context, t *testing.T, u *user, slug string) *mtproto.Chatlists_ChatlistInvite {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChatlistsCheckChatlistInvite{
		Constructor: u.c.Constructor(mtproto.Predicate_chatlists_checkChatlistInvite),
		Slug:        slug,
	})
	if err != nil {
		t.Fatalf("chatlists.checkChatlistInvite: %v", err)
	}
	return o.(interface {
		To_Chatlists_ChatlistInvite() *mtproto.Chatlists_ChatlistInvite
	}).To_Chatlists_ChatlistInvite()
}

func chatlistUpdates(ctx context.Context, t *testing.T, u *user, folder int32) *mtproto.Chatlists_ChatlistUpdates {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChatlistsGetChatlistUpdates{
		Constructor: u.c.Constructor(mtproto.Predicate_chatlists_getChatlistUpdates),
		Chatlist:    chatlistInput(folder),
	})
	if err != nil {
		t.Fatalf("chatlists.getChatlistUpdates: %v", err)
	}
	return o.(interface {
		To_Chatlists_ChatlistUpdates() *mtproto.Chatlists_ChatlistUpdates
	}).To_Chatlists_ChatlistUpdates()
}

func peerChannelIds(peers []*mtproto.Peer) []int64 {
	out := make([]int64, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.GetChannelId())
	}
	slices.Sort(out)
	return out
}

func channelInputFrom(t *testing.T, chats []*mtproto.Chat, id int64) *mtproto.InputPeer {
	t.Helper()
	for _, c := range chats {
		if c.GetId() == id {
			return channelInputPeer(c)
		}
	}
	t.Fatalf("channel %d not in chats %v", id, chats)
	return nil
}

func TestSharedFolder(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobLaptop := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	getState(ctx, t, bobLaptop)

	_, news := createChannel(ctx, t, alice, "News", false)
	_, chat := createChannel(ctx, t, alice, "Group", true)
	updateDialogFilter(ctx, t, alice, 2, channelFolder(2, "Friends", false, channelInputPeer(news), channelInputPeer(chat)))

	// A folder holding a rule cannot be shared.
	updateDialogFilter(ctx, t, alice, 3, folderOf(3, "Contacts"))
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChatlistsExportChatlistInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_chatlists_exportChatlistInvite),
		Chatlist:    chatlistInput(3),
		Peers:       []*mtproto.InputPeer{channelInputPeer(news)},
	}); !mtclient.IsRPCError(err, "FILTER_NOT_SUPPORTED") {
		t.Fatalf("a folder of rules: %v", err)
	}

	r := exportChatlist(ctx, t, alice, 2, "Friends", channelInputPeer(news), channelInputPeer(chat))
	if r.GetFilter().GetPredicateName() != mtproto.Predicate_dialogFilterChatlist || !r.GetFilter().GetHasMyInvites() {
		t.Fatalf("the folder after sharing: %v", r.GetFilter())
	}
	url := r.GetInvite().GetUrl()
	i := strings.LastIndex(url, "/addlist/")
	if i < 0 {
		t.Fatalf("the link: %q", url)
	}
	slug := url[i+len("/addlist/"):]

	// The stranger sees the private channels, not forbidden.
	check := checkChatlist(ctx, t, bob, slug)
	if check.GetPredicateName() != mtproto.Predicate_chatlists_chatlistInvite || check.GetTitle_TEXTWITHENTITIES().GetText() != "Friends" {
		t.Fatalf("the link for a stranger: %v", check)
	}
	if got := peerChannelIds(check.GetPeers()); !slices.Equal(got, []int64{news.GetId(), chat.GetId()}) && !slices.Equal(got, []int64{chat.GetId(), news.GetId()}) {
		t.Fatalf("the channels of the link: %v", got)
	}
	for _, c := range check.GetChats() {
		if c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() {
			t.Fatalf("a channel of the link: %v", c)
		}
	}

	if _, err := bob.c.Invoke(ctx, &mtproto.TLChatlistsCheckChatlistInvite{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_checkChatlistInvite),
		Slug:        "nosuchlink",
	}); !mtclient.IsRPCError(err, "INVITE_SLUG_EXPIRED") {
		t.Fatalf("a link that does not exist: %v", err)
	}

	peers := []*mtproto.InputPeer{channelInputFrom(t, check.GetChats(), news.GetId()), channelInputFrom(t, check.GetChats(), chat.GetId())}
	o, err := bob.c.Invoke(ctx, &mtproto.TLChatlistsJoinChatlistInvite{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_joinChatlistInvite),
		Slug:        slug,
		Peers:       peers,
	})
	if err != nil {
		t.Fatalf("chatlists.joinChatlistInvite: %v", err)
	}
	joined := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	var folderId int32
	for _, u := range joined.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateDialogFilter {
			folderId = u.GetId_INT32()
		}
	}
	if folderId == 0 {
		t.Fatalf("no updateDialogFilter in the answer: %v", joined.GetUpdates())
	}
	if got := chatsOfUpdates(joined); !slices.Contains(got, news.GetId()) || !slices.Contains(got, chat.GetId()) {
		t.Fatalf("the channels in the answer: %v", got)
	}
	// Bob's other device gets the folder with seq.
	waitUpdate(ctx, t, bobLaptop, mtproto.Predicate_updateDialogFilter, func(u *mtproto.Update) bool { return u.GetId_INT32() == folderId })

	// Bob is in the channels now and the folder is his, as read back.
	if ids, _ := dialogFilters(ctx, t, bob); !slices.Contains(ids, folderId) {
		t.Fatalf("bob's folders %v, want %d", ids, folderId)
	}
	already := checkChatlist(ctx, t, bob, slug)
	if already.GetPredicateName() != mtproto.Predicate_chatlists_chatlistInviteAlready || already.GetFilterId() != folderId || len(already.GetAlreadyPeers()) != 2 || len(already.GetMissingPeers()) != 0 {
		t.Fatalf("the link for its importer: %v", already)
	}

	// Joining again answers the same folder.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChatlistsJoinChatlistInvite{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_joinChatlistInvite),
		Slug:        slug,
		Peers:       peers,
	}); err != nil {
		t.Fatalf("join again: %v", err)
	}

	// The owner adds a channel to the folder and the link; Bob's folder
	// reports it new.
	_, more := createChannel(ctx, t, alice, "More", false)
	updateDialogFilter(ctx, t, alice, 2, channelFolder(2, "Friends", true, channelInputPeer(news), channelInputPeer(chat), channelInputPeer(more)))
	if u := chatlistUpdates(ctx, t, bob, folderId); len(u.GetMissingPeers()) != 0 {
		t.Fatalf("new channels before the link has them: %v", u.GetMissingPeers())
	}
	// Desktop and Web name a link by its whole url here and in its deletion.
	o, err = alice.c.Invoke(ctx, &mtproto.TLChatlistsEditExportedInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_chatlists_editExportedInvite),
		Chatlist:    chatlistInput(2),
		Slug:        url,
		Peers:       []*mtproto.InputPeer{channelInputPeer(news), channelInputPeer(chat), channelInputPeer(more)},
	})
	if err != nil {
		t.Fatalf("chatlists.editExportedInvite: %v", err)
	}
	edited := o.(interface {
		To_ExportedChatlistInvite() *mtproto.ExportedChatlistInvite
	}).To_ExportedChatlistInvite()
	if len(edited.GetPeers()) != 3 {
		t.Fatalf("the edited link: %v", edited)
	}
	u := chatlistUpdates(ctx, t, bob, folderId)
	if len(u.GetMissingPeers()) != 1 || u.GetMissingPeers()[0].GetChannelId() != more.GetId() {
		t.Fatalf("new channels: %v", u.GetMissingPeers())
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChatlistsJoinChatlistUpdates{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_joinChatlistUpdates),
		Chatlist:    chatlistInput(folderId),
		Peers:       []*mtproto.InputPeer{channelInputFrom(t, u.GetChats(), more.GetId())},
	}); err != nil {
		t.Fatalf("chatlists.joinChatlistUpdates: %v", err)
	}
	if u = chatlistUpdates(ctx, t, bob, folderId); len(u.GetMissingPeers()) != 0 {
		t.Fatalf("new channels after joining them: %v", u.GetMissingPeers())
	}

	// The owner reads back the links; a folder not shared answers errors.
	o, err = alice.c.Invoke(ctx, &mtproto.TLChatlistsGetExportedInvites{
		Constructor: alice.c.Constructor(mtproto.Predicate_chatlists_getExportedInvites),
		Chatlist:    chatlistInput(2),
	})
	if err != nil {
		t.Fatalf("chatlists.getExportedInvites: %v", err)
	}
	links := o.(interface {
		To_Chatlists_ExportedInvites() *mtproto.Chatlists_ExportedInvites
	}).To_Chatlists_ExportedInvites()
	if len(links.GetInvites()) != 1 || len(links.GetChats()) != 3 {
		t.Fatalf("the links of the owner: %v", links)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChatlistsGetChatlistUpdates{
		Constructor: alice.c.Constructor(mtproto.Predicate_chatlists_getChatlistUpdates),
		Chatlist:    chatlistInput(3),
	}); !mtclient.IsRPCError(err, "FILTER_NOT_SUPPORTED") {
		t.Fatalf("updates of a plain folder: %v", err)
	}

	// Leaving: the channels of the folder are suggested, the chosen one is
	// left, the folder goes.
	raw, err := bob.c.InvokeRaw(ctx, &mtproto.TLChatlistsGetLeaveChatlistSuggestions{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_getLeaveChatlistSuggestions),
		Chatlist:    chatlistInput(folderId),
	})
	if err != nil {
		t.Fatalf("chatlists.getLeaveChatlistSuggestions: %v", err)
	}
	d := mtproto.NewDecodeBuf(raw)
	if crc := d.Int(); mtproto.TLConstructor(crc) != mtproto.CRC32_vector || d.Int() != 3 {
		t.Fatalf("suggestions: constructor %#x", uint32(crc))
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLChatlistsLeaveChatlist{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_leaveChatlist),
		Chatlist:    chatlistInput(folderId),
		Peers:       []*mtproto.InputPeer{channelInputFrom(t, check.GetChats(), news.GetId())},
	})
	if err != nil {
		t.Fatalf("chatlists.leaveChatlist: %v", err)
	}
	left := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	found := false
	for _, up := range left.GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateDialogFilter && up.GetId_INT32() == folderId && up.GetFilter() == nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("no updateDialogFilter without a folder: %v", left.GetUpdates())
	}
	if ids, _ := dialogFilters(ctx, t, bob); slices.Contains(ids, folderId) {
		t.Fatalf("bob's folders after leaving: %v", ids)
	}

	// The deleted link stops working.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChatlistsDeleteExportedInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_chatlists_deleteExportedInvite),
		Chatlist:    chatlistInput(2),
		Slug:        url,
	}); err != nil {
		t.Fatalf("chatlists.deleteExportedInvite: %v", err)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChatlistsCheckChatlistInvite{
		Constructor: bob.c.Constructor(mtproto.Predicate_chatlists_checkChatlistInvite),
		Slug:        slug,
	}); !mtclient.IsRPCError(err, "INVITE_SLUG_EXPIRED") {
		t.Fatalf("a deleted link: %v", err)
	}
}

// chatsOfUpdates returns the ids of the channels of r.
func chatsOfUpdates(r *mtproto.Updates) []int64 {
	out := make([]int64, 0, len(r.GetChats()))
	for _, c := range r.GetChats() {
		out = append(out, c.GetId())
	}
	return out
}

// A basic group of its creator shared in a folder becomes a supergroup and
// the folder follows it.
func TestSharedFolderBasicGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	chatId := createChat(ctx, t, alice, "Basic", bob)
	input := mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: chatId}).To_InputPeer()
	updateDialogFilter(ctx, t, alice, 2, channelFolder(2, "Basic", false, input))

	r := exportChatlist(ctx, t, alice, 2, "", input)
	if len(r.GetInvite().GetPeers()) != 1 || r.GetInvite().GetPeers()[0].GetPredicateName() != mtproto.Predicate_peerChannel {
		t.Fatalf("the peers of the link: %v", r.GetInvite().GetPeers())
	}
	if len(r.GetFilter().GetIncludePeers()) != 1 || r.GetFilter().GetIncludePeers()[0].GetPredicateName() != mtproto.Predicate_inputPeerChannel {
		t.Fatalf("the folder after the move: %v", r.GetFilter())
	}
}
