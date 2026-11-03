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
)

// The services of the catalog (task 024) are routed to bff: before an
// import its methods answer empty lists with hash 0, never
// ERR_ENTERPRISE_IS_BLOCKED, which Android asks again for every second; a
// set it does not have is STICKERSET_INVALID; a method of those services it
// does not serve stays blocked. Then a collection is imported with
// pluma-admin catalog import, and the reaction, its set and its files come
// back with ids of this server (catalogImport). A stand used again holds
// the catalog of the run before: the checks of emptiness are skipped then.
func TestCatalog(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	u := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	call := func(name string, req mtproto.TLObject) mtproto.TLObject {
		t.Helper()
		o, err := u.c.Invoke(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return o
	}
	r := call("messages.getAvailableReactions", &mtproto.TLMessagesGetAvailableReactions{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAvailableReactions),
	}).(interface {
		To_Messages_AvailableReactions() *mtproto.Messages_AvailableReactions
	}).To_Messages_AvailableReactions()
	if r.GetPredicateName() != mtproto.Predicate_messages_availableReactions {
		t.Errorf("messages.getAvailableReactions: %v", r)
	}
	fresh := len(r.GetReactions()) == 0
	all := call("messages.getAllStickers", &mtproto.TLMessagesGetAllStickers{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getAllStickers),
	}).(interface {
		To_Messages_AllStickers() *mtproto.Messages_AllStickers
	}).To_Messages_AllStickers()
	if all.GetPredicateName() != mtproto.Predicate_messages_allStickers || all.GetHash() != 0 {
		t.Errorf("messages.getAllStickers: %v", all)
	}
	colors := call("help.getPeerColors", &mtproto.TLHelpGetPeerColors{
		Constructor: u.c.Constructor(mtproto.Predicate_help_getPeerColors),
	}).(interface {
		To_Help_PeerColors() *mtproto.Help_PeerColors
	}).To_Help_PeerColors()
	if colors.GetPredicateName() != mtproto.Predicate_help_peerColors {
		t.Errorf("help.getPeerColors: %v", colors)
	}

	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetStickerSet{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getStickerSet),
		Stickerset:  &mtproto.InputStickerSet{Constructor: u.c.Constructor(mtproto.Predicate_inputStickerSetAnimatedEmoji)},
	}); fresh && !mtclient.IsRPCError(err, "STICKERSET_INVALID") {
		t.Errorf("messages.getStickerSet of an empty catalog: %v", err)
	}
	if _, err := u.c.Invoke(ctx, &mtproto.TLStickersCheckShortName{
		Constructor: u.c.Constructor(mtproto.Predicate_stickers_checkShortName),
		ShortName:   "pluma",
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("stickers.checkShortName: %v", err)
	}

	catalogImport(ctx, t, u)
	catalogInstall(ctx, t, u, laptop)
}

// catalogInstall installs the imported set on one device of the account
// (task 030): the other device gets it whole with its installed_date, the
// list carries it with a hash that answers NotModified, and an archive and
// an uninstall reach the other device as "load the sets again".
func catalogInstall(ctx context.Context, t *testing.T, u, laptop *user) {
	t.Helper()
	before := getState(ctx, t, laptop)
	input := func() *mtproto.InputStickerSet {
		return mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "AnimatedEmojies"}).To_InputStickerSet()
	}
	all := func(hash int64) *mtproto.Messages_AllStickers {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetAllStickers{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getAllStickers),
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("messages.getAllStickers: %v", err)
		}
		return o.(interface {
			To_Messages_AllStickers() *mtproto.Messages_AllStickers
		}).To_Messages_AllStickers()
	}
	archived := func() int32 {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetArchivedStickers{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getArchivedStickers),
			Limit:       100,
		})
		if err != nil {
			t.Fatalf("messages.getArchivedStickers: %v", err)
		}
		return o.(interface {
			To_Messages_ArchivedStickers() *mtproto.Messages_ArchivedStickers
		}).To_Messages_ArchivedStickers().GetCount()
	}
	toggle := func(archive, uninstall bool) {
		t.Helper()
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesToggleStickerSets{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_toggleStickerSets),
			Archive:     archive,
			Uninstall:   uninstall,
			Stickersets: []*mtproto.InputStickerSet{input()},
		}); err != nil {
			t.Fatalf("messages.toggleStickerSets: %v", err)
		}
		waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStickerSets, func(*mtproto.Update) bool { return true })
	}

	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesInstallStickerSet{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_installStickerSet),
		Stickerset:  input(),
		Archived:    mtproto.BoolFalse,
	})
	if err != nil {
		t.Fatalf("messages.installStickerSet: %v", err)
	}
	if res := o.(interface {
		To_Messages_StickerSetInstallResult() *mtproto.Messages_StickerSetInstallResult
	}).To_Messages_StickerSetInstallResult(); res.GetPredicateName() != mtproto.Predicate_messages_stickerSetInstallResultSuccess {
		t.Fatalf("messages.installStickerSet: %v", res)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewStickerSet, func(x *mtproto.Update) bool {
		set := x.GetStickerset_MESSAGES_STICKERSET()
		return set.GetSet().GetShortName() == "AnimatedEmojies" && set.GetSet().GetInstalledDate().GetValue() != 0 && len(set.GetDocuments()) == 1
	})

	list := all(0)
	if len(list.GetSets()) != 1 || list.GetSets()[0].GetShortName() != "AnimatedEmojies" || list.GetSets()[0].GetInstalledDate() == nil || list.GetHash() == 0 {
		t.Fatalf("installed sets: %v", list)
	}
	if again := all(list.GetHash()); again.GetPredicateName() != mtproto.Predicate_messages_allStickersNotModified {
		t.Fatalf("installed sets with their hash: %v", again)
	}

	toggle(true, false)
	if list := all(0); len(list.GetSets()) != 0 || list.GetHash() != 0 || archived() != 1 {
		t.Fatalf("after the archive: %v, %d archived", list, archived())
	}
	toggle(false, true)
	if n := archived(); n != 0 {
		t.Fatalf("after the uninstall: %d archived", n)
	}

	// Installed on one device, uninstalled on the other: the other's
	// difference from before ends with its own uninstall, not with the
	// install, which would put the set back there.
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesInstallStickerSet{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_installStickerSet),
		Stickerset:  input(),
		Archived:    mtproto.BoolFalse,
	}); err != nil {
		t.Fatalf("messages.installStickerSet: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewStickerSet, func(*mtproto.Update) bool { return true })
	if _, err := laptop.c.Invoke(ctx, &mtproto.TLMessagesUninstallStickerSet{
		Constructor: laptop.c.Constructor(mtproto.Predicate_messages_uninstallStickerSet),
		Stickerset:  input(),
	}); err != nil {
		t.Fatalf("messages.uninstallStickerSet: %v", err)
	}
	var last string
	for _, x := range otherUpdatesFrom(ctx, t, laptop, before) {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateNewStickerSet, mtproto.Predicate_updateStickerSets, mtproto.Predicate_updateStickerSetsOrder:
			last = x.GetPredicateName()
		}
	}
	if last != mtproto.Predicate_updateStickerSets {
		t.Fatalf("the other device's difference ends with %q, not its own uninstall", last)
	}
}

// otherUpdatesFrom returns the updates without pts of u's
// updates.getDifference from st, following slices.
func otherUpdatesFrom(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState) []*mtproto.Update {
	t.Helper()
	var (
		out       []*mtproto.Update
		pts, date = st.GetPts(), st.GetDate()
	)
	for range 100 {
		o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
			Constructor: u.c.Constructor(mtproto.Predicate_updates_getDifference),
			Pts:         pts,
			Date:        date,
			Qts:         st.GetQts(),
		})
		if err != nil {
			t.Fatalf("updates.getDifference: %v", err)
		}
		d := o.(interface {
			To_Updates_Difference() *mtproto.Updates_Difference
		}).To_Updates_Difference()
		out = append(out, d.GetOtherUpdates()...)
		if d.GetPredicateName() != mtproto.Predicate_updates_differenceSlice {
			return out
		}
		pts, date = d.GetIntermediateState().GetPts(), d.GetIntermediateState().GetDate()
	}
	t.Fatal("updates.getDifference: too many slices")
	return nil
}
