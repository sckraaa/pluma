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

package webpage

import (
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"mvdan.cc/xurls/v2"
)

// FirstLink returns the link a card is made of: the first link of the
// message in the order of the text, a hidden one (messageEntityTextUrl) or
// a visible one (messageEntityUrl, or found in the text as clients find it,
// with or without a scheme); "" if there is none.
func FirstLink(message string, entities []*mtproto.MessageEntity) string {
	units := utf16.Encode([]rune(message))
	first, at := "", int32(-1)
	for _, e := range entities {
		var link string
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityTextUrl:
			link = e.GetUrl()
		case mtproto.Predicate_messageEntityUrl:
			o, l := e.GetOffset(), e.GetLength()
			if o < 0 || l <= 0 || int(o+l) > len(units) {
				continue
			}
			link = string(utf16.Decode(units[o : o+l]))
		default:
			continue
		}
		if link != "" && (at < 0 || e.GetOffset() < at) {
			first, at = link, e.GetOffset()
		}
	}
	if first != "" {
		return first
	}
	return xurls.Relaxed().FindString(message)
}
