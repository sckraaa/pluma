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
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
	"google.golang.org/grpc/status"
)

// The sets users make (task 084, ADR 0053): stickers.createStickerSet and
// the methods that change a set, which only the account that made it may.

// errPackTypeInvalid is the answer to a set asked to be both of masks and
// of custom emoji (core.telegram.org/method/stickers.createStickerSet).
var errPackTypeInvalid = status.Error(mtproto.ErrBadRequest, "PACK_TYPE_INVALID")

// errNotOwner stops a change of a set by an account that did not make it.
var errNotOwner = errors.New("not the set's owner")

// errSetFull stops a sticker added to a set that holds all it may.
var errSetFull = errors.New("the set is full")

// errNoSticker stops a change of a sticker its set no longer has.
var errNoSticker = errors.New("no such sticker in the set")

// ownSet finds the set ask names if the account made it; any other set is
// STICKERSET_INVALID.
func (c *CatalogCore) ownSet(method string, ask *mtproto.InputStickerSet) (*catalog.Set, *mtproto.Messages_StickerSet, error) {
	var (
		set *catalog.Set
		err error
	)
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputStickerSetID:
		set, err = c.svcCtx.Catalog.SetByID(c.ctx, ask.GetId())
		if set != nil && set.AccessHash != ask.GetAccessHash() {
			set = nil
		}
	case mtproto.Predicate_inputStickerSetShortName:
		set, err = c.svcCtx.Catalog.SetByShortName(c.ctx, ask.GetShortName())
	}
	if err != nil {
		c.Logger.Errorf("%s - the catalog: %v", method, err)
		return nil, nil, err
	}
	if set == nil || set.OwnerID == 0 || set.OwnerID != c.MD.UserId {
		err = mtproto.ErrStickersetInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, nil, err
	}
	ms, err := c.decodeSet(method, set)
	if err != nil {
		return nil, nil, err
	}
	return set, ms, nil
}

// ownSetOf finds the set the sticker in is in; a document that is a
// sticker of no set a user made is STICKER_INVALID
// (core.telegram.org/method/stickers.changeSticker). Whether the account
// made that set is checked with the set locked (ownSticker).
func (c *CatalogCore) ownSetOf(method string, in *mtproto.InputDocument) (int64, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputDocument || in.GetId() == 0 {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrStickerInvalid)
		return 0, mtproto.ErrStickerInvalid
	}
	id, err := c.svcCtx.UserSets.SetOf(c.ctx, in.GetId())
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return 0, err
	}
	if id == 0 {
		c.Logger.Errorf("%s - sticker %d: %v", method, in.GetId(), mtproto.ErrStickerInvalid)
		return 0, mtproto.ErrStickerInvalid
	}
	return id, nil
}

// ownSticker is the place in the set being changed of the sticker in, a
// sticker of a set of the account: errNotOwner for another account's set,
// errNoSticker for a sticker the set has not or another access hash.
func (c *CatalogCore) ownSticker(ch *usersets.Change, in *mtproto.InputDocument) (int, error) {
	if ch.Set.OwnerID != c.MD.UserId {
		return 0, errNotOwner
	}
	at := usersets.Index(ch.MS, in.GetId())
	if at < 0 || ch.MS.GetDocuments()[at].GetAccessHash() != in.GetAccessHash() {
		return 0, errNoSticker
	}
	return at, nil
}

// changeSet changes the set id with fn (usersets.Store.Update), tells the
// accounts that have it and answers it as the account sees it. The errors
// of fn the clients know are answered as theirs.
func (c *CatalogCore) changeSet(method string, id int64, fn func(ch *usersets.Change) error) (*mtproto.Messages_StickerSet, error) {
	changed := false
	set, err := c.svcCtx.UserSets.Update(c.ctx, id, func(ch *usersets.Change) error {
		if err := fn(ch); err != nil {
			return err
		}
		changed = true
		return nil
	})
	switch {
	case errors.Is(err, usersets.ErrNoSet), errors.Is(err, errNotOwner):
		c.Logger.Errorf("%s - set %d: %v", method, id, err)
		return nil, mtproto.ErrStickersetInvalid
	case errors.Is(err, errNoSticker):
		c.Logger.Errorf("%s - set %d: %v", method, id, err)
		return nil, mtproto.ErrStickerInvalid
	case errors.Is(err, errSetFull):
		c.Logger.Errorf("%s - set %d: %v", method, id, err)
		return nil, mtproto.ErrStickerpackStickersTooMuch
	case err != nil:
		c.Logger.Errorf("%s - set %d: %v", method, id, err)
		return nil, err
	}
	// The accounts that have the set are told before the answer is made:
	// the set is changed by now, whatever becomes of the answer.
	if changed {
		stored, err := c.decodeSet(method, set)
		if err != nil {
			return nil, err
		}
		c.tellHolders(method, set.ID, installed.KindOf(stored.GetSet()))
	}
	return c.viewOf(method, set)
}

// tellHolders tells the accounts that have the set to load their sets of
// kind again. The set is changed by then: an account that is not told
// finds the set's new hash the next time it loads its sets, so a failure
// is logged, not answered.
func (c *CatalogCore) tellHolders(method string, setID int64, kind installed.Kind) {
	if err := c.svcCtx.Installed.Changed(c.ctx, setID, kind); err != nil {
		c.Logger.Errorf("%s - telling the accounts that have set %d: %v", method, setID, err)
	}
}

// viewOf is a stored set as the account sees it: with creator if it made
// the set, and with its installed_date, and archived, if it has it.
func (c *CatalogCore) viewOf(method string, set *catalog.Set) (*mtproto.Messages_StickerSet, error) {
	ms, err := c.decodeSet(method, set)
	if err != nil {
		return nil, err
	}
	e, err := c.svcCtx.Installed.Entry(c.ctx, c.MD.UserId, set.ID)
	if err != nil {
		c.Logger.Errorf("%s - the account's sets: %v", method, err)
		return nil, err
	}
	switch {
	case e == nil:
		return ms, nil
	case e.ArchivedAt > 0:
		out := asInstalled(ms, e.InstalledAt)
		out.Set.Archived = true
		return out, nil
	default:
		return asInstalled(ms, e.InstalledAt), nil
	}
}

// newSetID is a new positive id and an access hash, as the catalog's sets
// have them.
func newSetID() (int64, int64, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:8])>>1) | 1, int64(binary.LittleEndian.Uint64(b[8:])), nil
}

// freeShortName makes a short name nobody has of a title: the title's
// letters, with digits after them when a set has that name, and "set" with
// digits for a title without a Latin or Russian letter. Android and iOS
// make a set of one sticker with an empty short name and leave the name to
// the server (StickerMakerView.java:1507, MediaEditorScreen.swift:8195),
// and ask for one to offer with stickers.suggestShortName.
func (c *CatalogCore) freeShortName(method, title string) (string, error) {
	base := usersets.ShortNameOf(title, "")
	for try := 0; try < 8; try++ {
		name := base
		if name == "" || try > 0 {
			var b [4]byte
			if _, err := rand.Read(b[:]); err != nil {
				return "", err
			}
			suffix := fmt.Sprintf("%d", binary.LittleEndian.Uint32(b[:])%1000000)
			if base == "" {
				name = "set_" + suffix
			} else {
				name = usersets.ShortNameOf(title, suffix)
			}
		}
		taken, err := c.svcCtx.UserSets.ShortNameTaken(c.ctx, name)
		if err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return "", err
		}
		if !taken {
			return name, nil
		}
	}
	err := mtproto.ErrPackShortNameOccupied
	c.Logger.Errorf("%s - no free short name: %v", method, err)
	return "", err
}

// stickerSource reads the document a sticker is to be made of: the
// document itself, as media has it, and its file. A document that is not
// there, has another access hash or is larger than any sticker may be is
// STICKER_FILE_INVALID.
func (c *CatalogCore) stickerSource(method string, in *mtproto.InputDocument) (*mtproto.Document, []byte, error) {
	invalid := func(why string) (*mtproto.Document, []byte, error) {
		c.Logger.Errorf("%s - document %d: %s", method, in.GetId(), why)
		return nil, nil, mtproto.ErrStickerFileInvalid
	}
	if in.GetPredicateName() != mtproto.Predicate_inputDocument || in.GetId() == 0 {
		return invalid("not a document")
	}
	doc, err := c.svcCtx.Media.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: in.GetId()})
	if err != nil {
		c.Logger.Errorf("%s - media: %v", method, err)
		return nil, nil, err
	}
	if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetAccessHash() != in.GetAccessHash() {
		return invalid("no such document")
	}
	size := doc.GetSize2_INT64()
	if size <= 0 || size > stickerfile.MaxSize {
		return invalid(fmt.Sprintf("%d bytes", size))
	}
	file, err := c.svcCtx.Dfs.DfsDownloadFile(c.ctx, &dfs.TLDfsDownloadFile{
		Location: mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
			Id:            doc.GetId(),
			AccessHash:    doc.GetAccessHash(),
			FileReference: []byte{},
		}).To_InputFileLocation(),
		Offset: 0,
		Limit:  1 << 20, // the most dfs answers at once, and no sticker is larger
	})
	if err != nil {
		c.Logger.Errorf("%s - dfs: %v", method, err)
		return nil, nil, err
	}
	if int64(len(file.GetBytes())) != size {
		// dfs answers no bytes for a file it could not read.
		c.Logger.Errorf("%s - document %d: %d bytes of %d read", method, in.GetId(), len(file.GetBytes()), size)
		return nil, nil, mtproto.ErrInternalServerError
	}
	return doc, file.GetBytes(), nil
}

// setShape is what the stickers of a set are made for.
type setShape struct {
	id, accessHash int64
	kind           installed.Kind
	textColor      bool
}

func shapeOf(set *mtproto.StickerSet) setShape {
	return setShape{id: set.GetId(), accessHash: set.GetAccessHash(), kind: installed.KindOf(set), textColor: set.GetTextColor()}
}

// makeSticker makes the sticker item of a set: checks its emoji and its
// file (stickerfile.Inspect) and stores the file as a document of its own,
// a sticker of that set. The document the user gave is left as it is: it
// may be a sticker of another set (Telegram Desktop and Android add
// existing stickers to a set, api_stickers_creator.cpp:240,
// StickersDialogs.java:203), and messages may carry it.
func (c *CatalogCore) makeSticker(method string, shape setShape, item *mtproto.InputStickerSetItem) (usersets.Sticker, usersets.Link, error) {
	var none usersets.Sticker
	emoji, ok := stickerfile.SplitEmoji(item.GetEmoji())
	if !ok {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrStickerEmojiInvalid)
		return none, usersets.Link{}, mtproto.ErrStickerEmojiInvalid
	}
	source, data, err := c.stickerSource(method, item.GetDocument())
	if err != nil {
		return none, usersets.Link{}, err
	}
	info, err := stickerfile.Inspect(data, shape.kind == installed.Emojis)
	if err != nil {
		c.Logger.Errorf("%s - document %d: %v", method, source.GetId(), err)
		return none, usersets.Link{}, err
	}
	stored, err := c.svcCtx.Documents.Import(c.ctx, mtproto.MakeTLDocument(&mtproto.Document{
		Date:       int32(time.Now().Unix()),
		MimeType:   info.Mime(),
		Attributes: stickerAttributes(shape, info, emoji[0], item.GetMaskCoords()),
	}).To_Document(), data, nil, 0)
	if err != nil {
		c.Logger.Errorf("%s - storing the sticker: %v", method, err)
		return none, usersets.Link{}, err
	}
	st := usersets.Sticker{Document: stored, Emoji: emoji}
	if shape.kind != installed.Masks && item.GetKeywords() != nil {
		st.Keywords = stickerfile.SplitKeywords(item.GetKeywords().GetValue())
	}
	return st, usersets.Link{DocumentID: stored.GetId(), SourceID: source.GetId()}, nil
}

// stickerAttributes are the attributes of a sticker of a set: its size,
// what it is in the set (documentAttributeSticker, or
// documentAttributeCustomEmoji in a set of custom emoji) with the first of
// its emoji as alt, the video of a video sticker, and its file name.
func stickerAttributes(shape setShape, info *stickerfile.Info, alt string, mask *mtproto.MaskCoords) []*mtproto.DocumentAttribute {
	set := mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: shape.id, AccessHash: shape.accessHash}).To_InputStickerSet()
	attrs := []*mtproto.DocumentAttribute{
		mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: info.W, H: info.H}).To_DocumentAttribute(),
	}
	if shape.kind == installed.Emojis {
		attrs = append(attrs, mtproto.MakeTLDocumentAttributeCustomEmoji(&mtproto.DocumentAttribute{
			Alt: alt, Stickerset: set, TextColor: shape.textColor,
		}).To_DocumentAttribute())
	} else {
		a := &mtproto.DocumentAttribute{Alt: alt, Stickerset: set, Mask: shape.kind == installed.Masks}
		if shape.kind == installed.Masks {
			a.MaskCoords = mask
		}
		attrs = append(attrs, mtproto.MakeTLDocumentAttributeSticker(a).To_DocumentAttribute())
	}
	if info.Format == stickerfile.Video {
		attrs = append(attrs, mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{
			Nosound: true, Duration: info.Duration, Duration_FLOAT64: info.Duration, Duration_INT32: int32(info.Duration + 0.5), W: info.W, H: info.H,
		}).To_DocumentAttribute())
	}
	return append(attrs, mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: info.FileName()}).To_DocumentAttribute())
}

// setAlt writes the first emoji of a sticker in the set's copy of its
// document (stickers.changeSticker): clients read the emoji of a sticker
// of a set by the set's packs, and its alt where they have no set.
func setAlt(doc *mtproto.Document, alt string) {
	for _, a := range doc.GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeSticker, mtproto.Predicate_documentAttributeCustomEmoji:
			a.Alt = alt
		}
	}
}

// makeThumb makes the set's own thumbnail of the document in: a static, an
// animated or a video image as a sticker or a custom emoji is
// (stickerfile.Inspect), stored as a document of its own, which
// upload.getFile serves for inputStickerSetThumb (usersets.ThumbFiles).
// The photoSize tells its kind by its type, as the clients read it: "a"
// for TGS, "v" for WEBM, anything else an image (Android
// ImageLocation.java:303-306).
func (c *CatalogCore) makeThumb(method string, in *mtproto.InputDocument) (int64, *mtproto.PhotoSize, error) {
	_, data, err := c.stickerSource(method, in)
	if errors.Is(err, mtproto.ErrStickerFileInvalid) {
		return 0, nil, mtproto.ErrStickerThumbPngNopng
	}
	if err != nil {
		return 0, nil, err
	}
	info, err := stickerfile.Inspect(data, true)
	if err != nil {
		// A thumbnail of the size of a sticker is taken too.
		info, err = stickerfile.Inspect(data, false)
	}
	if err != nil {
		c.Logger.Errorf("%s - the thumbnail: %v", method, err)
		if errors.Is(err, mtproto.ErrStickerTgsNotgs) {
			return 0, nil, mtproto.ErrStickerThumbTgsNotgs
		}
		return 0, nil, mtproto.ErrStickerThumbPngNopng
	}
	stored, err := c.svcCtx.Documents.Import(c.ctx, mtproto.MakeTLDocument(&mtproto.Document{
		Date:     int32(time.Now().Unix()),
		MimeType: info.Mime(),
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: info.W, H: info.H}).To_DocumentAttribute(),
			mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: info.FileName()}).To_DocumentAttribute(),
		},
	}).To_Document(), data, nil, 0)
	if err != nil {
		c.Logger.Errorf("%s - storing the thumbnail: %v", method, err)
		return 0, nil, err
	}
	typ := "s"
	switch info.Format {
	case stickerfile.Animated:
		typ = "a"
	case stickerfile.Video:
		typ = "v"
	}
	return stored.GetId(), mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: typ, W: info.W, H: info.H, Size2: int32(len(data))}).To_PhotoSize(), nil
}

// putThumb gives the set its own thumbnail, or takes it away for a nil
// size; the version tells clients that it changed.
func putThumb(ch *usersets.Change, documentID int64, size *mtproto.PhotoSize) {
	set := ch.MS.Set
	version := set.GetThumbVersion().GetValue() + 1
	ch.Set.ThumbDocumentID = documentID
	if size == nil {
		set.Thumbs, set.ThumbDcId, set.ThumbVersion = nil, nil, nil
		return
	}
	set.Thumbs, set.ThumbDcId, set.ThumbVersion = []*mtproto.PhotoSize{size}, mtproto.MakeFlagsInt32(1), mtproto.MakeFlagsInt32(version)
}

// selfUser tells if in names the account itself.
func (c *CatalogCore) selfUser(in *mtproto.InputUser) bool {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputUserSelf:
		return true
	case mtproto.Predicate_inputUser:
		return in.GetUserId() == c.MD.UserId
	}
	return false
}

// covered is a set as lists of sets give it: a set of custom emoji with
// all its emoji (stickerSetFullCovered, as Telegram gives emoji sets), a
// set of stickers with its first sticker (stickerSetCovered), an empty one
// alone (stickerSetNoCovered).
func covered(ms *mtproto.Messages_StickerSet) *mtproto.StickerSetCovered {
	switch {
	case ms.GetSet().GetEmojis():
		return mtproto.MakeTLStickerSetFullCovered(&mtproto.StickerSetCovered{
			Set: ms.GetSet(), Packs: ms.GetPacks(), Keywords: ms.GetKeywords(), Documents: ms.GetDocuments(),
		}).To_StickerSetCovered()
	case len(ms.GetDocuments()) == 0:
		return mtproto.MakeTLStickerSetNoCovered(&mtproto.StickerSetCovered{Set: ms.GetSet()}).To_StickerSetCovered()
	default:
		return mtproto.MakeTLStickerSetCovered(&mtproto.StickerSetCovered{Set: ms.GetSet(), Cover: ms.GetDocuments()[0]}).To_StickerSetCovered()
	}
}
