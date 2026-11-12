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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// fullUserOf is users.getFullUser of to as from sees it: the full user and
// the user that comes with it.
func fullUserOf(ctx context.Context, t *testing.T, from *user, to *mtproto.InputUser) (*mtproto.UserFull, *mtproto.User) {
	t.Helper()
	o, err := from.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
		Constructor: from.c.Constructor(mtproto.Predicate_users_getFullUser),
		Id:          to,
	})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	full := o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull()
	for _, u := range full.GetUsers() {
		if u.GetId() == full.GetFullUser().GetId() {
			return full.GetFullUser(), u
		}
	}
	t.Fatalf("users.getFullUser came without its user: %v", full.GetUsers())
	return nil, nil
}

// TestUserLook (task 084) end to end, on a stand without a catalog: Alice
// gives her name the color 3, a built-in one; her other device and Bob, who
// has her as a contact, get updateUser and read the color. A color that is
// none, a profile color the catalog does not list and a collectible color
// are COLOR_INVALID; a status that is no custom emoji is DOCUMENT_INVALID
// and a collectible one COLLECTIBLE_INVALID; the recent and the
// collectible statuses are empty lists, not errors.
func TestUserLook(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toAlice := inputUserOf(ctx, t, bob, alice)
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsAddContact{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_addContact),
		Id:          toAlice,
		FirstName:   "Alice",
	}); err != nil {
		t.Fatalf("contacts.addContact: %v", err)
	}
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	color := func(forProfile bool, c *mtproto.PeerColor) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateColor{
			Constructor:         alice.c.Constructor(mtproto.Predicate_account_updateColor),
			ForProfile:          forProfile,
			Color_FLAGPEERCOLOR: c,
		})
		return err
	}
	peerColor := func(c int32) *mtproto.PeerColor {
		return &mtproto.PeerColor{Constructor: alice.c.Constructor(mtproto.Predicate_peerColor), Color: wrapperspb.Int32(c)}
	}
	if _, me := fullUserOf(ctx, t, alice, mtproto.MakeTLInputUserSelf(nil).To_InputUser()); me.GetColor_FLAGPEERCOLOR() != nil || me.GetProfileColor() != nil {
		t.Errorf("a new account has colors: %v, %v", me.GetColor_FLAGPEERCOLOR(), me.GetProfileColor())
	}
	if err := color(false, peerColor(3)); err != nil {
		t.Fatalf("account.updateColor: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == alice.self.GetId() })
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == alice.self.GetId() })
	if _, seen := fullUserOf(ctx, t, bob, toAlice); seen.GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 3 {
		t.Errorf("Bob reads Alice with the color %v, want 3", seen.GetColor_FLAGPEERCOLOR())
	}
	// The first color is a color of its own, not "none".
	if err := color(false, peerColor(0)); err != nil {
		t.Fatalf("account.updateColor to 0: %v", err)
	}
	if _, me := fullUserOf(ctx, t, laptop, mtproto.MakeTLInputUserSelf(nil).To_InputUser()); me.GetColor_FLAGPEERCOLOR() == nil || me.GetColor_FLAGPEERCOLOR().GetColor() == nil ||
		me.GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 0 {
		t.Errorf("the color 0 reads as %v, want a peerColor with the color 0", me.GetColor_FLAGPEERCOLOR())
	}
	// Without a color the name takes its color from the id again.
	if err := color(false, nil); err != nil {
		t.Fatalf("account.updateColor without a color: %v", err)
	}
	if _, me := fullUserOf(ctx, t, laptop, mtproto.MakeTLInputUserSelf(nil).To_InputUser()); me.GetColor_FLAGPEERCOLOR() != nil {
		t.Errorf("after the reset the color reads as %v", me.GetColor_FLAGPEERCOLOR())
	}
	if err := color(false, peerColor(-5)); !mtclient.IsRPCError(err, "COLOR_INVALID") {
		t.Errorf("a color that is none: %v, want COLOR_INVALID", err)
	}
	if err := color(true, peerColor(3)); !mtclient.IsRPCError(err, "COLOR_INVALID") {
		t.Errorf("a profile color of an empty catalog: %v, want COLOR_INVALID", err)
	}
	if err := color(false, &mtproto.PeerColor{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerColorCollectible), CollectibleId: 1}); !mtclient.IsRPCError(err, "COLOR_INVALID") {
		t.Errorf("a collectible color: %v, want COLOR_INVALID", err)
	}
	if err := color(false, &mtproto.PeerColor{
		Constructor: alice.c.Constructor(mtproto.Predicate_peerColor), Color: wrapperspb.Int32(1), BackgroundEmojiId_FLAGINT64: wrapperspb.Int64(1),
	}); !mtclient.IsRPCError(err, "DOCUMENT_INVALID") {
		t.Errorf("a pattern that is no custom emoji: %v, want DOCUMENT_INVALID", err)
	}

	status := func(s *mtproto.EmojiStatus) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateEmojiStatus{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateEmojiStatus),
			EmojiStatus: s,
		})
		return err
	}
	if err := status(&mtproto.EmojiStatus{Constructor: alice.c.Constructor(mtproto.Predicate_emojiStatusEmpty)}); err != nil {
		t.Errorf("no status for an account without one: %v", err)
	}
	if err := status(&mtproto.EmojiStatus{Constructor: alice.c.Constructor(mtproto.Predicate_emojiStatus), DocumentId: 1}); !mtclient.IsRPCError(err, "DOCUMENT_INVALID") {
		t.Errorf("a status of no custom emoji: %v, want DOCUMENT_INVALID", err)
	}
	if err := status(&mtproto.EmojiStatus{Constructor: alice.c.Constructor(mtproto.Predicate_inputEmojiStatusCollectible), CollectibleId: 1}); !mtclient.IsRPCError(err, "COLLECTIBLE_INVALID") {
		t.Errorf("a collectible status: %v, want COLLECTIBLE_INVALID", err)
	}
	for name, req := range map[string]mtproto.TLObject{
		"account.getRecentEmojiStatuses":      &mtproto.TLAccountGetRecentEmojiStatuses{Constructor: alice.c.Constructor(mtproto.Predicate_account_getRecentEmojiStatuses)},
		"account.getCollectibleEmojiStatuses": &mtproto.TLAccountGetCollectibleEmojiStatuses{Constructor: alice.c.Constructor(mtproto.Predicate_account_getCollectibleEmojiStatuses)},
	} {
		o, err := alice.c.Invoke(ctx, req)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if list := o.(interface {
			To_Account_EmojiStatuses() *mtproto.Account_EmojiStatuses
		}).To_Account_EmojiStatuses(); list.GetPredicateName() != mtproto.Predicate_account_emojiStatuses || len(list.GetStatuses()) != 0 {
			t.Errorf("%s: %v, want an empty list", name, list)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountClearRecentEmojiStatuses{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_clearRecentEmojiStatuses),
	}); err != nil {
		t.Errorf("account.clearRecentEmojiStatuses: %v", err)
	}
}

// TestRingtones (task 084) end to end: an Ogg sound uploaded is in the list
// at once, as an MP3 document, and the other device is told to load the
// list; saving it again changes nothing; unsaved, it is gone. A sound too
// long and a file that is no sound are refused with the errors tdesktop
// tells apart.
func TestRingtones(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	upload := func(path, name, mime string) (*mtproto.Document, error) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fileID := randomID()
		if _, err = alice.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_saveFilePart),
			FileId:      fileID,
			FilePart:    0,
			Bytes:       data,
		}); err != nil {
			t.Fatalf("upload.saveFilePart: %v", err)
		}
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountUploadRingtone{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_uploadRingtone),
			File:        mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: fileID, Parts: 1, Name: name}).To_InputFile(),
			FileName:    name,
			MimeType:    mime,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Document() *mtproto.Document }).To_Document(), nil
	}
	list := func(u *user, hash int64) *mtproto.Account_SavedRingtones {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLAccountGetSavedRingtones{
			Constructor: u.c.Constructor(mtproto.Predicate_account_getSavedRingtones),
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("account.getSavedRingtones: %v", err)
		}
		return o.(interface {
			To_Account_SavedRingtones() *mtproto.Account_SavedRingtones
		}).To_Account_SavedRingtones()
	}
	save := func(d *mtproto.Document, unsave bool) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountSaveRingtone{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_saveRingtone),
			Id: &mtproto.InputDocument{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputDocument), Id: d.GetId(), AccessHash: d.GetAccessHash(), FileReference: []byte{},
			},
			Unsave: mtproto.ToBool(unsave),
		})
		return err
	}

	if l := list(alice, 0); l.GetPredicateName() != mtproto.Predicate_account_savedRingtones || len(l.GetRingtones()) != 0 {
		t.Fatalf("the sounds of a new account: %v", l)
	}
	// One second of a 440 Hz tone:
	// ffmpeg -f lavfi -i sine=frequency=440:duration=1 -c:a libopus -b:a 32k ringtone.ogg
	d, err := upload("testdata/ringtone.ogg", "ding.ogg", "audio/ogg")
	if err != nil {
		t.Fatalf("account.uploadRingtone: %v", err)
	}
	if d.GetPredicateName() != mtproto.Predicate_document || d.GetMimeType() != "audio/mpeg" || d.GetSize2_INT64() == 0 {
		t.Fatalf("the uploaded sound: %v, want an MP3 document", d)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateSavedRingtones, func(*mtproto.Update) bool { return true })
	l := list(laptop, 0)
	if len(l.GetRingtones()) != 1 || l.GetRingtones()[0].GetId() != d.GetId() || l.GetHash() == 0 {
		t.Fatalf("the list on the other device: %v, want the uploaded sound", l)
	}
	if same := list(laptop, l.GetHash()); same.GetPredicateName() != mtproto.Predicate_account_savedRingtonesNotModified {
		t.Errorf("the list by its hash: %v, want not modified", same)
	}
	if err = save(d, false); err != nil {
		t.Errorf("saving a saved sound: %v", err)
	}
	if again := list(alice, 0); len(again.GetRingtones()) != 1 {
		t.Errorf("after saving it again: %d sounds, want 1", len(again.GetRingtones()))
	}

	// Six seconds of the tone as an MP3:
	// ffmpeg -f lavfi -i sine=frequency=440:duration=6 -c:a libmp3lame -b:a 32k ringtone-long.mp3
	if _, err = upload("testdata/ringtone-long.mp3", "long.mp3", "audio/mpeg"); !mtclient.IsRPCError(err, "RINGTONE_DURATION_TOO_LONG") {
		t.Errorf("a sound of six seconds: %v, want RINGTONE_DURATION_TOO_LONG", err)
	}
	if _, err = upload("testdata/profile-video.mp4", "video.mp3", "audio/mpeg"); !mtclient.IsRPCError(err, "RINGTONE_MIME_INVALID") {
		t.Errorf("a video named as a sound: %v, want RINGTONE_MIME_INVALID", err)
	}
	if _, err = upload("testdata/ringtone.ogg", "ding.ogg", "application/octet-stream"); !mtclient.IsRPCError(err, "RINGTONE_MIME_INVALID") {
		t.Errorf("a sound sent as no audio: %v, want RINGTONE_MIME_INVALID", err)
	}
	if err = save(&mtproto.Document{Id: d.GetId(), AccessHash: d.GetAccessHash() + 1}, false); !mtclient.IsRPCError(err, "RINGTONE_INVALID") {
		t.Errorf("a wrong access hash: %v, want RINGTONE_INVALID", err)
	}

	if err = save(d, true); err != nil {
		t.Fatalf("account.saveRingtone unsave: %v", err)
	}
	if after := list(alice, 0); len(after.GetRingtones()) != 0 {
		t.Errorf("after the unsave: %v", after.GetRingtones())
	}
}

// TestContactExtras (task 084) end to end: Bob keeps a private note on his
// contact Alice, reads it in her full user and loses it with the contact;
// Alice's temporary profile link gives Bob her user and an unknown one is
// IMPORT_TOKEN_INVALID; Bob suggests a birthday, which Alice gets as a
// service message, and once she has one a suggestion is BIRTHDAY_ALREADY.
func TestContactExtras(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toAlice := inputUserOf(ctx, t, bob, alice)
	toBob := inputUserOf(ctx, t, alice, bob)
	getState(ctx, t, alice)

	text := func(s string) *mtproto.TextWithEntities {
		return &mtproto.TextWithEntities{Constructor: bob.c.Constructor(mtproto.Predicate_textWithEntities), Text: s, Entities: []*mtproto.MessageEntity{}}
	}
	note := func(s string) error {
		_, err := bob.c.Invoke(ctx, &mtproto.TLContactsUpdateContactNote{
			Constructor: bob.c.Constructor(mtproto.Predicate_contacts_updateContactNote),
			Id:          toAlice,
			Note:        text(s),
		})
		return err
	}
	if err := note("too early"); !mtclient.IsRPCError(err, "CONTACT_MISSING") {
		t.Errorf("a note on a user who is no contact: %v, want CONTACT_MISSING", err)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsAddContact{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_addContact),
		Id:          toAlice,
		FirstName:   "Alice",
		Note:        text("from the conference"),
	}); err != nil {
		t.Fatalf("contacts.addContact: %v", err)
	}
	if full, _ := fullUserOf(ctx, t, bob, toAlice); full.GetNote().GetText() != "from the conference" {
		t.Errorf("the note sent with the contact reads as %v", full.GetNote())
	}
	if err := note("likes tea"); err != nil {
		t.Fatalf("contacts.updateContactNote: %v", err)
	}
	if full, _ := fullUserOf(ctx, t, bob, toAlice); full.GetNote().GetText() != "likes tea" {
		t.Errorf("the changed note reads as %v", full.GetNote())
	}
	// The note is Bob's alone.
	if full, _ := fullUserOf(ctx, t, alice, toBob); full.GetNote() != nil {
		t.Errorf("Alice reads a note on Bob: %v", full.GetNote())
	}
	if full, _ := fullUserOf(ctx, t, alice, mtproto.MakeTLInputUserSelf(nil).To_InputUser()); full.GetNote() != nil {
		t.Errorf("Alice reads a note on herself: %v", full.GetNote())
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsDeleteContacts{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_deleteContacts),
		Id:          []*mtproto.InputUser{toAlice},
	}); err != nil {
		t.Fatalf("contacts.deleteContacts: %v", err)
	}
	if full, _ := fullUserOf(ctx, t, bob, toAlice); full.GetNote() != nil {
		t.Errorf("the note outlived the contact: %v", full.GetNote())
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsAddContact{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_addContact),
		Id:          toAlice,
		FirstName:   "Alice",
	}); err != nil {
		t.Fatalf("contacts.addContact again: %v", err)
	}
	if full, _ := fullUserOf(ctx, t, bob, toAlice); full.GetNote() != nil {
		t.Errorf("the contact added again has the old note: %v", full.GetNote())
	}

	// The temporary link to Alice's profile.
	o, err := alice.c.Invoke(ctx, &mtproto.TLContactsExportContactToken{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_exportContactToken),
	})
	if err != nil {
		t.Fatalf("contacts.exportContactToken: %v", err)
	}
	exported := o.(interface {
		To_ExportedContactToken() *mtproto.ExportedContactToken
	}).To_ExportedContactToken()
	at := strings.LastIndex(exported.GetUrl(), "/contact/")
	if !strings.HasPrefix(exported.GetUrl(), "https://") || at < 0 {
		t.Fatalf("the link %q, want https://<host>/contact/<token>", exported.GetUrl())
	}
	if left := int64(exported.GetExpires()) - time.Now().Unix(); left < 240 || left > 330 {
		t.Errorf("the link ends in %d seconds, want about 300", left)
	}
	token := exported.GetUrl()[at+len("/contact/"):]
	o, err = bob.c.Invoke(ctx, &mtproto.TLContactsImportContactToken{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_importContactToken),
		Token:       token,
	})
	if err != nil {
		t.Fatalf("contacts.importContactToken: %v", err)
	}
	if u := o.(interface{ To_User() *mtproto.User }).To_User(); u.GetId() != alice.self.GetId() || u.GetAccessHash() == nil || u.GetSelf() {
		t.Errorf("the user of the link: %v, want Alice with an access hash", u)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLContactsImportContactToken{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_importContactToken),
		Token:       strings.Repeat("A", len(token)),
	}); !mtclient.IsRPCError(err, "IMPORT_TOKEN_INVALID") {
		t.Errorf("an unknown token: %v, want IMPORT_TOKEN_INVALID", err)
	}

	// A birthday suggested to Alice.
	suggest := func(day, month int32) error {
		_, err := bob.c.Invoke(ctx, &mtproto.TLUsersSuggestBirthday{
			Constructor: bob.c.Constructor(mtproto.Predicate_users_suggestBirthday),
			Id:          toAlice,
			Birthday:    &mtproto.Birthday{Constructor: bob.c.Constructor(mtproto.Predicate_birthday), Day: day, Month: month},
		})
		return err
	}
	if err = suggest(31, 2); !mtclient.IsRPCError(err, "BIRTHDAY_INVALID") {
		t.Errorf("the 31st of February: %v, want BIRTHDAY_INVALID", err)
	}
	if err = suggest(4, 3); err != nil {
		t.Fatalf("users.suggestBirthday: %v", err)
	}
	if _, err = alice.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, m := range newMessages(us) {
			if a := m.GetAction(); a.GetPredicateName() == mtproto.Predicate_messageActionSuggestBirthday && a.GetBirthday().GetDay() == 4 && a.GetBirthday().GetMonth() == 3 {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Alice got no suggestion: %v", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBirthday{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBirthday),
		Birthday:    mtproto.MakeTLBirthday(&mtproto.Birthday{Month: 3, Day: 4}).To_Birthday(),
	}); err != nil {
		t.Fatalf("account.updateBirthday: %v", err)
	}
	if err = suggest(5, 6); !mtclient.IsRPCError(err, "BIRTHDAY_ALREADY") {
		t.Errorf("a suggestion to a user with a birthday: %v, want BIRTHDAY_ALREADY", err)
	}
}

// TestAccountSmallSettings (task 084) end to end: what an account saves
// for auto-download is accepted and does not change the presets everybody
// gets; the invite text is the address of the site alone; a muted user is
// an exception of the users and of no other category.
func TestAccountSmallSettings(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)

	presets := func() *mtproto.Account_AutoDownloadSettings {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetAutoDownloadSettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_getAutoDownloadSettings),
		})
		if err != nil {
			t.Fatalf("account.getAutoDownloadSettings: %v", err)
		}
		return o.(interface {
			To_Account_AutoDownloadSettings() *mtproto.Account_AutoDownloadSettings
		}).To_Account_AutoDownloadSettings()
	}
	before := presets()
	for _, high := range []bool{false, true} {
		if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountSaveAutoDownloadSettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_saveAutoDownloadSettings),
			High:        high,
			Settings: &mtproto.AutoDownloadSettings{
				Constructor: alice.c.Constructor(mtproto.Predicate_autoDownloadSettings), Disabled: true, PhotoSizeMax: 0, VideoSizeMax_INT64: 7, FileSizeMax_INT64: 0,
			},
		}); err != nil {
			t.Fatalf("account.saveAutoDownloadSettings (high %v): %v", high, err)
		}
	}
	after := presets()
	if after.GetMedium().GetDisabled() || after.GetHigh().GetDisabled() || after.GetMedium().GetVideoSizeMax_INT64() != before.GetMedium().GetVideoSizeMax_INT64() ||
		after.GetHigh().GetVideoUploadMaxbitrate() != before.GetHigh().GetVideoUploadMaxbitrate() || after.GetHigh().GetVideoUploadMaxbitrate() == 0 {
		t.Errorf("the presets follow what was saved: %v, before %v", after, before)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLHelpGetInviteText{Constructor: alice.c.Constructor(mtproto.Predicate_help_getInviteText)})
	if err != nil {
		t.Fatalf("help.getInviteText: %v", err)
	}
	if m := o.(interface {
		To_Help_InviteText() *mtproto.Help_InviteText
	}).To_Help_InviteText().GetMessage(); !strings.HasPrefix(m, "https://") || strings.ContainsAny(m, " \n") {
		t.Errorf("the invite text %q, want an address alone", m)
	}

	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
		Peer: &mtproto.InputNotifyPeer{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputNotifyPeer),
			Peer:        toBob,
		},
		Settings: &mtproto.InputPeerNotifySettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerNotifySettings),
			MuteUntil:   wrapperspb.Int32(int32(time.Now().Unix()) + 3600),
		},
	}); err != nil {
		t.Fatalf("account.updateNotifySettings: %v", err)
	}
	exceptions := func(category string) []*mtproto.Update {
		t.Helper()
		in := &mtproto.TLAccountGetNotifyExceptions{Constructor: alice.c.Constructor(mtproto.Predicate_account_getNotifyExceptions)}
		if category != "" {
			in.Peer = &mtproto.InputNotifyPeer{Constructor: alice.c.Constructor(category)}
		}
		o, err := alice.c.Invoke(ctx, in)
		if err != nil {
			t.Fatalf("account.getNotifyExceptions %s: %v", category, err)
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates()
	}
	for _, category := range []string{"", mtproto.Predicate_inputNotifyUsers} {
		if us := exceptions(category); len(us) != 1 || us[0].GetPeer_NOTIFYPEER().GetPeer().GetUserId() != bob.self.GetId() || us[0].GetNotifySettings().GetMuteUntil() == nil {
			t.Errorf("the exceptions of %q: %v, want the muted user", category, us)
		}
	}
	for _, category := range []string{mtproto.Predicate_inputNotifyChats, mtproto.Predicate_inputNotifyBroadcasts} {
		if us := exceptions(category); len(us) != 0 {
			t.Errorf("the exceptions of %s: %v, want none", category, us)
		}
	}
}
