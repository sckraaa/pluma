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

package notify

import (
	"encoding/json"
	"strconv"
)

// androidPayload is what PushListenerController reads after decryption.
// custom must be present: Android drops a push without it.
type androidPayload struct {
	LocKey  string         `json:"loc_key"`
	LocArgs []string       `json:"loc_args"`
	Custom  map[string]any `json:"custom"`
	UserId  int64          `json:"user_id"`
}

// Android returns the payload of n for Android (to be encrypted). hideText
// leaves the text out (show_previews off): MESSAGE_NOTEXT and the like.
func Android(n *Notification, hideText bool) ([]byte, error) {
	key, args := LocKey(n, hideText)
	if args == nil {
		args = []string{}
	}
	return json.Marshal(androidPayload{LocKey: key, LocArgs: args, Custom: custom(n), UserId: n.UserId})
}

// LocKey returns the loc_key of n and its loc_args.
func LocKey(n *Notification, hideText bool) (string, []string) {
	base := []string{n.FromName}
	msgPrefix := "MESSAGE_"
	if n.Chat {
		base = append(base, n.ChatTitle)
		msgPrefix = "CHAT_MESSAGE_"
	}
	c := n.Content
	if hideText {
		c = ContentNoText
	}
	switch n.Kind {
	case KindMessage:
		switch {
		case n.Album:
			if n.Chat {
				return "CHAT_MESSAGES", base
			}
			return "MESSAGES", base
		case n.Forwarded:
			return msgPrefix + "FWDS", append(base, strconv.Itoa(n.Count))
		}
		if arg, ok := contentArg(c, n.Text); ok {
			return msgPrefix + string(c), append(base, arg)
		}
		return msgPrefix + string(c), base
	case KindAction:
		if n.Action == ActionAddMember || n.Action == ActionDeleteMember {
			return "CHAT_" + string(n.Action), append(base, n.MemberName)
		}
		return "CHAT_" + string(n.Action), base
	case KindPinned:
		arg, ok := contentArg(c, n.Text)
		switch {
		case ok:
			return "PINNED_" + string(c), append(base, arg)
		case !n.Chat:
			// The private form of PINNED_* reads a second argument.
			return "PINNED_" + string(c), append(base, "")
		}
		return "PINNED_" + string(c), base
	case KindReadHistory:
		return "READ_HISTORY", nil
	case KindMuted:
		return "MESSAGE_MUTED", nil
	case KindSessionRevoke:
		return "SESSION_REVOKE", nil
	}
	return "", nil
}

// contentArg is the argument a content adds to loc_args, if any.
func contentArg(c Content, text string) (string, bool) {
	switch c {
	case ContentText, ContentSticker, ContentContact, ContentPoll, ContentQuiz:
		return text, true
	}
	return "", false
}

// custom are the ids of a notification, as numbers.
func custom(n *Notification) map[string]any {
	c := map[string]any{}
	if n.Kind == KindSessionRevoke {
		return c
	}
	if n.Chat {
		c["chat_id"] = n.PeerId
		if n.FromId != 0 {
			c["chat_from_id"] = n.FromId
		}
	} else {
		c["from_id"] = n.PeerId
	}
	if n.MsgId != 0 {
		c["msg_id"] = n.MsgId
	}
	if n.Kind == KindReadHistory {
		c["max_id"] = n.MaxId
	}
	if n.Mention {
		c["mention"] = 1
	}
	if n.Silent {
		c["silent"] = 1
	}
	return c
}
