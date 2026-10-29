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

package conformance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// ServiceTexts reads the latest limit messages that Telegram's service
// account (777000) sent to the signed-in account of c and returns them as
// templates in Markdown, one per different text, for Pluma's server to send
// the same (task 017). What belongs to the account is replaced first: the
// codes, names, the phone, dates and times, addresses, and the values of
// the "Device:"/"Location:" lines, whatever their language. Bold and other
// entities are kept as markup (**bold**). It only reads.
func ServiceTexts(ctx context.Context, c *mtclient.Client, limit int) (doc string, messages, texts int, err error) {
	self, err := selfUser(ctx, c)
	if err != nil {
		return "", 0, 0, err
	}
	peer, err := servicePeer(ctx, c)
	if err != nil {
		return "", 0, 0, err
	}
	o, err := c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       int32(limit),
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("messages.getHistory: %w", err)
	}
	var list []*mtproto.Message
	switch r := o.(type) {
	case *mtproto.TLMessagesMessages:
		list = r.Data2.Messages
	case *mtproto.TLMessagesMessagesSlice:
		list = r.Data2.Messages
	default:
		return "", 0, 0, fmt.Errorf("messages.getHistory: %T", o)
	}

	scrub := newScrubber(self)
	seen := map[string]int{}
	var order []string
	dates := map[string][]string{}
	for _, m := range list {
		if m.GetMessage() == "" {
			continue
		}
		t := scrub.text(markup(m.GetMessage(), m.GetEntities()))
		if seen[t] == 0 {
			order = append(order, t)
		}
		seen[t]++
		dates[t] = append(dates[t], time.Unix(int64(m.GetDate()), 0).UTC().Format("2006-01-02"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Messages of Telegram's service account (777000)\n\n%d messages, newest first. Read %s.\n",
		len(list), time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	for i, t := range order {
		d := dates[t]
		fmt.Fprintf(&b, "\n## %d. %d× (%s … %s)\n\n```\n%s\n```\n", i+1, seen[t], d[len(d)-1], d[0], t)
	}
	return b.String(), len(list), len(order), nil
}

// ScrubServiceText is the template of one text of the service account to
// the account of self, as ServiceTexts writes it: the entities as markup,
// nothing of the account left.
func ScrubServiceText(self *mtproto.User, text string, entities []*mtproto.MessageEntity) string {
	return newScrubber(self).text(markup(text, entities))
}

// telegramServiceUserId is the account Telegram sends its service messages
// from.
const telegramServiceUserId = 777000

func selfUser(ctx context.Context, c *mtclient.Client) (*mtproto.User, error) {
	raw, err := c.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
		Constructor: c.Constructor(mtproto.Predicate_users_getUsers),
		Id:          []*mtproto.InputUser{mtproto.MakeTLInputUserSelf(nil).To_InputUser()},
	})
	if err != nil {
		return nil, fmt.Errorf("users.getUsers: %w", err)
	}
	users, err := mtclient.DecodeVector(raw)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u, ok := u.(*mtproto.TLUser); ok {
			return u.To_User(), nil
		}
	}
	return nil, errors.New("users.getUsers: no user")
}

// servicePeer finds the service account among the dialogs, for its access
// hash.
func servicePeer(ctx context.Context, c *mtclient.Client) (*mtproto.InputPeer, error) {
	o, err := c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
		Limit:       100,
	})
	if err != nil {
		return nil, fmt.Errorf("messages.getDialogs: %w", err)
	}
	var users []*mtproto.User
	switch r := o.(type) {
	case *mtproto.TLMessagesDialogs:
		users = r.Data2.Users
	case *mtproto.TLMessagesDialogsSlice:
		users = r.Data2.Users
	default:
		return nil, fmt.Errorf("messages.getDialogs: %T", o)
	}
	for _, u := range users {
		if u.GetId() == telegramServiceUserId {
			return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{
				UserId:     telegramServiceUserId,
				AccessHash: u.GetAccessHash().GetValue(),
			}).To_InputPeer(), nil
		}
	}
	return nil, errors.New("the service account is not among the latest 100 dialogs")
}

// markup puts the entities into the text: **bold**, __italic__, `code`,
// [text](url) for text links; offsets are in UTF-16 units.
func markup(text string, entities []*mtproto.MessageEntity) string {
	units := utf16.Encode([]rune(text))
	type mark struct {
		at    int
		text  string
		close bool
	}
	var marks []mark
	for _, e := range entities {
		open, end := "", ""
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityBold:
			open, end = "**", "**"
		case mtproto.Predicate_messageEntityItalic:
			open, end = "__", "__"
		case mtproto.Predicate_messageEntityCode:
			open, end = "`", "`"
		case mtproto.Predicate_messageEntityTextUrl:
			open, end = "[", "]("+e.GetUrl()+")"
		case mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityMention, mtproto.Predicate_messageEntityHashtag:
			continue
		default:
			open, end = "<"+e.GetPredicateName()+">", "</"+e.GetPredicateName()+">"
		}
		from, to := int(e.GetOffset()), int(e.GetOffset()+e.GetLength())
		if from < 0 || to > len(units) || from > to {
			continue
		}
		marks = append(marks, mark{from, open, false}, mark{to, end, true})
	}
	// At one position the closing marks go first.
	sort.SliceStable(marks, func(i, j int) bool {
		if marks[i].at != marks[j].at {
			return marks[i].at < marks[j].at
		}
		return marks[i].close && !marks[j].close
	})
	var b strings.Builder
	pos := 0
	for _, m := range marks {
		b.WriteString(string(utf16.Decode(units[pos:m.at])))
		b.WriteString(m.text)
		pos = m.at
	}
	b.WriteString(string(utf16.Decode(units[pos:])))
	return b.String()
}

type scrubber struct {
	names []string
	rules []rule
}

type rule struct {
	re   *regexp.Regexp
	with string
}

func newScrubber(self *mtproto.User) *scrubber {
	s := &scrubber{}
	for _, n := range []string{
		strings.TrimSpace(self.GetFirstName().GetValue() + " " + self.GetLastName().GetValue()),
		self.GetFirstName().GetValue(),
		self.GetLastName().GetValue(),
	} {
		if len([]rune(n)) >= 2 {
			s.names = append(s.names, n)
		}
	}
	s.rules = []rule{
		// Values of "Label: value" lines in a block of them (Device:,
		// Location:, in any language): the next two rules see none.
		{regexp.MustCompile(`(?m)^([^\n:*]{2,30}): [^\n]+$`), "$1: {value}"},
		// Times first: 15:00:20 would pass for an IPv6 address.
		{regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\b`), "{time}"},
		{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b|\b[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{0,4}){2,7}\b`), "{ip}"},
		{regexp.MustCompile(`\b\d{1,4}[./-]\d{1,2}[./-]\d{1,4}\b`), "{date}"},
		// A code of letters and digits alone on its line (my.telegram.org).
		{regexp.MustCompile(`(?m)^[A-Za-z0-9_-]*\d[A-Za-z0-9_-]*[A-Za-z][A-Za-z0-9_-]*$|(?m)^[A-Za-z0-9_-]*[A-Za-z][A-Za-z0-9_-]*\d[A-Za-z0-9_-]*$`), "{code}"},
		{regexp.MustCompile(`\+?\d[\d -]{8,}\d`), "{phone}"},
		{regexp.MustCompile(`\b\d{5,8}\b`), "{code}"},
	}
	return s
}

// text replaces what belongs to the account. A line that starts the
// message ("Login code: 12345. …") keeps its words: only lines of a block
// of at least two "Label: value" lines lose their values.
func (s *scrubber) text(t string) string {
	for _, n := range s.names {
		t = strings.ReplaceAll(t, n, "{name}")
	}
	var out []string
	for _, para := range strings.Split(t, "\n\n") {
		lines := strings.Split(para, "\n")
		block := len(lines) >= 2
		for _, l := range lines {
			if !s.rules[0].re.MatchString(l) || len([]rune(l)) > 120 {
				block = false
			}
		}
		if block {
			para = s.rules[0].re.ReplaceAllString(para, s.rules[0].with)
		}
		for _, r := range s.rules[1:] {
			para = r.re.ReplaceAllString(para, r.with)
		}
		out = append(out, para)
	}
	return strings.Join(out, "\n\n")
}
