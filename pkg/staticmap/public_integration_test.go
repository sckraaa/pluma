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

package staticmap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// publicTileserverConfig is the public config of the image
// (teamgramd/deploy/maps/build-styles.mjs, config-public.json) cut to one
// style served as JSON: no renderer, no static image, no front page.
const publicTileserverConfig = `{
  "options": {
    "paths": {"root": "/data", "styles": ""},
    "frontPage": false,
    "serveAllFonts": false,
    "serveAllStyles": false,
    "serveStaticMaps": false
  },
  "styles": {
    "lines": {"style": "lines.json", "serve_rendered": false, "serve_data": true}
  }
}`

// The links in the style JSON of the public tiles (task 112, ADR 0070) are
// built from what Caddy passes: the site's Host, X-Forwarded-Proto and the
// key as X-Forwarded-Path, so that maps-tiles itself never holds the key;
// a Host it is not told of gets no absolute links. Neither a static image
// nor a CORS header of its own (Caddy sets those) is served.
func TestTileserverGLPublicLinks(t *testing.T) {
	ctx := context.Background()
	var style map[string]any
	if err := json.Unmarshal(linesStyle(), &style); err != nil {
		t.Fatal(err)
	}
	style["glyphs"] = "{fontstack}/{range}.pbf"
	styleJSON, err := json.Marshal(style)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := testinfra.StartTileserverGLWith(ctx, map[string][]byte{
		"config.json": []byte(publicTileserverConfig),
		"lines.json":  styleJSON,
	}, map[string]string{"TILESERVER_GL_ALLOWED_HOSTS": "maps.example"}, "--no-cors", "--silent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ts.Terminate(context.Background()) })

	get := func(path, host string, headers map[string]string) (int, http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, resp.Header, body
	}
	glyphs := func(body []byte) string {
		t.Helper()
		var s struct {
			Glyphs string `json:"glyphs"`
		}
		if err := json.Unmarshal(body, &s); err != nil {
			t.Fatalf("style.json: %v: %s", err, body)
		}
		return s.Glyphs
	}

	const key = "Ab0-_cdefghijklmnopqrstuvwxyz1234"
	caddy := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Path": "/" + key}
	status, header, body := get("/styles/lines/style.json", "maps.example", caddy)
	if status != http.StatusOK {
		t.Fatalf("style.json: %d %s", status, body)
	}
	if got, want := glyphs(body), "https://maps.example/"+key+"/fonts/{fontstack}/{range}.pbf"; got != want {
		t.Errorf("glyphs %q, want %q", got, want)
	}
	if v := header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("Access-Control-Allow-Origin %q from tileserver-gl: Caddy would send it twice", v)
	}

	status, _, body = get("/styles/lines/style.json", "other.example", caddy)
	if status != http.StatusOK {
		t.Fatalf("style.json for another host: %d %s", status, body)
	}
	if got := glyphs(body); len(got) >= 4 && got[:4] == "http" {
		t.Errorf("glyphs %q for a host that is not allowed", got)
	}

	// No renderer: the static image, whose URL holds coordinates, is not
	// there. (The viewer pages of tileserver-gl stay; Caddy passes only the
	// paths of a map's resources.)
	if status, _, _ := get("/styles/lines/static/0,0,1/16x16.png", "maps.example", caddy); status != http.StatusNotFound {
		t.Errorf("a static image answered %d, want 404", status)
	}
}
