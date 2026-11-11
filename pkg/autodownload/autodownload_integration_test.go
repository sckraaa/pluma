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

//go:build integration

package autodownload

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// What an account saved (migrate-20261114-user-look.sql): by the name it
// was saved under, the last save of a name wins, each account apart.
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))

	saved, err := s.Saved(ctx, 7)
	if err != nil || len(saved) != 0 {
		t.Fatalf("an account that saved nothing: %v, %v", saved, err)
	}
	custom := Settings{Disabled: true, AudioPreloadNext: true, PhotoSizeMax: 1000, VideoSizeMax: 5 << 32, FileSizeMax: 0, VideoUploadMaxbitrate: 60, SmallQueueActiveOperationsMax: 3}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Save(ctx, 7, Medium, Settings{PhotoSizeMax: 1}, 100))
	must(s.Save(ctx, 7, Medium, custom, 200))
	must(s.Save(ctx, 7, Medium, custom, 300)) // the same again
	must(s.Save(ctx, 7, High, Settings{VideoSizeMax: 9}, 200))
	must(s.Save(ctx, 8, Low, Settings{FileSizeMax: 4}, 200))
	if err = s.Save(ctx, 7, Preset(3), custom, 200); err == nil {
		t.Error("a preset that is none was saved")
	}

	saved, err = s.Saved(ctx, 7)
	if err != nil || len(saved) != 2 || saved[Medium] != custom || saved[High].VideoSizeMax != 9 {
		t.Errorf("account 7 saved %+v, %v", saved, err)
	}
	if _, ok := saved[Low]; ok {
		t.Error("account 7 has a low preset it never saved")
	}
	saved, err = s.Saved(ctx, 8)
	if err != nil || len(saved) != 1 || saved[Low].FileSizeMax != 4 {
		t.Errorf("account 8 saved %+v, %v", saved, err)
	}
}
