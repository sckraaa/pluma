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

package cdn

import (
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// FileKind is the kind of a file in cdn_files.
type FileKind int8

const (
	KindDocument  FileKind = 1 // inputDocumentFileLocation
	KindPhoto     FileKind = 2 // inputPhotoFileLocation, one size of the photo
	KindEncrypted FileKind = 3 // inputEncryptedFileLocation, a secret chat file
)

// FileKey identifies a file of cdn_files.
type FileKey struct {
	Kind FileKind
	Id   int64
	// Thumb is the size of a photo (thumb_size); empty for the other kinds.
	Thumb string
}

// maxThumbLen is the width of cdn_files.thumb_size.
const maxThumbLen = 8

// Wants reports whether a request of upload.getFile may be answered with
// upload.fileCdnRedirect (ADR 0065, p. 2): the client said cdn_supported
// (Android always, FileLoadOperation.java:2465-2470; tdesktop on cloud
// files, download_manager_mtproto.cpp:580-582; Web K never) and did not ask
// for precise, whose rules (1 KB) are looser than those of getCdnFile (4 KB).
func Wants(req *mtproto.TLUploadGetFile) bool {
	return req.GetCdnSupported() && !req.GetPrecise()
}

// Key returns the file of cdn_files that loc names, and false for a
// location whose files do not go through the CDN (ADR 0065, p. 2):
//   - a document itself; its thumbnails (thumb_size set) are previews and
//     stay at home, so the size that decides is always the document's;
//   - a size of a photo (thumb_size is the size; a photo has no file
//     without one);
//   - a secret chat file.
//
// Avatars (inputPeerPhotoFileLocation), sticker set thumbnails, group call
// streams, secure, takeout and legacy locations stay at home.
func Key(loc *mtproto.InputFileLocation) (FileKey, bool) {
	if loc == nil || loc.GetId() == 0 {
		return FileKey{}, false
	}
	switch loc.GetPredicateName() {
	case mtproto.Predicate_inputDocumentFileLocation:
		if loc.GetThumbSize() != "" {
			return FileKey{}, false
		}
		return FileKey{Kind: KindDocument, Id: loc.GetId()}, true
	case mtproto.Predicate_inputPhotoFileLocation:
		thumb := loc.GetThumbSize()
		if thumb == "" || len(thumb) > maxThumbLen {
			return FileKey{}, false
		}
		return FileKey{Kind: KindPhoto, Id: loc.GetId(), Thumb: thumb}, true
	case mtproto.Predicate_inputEncryptedFileLocation:
		return FileKey{Kind: KindEncrypted, Id: loc.GetId()}, true
	}
	return FileKey{}, false
}

// DefaultMinFileSize is CDN_MIN_FILE_SIZE by default: 1 MiB.
const DefaultMinFileSize = 1 << 20

// ValidMinFileSize checks CDN_MIN_FILE_SIZE: a positive multiple of 1 MiB,
// the largest part of upload.getFile (task 102).
func ValidMinFileSize(n int64) error {
	if n <= 0 || n%(1<<20) != 0 {
		return fmt.Errorf("cdn: CDN_MIN_FILE_SIZE %d is not a positive multiple of 1 MiB", n)
	}
	return nil
}

// Eligible reports whether a file of size bytes goes through the CDN with
// the threshold minSize (CDN_MIN_FILE_SIZE).
func Eligible(size, minSize int64) bool { return size >= minSize }

// MaxPushAttempts is how many times a copy is claimed for a push before it
// is failed for good (an alert, task 102).
const MaxPushAttempts = 8

// pushBackoff is the wait before the next push of a copy after a failed
// attempt: the 1st, 2nd, 3rd and every later one.
var pushBackoff = [...]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// RetryAt returns when a copy whose push failed after attempts claims is
// tried again (unix ms), and false if it is not: MaxPushAttempts were made.
func RetryAt(attempts int32, now int64) (int64, bool) {
	if attempts >= MaxPushAttempts {
		return 0, false
	}
	i := min(max(int(attempts), 1), len(pushBackoff)) - 1
	return now + pushBackoff[i].Milliseconds(), true
}
