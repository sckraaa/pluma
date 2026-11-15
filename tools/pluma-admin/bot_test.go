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
	"errors"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// bot token and bot set leave the built-in inline bots alone: no token signs
// in as them and their flags are the registry's.
func TestBuiltinBotsAreNotEditable(t *testing.T) {
	for _, b := range inlinebots.All() {
		if err := checkEditable(b.Id); !errors.Is(err, errBuiltinBot) {
			t.Errorf("@%s: %v", b.Username, err)
		}
	}
	if err := checkEditable(136907713); err != nil {
		t.Errorf("a bot of an owner: %v", err)
	}
}
