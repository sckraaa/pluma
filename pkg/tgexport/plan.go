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

package tgexport

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/teamgram-server/pkg/polls"
)

// Reasons a record is not imported (Item.Skip).
const (
	// SkipAuthor: the owner chose to skip its author.
	SkipAuthor = "author_skipped"
	// SkipUnsupported: the export itself could not describe the message.
	SkipUnsupported = "unsupported"
	// SkipAction: a service message with no counterpart in an imported
	// group (a call, a gift, a topic and so on).
	SkipAction = "service_not_imported"
	// SkipActor: a service message whose actor, or a member it names,
	// is no Pluma account the mapping knows.
	SkipActor = "service_unresolved"
	// SkipPhoto: the new photo of the group is not in the export.
	SkipPhoto = "service_photo_missing"
	// SkipMedia: media with no counterpart and no text beside it.
	SkipMedia = "media_not_imported"
	// SkipMissing: the file is not in the export, the message has no
	// text and the operator chose to go on without such messages.
	SkipMissing = "media_missing"
	// SkipEmpty: nothing to import in the message.
	SkipEmpty = "empty"
	// SkipInvalid: a poll or a checklist Pluma does not accept.
	SkipInvalid = "invalid"
	// SkipUnmatched: a message whose author has no Pluma account. It is
	// a fault of the plan, never the outcome of an import.
	SkipUnmatched = "author_unmatched"
)

// MaxFileSize is the largest file dfs takes: 8000 parts of 512 KB
// (app/service/dfs/internal/model/file.go).
const MaxFileSize = 8000 * 512 << 10

// Options are the operator's choices for a plan.
type Options struct {
	// SkipMissingMedia imports a message whose file is not in the export
	// as its text alone, and skips it when it has none. Without it such a
	// message is a fault of the plan.
	SkipMissingMedia bool
}

// ItemReaction is a reaction that is imported: the export names who set
// it, and the mapping knows that account.
type ItemReaction struct {
	User  int64
	Emoji string
	Date  int64
}

// Item is what becomes of one record.
type Item struct {
	Rec *Record
	// Skip is why the record is not imported; empty for one that is.
	Skip string
	// Sender is the Pluma account the message is from.
	Sender int64
	// ReplyTo is the id, in the export, of the message replied to, when
	// that message is an earlier record of the export.
	ReplyTo int64
	// File is the file to upload: the message's media or the photo of a
	// service message. Nil when there is none, or none to be had.
	File *Media
	// ForwardUser is the Pluma account a forward is from; ForwardName
	// its name when that is all there is. Both empty: no forward header.
	ForwardUser int64
	ForwardName string
	// ActionUsers are the Pluma accounts of the members a service
	// message names.
	ActionUsers []int64
	Reactions   []ItemReaction
	// Create tells that the record is the creation of the group.
	Create bool
}

// Resolver decides, record by record in the export's order, what becomes
// of each: Plan counts its answers, the import writes them. Both get the
// same answers for the same export, mapping and options.
type Resolver struct {
	ex      *Export
	census  *Census
	mapping *Mapping
	opts    Options
	plan    *Plan

	lastId   int64
	lastDate int64
	// seen are the records so far by their id: true for those imported.
	seen map[int64]bool
}

// NewResolver starts at the first record of ex.
func NewResolver(ex *Export, census *Census, mapping *Mapping, opts Options) *Resolver {
	return &Resolver{ex: ex, census: census, mapping: mapping, opts: opts, plan: newPlan(census), seen: map[int64]bool{}}
}

// User is the Pluma account of the Telegram user id, when the mapping has
// one: who a mention by name is of.
func (r *Resolver) User(telegramId int64) (int64, bool) {
	return r.mapping.user("user" + strconv.FormatInt(telegramId, 10))
}

// named is the Pluma account of a name a service message gives without an
// id: the one user of the export with that name, or the owner's answer in
// service_names.
func (r *Resolver) named(name string) (int64, bool) {
	if name == "" {
		return 0, false
	}
	if key := r.census.keyOfName(name); key != "" {
		return r.mapping.user(key)
	}
	if id := r.mapping.ServiceNames[name]; id != nil && *id > 0 {
		return *id, true
	}
	return 0, false
}

// Plan is what the resolver counted so far.
func (r *Resolver) Plan() *Plan { return r.plan }

// Next decides what becomes of rec, the record after the last one given.
// An error is a broken order of the export, after which nothing can be
// decided.
func (r *Resolver) Next(rec *Record) (*Item, error) {
	if len(r.seen) > 0 && rec.Id <= r.lastId {
		return nil, fmt.Errorf("tgexport: record %d: message id %d does not follow %d: the export is not in ascending order", rec.Seq, rec.Id, r.lastId)
	}
	r.lastId = rec.Id
	it := &Item{Rec: rec}
	p := r.plan
	p.Records++
	if rec.Kind != KindUnsupported {
		if rec.Date < r.lastDate {
			p.disorder(rec.Id)
		}
		r.lastDate = max(r.lastDate, rec.Date)
		if p.FirstDate == 0 {
			p.FirstDate = rec.Date
		}
		p.LastDate = r.lastDate
	}
	switch rec.Kind {
	case KindUnsupported:
		it.Skip = SkipUnsupported
	case KindService:
		r.service(it)
	default:
		r.message(it)
	}
	if it.Skip == "" && rec.ReplyTo != 0 {
		// A reply is kept when its target is an earlier record that is
		// imported; the import journal turns the id into the new one.
		switch {
		case rec.ReplyToPeer != "":
			p.RepliesElsewhere++
		case r.seen[rec.ReplyTo]:
			it.ReplyTo = rec.ReplyTo
		default:
			p.RepliesLost++
		}
	}
	r.seen[rec.Id] = it.Skip == ""
	p.count(it)
	return it, nil
}

func (r *Resolver) message(it *Item) {
	rec, p := it.Rec, r.plan
	switch {
	case rec.From.Key == "":
		p.addNoAuthor(rec.Id)
		it.Skip = SkipUnmatched
		return
	case r.mapping.skipped(rec.From.Key):
		it.Skip = SkipAuthor
		return
	}
	sender, ok := r.mapping.user(rec.From.Key)
	if !ok {
		// Mapping.Validate lists the author: the plan fails, and the
		// record is decided no further.
		it.Skip = SkipUnmatched
		return
	}
	it.Sender = sender
	text := Plain(rec.Text) != ""
	if m := rec.Media; m != nil {
		switch {
		case m.Type == MediaOther:
			if !text {
				it.Skip = SkipMedia
				p.mediaNotImported(m.Other)
				return
			}
			p.ContentDropped++
		case m.Type.HasFile():
			switch {
			case r.file(it, m):
				it.File = m
			case r.opts.SkipMissingMedia && !text:
				it.Skip = SkipMissing
				return
			}
			// A file that is not there fails the plan unless the operator
			// chose to go on, and then the text is imported alone.
		case m.Type == MediaPoll && !validPoll(m.Poll), m.Type == MediaTodo && !validTodo(m.Todo):
			it.Skip = SkipInvalid
			return
		}
	} else if !text {
		it.Skip = SkipEmpty
		return
	}
	if rec.Buttons {
		p.ButtonsDropped++
	}
	if f := rec.Forward; f != nil {
		if id, ok := r.mapping.user(f.Key); ok {
			it.ForwardUser = id
		} else if f.Name != "" {
			it.ForwardName = f.Name
		} else {
			p.ForwardsDropped++
		}
	}
	for _, x := range rec.Reactions {
		p.Reactions.Counted += int64(x.Count)
		switch x.Type {
		case "custom_emoji":
			p.Reactions.Custom += int64(x.Count)
			continue
		case "paid":
			p.Reactions.Paid += int64(x.Count)
			continue
		}
		p.Reactions.Named += int64(len(x.Recent))
		if x.Emoji == "" {
			continue
		}
		for _, n := range x.Recent {
			id, ok := r.mapping.user(n.From.Key)
			if !ok {
				p.Reactions.Unmapped++
				continue
			}
			if it.addReaction(ItemReaction{User: id, Emoji: x.Emoji, Date: n.Date}) {
				p.Reactions.Imported++
			}
		}
	}
}

// userReactionsMax is the most reactions one user has on one message
// (pkg/reactions.UserMax).
const userReactionsMax = 3

func textLen(parts []TextPart) int { return utf8.RuneCountInString(Plain(parts)) }

// validPoll tells whether Pluma takes the poll (polls.Check): a question
// and two to twelve answers within the lengths.
func validPoll(p *Poll) bool {
	if n := textLen(p.Question); n == 0 || n > polls.QuestionMax {
		return false
	}
	if len(p.Answers) < 2 || len(p.Answers) > polls.AnswersMax {
		return false
	}
	for _, a := range p.Answers {
		if n := textLen(a.Text); n == 0 || n > polls.AnswerMax {
			return false
		}
	}
	return true
}

// validTodo tells whether Pluma takes the checklist (polls.CheckTodo).
func validTodo(t *Todo) bool {
	if n := textLen(t.Title); n == 0 || n > polls.TodoTitleMax {
		return false
	}
	if len(t.Items) == 0 || len(t.Items) > polls.TodoItemsMax {
		return false
	}
	seen := map[int32]bool{}
	for _, i := range t.Items {
		if n := textLen(i.Text); n == 0 || n > polls.TodoItemMax || i.Id <= 0 || seen[i.Id] {
			return false
		}
		seen[i.Id] = true
	}
	return true
}

func (it *Item) addReaction(n ItemReaction) bool {
	mine := 0
	for _, x := range it.Reactions {
		if x.User != n.User {
			continue
		}
		if x.Emoji == n.Emoji {
			return false
		}
		mine++
	}
	if mine >= userReactionsMax {
		return false
	}
	it.Reactions = append(it.Reactions, n)
	return true
}

// file checks that the file of m is in the export's folder, and notes it
// in the plan. It tells whether the file is there.
func (r *Resolver) file(it *Item, m *Media) bool {
	p := r.plan
	if m.Missing == "" {
		size, err := r.ex.Stat(m.Path)
		switch {
		case err != nil:
			m.Missing = MissingOnDisk
		case size == 0:
			m.Missing = MissingOnDisk
		case size > MaxFileSize:
			p.addTooLarge(it.Rec.Id)
			m.Missing = MissingTooLarge
		default:
			m.Size = size
		}
	}
	if m.Missing != "" {
		p.missing(it.Rec.Id, m.Missing)
		return false
	}
	p.Files++
	p.Bytes += m.Size
	return true
}

func (r *Resolver) service(it *Item) {
	rec, p := it.Rec, r.plan
	a := rec.Action
	if a.Name == "topic_created" {
		p.ForumTopics++
	}
	switch a.Name {
	case "create_group", "create_channel", "edit_group_title", "edit_group_photo", "delete_group_photo",
		"invite_members", "remove_members", "join_group_by_link", "join_group_by_request", "pin_message":
	default:
		// migrate_from_group and migrate_to_supergroup are the two ends of
		// one change inside the group that is imported as one.
		it.Skip = SkipAction
		p.action(a.Name)
		return
	}
	if r.mapping.skipped(rec.From.Key) {
		it.Skip = SkipAuthor
		return
	}
	sender, ok := r.mapping.user(rec.From.Key)
	if !ok {
		it.Skip = SkipActor
		return
	}
	it.Sender = sender
	switch a.Name {
	case "create_group", "create_channel":
		it.Create = true
	case "invite_members", "remove_members":
		if len(a.Members) == 0 || a.Name == "remove_members" && len(a.Members) != 1 {
			it.Skip = SkipActor
			return
		}
		for _, name := range a.Members {
			id, ok := r.named(name)
			if !ok {
				it.Skip = SkipActor
				it.ActionUsers = nil
				return
			}
			it.ActionUsers = append(it.ActionUsers, id)
		}
	case "join_group_by_link":
		id, ok := r.named(a.Inviter)
		if !ok {
			it.Skip = SkipActor
			return
		}
		it.ActionUsers = []int64{id}
	case "edit_group_photo":
		if a.Photo == nil || !r.file(it, a.Photo) {
			it.Skip = SkipPhoto
			return
		}
		it.File = a.Photo
	}
}

// ReactionCounts tell how much of the reactions of the export an import
// carries.
type ReactionCounts struct {
	// Counted is the sum of the counters: every reaction there was.
	Counted int64
	// Named are those the export names the author of; Imported those of
	// them set by a mapped account on an imported message, with a
	// standard emoji.
	Named    int64
	Imported int64
	// Unmapped are named reactions of people without a Pluma account;
	// Custom and Paid are reactions with a custom emoji and paid ones,
	// by their counters.
	Unmapped int64
	Custom   int64
	Paid     int64
}

// PlanAuthor is a peer of the export and the account its messages, service
// messages and reactions are imported as.
type PlanAuthor struct {
	Key       string
	Names     []string
	Messages  int64
	Service   int64
	Reactions int64
	// Pluma is the confirmed account of the mapping, 0 for none; Skip the
	// owner's choice to leave the peer out.
	Pluma int64
	Skip  bool
}

// listMax is how many ids of a kind of fault the plan keeps to show.
const listMax = 20

// Plan is what an import of an export with a mapping would do.
type Plan struct {
	Header  Header
	Records int64
	// Import is how many messages are written; ByKind counts them by
	// what they are ("text", a MediaType, "service").
	Import int64
	ByKind map[string]int64
	// Skipped counts the records left out by reason; Actions the service
	// messages of SkipAction by their action; NotImported the media of
	// SkipMedia by its kind.
	Skipped     map[string]int64
	Actions     map[string]int64
	NotImported map[string]int64

	// Faults that make the import impossible.
	Unmatched    []Unmatched
	MappingFault []string
	NoAuthor     []int64
	Disorder     []int64
	TooLarge     []int64
	// Missing counts the files that are not in the export by reason;
	// MissingIds are the first messages of them. A fault unless the
	// operator chose Options.SkipMissingMedia.
	Missing      map[Missing]int64
	MissingIds   []int64
	MissingFault bool

	// Files and Bytes are what is uploaded.
	Files int64
	Bytes int64

	// RepliesLost are replies whose target is not in the export or is
	// not imported; RepliesElsewhere replies to messages of other chats.
	RepliesLost      int64
	RepliesElsewhere int64
	// ForwardsDropped are forwards the export names nobody for.
	ForwardsDropped int64
	// ContentDropped are messages imported as their text without media
	// that has no counterpart; ButtonsDropped those that lose the
	// buttons of a bot.
	ContentDropped int64
	ButtonsDropped int64
	Reactions      ReactionCounts
	// ForumTopics counts the topics created: more than none means a
	// forum, which is imported as one flat history.
	ForumTopics int64
	// Edited counts the imported messages that carry an edit date. The
	// export gives the date of every edit, also of those Telegram's clients
	// do not show (Record.Edited); all of them are imported as edited.
	Edited int64
	// Authors are the peers of the export with what the mapping decides
	// for each, by their key.
	Authors []PlanAuthor
	// Members are the accounts that join, besides the creator.
	Members   []int64
	FirstDate int64
	LastDate  int64

	noAuthor   int64
	disordered int64
	tooLarge   int64
}

func newPlan(c *Census) *Plan {
	return &Plan{
		Header: c.Header, ByKind: map[string]int64{}, Skipped: map[string]int64{}, Actions: map[string]int64{},
		NotImported: map[string]int64{}, Missing: map[Missing]int64{},
	}
}

func keep(list *[]int64, id int64) {
	if len(*list) < listMax {
		*list = append(*list, id)
	}
}

func (p *Plan) disorder(id int64)             { p.disordered++; keep(&p.Disorder, id) }
func (p *Plan) addNoAuthor(id int64)          { p.noAuthor++; keep(&p.NoAuthor, id) }
func (p *Plan) addTooLarge(id int64)          { p.tooLarge++; keep(&p.TooLarge, id) }
func (p *Plan) action(name string)            { p.Actions[name]++ }
func (p *Plan) mediaNotImported(kind string)  { p.NotImported[kind]++ }
func (p *Plan) missing(id int64, why Missing) { p.Missing[why]++; keep(&p.MissingIds, id) }

// count adds the decided item to the totals.
func (p *Plan) count(it *Item) {
	if it.Skip != "" {
		p.Skipped[it.Skip]++
		return
	}
	p.Import++
	if it.Rec.Kind == KindMessage && it.Rec.Edited > 0 {
		p.Edited++
	}
	switch {
	case it.Rec.Kind == KindService:
		p.ByKind["service"]++
	case it.Rec.Media != nil && it.Rec.Media.Type != MediaOther && (!it.Rec.Media.Type.HasFile() || it.File != nil):
		p.ByKind[string(it.Rec.Media.Type)]++
	default:
		p.ByKind["text"]++
	}
}

// finish adds what the mapping alone tells.
func (p *Plan) finish(m *Mapping, c *Census, opts Options, exists func(int64) bool) {
	p.Unmatched, p.MappingFault = m.Validate(c, exists)
	p.Members = m.Members()
	keys := make([]string, 0, len(c.Peers))
	for key := range c.Peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s := c.Peers[key]
		a := PlanAuthor{Key: key, Names: s.Names, Messages: s.Messages, Service: s.Service, Reactions: s.Reactions, Skip: m.skipped(key)}
		a.Pluma, _ = m.user(key)
		p.Authors = append(p.Authors, a)
	}
	missing := int64(0)
	for _, n := range p.Missing {
		missing += n
	}
	// The photo of a service message that is not there never stops an
	// import: that message is skipped.
	p.MissingFault = !opts.SkipMissingMedia && missing > p.Skipped[SkipPhoto]
}

// Faults are the reasons the import cannot start, empty when it can.
func (p *Plan) Faults() []string {
	var out []string
	out = append(out, p.MappingFault...)
	for _, u := range p.Unmatched {
		out = append(out, fmt.Sprintf("author %s (%d messages) has no confirmed Pluma account: %s", u.Key, u.Messages, u.Why))
	}
	if p.noAuthor > 0 {
		out = append(out, fmt.Sprintf("%d messages name no author (first ids %s)", p.noAuthor, ids(p.NoAuthor)))
	}
	if p.disordered > 0 {
		out = append(out, fmt.Sprintf("%d messages are dated before the message they follow (first ids %s): the order of the export is not the order of time", p.disordered, ids(p.Disorder)))
	}
	if p.tooLarge > 0 {
		out = append(out, fmt.Sprintf("%d files are larger than %d bytes (messages %s)", p.tooLarge, int64(MaxFileSize), ids(p.TooLarge)))
	}
	if p.MissingFault {
		out = append(out, fmt.Sprintf("files of messages are not in the export (%s; first ids %s): export them, or run with -missing-media=skip", p.missingSummary(), ids(p.MissingIds)))
	}
	return out
}

func (p *Plan) missingSummary() string {
	reasons := make([]string, 0, len(p.Missing))
	for why, n := range p.Missing {
		reasons = append(reasons, fmt.Sprintf("%s: %d", why, n))
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ", ")
}

func ids(list []int64) string {
	s := make([]string, 0, len(list))
	for _, id := range list {
		s = append(s, strconv.FormatInt(id, 10))
	}
	return strings.Join(s, ", ")
}

// Build reads the export twice, once for the census of its people and once
// record by record, and returns what an import with the mapping would do.
// exists tells whether a Pluma account is there; nil checks none.
func Build(ex *Export, mapping *Mapping, opts Options, exists func(int64) bool) (*Plan, *Census, error) {
	census, err := Scan(ex)
	if err != nil {
		return nil, nil, err
	}
	r := NewResolver(ex, census, mapping, opts)
	if err = ex.Each(func(rec *Record) error {
		_, err := r.Next(rec)
		return err
	}); err != nil {
		return nil, nil, err
	}
	r.plan.finish(mapping, census, opts, exists)
	return r.plan, census, nil
}
