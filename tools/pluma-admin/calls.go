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
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// callsConfig is the part of the bff config calls needs.
type callsConfig struct {
	Mysql sqlx.Config
}

// epochBlocks is how many heights back an epoch is named by its height.
const epochBlocks = 64

// callView is what calls show prints of a call.
type callView struct {
	Call *groupcalls.Call
	// Members are the users of the group state of the chain.
	Members []int64
	// Phase is the step of the emoji verification at the height;
	// Committed and Revealed who sent theirs.
	Phase     tde2e.Phase
	Committed map[int64]bool
	Revealed  map[int64]bool
	InCall    map[int64]bool
	Diags     map[int64]groupcalls.StoredDiag
	// Heights names the height of the prefix of a recent block's hash.
	Heights map[string]int32
}

func callsCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 3 || args[1] != "show" {
		return errUsage
	}
	id, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return errUsage
	}
	var c callsConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	v, err := loadCall(ctx, groupcalls.NewStore(sqlx.NewMySQL(&c.Mysql)), id)
	if err != nil {
		return err
	}
	renderCall(stdout, v, time.Now())
	return nil
}

func loadCall(ctx context.Context, s *groupcalls.Store, id int64) (*callView, error) {
	call, err := s.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("call %d: %w", id, err)
	}
	v := &callView{Call: call, InCall: map[int64]bool{}, Diags: map[int64]groupcalls.StoredDiag{}, Heights: map[string]int32{}}
	if call.ChainHeight >= 0 {
		ch, ver, err := s.Verification(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, p := range ch.GroupState().Participants {
			v.Members = append(v.Members, p.UserID)
		}
		v.Phase, v.Committed, v.Revealed = ver.Phase(), map[int64]bool{}, map[int64]bool{}
		for _, uid := range v.Members {
			v.Committed[uid], v.Revealed[uid] = ver.Committed(uid), ver.Revealed(uid)
		}
		blocks, err := s.Blocks(ctx, id, groupcalls.SubChainMain, max(call.ChainHeight-epochBlocks, 0), call.ChainHeight+1)
		if err != nil {
			return nil, err
		}
		for _, b := range blocks {
			v.Heights[hex.EncodeToString(b.Hash[:8])] = b.Height
		}
	}
	in, err := s.InCall(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, p := range in {
		v.InCall[p.UserId] = true
	}
	diags, err := s.Diags(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, d := range diags {
		v.Diags[d.UserId] = d
	}
	return v, nil
}

// age is how long before now a unix ms time was, "-" for none.
func age(now time.Time, ms int64) string {
	if ms == 0 {
		return "-"
	}
	return now.Sub(time.UnixMilli(ms)).Round(time.Second).String()
}

// epochs names each epoch by its height when the chain has it.
func (v *callView) epochs(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		if h, ok := v.Heights[e]; ok {
			out = append(out, fmt.Sprintf("%d", h))
		} else {
			out = append(out, e)
		}
	}
	return strings.Join(out, ",")
}

func phaseName(p tde2e.Phase) string {
	switch p {
	case tde2e.PhaseCommit:
		return "commit"
	case tde2e.PhaseReveal:
		return "reveal"
	}
	return "done"
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func renderCall(w io.Writer, v *callView, now time.Time) {
	c := v.Call
	state := "live"
	if c.Discarded {
		state = "over"
	}
	fmt.Fprintf(w, "call %d (peer %d of type %d): %s, version %d, %d in the call, SFU node %q\n",
		c.Id, c.PeerId, c.PeerType, state, c.Version, c.ParticipantsCount, c.SfuNode)
	if c.ChainHeight < 0 {
		fmt.Fprintln(w, "chain: no blocks")
	} else {
		fmt.Fprintf(w, "chain: height %d, block %s, %d broadcasts, emoji verification at the height: %s\n",
			c.ChainHeight, hex.EncodeToString(c.ChainHash[:8]), c.BroadcastCount, phaseName(v.Phase))
	}
	users := slices.Clone(v.Members)
	for uid := range v.InCall {
		if !slices.Contains(users, uid) {
			users = append(users, uid)
		}
	}
	for uid := range v.Diags {
		if !slices.Contains(users, uid) {
			users = append(users, uid)
		}
	}
	slices.Sort(users)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "user\tchain\tcall\tcommit/reveal\tpoll 0\tpoll 1\tsends with\treport\theight\tblock\terror\tencrypted/failed\tsenders ok/unknown/old/dup/failed\tlost\trejoins 5m\talarms")
	for _, uid := range users {
		member := slices.Contains(v.Members, uid)
		cr := "-"
		if member {
			cr = yes(v.Committed[uid]) + "/" + yes(v.Revealed[uid])
		}
		d := v.Diags[uid].Diag
		if d == nil {
			d = &groupcalls.E2EDiag{}
		}
		poll := func(i int) string {
			if d.Poll[i].At == 0 {
				return "-"
			}
			return fmt.Sprintf("%d (%s ago)", d.Poll[i].Offset, age(now, d.Poll[i].At))
		}
		sends := "-"
		if d.SendAt != 0 {
			sends = fmt.Sprintf("%s (%s ago)", v.epochs(d.SendEpochs), age(now, d.SendAt))
		}
		report, height, block, failure, frames, senders := "-", "-", "-", "-", "-", "-"
		if r := d.Report; r != nil {
			report = fmt.Sprintf("#%d (%s ago)", r.Seq, age(now, d.ReportAt))
			height, block, failure = strconv.Itoa(int(r.Height)), r.LastBlock, strconv.Itoa(int(r.Error))
			if block == "" {
				block = "-"
			} else if h, ok := v.Heights[block]; ok {
				block = fmt.Sprintf("%s (%d)", block, h)
			}
			frames = fmt.Sprintf("%d/%d", r.Encrypted, r.EncryptFailed)
			var parts []string
			for _, s := range r.Senders {
				parts = append(parts, fmt.Sprintf("%d:%d/%d/%d/%d/%d", s.User, s.OK, s.UnknownEpoch, s.TooOld, s.Duplicate, s.Failed))
			}
			if len(parts) > 0 {
				senders = strings.Join(parts, " ")
			}
		}
		alarms := "-"
		if len(d.Alarms) > 0 {
			alarms = strings.Join(d.Alarms, ",")
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			uid, yes(member), yes(v.InCall[uid]), cr, poll(0), poll(1), sends, report, height, block, failure, frames, senders, d.ReportsLost, d.Rejoins, alarms)
	}
	tw.Flush()
}
