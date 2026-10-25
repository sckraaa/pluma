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

// Hide says how much of a notification iOS may show.
type Hide int

const (
	HideNone Hide = iota
	HideText      // show_previews off: who wrote, not what
	HideAll       // the device is locked past its period: nothing
)

// IOSPush is a push to iOS: Payload is encrypted into "p"; the rest says
// how APNs gets it.
type IOSPush struct {
	Payload []byte
	// Alert: shown to the user (apns-push-type alert); else a background
	// push the app handles silently.
	Alert    bool
	Sound    bool
	ThreadId string
	// Placeholder is the outer alert, shown only if the extension cannot
	// decrypt: it says nothing about the message.
	Placeholder string
}

type iosAps struct {
	Alert    *iosAlert `json:"alert,omitempty"`
	Sound    string    `json:"sound,omitempty"`
	Category string    `json:"category,omitempty"`
	ThreadId string    `json:"thread-id,omitempty"`
}

type iosAlert struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
}

// IOS returns the push of n for iOS in the language lang (the
// authorization's lang_code), nil if iOS gets none. Telegram-iOS shows the
// text of the payload as it is and reads the ids as strings; a message
// push has no loc-key, a service push has nothing else.
func IOS(n *Notification, lang string, hide Hide) (*IOSPush, error) {
	t := textsFor(lang)
	ids := map[string]any{}
	if n.Chat {
		ids["chat_id"] = strconv.FormatInt(n.PeerId, 10)
	} else {
		ids["from_id"] = strconv.FormatInt(n.PeerId, 10)
	}
	switch n.Kind {
	case KindReadHistory:
		ids["loc-key"] = "READ_HISTORY"
		ids["max_id"] = strconv.FormatInt(int64(n.MaxId), 10)
		return background(ids)
	case KindSessionRevoke:
		return background(map[string]any{"loc-key": "SESSION_REVOKE"})
	case KindMuted:
		return nil, nil // iOS registers with no_muted
	}

	title, body := iosText(n, t, hide)
	aps := iosAps{
		Alert:    &iosAlert{Title: title, Body: body},
		Category: "r",
		ThreadId: threadId(n),
	}
	if n.Kind == KindMessage && n.Content != ContentText && n.Content != ContentNoText && hide == HideNone {
		aps.Category = "m"
	}
	if !n.Silent {
		aps.Sound = "default"
	}
	ids["aps"] = aps
	if n.MsgId != 0 {
		ids["msg_id"] = strconv.FormatInt(int64(n.MsgId), 10)
	}
	if n.Silent {
		ids["silent"] = "1"
	}
	payload, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return &IOSPush{Payload: payload, Alert: true, Sound: !n.Silent, ThreadId: aps.ThreadId, Placeholder: t.newMessage}, nil
}

func background(fields map[string]any) (*IOSPush, error) {
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return &IOSPush{Payload: payload}, nil
}

// threadId groups the notifications of a dialog: its id as the clients
// write it, negative for a basic group.
func threadId(n *Notification) string {
	if n.Chat {
		return strconv.FormatInt(-n.PeerId, 10)
	}
	return strconv.FormatInt(n.PeerId, 10)
}

// iosText returns the title and body of n. A private chat: the author, the
// message. A group: the group, "author: message", or the action.
func iosText(n *Notification, t *texts, hide Hide) (string, string) {
	switch hide {
	case HideAll:
		return "", t.newMessage
	case HideText:
		if n.Chat {
			return n.ChatTitle, t.newMessage
		}
		return n.FromName, t.newMessage
	}
	var body string
	switch n.Kind {
	case KindMessage:
		switch {
		case n.Album:
			body = t.album
		case n.Forwarded:
			body = format(t.forwarded, strconv.Itoa(n.Count))
		default:
			body = t.content(n.Content, n.Text)
		}
	case KindAction:
		body = format(t.actions[n.Action], n.FromName, n.ChatTitle, n.MemberName)
	case KindPinned:
		if n.Content == ContentNoText {
			body = format(t.pinnedNoText, n.FromName)
		} else {
			body = format(t.pinned, n.FromName, t.content(n.Content, n.Text))
		}
	}
	if !n.Chat {
		return n.FromName, body
	}
	if n.Kind == KindMessage {
		body = n.FromName + ": " + body
	}
	return n.ChatTitle, body
}
