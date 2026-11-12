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

package tgexport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
)

// Mapping tells which Pluma account each person of the exported group is:
// the file the owner fills and confirms before an import (ADR 0051, p. 3).
type Mapping struct {
	// ExportChatId is the id of the exported chat the mapping is for.
	ExportChatId int64 `json:"export_chat_id"`
	// Creator is the Pluma account that becomes the creator of the group.
	Creator int64 `json:"creator"`
	// Exporter is the Pluma account of whoever made the export: the
	// answers of polls the export marks chosen are that account's votes.
	// 0: no votes are imported.
	Exporter int64 `json:"exporter"`
	// Authors are the peers the export names with an id, by that id
	// ("user<id>", "channel<id>").
	Authors map[string]*Author `json:"authors"`
	// ServiceNames are the names service messages give without an id
	// (the members added or removed, an inviter) that no author of the
	// export has alone: the Pluma account of each, or null for a service
	// message that is not imported.
	ServiceNames map[string]*int64 `json:"service_names"`
	// ExtraMembers are Pluma accounts that join the group though the
	// export does not name them.
	ExtraMembers []int64 `json:"extra_members"`
}

// Author is one peer of the export.
type Author struct {
	// Names are the names the export gives it; Messages, Service and
	// Reactions count what it wrote, did and reacted. They are written
	// for the owner and not read back.
	Names     []string `json:"names"`
	Messages  int64    `json:"messages"`
	Service   int64    `json:"service,omitempty"`
	Reactions int64    `json:"reactions,omitempty"`
	// Pluma is the id of its Pluma account; PlumaName that account's
	// name, for the owner to check.
	Pluma     *int64 `json:"pluma"`
	PlumaName string `json:"pluma_name,omitempty"`
	// Suggested tells that Pluma was found by the name, not set by the
	// owner.
	Suggested bool `json:"suggested,omitempty"`
	// Confirmed is the owner's word that Pluma is right. A row without
	// it maps nobody.
	Confirmed bool `json:"confirmed"`
	// Skip is the owner's choice not to import what this peer wrote.
	Skip bool `json:"skip,omitempty"`
}

// mapped tells whether the author is a confirmed Pluma account.
func (a *Author) mapped() bool {
	return a != nil && !a.Skip && a.Confirmed && a.Pluma != nil && *a.Pluma > 0
}

// Account is a Pluma account a mapping may name.
type Account struct {
	Id   int64
	Name string // first and last name
}

// LoadMapping reads a mapping file.
func LoadMapping(path string) (*Mapping, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Mapping
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("mapping %s: %w", path, err)
	}
	return &m, nil
}

// Save writes the mapping to path, for the owner to edit.
func (m *Mapping) Save(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Seen is what a census found of one peer.
type Seen struct {
	Names     []string
	Messages  int64
	Service   int64
	Reactions int64
}

// Census is what one pass over an export finds about its people: the
// peers named with an id, and the names service messages give without one.
type Census struct {
	Header  Header
	Records int64
	// Peers are the peers named with an id, by their key.
	Peers map[string]*Seen
	// names are the keys of the users each name belongs to.
	names map[string][]string
	// ServiceNames count the uses of each name a service message gives
	// without an id.
	ServiceNames map[string]int64
}

func (c *Census) see(p Peer) *Seen {
	if p.Key == "" {
		return nil
	}
	s := c.Peers[p.Key]
	if s == nil {
		s = &Seen{}
		c.Peers[p.Key] = s
	}
	if p.Name != "" && !slices.Contains(s.Names, p.Name) {
		s.Names = append(s.Names, p.Name)
		if strings.HasPrefix(p.Key, "user") && !slices.Contains(c.names[p.Name], p.Key) {
			c.names[p.Name] = append(c.names[p.Name], p.Key)
		}
	}
	return s
}

// Scan reads the whole export once and counts its people.
func Scan(ex *Export) (*Census, error) {
	c := &Census{Header: ex.Header(), Peers: map[string]*Seen{}, names: map[string][]string{}, ServiceNames: map[string]int64{}}
	err := ex.Each(func(r *Record) error {
		c.Records++
		switch r.Kind {
		case KindMessage:
			if s := c.see(r.From); s != nil {
				s.Messages++
			}
			for _, x := range r.Reactions {
				for _, n := range x.Recent {
					if s := c.see(n.From); s != nil {
						s.Reactions++
					}
				}
			}
		case KindService:
			if s := c.see(r.From); s != nil {
				s.Service++
			}
			for _, name := range r.Action.serviceNames() {
				c.ServiceNames[name]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// serviceNames are the names the action gives without an id, for the
// actions that are imported.
func (a *Action) serviceNames() []string {
	if a == nil {
		return nil
	}
	switch a.Name {
	case "invite_members", "remove_members":
		return a.Members
	case "join_group_by_link":
		return []string{a.Inviter}
	}
	return nil
}

// keyOfName is the key of the one user the export names so, "" when no
// user or several have the name.
func (c *Census) keyOfName(name string) string {
	if keys := c.names[name]; len(keys) == 1 {
		return keys[0]
	}
	return ""
}

// Suggest makes the mapping of a census for the owner to fill: every peer
// with what the export tells of it and, when exactly one of accounts has
// one of its names, that account, not confirmed. The rows the owner has
// already filled in old (nil for none) are kept as they are, but for the
// name of the account, which is always the one it has now.
func Suggest(c *Census, accounts []Account, old *Mapping) *Mapping {
	m := &Mapping{ExportChatId: c.Header.Id, Authors: map[string]*Author{}, ServiceNames: map[string]*int64{}, ExtraMembers: []int64{}}
	if old != nil && old.ExportChatId == c.Header.Id {
		m.Creator, m.Exporter = old.Creator, old.Exporter
		if old.ExtraMembers != nil {
			m.ExtraMembers = old.ExtraMembers
		}
	}
	byName := map[string][]Account{}
	byId := map[int64]string{}
	for _, a := range accounts {
		byId[a.Id] = a.Name
		if a.Name != "" {
			byName[a.Name] = append(byName[a.Name], a)
		}
	}
	for key, s := range c.Peers {
		row := &Author{Names: slices.Clone(s.Names), Messages: s.Messages, Service: s.Service, Reactions: s.Reactions}
		if row.Names == nil {
			row.Names = []string{}
		}
		sort.Strings(row.Names)
		var prev *Author
		if old != nil {
			prev = old.Authors[key]
		}
		switch {
		case prev != nil && (prev.Pluma != nil || prev.Skip || prev.Confirmed):
			row.Pluma, row.Suggested, row.Confirmed, row.Skip = prev.Pluma, prev.Suggested, prev.Confirmed, prev.Skip
			// The name is the account's own, read anew: it follows an
			// id the owner changed by hand.
			if prev.Pluma != nil {
				row.PlumaName = byId[*prev.Pluma]
			}
		case strings.HasPrefix(key, "user"):
			var found []Account
			for _, name := range s.Names {
				for _, a := range byName[name] {
					if !slices.ContainsFunc(found, func(f Account) bool { return f.Id == a.Id }) {
						found = append(found, a)
					}
				}
			}
			if len(found) == 1 {
				id := found[0].Id
				row.Pluma, row.PlumaName, row.Suggested = &id, found[0].Name, true
			}
		}
		m.Authors[key] = row
	}
	for name := range c.ServiceNames {
		if name == "" || c.keyOfName(name) != "" {
			continue
		}
		var id *int64
		if old != nil {
			id = old.ServiceNames[name]
		}
		m.ServiceNames[name] = id
	}
	return m
}

// Members are the Pluma accounts that join the imported group besides its
// creator: every mapped peer, the people of ServiceNames and ExtraMembers,
// and the exporter, whose poll votes are imported, in ascending order.
func (m *Mapping) Members() []int64 {
	var out []int64
	add := func(id int64) {
		if id > 0 && id != m.Creator && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, a := range m.Authors {
		if a.mapped() {
			add(*a.Pluma)
		}
	}
	for _, id := range m.ServiceNames {
		if id != nil {
			add(*id)
		}
	}
	for _, id := range m.ExtraMembers {
		add(id)
	}
	add(m.Exporter)
	slices.Sort(out)
	return out
}

// Accounts are all the Pluma accounts the mapping names.
func (m *Mapping) Accounts() []int64 {
	out := m.Members()
	if m.Creator > 0 {
		out = append(out, m.Creator)
	}
	if m.Exporter > 0 && !slices.Contains(out, m.Exporter) {
		out = append(out, m.Exporter)
	}
	slices.Sort(out)
	return out
}

// Unmatched is an author of messages the mapping gives no account.
type Unmatched struct {
	Key      string
	Names    []string
	Messages int64
	// Why: "missing" (no row), "no_account" (no pluma id), "unconfirmed".
	Why string
}

// Validate checks the mapping against the census of its export. exists
// tells whether a Pluma account is there (nil: not checked). It returns
// the authors of messages without an account, which make an import
// impossible (the owner's decision of 2026-11-11: there is no fallback),
// and every other fault as a line of text.
func (m *Mapping) Validate(c *Census, exists func(id int64) bool) (unmatched []Unmatched, faults []string) {
	if m.ExportChatId != c.Header.Id {
		faults = append(faults, fmt.Sprintf("the mapping is of chat %d, the export of chat %d", m.ExportChatId, c.Header.Id))
	}
	check := func(what string, id int64) {
		if exists != nil && id > 0 && !exists(id) {
			faults = append(faults, fmt.Sprintf("%s: there is no Pluma account %d", what, id))
		}
	}
	if m.Creator <= 0 {
		faults = append(faults, `"creator" is not set: the Pluma account that will own the group`)
	}
	check("creator", m.Creator)
	check("exporter", m.Exporter)
	keys := make([]string, 0, len(c.Peers))
	for key := range c.Peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s, a := c.Peers[key], m.Authors[key]
		if a != nil && a.Skip && a.Pluma != nil {
			faults = append(faults, fmt.Sprintf("%s: both an account and \"skip\"", key))
		}
		if a != nil && a.Pluma != nil {
			check(key, *a.Pluma)
		}
		if s.Messages == 0 || a.mapped() || a != nil && a.Skip {
			continue
		}
		u := Unmatched{Key: key, Names: s.Names, Messages: s.Messages, Why: "unconfirmed"}
		switch {
		case a == nil:
			u.Why = "missing"
		case a.Pluma == nil || *a.Pluma <= 0:
			u.Why = "no_account"
		}
		unmatched = append(unmatched, u)
	}
	names := make([]string, 0, len(m.ServiceNames))
	for name := range m.ServiceNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if id := m.ServiceNames[name]; id != nil {
			if *id <= 0 {
				faults = append(faults, "service_names: an account id must be positive")
			}
			check("service_names", *id)
		}
	}
	for _, id := range m.ExtraMembers {
		if id <= 0 {
			faults = append(faults, "extra_members: an account id must be positive")
		}
		check("extra_members", id)
	}
	return unmatched, faults
}

// user is the Pluma account of the peer key, when the mapping has one.
func (m *Mapping) user(key string) (int64, bool) {
	if a := m.Authors[key]; a.mapped() {
		return *a.Pluma, true
	}
	return 0, false
}

// skipped tells whether the owner chose not to import what key wrote.
func (m *Mapping) skipped(key string) bool {
	a := m.Authors[key]
	return a != nil && a.Skip
}

// IsNotExist tells whether err is of a file that is not there.
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
