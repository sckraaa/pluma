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

// Package importer puts a catalog collected from Telegram (task 024, ADR
// 0028) into the server: its documents into storage with ids of this
// server, every reference rewritten to them, the hashes counted over them,
// and the catalog stored at once.
package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest lists what a collection holds; it is manifest.json at the root
// of the collection, and every file it names is relative to that root.
// Objects are as tweb returns them (pkg/catalog/tl).
type Manifest struct {
	Layer       int    `json:"layer"`        // the collecting client's, must be the server's
	Client      string `json:"client"`       // the client and its version
	CollectedAt int64  `json:"collected_at"` // unix time
	// Answers are the answers of the catalog's methods.
	Answers []AnswerFile `json:"answers"`
	// Sets are the sticker and emoji sets as messages.getStickerSet gives
	// them: every set the answers name, and the sets asked for by kind.
	Sets []SetFile `json:"sets"`
	// ExtraDocuments are documents named only by id (custom emoji of the
	// emoji lists and statuses), one document per file.
	ExtraDocuments []string `json:"extra_documents"`
	// WallPapers are wallpapers fetched by slug (account.getWallPaper) that
	// the gallery may not hold, such as the one clients name in code.
	WallPapers []string `json:"wallpapers"`
	// Files are the files of the documents, by their id in the collection.
	Files []DocumentFiles `json:"files"`
}

// AnswerFile is the answer of method for arg ("" for none).
type AnswerFile struct {
	Method string `json:"method"`
	Arg    string `json:"arg"`
	File   string `json:"file"`
}

// SetFile is a set, and the kind clients ask for it by, if any
// (catalog.KindOf).
type SetFile struct {
	File string `json:"file"`
	Kind string `json:"kind"`
}

// DocumentFiles are the files of one document: the document's own and its
// downloadable thumbnails by type.
type DocumentFiles struct {
	ID     string            `json:"id"` // as a decimal string: tweb's longs pass 2^53
	File   string            `json:"file"`
	Thumbs map[string]string `json:"thumbs"`
}

// readManifest reads the manifest of the collection at root and checks its
// layer and that its files stay inside it (os.Root refuses them anyway).
func readManifest(root *os.Root, layer int) (*Manifest, error) {
	b, err := root.ReadFile("manifest.json")
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("importer: manifest: %w", err)
	}
	if m.Layer != layer {
		return nil, fmt.Errorf("importer: the collection is of layer %d, this importer reads %d", m.Layer, layer)
	}
	var names []string
	for _, a := range m.Answers {
		names = append(names, a.File)
	}
	for _, s := range m.Sets {
		names = append(names, s.File)
	}
	names = append(names, m.ExtraDocuments...)
	names = append(names, m.WallPapers...)
	for _, f := range m.Files {
		names = append(names, f.File)
		for _, t := range f.Thumbs {
			names = append(names, t)
		}
	}
	for _, n := range names {
		if n == "" || filepath.IsAbs(n) || strings.HasPrefix(filepath.Clean(n), "..") {
			return nil, fmt.Errorf("importer: manifest: a file %q outside the collection", n)
		}
	}
	return m, nil
}
