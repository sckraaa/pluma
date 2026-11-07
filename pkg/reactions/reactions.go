// Package reactions keeps the reactions to messages of private chats and
// basic groups (task 053): the key a reaction is stored under, the limits,
// and the reactions of a message as one viewer sees them.
//
// A reaction is stored once per user and message, under the message's
// dialog_message_id (table message_reactions). Every viewer gets an object
// of its own (min = false, its chosen_order and my flags): with min = true
// iOS and Android keep one reaction of the viewer only and Web K replaces
// the object whole (docs/tasks/053-research/clients.md, 2.3).
package reactions

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Limits, as Telegram Premium (owner, 2026-11-03: every user gets them, Pluma
// has no Premium). Telegram's values (help.getAppConfig, 2026-11-03):
// reactions_user_max_premium 3, reactions_uniq_max 11, reactions_in_chat_max
// 100.
const (
	// UserMax is the most reactions one user puts on one message, and
	// the most tags on one message of Saved Messages.
	UserMax = 3
	// UniqMax is the most different reactions on one message.
	UniqMax = 11
	// InChatMax is the most reactions a group may allow
	// (messages.setChatAvailableReactions with chatReactionsSome).
	InChatMax = 100
	// RecentMax is the most peers of recent_reactions. Telegram's number
	// is not verified; the clients show the avatars of a few reactions.
	RecentMax = 5
	// TagTitleMax is the longest title of a tag of Saved Messages, in
	// characters: Web K limits it to 12 (components/chat/contextMenu.ts:
	// 777-799); Telegram's own limit is not verified.
	TagTitleMax = 12
)

// Kind is the kind of chat a message is in.
type Kind int

const (
	Private Kind = iota + 1
	Group
	// Saved is the chat with oneself: reactions are tags there.
	Saved
	// Broadcast is a channel's posts: the counts only, no one's reactions
	// listed, as the subscribers of a channel are not shown to each other
	// (task 044; how Telegram fills them is not verified).
	Broadcast
)

// Key is the key a reaction is stored under: "e" and the emoticon, or "c" and
// the document id of a custom emoji. Paid reactions and anything else are
// REACTION_INVALID.
func Key(r *mtproto.Reaction) (string, error) {
	switch r.GetPredicateName() {
	case mtproto.Predicate_reactionEmoji:
		if r.GetEmoticon() == "" {
			return "", mtproto.ErrReactionInvalid
		}
		return "e" + r.GetEmoticon(), nil
	case mtproto.Predicate_reactionCustomEmoji:
		if r.GetDocumentId() == 0 {
			return "", mtproto.ErrReactionInvalid
		}
		return "c" + strconv.FormatInt(r.GetDocumentId(), 10), nil
	}
	return "", mtproto.ErrReactionInvalid
}

// FromKey is the reaction of a key; nil for a key Key never made.
func FromKey(key string) *mtproto.Reaction {
	switch {
	case strings.HasPrefix(key, "e") && len(key) > 1:
		return mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: key[1:]}).To_Reaction()
	case strings.HasPrefix(key, "c"):
		id, err := strconv.ParseInt(key[1:], 10, 64)
		if err != nil || id == 0 {
			return nil
		}
		return mtproto.MakeTLReactionCustomEmoji(&mtproto.Reaction{DocumentId: id}).To_Reaction()
	}
	return nil
}

// Keys checks the reactions a user sets on a message (messages.sendReaction:
// the whole new list, in the user's order) and returns their keys. Empty is
// none. More than UserMax is REACTIONS_TOO_MANY, a repeated one or one Key
// refuses REACTION_INVALID.
func Keys(list []*mtproto.Reaction) ([]string, error) {
	if len(list) > UserMax {
		return nil, mtproto.ErrReactionsTooMany
	}
	keys := make([]string, 0, len(list))
	for _, r := range list {
		k, err := Key(r)
		if err != nil {
			return nil, err
		}
		if slices.Contains(keys, k) {
			return nil, mtproto.ErrReactionInvalid
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// Row is one user's reaction to a message.
type Row struct {
	UserId      int64
	Reaction    string // Key
	ChosenOrder int32  // order among the user's reactions to the message
	Big         bool
	Unread      bool // the message's author has not seen it
	Date        int64
}

// View is the reactions of a message from author as viewer sees them, nil
// without any. Results go by count, ties by the earliest reaction;
// recent_reactions are the newest first, at most RecentMax, outside Saved
// Messages. unread shows to the author only, for reactions of others.
func View(rows []Row, viewer, author int64, kind Kind) *mtproto.MessageReactions {
	if len(rows) == 0 {
		return nil
	}
	return view(rows, viewer, author, kind)
}

// ViewOrEmpty is View, with an empty object for a message without reactions:
// the answer of messages.getMessagesReactions and the update after the last
// reaction is taken back, which make the clients clear theirs (Desktop
// clears the reactions of a polled message nothing came for,
// data_message_reactions.cpp:1731-1736).
func ViewOrEmpty(rows []Row, viewer, author int64, kind Kind) *mtproto.MessageReactions {
	return view(rows, viewer, author, kind)
}

func view(rows []Row, viewer, author int64, kind Kind) *mtproto.MessageReactions {
	type result struct {
		key   string
		count int32
		first int64
		order *int32
	}
	byKey := map[string]*result{}
	var results []*result
	for _, r := range rows {
		res, ok := byKey[r.Reaction]
		if !ok {
			res = &result{key: r.Reaction, first: r.Date}
			byKey[r.Reaction] = res
			results = append(results, res)
		}
		res.count++
		res.first = min(res.first, r.Date)
		if r.UserId == viewer {
			o := r.ChosenOrder
			res.order = &o
		}
	}
	slices.SortStableFunc(results, func(a, b *result) int {
		if c := cmp.Compare(b.count, a.count); c != 0 {
			return c
		}
		return cmp.Compare(a.first, b.first)
	})

	out := &mtproto.MessageReactions{
		CanSeeList:      kind != Saved && kind != Broadcast,
		ReactionsAsTags: kind == Saved,
		Results:         make([]*mtproto.ReactionCount, 0, len(results)),
		RecentReactions: []*mtproto.MessagePeerReaction{},
	}
	for _, res := range results {
		rc := &mtproto.ReactionCount{Reaction_REACTION: FromKey(res.key), Count: res.count}
		if res.order != nil {
			// Not MakeFlagsInt32: it drops 0, the first of the viewer's.
			rc.ChosenOrder = wrapperspb.Int32(*res.order)
		}
		out.Results = append(out.Results, mtproto.MakeTLReactionCount(rc).To_ReactionCount())
	}
	if kind != Saved && kind != Broadcast {
		recent := slices.Clone(rows)
		slices.SortStableFunc(recent, func(a, b Row) int { return cmp.Compare(b.Date, a.Date) })
		for _, r := range recent[:min(len(recent), RecentMax)] {
			out.RecentReactions = append(out.RecentReactions, mtproto.MakeTLMessagePeerReaction(&mtproto.MessagePeerReaction{
				Big:               r.Big,
				Unread:            r.Unread && viewer == author && r.UserId != author,
				My:                r.UserId == viewer,
				PeerId:            mtproto.MakePeerUser(r.UserId),
				Date:              int32(r.Date),
				Reaction_REACTION: FromKey(r.Reaction),
			}).To_MessagePeerReaction())
		}
	}
	return mtproto.MakeTLMessageReactions(out).To_MessageReactions()
}
