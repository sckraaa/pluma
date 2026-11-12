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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// The service keeps its state in one JSON file of its volume: some hundred
// devices at most (Apple allows 100 iPhones a year), so a file written
// whole on every change (temporary file, fsync, rename) is enough, and the
// service stays apart from the messenger's databases and their secrets.

// Device is an iPhone that asked for the app. UDIDs are personal data: they
// never reach the logs.
type Device struct {
	UDID    string    `json:"udid"`
	Product string    `json:"product"` // iPhone16,1
	OS      string    `json:"os"`      // 26.1
	AppleID string    `json:"apple_id,omitempty"`
	Token   string    `json:"token"` // the device's link to its status, manifest and app
	Added   time.Time `json:"added"`
	Failure string    `json:"failure,omitempty"` // why Apple refused it, for the operator
}

// Bundle is an app or extension of a build: its path in the app, its
// bundle ID and its entitlements (the plist CI signed it with).
type Bundle struct {
	Path         string `json:"path"` // "" for the app, "PlugIns/ShareExtension.appex"
	BundleID     string `json:"bundle_id"`
	Entitlements string `json:"entitlements"`
}

// Build is the IPA CI signed last, the source of every install.
type Build struct {
	ID       string    `json:"id"` // SHA-256 of the IPA
	Version  string    `json:"version"`
	Number   string    `json:"number"`
	Uploaded time.Time `json:"uploaded"`
	Bundles  []Bundle  `json:"bundles"`
}

// Signed is the IPA re-signed for the devices.
type Signed struct {
	Build   string    `json:"build"`
	File    string    `json:"file"`
	Devices []string  `json:"devices"` // Apple's ids of the devices of its profiles
	Signed  time.Time `json:"signed"`
}

type state struct {
	Devices []*Device `json:"devices"`
	Build   *Build    `json:"build,omitempty"`
	Signed  *Signed   `json:"signed,omitempty"`
}

type store struct {
	path string
	mu   sync.Mutex
	st   state
}

func openStore(dir string) (*store, error) {
	s := &store{path: filepath.Join(dir, "state.json")}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &s.st); err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, err)
		}
	}
	return s, nil
}

// save writes the state; the caller holds mu.
func (s *store) save() error {
	raw, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path) //nolint:gosec // G703: the operator's DATA_DIR, no outside input
}

// update changes the state under the lock and saves it; a failed save
// leaves the state as it was.
func (s *store) update(f func(st *state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := json.Marshal(&s.st)
	if err != nil {
		return err
	}
	if err := f(&s.st); err != nil {
		return err
	}
	if err := s.save(); err != nil {
		s.st = state{}
		_ = json.Unmarshal(before, &s.st)
		return err
	}
	return nil
}

// view reads a copy of the state.
func (s *store) view() state {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(&s.st)
	var st state
	_ = json.Unmarshal(raw, &st)
	return st
}

func (st *state) device(udid string) *Device {
	for _, d := range st.Devices {
		if d.UDID == udid {
			return d
		}
	}
	return nil
}

func (st *state) byToken(token string) *Device {
	for _, d := range st.Devices {
		if d.Token == token {
			return d
		}
	}
	return nil
}

// addedSince counts the devices added after t.
func (st *state) addedSince(t time.Time) int {
	n := 0
	for _, d := range st.Devices {
		if d.Added.After(t) {
			n++
		}
	}
	return n
}

// ready reports whether the current IPA is signed for d.
func (st *state) ready(d *Device) bool {
	if d.AppleID == "" || st.Build == nil || st.Signed == nil || st.Signed.Build != st.Build.ID {
		return false
	}
	for _, id := range st.Signed.Devices {
		if id == d.AppleID {
			return true
		}
	}
	return false
}

// signedBuild is the number of the build whose signed IPA is served, the
// one every registered iPhone gets; false while there is none or the last
// uploaded build is not signed yet.
func (st *state) signedBuild() (int32, bool) {
	if st.Build == nil || st.Signed == nil || st.Signed.Build != st.Build.ID {
		return 0, false
	}
	n, err := strconv.ParseInt(st.Build.Number, 10, 32)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}

func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func newToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
