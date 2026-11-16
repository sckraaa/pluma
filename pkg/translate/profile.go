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

package translate

import (
	"fmt"
	"slices"
	"strconv"
)

// Profile is what depends on the model (ADR 0064, section 1): its
// languages, its prompts, how its answer is read, its sampling. The tags of
// the markup are <sN>…</sN> for every profile (markup.go); a model that
// needs another syntax brings a profile and a markup of its own.
//
// A change of a prompt or of a parameter is a new Version: the ID is a part
// of the key of the cache, so the translations of the old one are not given
// any more and expire.
type Profile interface {
	// Name is the name of the profile in the configuration (Translate.Profile).
	Name() string
	// Version is the version of the prompts and parameters.
	Version() int
	// ID is Name and Version, as the key of the cache takes them.
	ID() string
	// Lang returns the language of the model for to_lang as a client sends
	// it ("en", "pt-BR", "zh_TW"), or ErrToLangInvalid.
	Lang(toLang string) (string, error)
	// Request returns the request that translates text into lang, a
	// language Lang returned. tagged: text carries the <sN> tags of the
	// markup, and the answer keeps them.
	Request(lang, text string, tagged bool) (Request, error)
	// Answer returns the translation in answer, the completion of a
	// Request with the same tagged. For tagged it fails when the answer
	// is not in the form the prompt asked for; the markup then takes its
	// fallback.
	Answer(answer string, tagged bool) (string, error)
}

// DefaultSlotContext is the context of one slot of the engine in tokens:
// TRANSLATE_CTX 16384 over TRANSLATE_SLOTS 4 (ADR 0064, section 12).
const DefaultSlotContext = 4096

// profiles are the profiles by name.
var profiles = map[string]func(slotContext int) Profile{
	hymtName: newHYMT,
}

// NewProfile returns the profile named name for an engine whose slots have
// slotContext tokens of context each (0 takes DefaultSlotContext).
func NewProfile(name string, slotContext int) (Profile, error) {
	newProfile, ok := profiles[name]
	if !ok {
		return nil, fmt.Errorf("translate: unknown profile %q (known: %v)", name, ProfileNames())
	}
	if slotContext < 0 {
		return nil, fmt.Errorf("translate: negative slot context %d", slotContext)
	}
	if slotContext == 0 {
		slotContext = DefaultSlotContext
	}
	return newProfile(slotContext), nil
}

// ProfileNames returns the names NewProfile knows, sorted.
func ProfileNames() []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func profileID(name string, version int) string {
	return name + "/" + strconv.Itoa(version)
}
