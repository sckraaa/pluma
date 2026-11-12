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
	"crypto/md5" //nolint:gosec // MTProto's md5_checksum of an uploaded file, a check of the upload, not security
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/tgexport"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The files of an imported chat (task 083, ADR 0051, p. 5) go the way a
// client's do: the parts of the file to dfs, then media makes the photo
// with its sizes or the document with its attributes, as messages.sendMedia
// does with inputMediaUploadedPhoto and inputMediaUploadedDocument
// (app/bff/messages/internal/core/core_messages_helper.go). The media made
// of a file is remembered (channel_import_media) before the message is
// written, so an import that is run again uploads nothing twice.
//
// media answers an upload with the photo or the document even when it could
// not record it (uploadedDocumentMedia and uploadPhotoFile do not return
// the error of the last insert), and a message of an imported history with
// a file nobody can open would be found by its readers alone. So every
// upload is read back before it is remembered: recorded, and for a document
// of the size the file has.

// bigFileSize is from where a client sends a file as inputFileBig, without
// a checksum (core.telegram.org/api/files: files over 10 MB).
const bigFileSize = 10 << 20

// importUploader is the way of an upload: dfs and media.
type importUploader interface {
	WritePart(ctx context.Context, owner, fileId int64, part int32, bytes []byte) error
	UploadPhoto(ctx context.Context, owner int64, file *mtproto.InputFile) (*mtproto.Photo, error)
	UploadDocument(ctx context.Context, owner int64, media *mtproto.InputMedia) (*mtproto.MessageMedia, error)
	// Photo and Document read what media has recorded under an id:
	// photoEmpty or documentEmpty for nothing.
	Photo(ctx context.Context, id int64) (*mtproto.Photo, error)
	Document(ctx context.Context, id int64) (*mtproto.Document, error)
}

// importMediaStore remembers the media made of the files of a chat.
type importMediaStore interface {
	// Media returns nil for a file not uploaded yet.
	Media(ctx context.Context, sourceChatId int64, hash []byte) (*mtproto.MessageMedia, error)
	SaveMedia(ctx context.Context, sourceChatId int64, hash []byte, size int64, media *mtproto.MessageMedia) error
}

// fileHash names a file of an export among the files of its chat: its path
// and size.
func fileHash(m *tgexport.Media) []byte {
	sum := sha256.Sum256([]byte(m.Path + "\x00" + strconv.FormatInt(m.Size, 10)))
	return sum[:]
}

// importFiles uploads the files of one export.
type importFiles struct {
	ex       *tgexport.Export
	up       importUploader
	store    importMediaStore
	waveform func(ctx context.Context, path string) []byte
	// uploaded and reused count the files sent now and those found
	// uploaded by an earlier run.
	uploaded, reused int64
	bytes            int64
}

// media returns the media of the file m of the export, uploading it unless
// an earlier run did.
func (f *importFiles) media(ctx context.Context, m *tgexport.Media) (*mtproto.MessageMedia, error) {
	hash := fileHash(m)
	chat := f.ex.Header().Id
	made, err := f.store.Media(ctx, chat, hash)
	if err != nil {
		return nil, fmt.Errorf("uploaded files: %w", err)
	}
	if made != nil {
		f.reused++
		// One file of an export may be the media of several messages: the
		// export writes a file it has already loaded once
		// (export_api_wrap.cpp:3404-3411), as for a photo forwarded within
		// the group. The spoiler is each message's own.
		if made.GetSpoiler() != m.Spoiler {
			made = proto.Clone(made).(*mtproto.MessageMedia)
			made.Spoiler = m.Spoiler
		}
		return made, nil
	}
	owner, err := randomId()
	if err != nil {
		return nil, err
	}
	file, err := f.send(ctx, owner, m)
	if err != nil {
		return nil, err
	}
	if m.Type == tgexport.MediaPhoto {
		photo, err := f.up.UploadPhoto(ctx, owner, file)
		if err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
		if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
			return nil, errors.New("media: no photo made of the file")
		}
		stored, err := f.up.Photo(ctx, photo.GetId())
		if err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
		if stored.GetPredicateName() != mtproto.Predicate_photo || len(stored.GetSizes()) == 0 {
			return nil, fmt.Errorf("media: photo %d was not recorded", photo.GetId())
		}
		made = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Spoiler: m.Spoiler, Photo_FLAGPHOTO: photo}).To_MessageMedia()
	} else {
		var waveform []byte
		if m.Type == tgexport.MediaVoice && f.waveform != nil {
			if p, err := f.ex.Path(m.Path); err == nil {
				waveform = f.waveform(ctx, p)
			}
		}
		made, err = f.up.UploadDocument(ctx, owner, mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			Spoiler:    m.Spoiler,
			File:       file,
			MimeType:   mimeType(m),
			Attributes: documentAttributes(m, waveform),
		}).To_InputMedia())
		if err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
		if made.GetPredicateName() != mtproto.Predicate_messageMediaDocument || made.GetDocument().GetId() == 0 {
			return nil, errors.New("media: no document made of the file")
		}
		id := made.GetDocument().GetId()
		stored, err := f.up.Document(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
		if stored.GetPredicateName() != mtproto.Predicate_document {
			return nil, fmt.Errorf("media: document %d was not recorded", id)
		}
		if size := stored.GetSize2_INT64(); size != m.Size {
			return nil, fmt.Errorf("media: document %d is recorded with %d bytes, the file has %d", id, size, m.Size)
		}
	}
	if err = f.store.SaveMedia(ctx, chat, hash, m.Size, made); err != nil {
		return nil, fmt.Errorf("uploaded files: %w", err)
	}
	f.uploaded++
	f.bytes += m.Size
	return made, nil
}

// send writes the parts of the file to dfs as owner and returns the file as
// a client names it. The file is read part by part, never whole.
func (f *importFiles) send(ctx context.Context, owner int64, m *tgexport.Media) (*mtproto.InputFile, error) {
	fileId, err := randomId()
	if err != nil {
		return nil, err
	}
	r, err := f.ex.OpenFile(m.Path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	sum := md5.New() //nolint:gosec // inputFile.md5_checksum, which dfs checks the parts against
	buf := make([]byte, photoPartSize)
	parts, size := int32(0), int64(0)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			if werr := f.up.WritePart(ctx, owner, fileId, parts, buf[:n]); werr != nil {
				return nil, fmt.Errorf("dfs: part %d: %w", parts, werr)
			}
			sum.Write(buf[:n])
			parts++
			size += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if size != m.Size {
		return nil, fmt.Errorf("the file changed while it was read: %d bytes, %d planned", size, m.Size)
	}
	name := path.Base(m.Path)
	if size > bigFileSize {
		return mtproto.MakeTLInputFileBig(&mtproto.InputFile{Id_INT64: fileId, Parts: parts, Name: name}).To_InputFile(), nil
	}
	return mtproto.MakeTLInputFile(&mtproto.InputFile{
		Id_INT64: fileId, Parts: parts, Name: name, Md5Checksum: hex.EncodeToString(sum.Sum(nil)),
	}).To_InputFile(), nil
}

// mimeType is the type of the document: the export's, or the one of the
// file's extension when it gives none.
func mimeType(m *tgexport.Media) string {
	if m.MimeType != "" {
		return m.MimeType
	}
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(m.Path))); t != "" {
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		return t
	}
	return "application/octet-stream"
}

// documentAttributes are the attributes a client sends with a document of
// this kind (constructor/documentAttribute*): the file's name, and what
// makes it a video, a round video message, a voice message, music, an
// animation or a sticker. The export keeps no sticker set, so a sticker
// comes without one.
func documentAttributes(m *tgexport.Media, waveform []byte) []*mtproto.DocumentAttribute {
	name := m.FileName
	if name == "" {
		name = path.Base(m.Path)
	}
	video := func(round, nosound bool) *mtproto.DocumentAttribute {
		return mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{
			RoundMessage: round, SupportsStreaming: !round, Nosound: nosound,
			Duration: float64(m.Duration), Duration_FLOAT64: float64(m.Duration), Duration_INT32: m.Duration,
			W: m.Width, H: m.Height,
		}).To_DocumentAttribute()
	}
	audio := func(voice bool) *mtproto.DocumentAttribute {
		a := &mtproto.DocumentAttribute{Voice: voice, Duration: float64(m.Duration), Duration_FLOAT64: float64(m.Duration), Duration_INT32: m.Duration}
		if voice {
			a.Waveform = waveform
		} else {
			if m.Title != "" {
				a.Title = wrapperspb.String(m.Title)
			}
			if m.Performer != "" {
				a.Performer = wrapperspb.String(m.Performer)
			}
		}
		return mtproto.MakeTLDocumentAttributeAudio(a).To_DocumentAttribute()
	}
	size := func() *mtproto.DocumentAttribute {
		return mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: m.Width, H: m.Height}).To_DocumentAttribute()
	}
	var out []*mtproto.DocumentAttribute
	switch m.Type {
	case tgexport.MediaVideo:
		out = append(out, video(false, false))
	case tgexport.MediaRound:
		out = append(out, video(true, false))
	case tgexport.MediaVoice:
		out = append(out, audio(true))
	case tgexport.MediaAudio:
		out = append(out, audio(false))
	case tgexport.MediaAnimation:
		if strings.HasPrefix(mimeType(m), "video/") {
			out = append(out, video(false, true))
		}
		out = append(out, mtproto.MakeTLDocumentAttributeAnimated(&mtproto.DocumentAttribute{}).To_DocumentAttribute())
	case tgexport.MediaSticker:
		if m.Width > 0 && m.Height > 0 {
			out = append(out, size())
		}
		out = append(out, mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
			Alt:        m.StickerEmoji,
			Stickerset: mtproto.MakeTLInputStickerSetEmpty(nil).To_InputStickerSet(),
		}).To_DocumentAttribute())
	default:
		if m.Width > 0 && m.Height > 0 {
			out = append(out, size())
		}
	}
	return append(out, mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: name}).To_DocumentAttribute())
}
