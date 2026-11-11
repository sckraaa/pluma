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

package moderation

import (
	"github.com/teamgram/proto/mtproto"
)

// MediaOf is what the panel needs to show the media of a message, nil for
// none.
func MediaOf(m *mtproto.MessageMedia) *SnapshotMedia {
	if m == nil {
		return nil
	}
	s := &SnapshotMedia{Kind: m.GetPredicateName()}
	if p := m.GetPhoto_FLAGPHOTO(); p != nil && p.GetId() != 0 {
		s.PhotoId = p.GetId()
		s.AccessHash = p.GetAccessHash()
		s.FileRef = p.GetFileReference()
		s.Mime = "image/jpeg"
		var best int32
		for _, size := range p.GetSizes() {
			if area := size.GetW() * size.GetH(); area > best {
				best, s.ThumbSize = area, size.GetType()
			}
		}
	}
	if d := m.GetDocument(); d != nil && d.GetId() != 0 {
		s.DocumentId = d.GetId()
		s.AccessHash = d.GetAccessHash()
		s.FileRef = d.GetFileReference()
		s.Mime = d.GetMimeType()
	}
	return s
}
