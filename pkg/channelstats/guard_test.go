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

package channelstats

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The statements that change who is a member of a channel: the counter of
// its members, a participant row written, or the state of one changed.
var (
	countSet      = regexp.MustCompile(`(?is)^\s*UPDATE\s+channels\s+SET\s+(.*?)(\sWHERE\s|$)`)
	countAssign   = regexp.MustCompile(`(?i)(^|,)\s*participants_count\s*=`)
	participantIn = regexp.MustCompile(`(?is)^\s*INSERT\s+(IGNORE\s+)?INTO\s+channel_participants\b`)
	stateSet      = regexp.MustCompile(`(?is)^\s*UPDATE\s+channel_participants\s+SET\s+(.*?)(\sWHERE\s|$)`)
	stateAssign   = regexp.MustCompile(`(?i)(^|,)\s*state\s*=`)
)

// notEvents are the functions that change membership without an event, with
// why: each is checked to be still such a function, so that the list does
// not outlive the code.
var notEvents = map[string]string{
	"app/service/biz/channel/internal/core/channel.createChannel_handler.go:ChannelCreateChannel": "the creator of a new channel joins nothing",
	"app/service/biz/channel/internal/dao/delete_channel.go:DeleteChannel":                        "the channel is gone with its statistics",
	"app/service/biz/channel/internal/dao/ghost.go:SetUserHidden":                                 "an operator turning an account into a ghost or back hides or shows its rows; nobody joins or leaves (ADR 0048)",
	"app/service/biz/channel/internal/dao/import.go:ImportFinish":                                 "an imported group is new; its importer creates it",
	"app/service/biz/channel/internal/dao/rights.go:EditBanned":                                   "a kicked user freed goes from kicked to left: it was out and stays out",
	"app/messenger/msg/internal/dao/migrate.go:MigrateChat":                                       "the members of a basic group move to its supergroup, they do not join it",
	"app/messenger/msg/internal/dao/channel_send.go:recordSlowmode":                               "a guest's slow mode row is written as out of the group",
	"pkg/ghost/ghost.go:JoinChannel":                                                              "a ghost is never counted (ADR 0048)",
	"pkg/ghost/ghost.go:LeaveChannel":                                                             "a ghost is never counted (ADR 0048)",
}

// eventCalls are the calls that record a join or a leave.
var eventCalls = []string{"MemberJoined", "MembersJoined", "MemberLeft"}

// TestEveryMembershipChangeRecorded guards the member events (task 103):
// every function of the server that changes who is a member of a channel
// records it with channelstats, or is listed in notEvents with the reason.
func TestEveryMembershipChangeRecorded(t *testing.T) {
	root := repoRoot(t)
	found := map[string]bool{}
	for _, dir := range []string{"app", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			for _, fn := range membershipFuncs(t, path) {
				found[filepath.ToSlash(rel)+":"+fn.name] = fn.records
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(found) == 0 {
		t.Fatal("no function changes membership: the patterns no longer match the code")
	}
	var keys []string
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, listed := notEvents[k]
		switch {
		case found[k] && listed:
			t.Errorf("%s records member events and is listed as not recording them", k)
		case !found[k] && !listed:
			t.Errorf("%s changes membership without channelstats.%s: record the event in its transaction, or list it in notEvents with why", k, strings.Join(eventCalls, "/"))
		}
	}
	for k := range notEvents {
		if _, ok := found[k]; !ok {
			t.Errorf("%s is listed in notEvents but changes no membership", k)
		}
	}
	// The writers of the events are found as such.
	for _, k := range []string{
		"app/service/biz/channel/internal/dao/membership.go:Join",
		"app/service/biz/channel/internal/dao/membership.go:Leave",
		"app/service/biz/channel/internal/dao/invite.go:writeMembers",
		"app/service/biz/channel/internal/dao/join_requests.go:approve",
		"app/service/biz/channel/internal/dao/rights.go:kick",
	} {
		if !found[k] {
			t.Errorf("%s: not found as a function that records member events", k)
		}
	}
}

type membershipFunc struct {
	name    string
	records bool
}

// membershipFuncs are the functions of the file that change membership, and
// whether each records the events.
func membershipFuncs(t *testing.T, path string) []membershipFunc {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	consts := stringConsts(f)
	var out []membershipFunc
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		changes, records := false, false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "channelstats" && slices.Contains(eventCalls, sel.Sel.Name) {
						records = true
					}
				}
			case ast.Expr:
				if s, ok := constString(n, consts); ok && changesMembership(s) {
					changes = true
					return false
				}
			}
			return true
		})
		if changes {
			out = append(out, membershipFunc{name: fn.Name.Name, records: records})
		}
	}
	return out
}

func changesMembership(query string) bool {
	if m := countSet.FindStringSubmatch(query); m != nil && countAssign.MatchString(m[1]) {
		return true
	}
	if m := stateSet.FindStringSubmatch(query); m != nil && stateAssign.MatchString(m[1]) {
		return true
	}
	return participantIn.MatchString(query)
}

// stringConsts are the string constants declared in the file.
func stringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			v := spec.(*ast.ValueSpec)
			for i, name := range v.Names {
				if i < len(v.Values) {
					if s, ok := constString(v.Values[i], out); ok {
						out[name.Name] = s
					}
				}
			}
		}
	}
	return out
}

// constString is the value of a string literal, a constant of consts, or a
// sum of them.
func constString(e ast.Expr, consts map[string]string) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[e.Name]
		return s, ok
	case *ast.ParenExpr:
		return constString(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := constString(e.X, consts)
		if !ok {
			return "", false
		}
		r, ok := constString(e.Y, consts)
		return l + r, ok
	}
	return "", false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestMembershipSQL(t *testing.T) {
	for query, want := range map[string]bool{
		"UPDATE channels SET participants_count = participants_count + 1, admins_count = admins_count + ? WHERE id = ?": true,
		"UPDATE channels SET deleted = 1, participants_count = 0 WHERE id = ?":                                          true,
		"UPDATE channels SET top_message = ? WHERE id = ?":                                                              false,
		"SELECT c.participants_count FROM channels c WHERE c.id = ?":                                                    false,
		"INSERT INTO channel_participants (channel_id, user_id) VALUES (?, ?)":                                          true,
		"UPDATE channel_participants SET state = ?, role = ? WHERE channel_id = ?":                                      true,
		"UPDATE channel_participants SET role = ?, admin_rights = 0, state = ? WHERE channel_id = ?":                    true,
		"UPDATE channel_participants SET hidden = ? WHERE channel_id = ? AND state = ?":                                 false,
		"UPDATE channel_participants SET `rank` = ? WHERE channel_id = ? AND user_id = ?":                               false,
		"UPDATE chats SET participants_count = ? WHERE id = ?":                                                          false,
		"UPDATE channels SET top_message = ? WHERE id = ? AND participants_count = 0":                                   false,
		"UPDATE channel_participants SET banned_rights = 0, until_date = 0 WHERE channel_id = ?":                        false,
		"INSERT IGNORE INTO channel_participants (channel_id, user_id) VALUES (?, ?)":                                   true,
	} {
		if got := changesMembership(query); got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
}
