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

package recognizer

import (
	"testing"
	"time"
)

func TestJoin(t *testing.T) {
	if got := join([]string{" Первая часть. ", "", "  ", "Вторая."}); got != "Первая часть. Вторая." {
		t.Fatalf("got %q", got)
	}
	if got := join(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPieceLimit(t *testing.T) {
	// A piece of twenty seconds may take its minute and four times itself;
	// the limit is the piece's, never the record's.
	if got := PieceLimit(20 * 16000); got != 140*time.Second {
		t.Fatalf("got %s", got)
	}
	if PieceLimit(0) != time.Minute {
		t.Fatal("an empty piece")
	}
}
