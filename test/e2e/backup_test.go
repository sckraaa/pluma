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
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// teamgramServices are the services of docker-compose.yaml.
var teamgramServices = []string{"gnetway", "session", "bff", "sync", "msg", "biz", "media", "dfs", "authsession", "status", "idgen"}

func stackOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(runScript, append([]string{"compose"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("compose %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// historyTexts returns the texts of u's history with peer.
func historyTexts(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) []string {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	var out []string
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		out = append(out, m.GetMessage())
	}
	return out
}

// TestBackupAndRestore follows docs/ops/backup.md: a backup and its restore
// check, then the database is lost and rebuilt from the backup by the
// runbook; the users sign in again and their history is there.
func TestBackupAndRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	alice, bob, peer := twoUsers(ctx, t)
	tag := fmt.Sprintf("backup %d", randomID())
	sendTo(ctx, t, alice, peer, tag)
	t.Cleanup(func() { restore(t) })

	stack(t, "run", "--rm", "backup", "now")
	stack(t, "run", "--rm", "backup", "check-restore")
	if out := stackOut(t, "run", "--rm", "backup", "snapshots"); !strings.Contains(out, "mysql") || !strings.Contains(out, "files") || !strings.Contains(out, "secrets") {
		t.Fatalf("snapshots without the mysql, files or secrets tag:\n%s", out)
	}

	// The database is lost: MySQL starts empty (its data is in memory).
	stack(t, append([]string{"stop"}, teamgramServices...)...)
	stack(t, "rm", "--stop", "--force", "mysql")
	stack(t, "up", "--detach", "--wait", "mysql")

	// Runbook: load the dump, raise the box counters above what clients
	// saw, move the consumer groups to the end of their topics, start.
	stack(t, "run", "--rm", "backup", "restore-mysql")
	stack(t, "run", "--rm", "--no-deps", "bff", "./pluma-admin", "-f=../etc2/bff.yaml", "state-raise", "-by", "1000000")
	for _, g := range [][2]string{{"Inbox-MainCommunity-S", "Inbox-T"}, {"Sync-MainCommunity-S", "Sync-T"}} {
		stack(t, "exec", "-T", "kafka", "/opt/kafka/bin/kafka-consumer-groups.sh", "--bootstrap-server", "localhost:9094",
			"--reset-offsets", "--to-latest", "--execute", "--group", g[0], "--topic", g[1])
	}
	restore(t)

	// The allowlist, the accounts and the history came back.
	bob2 := login(ctx, t, bob.phone, "Bob after restore")
	if bob2.self.GetId() != bob.self.GetId() {
		t.Fatalf("Bob signed in as user %d after the restore, was %d", bob2.self.GetId(), bob.self.GetId())
	}
	alice2 := login(ctx, t, alice.phone, "Alice after restore")
	texts := historyTexts(ctx, t, bob2, peerOf(ctx, t, bob2, alice2))
	found := false
	for _, x := range texts {
		found = found || x == tag
	}
	if !found {
		t.Fatalf("Bob's history after the restore has no %q: %q", tag, texts)
	}
	// Writing works on the restored box state.
	sendTo(ctx, t, alice2, peerOf(ctx, t, alice2, bob2), tag+" after")
}
