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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/tgexport"

	"google.golang.org/protobuf/proto"
)

// The run of import-chat (task 083, ADR 0051): the plan, the check of the
// disk, the records batch by batch with their files uploaded first, the end
// of the import and the members. Every step can be run again: the import's
// journal tells where the records stopped, the uploaded files are
// remembered, a finished import is not finished twice, and a member who is
// in the group is not added again.

const (
	// importSource names exports of Telegram Desktop among the imports.
	importSource = "telegram"
	// importBatchMax is the most records of one channel.importMessages
	// (dao.ImportBatchMax of biz channel).
	importBatchMax = 200
	// importBatchBytes is from what size a batch is sent though it has
	// fewer records than the batch size.
	importBatchBytes = 1 << 20
	// importInviteMax is how many members one channel.inviteToChannel adds
	// (dao.InviteMax of biz channel).
	importInviteMax = 200
	// importSpare is the free space that must be left after an import.
	importSpare = 5 << 30
)

// importChannel is the channel service as the import uses it.
type importChannel interface {
	// State returns nil for a chat with no import that is not aborted.
	State(ctx context.Context, importId, sourceChatId int64) (*channel.ChannelImport, error)
	Create(ctx context.Context, in *channel.TLChannelImportCreate) (*channel.ChannelImport, error)
	Messages(ctx context.Context, importId, firstSeq int64, batch []*channel.ImportedMessage) (*channel.ChannelImport, error)
	Finish(ctx context.Context, importId int64) (*channel.ChannelImport, error)
	Abort(ctx context.Context, importId int64) (*channel.ChannelImport, error)
	// Invite adds users to the channel by its creator; those already in
	// are left as they are.
	Invite(ctx context.Context, creator, channelId int64, users []int64) error
}

// importAccounts are the Pluma accounts a mapping may name.
type importAccounts interface {
	Accounts(ctx context.Context) ([]tgexport.Account, error)
	// UploadedBytes is the size of the files of the chat uploaded already.
	UploadedBytes(ctx context.Context, sourceChatId int64) (int64, error)
}

// importBackend is everything an import talks to.
type importBackend struct {
	accounts importAccounts
	media    importMediaStore
	channel  importChannel
	up       importUploader
	ids      importIds
	// free is the free space of the disk a directory is on.
	free     func(dir string) (int64, error)
	waveform func(ctx context.Context, path string) []byte
	sleep    func(time.Duration)
}

// importOptions are the flags of plan and run.
type importOptions struct {
	dir         string
	mapping     string
	title       string
	skipMissing bool
	batch       int
	pause       time.Duration
	storageDir  string
	freeBytes   int64
}

// prepared is an export read and planned.
type prepared struct {
	ex      *tgexport.Export
	mapping *tgexport.Mapping
	plan    *tgexport.Plan
	census  *tgexport.Census
	// names are the names of the Pluma accounts by their id.
	names map[int64]string
}

// prepare opens the export, reads the mapping and builds the plan.
func prepare(ctx context.Context, be *importBackend, o importOptions) (*prepared, error) {
	ex, err := tgexport.Open(o.dir)
	if err != nil {
		return nil, err
	}
	if !ex.Header().Group() {
		return nil, fmt.Errorf("the export is of a %q, not of a group", ex.Header().Type)
	}
	mapping, err := tgexport.LoadMapping(o.mapping)
	if err != nil {
		return nil, err
	}
	accounts, err := be.accounts.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(accounts))
	for _, a := range accounts {
		names[a.Id] = a.Name
	}
	plan, census, err := tgexport.Build(ex, mapping, tgexport.Options{SkipMissingMedia: o.skipMissing}, func(id int64) bool { _, ok := names[id]; return ok })
	if err != nil {
		return nil, err
	}
	return &prepared{ex: ex, mapping: mapping, plan: plan, census: census, names: names}, nil
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func day(unix int64) string {
	if unix == 0 {
		return "-"
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}

// printAuthors writes whose messages become whose: every peer of the export
// beside the Pluma account the mapping gives it, by that account's own
// name (names, by id), which is what the owner confirms. An account given
// to several peers is pointed out: it may be one person's two accounts, or
// a slip that would put two people's messages under one name.
func printAuthors(w io.Writer, p *tgexport.Plan, names map[int64]string) {
	if len(p.Authors) == 0 {
		return
	}
	fmt.Fprintln(w, "Authors:")
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	shared := map[int64][]string{}
	for _, a := range p.Authors {
		as := "no account"
		switch {
		case a.Skip:
			as = "skipped"
		case a.Pluma != 0:
			as = fmt.Sprintf("Pluma %d %q", a.Pluma, names[a.Pluma])
			shared[a.Pluma] = append(shared[a.Pluma], a.Key)
		}
		fmt.Fprintf(tw, "  %s\t%q\t%d messages, %d service, %d reactions\t-> %s\n", a.Key, a.Names, a.Messages, a.Service, a.Reactions, as)
	}
	_ = tw.Flush()
	ids := make([]int64, 0, len(shared))
	for id, keys := range shared {
		if len(keys) > 1 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		fmt.Fprintf(w, "  NOTE: Pluma account %d %q is given to %d peers of the export: %s\n", id, names[id], len(shared[id]), strings.Join(shared[id], ", "))
	}
}

// printPlan writes what the import would do. It names the people of the
// export as the export names them and the Pluma accounts by their names
// (names, by id), for the operator who fills the mapping; it never prints a
// text of a message.
func printPlan(w io.Writer, p *tgexport.Plan, names map[int64]string) {
	fmt.Fprintf(w, "Export: chat %d (%s), %d records, %s .. %s\n", p.Header.Id, p.Header.Type, p.Records, day(p.FirstDate), day(p.LastDate))
	fmt.Fprintf(w, "To import: %d messages\n", p.Import)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, k := range sortedKeys(p.ByKind) {
		fmt.Fprintf(tw, "  %s\t%d\n", k, p.ByKind[k])
	}
	_ = tw.Flush()
	if len(p.Skipped) > 0 {
		fmt.Fprintln(w, "Not imported:")
		tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, k := range sortedKeys(p.Skipped) {
			fmt.Fprintf(tw, "  %s\t%d\n", k, p.Skipped[k])
		}
		for _, k := range sortedKeys(p.Actions) {
			fmt.Fprintf(tw, "    service %s\t%d\n", k, p.Actions[k])
		}
		for _, k := range sortedKeys(p.NotImported) {
			fmt.Fprintf(tw, "    media %s\t%d\n", k, p.NotImported[k])
		}
		_ = tw.Flush()
	}
	fmt.Fprintf(w, "Files: %d, %d bytes\n", p.Files, p.Bytes)
	if len(p.Missing) > 0 {
		missing := map[string]int64{}
		for why, n := range p.Missing {
			missing[string(why)] = n
		}
		for _, k := range sortedKeys(missing) {
			fmt.Fprintf(w, "  not in the export, %s: %d\n", k, missing[k])
		}
		fmt.Fprintf(w, "  first messages without their file: %v\n", p.MissingIds)
	}
	fmt.Fprintf(w, "Replies without their message: %d (to other chats: %d)\n", p.RepliesLost, p.RepliesElsewhere)
	if p.ForwardsDropped+p.ContentDropped+p.ButtonsDropped > 0 {
		fmt.Fprintf(w, "Imported in part: %d forwards without a source, %d messages without media that has no counterpart, %d without the buttons of a bot\n",
			p.ForwardsDropped, p.ContentDropped, p.ButtonsDropped)
	}
	if p.Edited > 0 {
		fmt.Fprintf(w, "Edit dates: %d messages keep theirs and show as changed (the export does not tell the edits Telegram hides)\n", p.Edited)
	}
	r := p.Reactions
	fmt.Fprintf(w, "Reactions: %d by the counters, %d with their author named, %d to import\n", r.Counted, r.Named, r.Imported)
	fmt.Fprintf(w, "  not imported: %d without a named author, %d of people with no account in the mapping, %d with a custom emoji, %d paid\n",
		max(r.Counted-r.Named-r.Custom-r.Paid, 0), r.Unmapped, r.Custom, r.Paid)
	if p.ForumTopics > 0 {
		fmt.Fprintf(w, "The group is a forum (%d topics): it is imported as one history, without topics.\n", p.ForumTopics)
	}
	fmt.Fprintf(w, "Members besides the creator: %d\n", len(p.Members))
	printAuthors(w, p, names)
	for _, u := range p.Unmatched {
		fmt.Fprintf(w, "NO ACCOUNT: %s %q, %d messages (%s)\n", u.Key, u.Names, u.Messages, u.Why)
	}
}

// planError is the plan's faults as one error.
func planError(p *tgexport.Plan) error {
	faults := p.Faults()
	if len(faults) == 0 {
		return nil
	}
	return fmt.Errorf("the import cannot start:\n  %s", strings.Join(faults, "\n  "))
}

// mappingKeys is the mapping as the import keeps it: the authors decided,
// export key to Pluma account, 0 for one skipped.
func mappingKeys(m *tgexport.Mapping) (string, error) {
	out := map[string]int64{}
	for key, a := range m.Authors {
		switch {
		case a.Skip:
			out[key] = 0
		case a.Confirmed && a.Pluma != nil && *a.Pluma > 0:
			out[key] = *a.Pluma
		}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// checkDisk refuses an import the storage has no room for. A file takes its
// size twice for a while: dfs keeps the parts of an upload for hours after
// the file is in the object store. One fifth is added for what a store
// spends around the data, and importSpare must stay free.
func checkDisk(ctx context.Context, be *importBackend, o importOptions, p *prepared, w io.Writer) error {
	if p.plan.Bytes == 0 {
		return nil
	}
	done, err := be.accounts.UploadedBytes(ctx, p.ex.Header().Id)
	if err != nil {
		return err
	}
	left := max(p.plan.Bytes-done, 0)
	if left == 0 {
		return nil
	}
	need := left*22/10 + importSpare
	free := o.freeBytes
	switch {
	case o.freeBytes > 0:
	case o.storageDir != "":
		if free, err = be.free(o.storageDir); err != nil {
			return fmt.Errorf("free space of %s: %w", o.storageDir, err)
		}
	default:
		return fmt.Errorf("%d bytes of files are to be uploaded: say how much room the storage has, with -storage-dir <a directory on its disk> or -free-bytes <bytes> (at least %d are needed)", left, need)
	}
	if free < need {
		return fmt.Errorf("not enough room in the storage: %d bytes free, %d needed for %d bytes of files", free, need, left)
	}
	fmt.Fprintf(w, "Storage: %d bytes free, %d needed.\n", free, need)
	return nil
}

// runImport imports the export of o.
func runImport(ctx context.Context, be *importBackend, o importOptions, w io.Writer) error {
	p, err := prepare(ctx, be, o)
	if err != nil {
		return err
	}
	printPlan(w, p.plan, p.names)
	if err = planError(p.plan); err != nil {
		return err
	}
	state, err := be.channel.State(ctx, 0, p.ex.Header().Id)
	if err != nil {
		return err
	}
	if state != nil && state.CreatorId != p.mapping.Creator {
		return fmt.Errorf("import %d of this chat was started for creator %d, the mapping names %d: abort it first, or put the creator back", state.Id, state.CreatorId, p.mapping.Creator)
	}
	files := &importFiles{ex: p.ex, up: be.up, store: be.media, waveform: be.waveform}
	if state == nil || state.State == importRunning {
		if err = checkDisk(ctx, be, o, p, w); err != nil {
			return err
		}
		if state != nil {
			fmt.Fprintf(w, "Import %d goes on from record %d.\n", state.Id, state.NextSeq)
		}
		if state, err = importRecords(ctx, be, o, p, files, state, w); err != nil {
			return fmt.Errorf("%w\nNothing is lost: run the same command again to go on", err)
		}
		if state, err = be.channel.Finish(ctx, state.Id); err != nil {
			return fmt.Errorf("finish: %w\nRun the same command again to go on", err)
		}
	}
	if state.State != importFinished {
		return fmt.Errorf("import %d is in state %d", state.Id, state.State)
	}
	members := p.mapping.Members()
	for chunk := range slices.Chunk(members, importInviteMax) {
		if err = be.channel.Invite(ctx, state.CreatorId, state.ChannelId, chunk); err != nil {
			return fmt.Errorf("members: %w\nThe history is imported; run the same command again to add the members", err)
		}
	}
	fmt.Fprintf(w, "Import %d is finished: channel %d, %d messages, %d records not imported, %d files uploaded now (%d bytes), %d found uploaded, %d members besides the creator.\n",
		state.Id, state.ChannelId, state.Messages, state.Skipped, files.uploaded, files.bytes, files.reused, len(members))
	return nil
}

// States of an import (dao.ImportRunning and so on of biz channel).
const (
	importRunning  = 0
	importFinished = 1
	importAborted  = 2
)

// importRecords writes the records of the export that the import state does
// not have yet; a nil state starts the import. It returns the import with
// every record of the export journaled.
func importRecords(ctx context.Context, be *importBackend, o importOptions, p *prepared, files *importFiles, state *channel.ChannelImport, w io.Writer) (*channel.ChannelImport, error) {
	header := p.ex.Header()
	title := o.title
	if title == "" {
		title = header.Name
	}
	if title == "" {
		return nil, errors.New("the export gives the group no name: name it with -title")
	}
	if p.plan.FirstDate == 0 {
		return nil, errors.New("the export has no message to import")
	}
	keys, err := mappingKeys(p.mapping)
	if err != nil {
		return nil, err
	}
	create := func(first *channel.ImportedMessage) error {
		var err error
		state, err = be.channel.Create(ctx, &channel.TLChannelImportCreate{
			CreatorId: p.mapping.Creator, SourceType: importSource, SourceChatId: header.Id,
			Title: title, Mapping: keys, First: first,
		})
		return err
	}
	if state != nil {
		// The authors decided since the import began are added to it; one
		// that was decided and is another now is refused.
		if err = create(nil); err != nil {
			return nil, fmt.Errorf("mapping: %w", err)
		}
	}
	resolver := tgexport.NewResolver(p.ex, p.census, p.mapping, tgexport.Options{SkipMissingMedia: o.skipMissing})
	builder := &importBuilder{mapping: p.mapping, resolver: resolver, files: files, ids: be.ids}
	var (
		batch []*channel.ImportedMessage
		size  int
		sent  int
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		next, err := be.channel.Messages(ctx, state.Id, batch[0].Seq, batch)
		if err != nil {
			return fmt.Errorf("records %d..%d: %w", batch[0].Seq, batch[len(batch)-1].Seq, err)
		}
		state, batch, size = next, nil, 0
		if sent++; sent%50 == 0 {
			fmt.Fprintf(w, "  %d of %d records, %d messages, %d files uploaded\n", state.NextSeq-1, p.plan.Records, state.Messages, files.uploaded)
		}
		be.sleep(o.pause)
		return nil
	}
	err = p.ex.Each(func(rec *tgexport.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		it, err := resolver.Next(rec)
		if err != nil {
			return err
		}
		if state == nil {
			// A new import begins with the creation of the group: the
			// export's own record of it, or one made for the creator at
			// the date the export begins.
			if it.Create && it.Skip == "" {
				first, err := builder.build(ctx, it)
				if err != nil {
					return err
				}
				return create(first)
			}
			if err = create(creationMessage(p.mapping.Creator, title, p.plan.FirstDate)); err != nil {
				return err
			}
			fmt.Fprintf(w, "Import %d: channel %d.\n", state.Id, state.ChannelId)
		}
		if rec.Seq < state.NextSeq {
			if rec.Seq == state.NextSeq-1 && rec.Id != state.LastSourceId {
				return fmt.Errorf("record %d of the export is message %d, the import has message %d there: this is not the export the import began with", rec.Seq, rec.Id, state.LastSourceId)
			}
			return nil
		}
		im, err := builder.build(ctx, it)
		if err != nil {
			return fmt.Errorf("record %d: %w", rec.Seq, err)
		}
		batch = append(batch, im)
		size += proto.Size(im)
		if len(batch) >= o.batch || size >= importBatchBytes {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.New("the export has no records")
	}
	if state.NextSeq != p.plan.Records+1 {
		return nil, fmt.Errorf("the import has %d records, the export %d: this is not the export the import began with", state.NextSeq-1, p.plan.Records)
	}
	return state, nil
}

// stateName names a state of an import.
func stateName(state int32) string {
	switch state {
	case importRunning:
		return "running"
	case importFinished:
		return "finished"
	case importAborted:
		return "aborted"
	}
	return fmt.Sprintf("state %d", state)
}

func printState(w io.Writer, s *channel.ChannelImport) {
	fmt.Fprintf(w, "Import %d of %s chat %d: %s; channel %d, creator %d; %d records journaled, %d messages, %d not imported; started %s.\n",
		s.Id, s.SourceType, s.SourceChatId, stateName(s.State), s.ChannelId, s.CreatorId, max(s.NextSeq-1, 0), s.Messages, s.Skipped,
		time.Unix(s.Date, 0).UTC().Format("2006-01-02 15:04"))
}
