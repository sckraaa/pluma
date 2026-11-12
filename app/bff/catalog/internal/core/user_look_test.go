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

package core

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/emojistatus"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
	"github.com/teamgram/teamgram-server/pkg/ringtones/saved"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// lookUsers is biz user; it keeps what it was asked.
type lookUsers struct {
	user_client.UserClient
	color  *userpb.TLUserSetColor
	status *userpb.TLUserUpdateEmojiStatus
}

func (f *lookUsers) UserSetColor(_ context.Context, in *userpb.TLUserSetColor) (*mtproto.Bool, error) {
	f.color = in
	return mtproto.BoolTrue, nil
}

func (f *lookUsers) UserUpdateEmojiStatus(_ context.Context, in *userpb.TLUserUpdateEmojiStatus) (*mtproto.Bool, error) {
	f.status = in
	return mtproto.BoolTrue, nil
}

// recentStatuses are the recent statuses of one account.
type recentStatuses struct {
	ids     []int64
	cleared int
}

func (f *recentStatuses) Recent(context.Context, int64) ([]emojistatus.Entry, error) {
	out := make([]emojistatus.Entry, len(f.ids))
	for i, id := range f.ids {
		out[i] = emojistatus.Entry{DocumentID: id}
	}
	return out, nil
}

func (f *recentStatuses) Clear(context.Context, int64) error {
	f.cleared++
	f.ids = nil
	return nil
}

func newUserLookCore(t *testing.T) (*CatalogCore, *lookUsers, *recentStatuses) {
	t.Helper()
	c, _, _ := newLookCore(t)
	users, recent := &lookUsers{}, &recentStatuses{}
	c.svcCtx.User, c.svcCtx.EmojiStatuses = users, recent
	return c, users, recent
}

// The color of an account in each of the three forms of the method: a
// color built in or listed for its kind, none when it is left out, a
// pattern that is a custom emoji of the catalog, with or without a color;
// a collectible color is COLOR_INVALID.
func TestAccountUpdateColor(t *testing.T) {
	c, users, _ := newUserLookCore(t)
	peerColor := func(color *int32, emoji int64) *mtproto.PeerColor {
		p := &mtproto.PeerColor{}
		if color != nil {
			p.Color = wrapperspb.Int32(*color)
		}
		if emoji != 0 {
			p.BackgroundEmojiId_FLAGINT64 = wrapperspb.Int64(emoji)
		}
		return mtproto.MakeTLPeerColor(p).To_PeerColor()
	}
	color := func(v int32) *int32 { return &v }
	for _, k := range []struct {
		name       string
		forProfile bool
		in         *mtproto.PeerColor
		want       error
		sent       int32
		emoji      int64
	}{
		{"the first built-in name color", false, peerColor(color(0), 0), nil, 0, 0},
		{"a listed name color with a pattern", false, peerColor(color(7), lookEmoji), nil, 7, lookEmoji},
		{"an unknown name color", false, peerColor(color(8), 0), errColorInvalid, 0, 0},
		{"a negative color", false, peerColor(color(-2), 0), errColorInvalid, 0, 0},
		{"no peerColor", true, nil, nil, userpb.NoColor, 0},
		{"a peerColor without a color", true, peerColor(nil, 0), nil, userpb.NoColor, 0},
		{"a pattern without a color", true, peerColor(nil, lookEmoji), nil, userpb.NoColor, lookEmoji},
		{"a listed profile color", true, peerColor(color(15), 0), nil, 15, 0},
		{"a name color for the profile", true, peerColor(color(3), 0), errColorInvalid, 0, 0},
		{"a pattern of a sticker", false, peerColor(color(1), lookSticker), mtproto.ErrDocumentInvalid, 0, 0},
		{"a pattern not in the catalog", true, peerColor(nil, 12345), mtproto.ErrDocumentInvalid, 0, 0},
		{"a collectible color", false, mtproto.MakeTLInputPeerColorCollectible(&mtproto.PeerColor{CollectibleId: 5}).To_PeerColor(), errColorInvalid, 0, 0},
	} {
		users.color = nil
		_, err := c.AccountUpdateColor(&mtproto.TLAccountUpdateColor{
			Constructor:         mtproto.TLConstructor_CRC32_account_updateColor_684d214e,
			ForProfile:          k.forProfile,
			Color_FLAGPEERCOLOR: k.in,
		})
		if !errors.Is(err, k.want) {
			t.Errorf("%s: %v, want %v", k.name, err, k.want)
			continue
		}
		if k.want != nil {
			if users.color != nil {
				t.Errorf("%s: sent to biz user", k.name)
			}
			continue
		}
		if users.color.GetColor() != k.sent || users.color.GetBackgroundEmojiId() != k.emoji || users.color.GetForProfile() != k.forProfile || users.color.GetUserId() != 1 {
			t.Errorf("%s: sent %v", k.name, users.color)
		}
	}

	// The older forms: an optional int, then a required one.
	if _, err := c.AccountUpdateColor(&mtproto.TLAccountUpdateColor{
		Constructor: mtproto.TLConstructor_CRC32_account_updateColor_7cefa15d, Color_FLAGINT32: wrapperspb.Int32(0), BackgroundEmojiId: wrapperspb.Int64(lookEmoji),
	}); err != nil || users.color.GetColor() != 0 || users.color.GetBackgroundEmojiId() != lookEmoji {
		t.Errorf("the form with an optional int: %v, sent %v", err, users.color)
	}
	if _, err := c.AccountUpdateColor(&mtproto.TLAccountUpdateColor{
		Constructor: mtproto.TLConstructor_CRC32_account_updateColor_7cefa15d, ForProfile: true,
	}); err != nil || users.color.GetColor() != userpb.NoColor || !users.color.GetForProfile() {
		t.Errorf("the form with an optional int left out: %v, sent %v", err, users.color)
	}
	if _, err := c.AccountUpdateColor(&mtproto.TLAccountUpdateColor{
		Constructor: mtproto.TLConstructor_CRC32_account_updateColor_a001cc43, Color_INT32: 0,
	}); err != nil || users.color.GetColor() != 0 {
		t.Errorf("the form with a required int 0: %v, sent %v", err, users.color)
	}
}

// A status is a custom emoji of the catalog, with or without an end, or
// none; a sticker is DOCUMENT_INVALID and a collectible one
// COLLECTIBLE_INVALID.
func TestAccountUpdateEmojiStatus(t *testing.T) {
	c, users, _ := newUserLookCore(t)
	update := func(s *mtproto.EmojiStatus) error {
		users.status = nil
		_, err := c.AccountUpdateEmojiStatus(&mtproto.TLAccountUpdateEmojiStatus{EmojiStatus: s})
		return err
	}
	if err := update(mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: lookEmoji, Until_FLAGINT32: wrapperspb.Int32(99)}).To_EmojiStatus()); err != nil ||
		users.status.GetEmojiStatusDocumentId() != lookEmoji || users.status.GetEmojiStatusUntil() != 99 || users.status.GetUserId() != 1 {
		t.Errorf("a status with an end: %v, sent %v", err, users.status)
	}
	if err := update(mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: lookEmoji}).To_EmojiStatus()); err != nil || users.status.GetEmojiStatusUntil() != 0 {
		t.Errorf("a status without an end: %v, sent %v", err, users.status)
	}
	if err := update(mtproto.MakeTLEmojiStatusEmpty(nil).To_EmojiStatus()); err != nil || users.status == nil || users.status.GetEmojiStatusDocumentId() != 0 {
		t.Errorf("no status: %v, sent %v", err, users.status)
	}
	for name, k := range map[string]struct {
		in   *mtproto.EmojiStatus
		want error
	}{
		"a sticker":            {mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: lookSticker}).To_EmojiStatus(), mtproto.ErrDocumentInvalid},
		"a document of no one": {mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: 12345}).To_EmojiStatus(), mtproto.ErrDocumentInvalid},
		"no document":          {mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{}).To_EmojiStatus(), mtproto.ErrDocumentInvalid},
		"an input collectible": {mtproto.MakeTLInputEmojiStatusCollectible(&mtproto.EmojiStatus{CollectibleId: 5}).To_EmojiStatus(), errCollectibleInvalid},
		"a collectible":        {mtproto.MakeTLEmojiStatusCollectible(&mtproto.EmojiStatus{CollectibleId: 5, DocumentId: lookEmoji}).To_EmojiStatus(), errCollectibleInvalid},
		"nothing":              {nil, mtproto.ErrDocumentInvalid},
	} {
		if err := update(k.in); !errors.Is(err, k.want) || users.status != nil {
			t.Errorf("%s: %v, sent %v; want %v and nothing sent", name, err, users.status, k.want)
		}
	}
}

// The recent statuses are the account's own, the newest first, with a hash
// that answers "not modified"; clearing empties them; the collectible ones
// are an empty list, not an error.
func TestEmojiStatusLists(t *testing.T) {
	c, _, recent := newUserLookCore(t)
	recent.ids = []int64{30, 10, 20}
	r, err := c.AccountGetRecentEmojiStatuses(&mtproto.TLAccountGetRecentEmojiStatuses{})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_account_emojiStatuses || r.GetHash() == 0 {
		t.Fatalf("the recent statuses: %v, %v", r, err)
	}
	var ids []int64
	for _, s := range r.GetStatuses() {
		if s.GetPredicateName() != mtproto.Predicate_emojiStatus || s.GetUntil_FLAGINT32() != nil {
			t.Errorf("a recent status %v, want emojiStatus without an end", s)
		}
		ids = append(ids, s.GetDocumentId())
	}
	if fmt.Sprint(ids) != "[30 10 20]" {
		t.Errorf("the recent statuses %v, want [30 10 20]", ids)
	}
	if again, err := c.AccountGetRecentEmojiStatuses(&mtproto.TLAccountGetRecentEmojiStatuses{Hash: r.GetHash()}); err != nil || again.GetPredicateName() != mtproto.Predicate_account_emojiStatusesNotModified {
		t.Errorf("the same hash: %v, %v; want not modified", again, err)
	}
	recent.ids = []int64{10, 30, 20}
	if other, err := c.AccountGetRecentEmojiStatuses(&mtproto.TLAccountGetRecentEmojiStatuses{Hash: r.GetHash()}); err != nil || other.GetPredicateName() != mtproto.Predicate_account_emojiStatuses || other.GetHash() == r.GetHash() {
		t.Errorf("another order with the old hash: %v, %v; want the list with another hash", other, err)
	}

	if ok, err := c.AccountClearRecentEmojiStatuses(&mtproto.TLAccountClearRecentEmojiStatuses{}); err != nil || !mtproto.FromBool(ok) || recent.cleared != 1 {
		t.Errorf("clear: %v, %v, cleared %d times", ok, err, recent.cleared)
	}
	if empty, err := c.AccountGetRecentEmojiStatuses(&mtproto.TLAccountGetRecentEmojiStatuses{Hash: r.GetHash()}); err != nil || empty.GetPredicateName() != mtproto.Predicate_account_emojiStatuses || empty.GetStatuses() == nil || len(empty.GetStatuses()) != 0 {
		t.Errorf("after the clear: %v, %v; want an empty list", empty, err)
	}

	col, err := c.AccountGetCollectibleEmojiStatuses(&mtproto.TLAccountGetCollectibleEmojiStatuses{Hash: 7})
	if err != nil || col.GetPredicateName() != mtproto.Predicate_account_emojiStatuses || col.GetStatuses() == nil || len(col.GetStatuses()) != 0 {
		t.Errorf("the collectible statuses: %v, %v; want an empty list", col, err)
	}
}

// soundMedia is media with the documents of sounds; it keeps the uploads.
type soundMedia struct {
	media_client.MediaClient
	docs      map[int64]*mtproto.Document
	uploaded  []*media.TLMediaUploadRingtoneFile
	uploadErr error
	nextId    int64
}

func (f *soundMedia) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	if d, ok := f.docs[in.GetId()]; ok {
		return d, nil
	}
	return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.GetId()}).To_Document(), nil
}

func (f *soundMedia) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	out := &media.Vector_Document{}
	// In another order than asked: the list must not depend on it.
	for i := len(in.GetIdList()) - 1; i >= 0; i-- {
		if d, ok := f.docs[in.GetIdList()[i]]; ok {
			out.Datas = append(out.Datas, d)
		}
	}
	return out, nil
}

func (f *soundMedia) MediaUploadRingtoneFile(_ context.Context, in *media.TLMediaUploadRingtoneFile) (*mtproto.Document, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	f.uploaded = append(f.uploaded, in)
	f.nextId++
	d := sound(f.nextId, ringtones.MimeMP3, 1000, 2)
	f.docs[d.GetId()] = d
	return d, nil
}

func sound(id int64, mime string, size int64, seconds float64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id: id, AccessHash: id * 10, MimeType: mime, Size2_INT64: size, Size2_INT32: int32(size),
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Duration_FLOAT64: seconds, Duration_INT32: int32(seconds)}).To_DocumentAttribute(),
		},
	}).To_Document()
}

// savedSounds is the list of one account, the newest first.
type savedSounds struct {
	ids  []int64
	full bool
}

func (f *savedSounds) Saved(context.Context, int64) ([]saved.Entry, error) {
	out := make([]saved.Entry, len(f.ids))
	for i, id := range f.ids {
		out[i] = saved.Entry{DocumentID: id}
	}
	if f.full {
		out = make([]saved.Entry, ringtones.SavedCountMax)
	}
	return out, nil
}

func (f *savedSounds) Save(_ context.Context, _, id int64) error {
	if f.full {
		return saved.ErrTooMany
	}
	f.ids = append([]int64{id}, f.ids...)
	return nil
}

func (f *savedSounds) Unsave(_ context.Context, _, id int64) error {
	for i, v := range f.ids {
		if v == id {
			f.ids = append(f.ids[:i], f.ids[i+1:]...)
			break
		}
	}
	return nil
}

func newSoundCore() (*CatalogCore, *soundMedia, *savedSounds) {
	m := &soundMedia{docs: map[int64]*mtproto.Document{}, nextId: 1000}
	s := &savedSounds{}
	c := New(context.Background(), &svc.ServiceContext{Media: m, Ringtones: s})
	c.MD = &metadata.RpcMetadata{UserId: 1, PermAuthKeyId: 2}
	return c, m, s
}

func inputSound(d *mtproto.Document) *mtproto.InputDocument {
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: d.GetId(), AccessHash: d.GetAccessHash()}).To_InputDocument()
}

// A sound to save is an MP3 document within the limits, named with its
// access hash; saved, it heads the list; the list keeps its order whatever
// order media answers in and has a hash; unsaving removes it.
func TestSavedRingtones(t *testing.T) {
	c, m, s := newSoundCore()
	ok1, ok2 := sound(1, "audio/mpeg", ringtones.SizeMax, ringtones.DurationMax), sound(2, "audio/mp3", 500, 1)
	bad := map[string]*mtproto.Document{
		"an Ogg sound":  sound(3, "audio/ogg", 500, 1),
		"too large":     sound(4, "audio/mpeg", ringtones.SizeMax+1, 1),
		"too long":      sound(5, "audio/mpeg", 500, ringtones.DurationMax+1),
		"not a sound":   mtproto.MakeTLDocument(&mtproto.Document{Id: 6, AccessHash: 60, MimeType: "audio/mpeg", Size2_INT64: 500}).To_Document(),
		"an empty file": sound(7, "audio/mpeg", 0, 1),
	}
	for _, d := range append([]*mtproto.Document{ok1, ok2}, bad["an Ogg sound"], bad["too large"], bad["too long"], bad["not a sound"], bad["an empty file"]) {
		m.docs[d.GetId()] = d
	}
	save := func(id *mtproto.InputDocument, unsave bool) error {
		r, err := c.AccountSaveRingtone(&mtproto.TLAccountSaveRingtone{Id: id, Unsave: mtproto.ToBool(unsave)})
		if err == nil && r.GetPredicateName() != mtproto.Predicate_account_savedRingtone {
			t.Errorf("answer %v, want account.savedRingtone", r)
		}
		return err
	}
	for name, d := range bad {
		if err := save(inputSound(d), false); !errors.Is(err, ringtones.ErrInvalid) {
			t.Errorf("%s: %v, want RINGTONE_INVALID", name, err)
		}
	}
	wrongHash := inputSound(ok1)
	wrongHash.AccessHash++
	for name, id := range map[string]*mtproto.InputDocument{
		"a wrong access hash":  wrongHash,
		"a document of no one": mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: 999, AccessHash: 1}).To_InputDocument(),
		"an empty input":       mtproto.MakeTLInputDocumentEmpty(nil).To_InputDocument(),
		"nothing":              nil,
	} {
		if err := save(id, false); !errors.Is(err, ringtones.ErrInvalid) {
			t.Errorf("%s: %v, want RINGTONE_INVALID", name, err)
		}
	}
	if len(s.ids) != 0 {
		t.Fatalf("saved %v by refused requests", s.ids)
	}

	if err := save(inputSound(ok1), false); err != nil {
		t.Fatal(err)
	}
	if err := save(inputSound(ok2), false); err != nil {
		t.Fatal(err)
	}
	list, err := c.AccountGetSavedRingtones(&mtproto.TLAccountGetSavedRingtones{})
	if err != nil || list.GetPredicateName() != mtproto.Predicate_account_savedRingtones || len(list.GetRingtones()) != 2 ||
		list.GetRingtones()[0].GetId() != 2 || list.GetRingtones()[1].GetId() != 1 || list.GetHash() == 0 {
		t.Fatalf("the list %v, %v; want the sounds 2 and 1", list, err)
	}
	if same, err := c.AccountGetSavedRingtones(&mtproto.TLAccountGetSavedRingtones{Hash: list.GetHash()}); err != nil || same.GetPredicateName() != mtproto.Predicate_account_savedRingtonesNotModified {
		t.Errorf("the same hash: %v, %v; want not modified", same, err)
	}
	if err = save(inputSound(ok2), true); err != nil {
		t.Fatal(err)
	}
	after, err := c.AccountGetSavedRingtones(&mtproto.TLAccountGetSavedRingtones{Hash: list.GetHash()})
	if err != nil || len(after.GetRingtones()) != 1 || after.GetRingtones()[0].GetId() != 1 {
		t.Errorf("after unsaving: %v, %v; want the sound 1", after, err)
	}
	// A document media no longer has is left out of the list.
	delete(m.docs, 1)
	if gone, err := c.AccountGetSavedRingtones(&mtproto.TLAccountGetSavedRingtones{}); err != nil || gone.GetRingtones() == nil || len(gone.GetRingtones()) != 0 {
		t.Errorf("without the document: %v, %v; want an empty list", gone, err)
	}

	s.full = true
	m.docs[1] = ok1
	if err = save(inputSound(ok1), false); !errors.Is(err, ringtones.ErrInvalid) {
		t.Errorf("a full list: %v, want RINGTONE_INVALID", err)
	}
}

// An upload stores the sound under the key that uploaded its parts and
// saves it: the answer is the document, already in the list. A full list
// refuses before anything is stored; the errors of the store pass through.
func TestAccountUploadRingtone(t *testing.T) {
	c, m, s := newSoundCore()
	file := mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 5, Parts: 1, Name: "ding.ogg"}).To_InputFile()
	d, err := c.AccountUploadRingtone(&mtproto.TLAccountUploadRingtone{File: file, FileName: "ding.ogg", MimeType: "audio/ogg"})
	if err != nil || d.GetMimeType() != ringtones.MimeMP3 {
		t.Fatalf("the upload: %v, %v", d, err)
	}
	if len(m.uploaded) != 1 || m.uploaded[0].GetOwnerId() != 2 || m.uploaded[0].GetFileName() != "ding.ogg" || m.uploaded[0].GetMimeType() != "audio/ogg" {
		t.Errorf("asked of media %v, want the file of the key 2", m.uploaded)
	}
	if fmt.Sprint(s.ids) != fmt.Sprint([]int64{d.GetId()}) {
		t.Errorf("the list %v, want the uploaded sound %d", s.ids, d.GetId())
	}

	if _, err = c.AccountUploadRingtone(&mtproto.TLAccountUploadRingtone{FileName: "x.mp3", MimeType: "audio/mpeg"}); !errors.Is(err, ringtones.ErrInvalid) {
		t.Errorf("no file: %v, want RINGTONE_INVALID", err)
	}
	m.uploadErr = ringtones.ErrDurationTooLong
	if _, err = c.AccountUploadRingtone(&mtproto.TLAccountUploadRingtone{File: file, FileName: "x.mp3", MimeType: "audio/mpeg"}); !errors.Is(err, ringtones.ErrDurationTooLong) {
		t.Errorf("a sound too long: %v, want RINGTONE_DURATION_TOO_LONG", err)
	}
	m.uploadErr = nil
	s.full = true
	before := len(m.uploaded)
	if _, err = c.AccountUploadRingtone(&mtproto.TLAccountUploadRingtone{File: file, FileName: "x.mp3", MimeType: "audio/mpeg"}); !errors.Is(err, ringtones.ErrInvalid) || len(m.uploaded) != before {
		t.Errorf("a full list: %v, %d uploads; want RINGTONE_INVALID and nothing stored", err, len(m.uploaded)-before)
	}
}
