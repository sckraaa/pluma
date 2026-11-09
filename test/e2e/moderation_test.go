//go:build e2e

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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Reports and restrictions (task 025, ADR 0039): a report goes through the
// tree of messages.report into the queue, and pluma-admin, the emergency
// access to the same decisions the panel makes, restricts and bans as the
// clients see it.

func invokeReport(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, ids []int32, option []byte, comment string) *mtproto.ReportResult {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesReportFC78AF9B{
		Constructor: mtproto.CRC32_messages_reportFC78AF9B,
		Peer:        peer,
		Id:          ids,
		Option:      option,
		Message:     comment,
	})
	if err != nil {
		t.Fatalf("messages.report: %v", err)
	}
	r, ok := o.(interface{ To_ReportResult() *mtproto.ReportResult })
	if !ok {
		t.Fatalf("messages.report returned %T", o)
	}
	return r.To_ReportResult()
}

// sendCodeFails tells that auth.sendCode for phone is refused with code.
func sendCodeFails(ctx context.Context, t *testing.T, phone, code string) {
	t.Helper()
	c, err := mtclient.Dial(ctx, config(t, "e2e banned"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, "banned", c)
	_, err = c.Invoke(ctx, &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone,
		ApiId:       1,
		ApiHash:     "e2e",
		Settings:    &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	})
	if !mtclient.IsRPCError(err, code) {
		t.Errorf("auth.sendCode of a banned account: %v, want %s", err, code)
	}
}

func TestModerationReportTreeAndRestriction(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	// Carol has Alice in contacts, Bob does not.
	importContacts(ctx, t, carol, phoneContact(carol.c, 1, formattedPhone(alice.phone), "Alice", ""))

	aliceToBob := peerOf(ctx, t, alice, bob)
	bobToAlice := peerOf(ctx, t, bob, alice)
	text := fmt.Sprintf("spammy %d", randomID())
	sendTo(ctx, t, alice, aliceToBob, text)
	m, _ := historyMessage(ctx, t, bob, bobToAlice, text)
	ids := []int32{m.GetId()}

	root := invokeReport(ctx, t, bob, bobToAlice, ids, nil, "")
	if root.GetPredicateName() != mtproto.Predicate_reportResultChooseOption || len(root.GetOptions()) == 0 {
		t.Fatalf("root of the report tree: %v", root)
	}
	var spam *mtproto.MessageReportOption
	for _, o := range root.GetOptions() {
		if string(o.GetOption()) == "spam" {
			spam = o
		}
	}
	if spam == nil {
		t.Fatalf("no spam reason in %v", root.GetOptions())
	}
	if !t.Run("an unknown option", func(t *testing.T) {
		_, err := bob.c.Invoke(ctx, &mtproto.TLMessagesReportFC78AF9B{
			Constructor: mtproto.CRC32_messages_reportFC78AF9B,
			Peer:        bobToAlice,
			Id:          ids,
			Option:      []byte("nonsense"),
		})
		if !mtclient.IsRPCError(err, "OPTION_INVALID") {
			t.Fatalf("option %v", err)
		}
	}) {
		return
	}

	comment := invokeReport(ctx, t, bob, bobToAlice, ids, spam.GetOption(), "")
	if comment.GetPredicateName() != mtproto.Predicate_reportResultAddComment || !comment.GetOptional() {
		t.Fatalf("after a reason: %v", comment)
	}
	done := invokeReport(ctx, t, bob, bobToAlice, ids, comment.GetOption(), "it is spam")
	if done.GetPredicateName() != mtproto.Predicate_reportResultReported {
		t.Fatalf("the end of the tree: %v", done)
	}

	queue := admin(t, "moderation", "reports")
	row := findQueueRow(queue, bob.self.GetId(), alice.self.GetId())
	if !strings.Contains(row, "spam") {
		t.Fatalf("no report in the queue: %s", queue)
	}

	admin(t, "moderation", "restrict", alice.phone, "-days", "1")
	_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        aliceToBob, Message: "still spammy", RandomId: randomID(),
	})
	if !mtclient.IsRPCError(err, "PEER_FLOOD") {
		t.Errorf("restricted, to a stranger: %v, want PEER_FLOOD", err)
	}
	// Carol has Alice in contacts: the chat stays open.
	sendTo(ctx, t, alice, peerOf(ctx, t, alice, carol), "hi carol")

	admin(t, "moderation", "lift", alice.phone)
	sendTo(ctx, t, alice, aliceToBob, "free again")

	admin(t, "moderation", "ban", alice.phone)
	sendCodeFails(ctx, t, alice.phone, "PHONE_NUMBER_BANNED")
	admin(t, "moderation", "lift", alice.phone)
}

// findQueueRow finds the row of the report's reporter and target.
func findQueueRow(queue string, reporter, target int64) string {
	for _, line := range strings.Split(queue, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[1] == fmt.Sprint(reporter) && fields[4] == fmt.Sprint(target) {
			return line
		}
	}
	return ""
}

// A report of a message the reporter does not have is refused: a report
// names only what the reporter could see.
func TestModerationReportUnknownMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesReportFC78AF9B{
		Constructor: mtproto.CRC32_messages_reportFC78AF9B,
		Peer:        peerOf(ctx, t, alice, bob),
		Id:          []int32{987654321},
		Option:      []byte("spam:c"),
	})
	var rpcErr *mtclient.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Message != "MESSAGE_ID_INVALID" {
		t.Errorf("a report of no message: %v", err)
	}
}
