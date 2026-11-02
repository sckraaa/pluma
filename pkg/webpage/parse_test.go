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

package webpage

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestParseMetaOpenGraph(t *testing.T) {
	page := `<!doctype html><html><head>
<meta charset="utf-8">
<title>Plain title</title>
<meta name="description" content="Plain description">
<meta property="og:title" content="OG title">
<meta property="og:description" content="OG description">
<meta property="og:site_name" content="Site">
<meta property="og:type" content="video.other">
<meta property="og:url" content="https://site.example/canonical">
<meta property="og:image" content="http://site.example/a.png">
<meta property="og:image:secure_url" content="https://site.example/a.png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:video:url" content="https://site.example/embed/1">
<meta property="og:video:type" content="text/html">
<meta property="og:video:width" content="1280">
<meta property="og:video:height" content="720">
<meta name="author" content="Jane Doe">
<meta name="twitter:card" content="summary_large_image">
</head><body><meta property="og:title" content="in the body"></body></html>`
	m, err := parseMeta(strings.NewReader(page), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	want := Meta{
		Title: "OG title", Description: "OG description", SiteName: "Site", Type: "video.other",
		Card: "summary_large_image", Image: "https://site.example/a.png", ImageWidth: 1200, ImageHeight: 630,
		Video: "https://site.example/embed/1", VideoWidth: 1280, VideoHeight: 720, VideoType: "text/html",
		Author: "Jane Doe", Canonical: "https://site.example/canonical",
	}
	if m != want {
		t.Fatalf("meta:\n got %+v\nwant %+v", m, want)
	}
}

// Without Open Graph the card falls back to Twitter Cards, then to the
// plain title and description.
func TestParseMetaFallbacks(t *testing.T) {
	m, err := parseMeta(strings.NewReader(`<html><head>
<title>
   A   spaced
   title </title>
<meta name="twitter:description" content="Twitter description">
<meta name="description" content="Plain description">
<meta name="twitter:image:src" content="/t.jpg">
</head></html>`), "text/html; charset=utf-8")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "A spaced title" || m.Description != "Twitter description" || m.Image != "/t.jpg" {
		t.Fatalf("meta: %+v", m)
	}
	m, err = parseMeta(strings.NewReader(`<head><meta name="description" content="Only this"><meta name="twitter:title" content="TW">`), "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "TW" || m.Description != "Only this" {
		t.Fatalf("meta: %+v", m)
	}
}

func TestParseMetaCharset(t *testing.T) {
	text, err := charmap.Windows1251.NewEncoder().String("Привет")
	if err != nil {
		t.Fatal(err)
	}
	page := `<html><head><meta http-equiv="Content-Type" content="text/html; charset=windows-1251"><meta property="og:title" content="` + text + `"></head>`
	m, err := parseMeta(strings.NewReader(page), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Привет" {
		t.Fatalf("title %q", m.Title)
	}
	m, err = parseMeta(strings.NewReader(`<head><meta property="og:title" content="`+text+`">`), "text/html; charset=windows-1251")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Привет" {
		t.Fatalf("title by Content-Type %q", m.Title)
	}
}

func TestParseMetaLimits(t *testing.T) {
	long := strings.Repeat("ж", maxDescription+100)
	m, err := parseMeta(strings.NewReader(`<head><meta property="og:title" content="`+long+`"><meta property="og:description" content="`+long+`"><meta property="og:image:width" content="-5"><meta property="og:image:height" content="9999999">`), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(m.Title)); n != maxTitle {
		t.Fatalf("title of %d runes, want %d", n, maxTitle)
	}
	if n := len([]rune(m.Description)); n != maxDescription {
		t.Fatalf("description of %d runes, want %d", n, maxDescription)
	}
	if m.ImageWidth != 0 || m.ImageHeight != 0 {
		t.Fatalf("sizes %d x %d, want none", m.ImageWidth, m.ImageHeight)
	}
	// A page far longer than its head is not read to its end.
	var b bytes.Buffer
	b.WriteString(`<head><meta property="og:title" content="T"></head><body>`)
	for b.Len() < 4*maxPage {
		b.WriteString("<p>filler</p>")
	}
	r := bytes.NewReader(b.Bytes())
	if m, err = parseMeta(r, "text/html"); err != nil || m.Title != "T" {
		t.Fatalf("meta %+v, %v", m, err)
	}
	if r.Len() < 2*maxPage {
		t.Fatalf("read %d bytes past the head", b.Len()-r.Len())
	}
}
