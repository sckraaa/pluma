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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The gifts of a user are none, as Telegram answers for a user without
// gifts: Pluma has no payments (ADR 0016). Web K opens the profile of
// Saved Messages, the list of its sub-chats, only after this answer
// (task 020).
func TestSavedStarGifts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	gifts := func(peer *mtproto.InputPeer, collection *wrapperspb.Int32Value) (*mtproto.Payments_SavedStarGifts, error) {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLPaymentsGetSavedStarGifts{
			Constructor:  alice.c.Constructor(mtproto.Predicate_payments_getSavedStarGifts),
			Peer:         peer,
			CollectionId: collection,
			Limit:        50,
		})
		if err != nil {
			return nil, err
		}
		r, ok := o.(interface {
			To_Payments_SavedStarGifts() *mtproto.Payments_SavedStarGifts
		})
		if !ok {
			t.Fatalf("payments.getSavedStarGifts returned %T", o)
		}
		return r.To_Payments_SavedStarGifts(), nil
	}

	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	for name, peer := range map[string]*mtproto.InputPeer{"self": self, "Bob": peerOf(ctx, t, alice, bob)} {
		r, err := gifts(peer, nil)
		if err != nil {
			t.Fatalf("payments.getSavedStarGifts of %s: %v", name, err)
		}
		if r.GetPredicateName() != mtproto.Predicate_payments_savedStarGifts || r.GetCount() != 0 || len(r.GetGifts()) != 0 || r.GetNextOffset() != nil {
			t.Errorf("gifts of %s: %s, count %d, %d gifts, next offset %v", name, r.GetPredicateName(), r.GetCount(), len(r.GetGifts()), r.GetNextOffset())
		}
	}
	if _, err := gifts(self, wrapperspb.Int32(1)); !mtclient.IsRPCError(err, "COLLECTION_ID_INVALID") {
		t.Errorf("a collection: %v, want COLLECTION_ID_INVALID", err)
	}
	group := createChat(ctx, t, alice, "Gifts", bob)
	if _, err := gifts(chatPeer(alice, group), nil); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("a basic group: %v, want PEER_ID_INVALID", err)
	}
}
