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

// Package webfile serves upload.getWebFile (ADR 0062, p. 5): the files the
// server hands out by an InputWebFileLocation instead of a file id, the
// thumbnails and content of inline bots at an address
// (inputWebFileLocation) and, from task 100, maps (inputWebFileGeoPointLocation).
// Each kind of location is a Source; the Server checks the range, keeps one
// copy of a file for all its parts and builds upload.webFile, the same way
// for every kind.
//
// Clients send upload.getWebFile to webfile_dc_id of help.getConfig
// (Android FileLoadOperation.java:411, iOS RemoteStorageConfiguration.swift:6-16,
// tdesktop file_download_mtproto.cpp:64, Web K apiFileManager.ts:381), DC 1
// at Pluma.
package webfile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

// ErrNotAvailable is a file the server cannot get: an address that is not
// public, an answer of a type not served, over the size limit, or an error
// of the host. core.telegram.org/method/upload.getWebFile names only
// DOCUMENT_INVALID and LOCATION_INVALID; this error of the handler is the
// server's own choice, as OFFSET_INVALID and LIMIT_INVALID are, the same
// as upload.getFile's.
var ErrNotAvailable = status.Error(mtproto.ErrBadRequest, "WEBFILE_NOT_AVAILABLE")

// maxLimit is the largest part a request may ask for, as in upload.getFile
// (core.telegram.org/api/files).
const maxLimit = 1 << 20

// Location is a resolved InputWebFileLocation.
type Location interface {
	// Key names the file in the cache: equal keys are the same bytes.
	Key() string
}

// Request says who asks: for limits per user and the language of a map.
type Request struct {
	UserId   int64
	LangCode string
}

// File is a whole file of a source.
type File struct {
	Bytes    []byte
	MimeType string
	// Mtime is the modification time clients get, unix seconds.
	Mtime int32
}

// Source serves one kind of InputWebFileLocation.
type Source interface {
	// Resolve checks a location and returns it resolved; its error is the
	// answer, normally mtproto.ErrLocationInvalid.
	Resolve(ctx context.Context, r Request, loc *mtproto.InputWebFileLocation) (Location, error)
	// Open returns the whole file: ErrNotAvailable or the source's own
	// error (an RPC error the client gets).
	Open(ctx context.Context, r Request, l Location) (File, error)
	// Cached is true when the source keeps the same bytes for every part
	// itself (the map, in Redis); the Server keeps no copy of it. Otherwise
	// the Server keeps the file in its DiskCache, so every part of a file
	// comes from one copy: tdesktop cancels a load whose parts tell
	// different sizes (file_download_mtproto.cpp:170-181).
	Cached() bool
}

// Server serves upload.getWebFile by the kind of the location.
type Server struct {
	sources map[string]Source
	cache   *DiskCache
}

// NewServer returns a Server that keeps the files of the sources that do
// not keep their own in cache.
func NewServer(cache *DiskCache) *Server {
	return &Server{sources: map[string]Source{}, cache: cache}
}

// Register serves the locations of the predicate (mtproto.Predicate_*)
// with src. It is called before the server serves requests; a predicate
// registered twice is a mistake of the program.
func (s *Server) Register(predicate string, src Source) {
	if _, ok := s.sources[predicate]; ok {
		panic(fmt.Sprintf("webfile: %s registered twice", predicate))
	}
	if !src.Cached() && s.cache == nil {
		panic(fmt.Sprintf("webfile: %s needs the cache", predicate))
	}
	s.sources[predicate] = src
}

var metricRequests = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "webfile",
	Name:      "requests_total",
	Help:      "Parts of web files asked for with upload.getWebFile, by kind of location and result.",
	Labels:    []string{"kind", "result"}, // result: ok, invalid, unavailable, error
})

// Get answers upload.getWebFile for the part [offset, offset+limit) of the
// file at loc. The rules are the same for every kind of location: offset
// below 0 is OFFSET_INVALID, limit outside 1 B..1 MiB is LIMIT_INVALID (as
// upload.getFile, which has no rule of a multiple of 4 KiB here: the
// documentation of web files gives none); a kind with no source is
// LOCATION_INVALID; a part past the end has no bytes and the full size.
func (s *Server) Get(ctx context.Context, r Request, loc *mtproto.InputWebFileLocation, offset, limit int32) (*mtproto.Upload_WebFile, error) {
	kind := kindLabel(loc.GetPredicateName())
	f, err := s.get(ctx, r, loc, offset, limit)
	metricRequests.Inc(kind, resultLabel(err))
	return f, err
}

func (s *Server) get(ctx context.Context, r Request, loc *mtproto.InputWebFileLocation, offset, limit int32) (*mtproto.Upload_WebFile, error) {
	switch {
	case limit <= 0, limit > maxLimit:
		return nil, mtproto.ErrLimitInvalid
	case offset < 0:
		return nil, mtproto.ErrOffsetInvalid
	}
	src, ok := s.sources[loc.GetPredicateName()]
	if !ok {
		return nil, mtproto.ErrLocationInvalid
	}
	l, err := src.Resolve(ctx, r, loc)
	if err != nil {
		return nil, err
	}
	if src.Cached() {
		f, err := src.Open(ctx, r, l)
		if err != nil {
			return nil, s.sourceError(ctx, err)
		}
		return webFile(int64(len(f.Bytes)), f.MimeType, f.Mtime, part(f.Bytes, offset, limit)), nil
	}
	h, err := s.cache.Get(ctx, l.Key(), func(ctx context.Context) (File, error) {
		return src.Open(ctx, r, l)
	})
	if err != nil {
		return nil, s.sourceError(ctx, err)
	}
	defer h.Close()
	b, err := h.Read(int64(offset), int64(limit))
	if err != nil {
		logx.WithContext(ctx).Errorf("webfile: a cached file not read: %v", err)
		return nil, ErrNotAvailable
	}
	return webFile(h.Size(), h.MimeType(), h.Mtime(), b), nil
}

// sourceError is the answer to an error of a source or the cache: an RPC
// error as it is, anything else, which no client should see, logged and
// ErrNotAvailable.
func (s *Server) sourceError(ctx context.Context, err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrNotAvailable
	}
	logx.WithContext(ctx).Errorf("webfile: a file not served: %v", err)
	return ErrNotAvailable
}

// part is the bytes of [offset, offset+limit) of b: none past its end.
func part(b []byte, offset, limit int32) []byte {
	if int64(offset) >= int64(len(b)) {
		return []byte{}
	}
	end := min(int64(offset)+int64(limit), int64(len(b)))
	return b[offset:end]
}

func webFile(size int64, mimeType string, mtime int32, b []byte) *mtproto.Upload_WebFile {
	return mtproto.MakeTLUploadWebFile(&mtproto.Upload_WebFile{
		Size2:    int32(size),
		MimeType: mimeType,
		FileType: FileType(mimeType),
		Mtime:    mtime,
		Bytes:    b,
	}).To_Upload_WebFile()
}

// FileType is the storage.FileType of a MIME type, storage.fileUnknown for
// any other.
func FileType(mimeType string) *mtproto.Storage_FileType {
	switch strings.ToLower(mimeType) {
	case "image/jpeg":
		return mtproto.MakeTLStorageFileJpeg(nil).To_Storage_FileType()
	case "image/png":
		return mtproto.MakeTLStorageFilePng(nil).To_Storage_FileType()
	case "image/gif":
		return mtproto.MakeTLStorageFileGif(nil).To_Storage_FileType()
	case "image/webp":
		return mtproto.MakeTLStorageFileWebp(nil).To_Storage_FileType()
	case "video/mp4":
		return mtproto.MakeTLStorageFileMp4(nil).To_Storage_FileType()
	case "audio/mpeg":
		return mtproto.MakeTLStorageFileMp3(nil).To_Storage_FileType()
	}
	return mtproto.MakeTLStorageFileUnknown(nil).To_Storage_FileType()
}

// kindLabel is the metric label of a kind of location: its predicate
// without "inputWebFile" ("Location", "GeoPointLocation"), "other" for a
// predicate that is no web file location.
func kindLabel(predicate string) string {
	if k, ok := strings.CutPrefix(predicate, "inputWebFile"); ok && k != "" {
		return k
	}
	return "other"
}

func resultLabel(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrNotAvailable):
		return "unavailable"
	case errors.Is(err, mtproto.ErrLocationInvalid), errors.Is(err, mtproto.ErrOffsetInvalid), errors.Is(err, mtproto.ErrLimitInvalid):
		return "invalid"
	}
	return "error"
}
