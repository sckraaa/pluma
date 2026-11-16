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
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/webappdata"
)

const testBotId = 136907800

// fakePlatform is pkg/botstore in memory, for the commands of the bot
// platform; writes counts every change.
type fakePlatform struct {
	bots     map[string]int64
	domain   string
	mainApp  string
	apps     map[string]botstore.App
	games    map[string]botstore.Game
	menu     *botstore.AttachMenu
	client   *botstore.LoginClient
	secret   string
	writes   int
	nextFile int64
}

func newFakePlatform() *fakePlatform {
	f := &fakePlatform{
		bots:  map[string]int64{"shopbot": testBotId},
		apps:  map[string]botstore.App{},
		games: map[string]botstore.Game{},
	}
	for _, b := range inlinebots.All() {
		f.bots[b.Username] = b.Id
	}
	return f
}

func (f *fakePlatform) ByUsername(_ context.Context, username string) (int64, error) {
	return f.bots[username], nil
}

func (f *fakePlatform) SetLoginDomain(_ context.Context, _ int64, domain string) error {
	f.writes++
	if domain != "" {
		d, ok := botstore.NormalizeLoginDomain(domain)
		if !ok {
			return botstore.ErrLoginDomainInvalid
		}
		domain = d
	}
	f.domain = domain
	return nil
}

func (f *fakePlatform) LoginDomain(context.Context, int64) (string, error) { return f.domain, nil }

func (f *fakePlatform) SetMainApp(_ context.Context, _ int64, url string) error {
	f.writes++
	f.mainApp = url
	return nil
}

func (f *fakePlatform) MainApp(context.Context, int64) (string, error) { return f.mainApp, nil }

func (f *fakePlatform) AddApp(_ context.Context, a botstore.App) (botstore.App, error) {
	f.writes++
	if _, ok := f.apps[a.ShortName]; ok {
		return botstore.App{}, botstore.ErrShortNameTaken
	}
	a.Id, a.AccessHash, a.Version = 1, 2, 1
	f.apps[a.ShortName] = a
	return a, nil
}

func (f *fakePlatform) EditApp(_ context.Context, a botstore.App) (botstore.App, error) {
	f.writes++
	if _, ok := f.apps[a.ShortName]; !ok {
		return botstore.App{}, botstore.ErrNoApp
	}
	a.Version++
	f.apps[a.ShortName] = a
	return a, nil
}

func (f *fakePlatform) DeleteApp(_ context.Context, _ int64, shortName string) error {
	f.writes++
	if _, ok := f.apps[shortName]; !ok {
		return botstore.ErrNoApp
	}
	delete(f.apps, shortName)
	return nil
}

func (f *fakePlatform) Apps(context.Context, int64) ([]botstore.App, error) {
	var out []botstore.App
	for _, a := range f.apps {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakePlatform) AppByShortName(_ context.Context, _ int64, shortName string) (*botstore.App, error) {
	if a, ok := f.apps[shortName]; ok {
		return &a, nil
	}
	return nil, nil
}

func (f *fakePlatform) AddGame(_ context.Context, g botstore.Game) (botstore.Game, error) {
	f.writes++
	g.Id = 7
	f.games[g.ShortName] = g
	return g, nil
}

func (f *fakePlatform) EditGame(_ context.Context, g botstore.Game) (botstore.Game, error) {
	f.writes++
	if _, ok := f.games[g.ShortName]; !ok {
		return botstore.Game{}, botstore.ErrNoGame
	}
	f.games[g.ShortName] = g
	return g, nil
}

func (f *fakePlatform) DeleteGame(_ context.Context, _ int64, shortName string) error {
	f.writes++
	if _, ok := f.games[shortName]; !ok {
		return botstore.ErrNoGame
	}
	delete(f.games, shortName)
	return nil
}

func (f *fakePlatform) Games(context.Context, int64) ([]botstore.Game, error) {
	var out []botstore.Game
	for _, g := range f.games {
		out = append(out, g)
	}
	return out, nil
}

func (f *fakePlatform) GameByShortName(_ context.Context, _ int64, shortName string) (*botstore.Game, error) {
	if g, ok := f.games[shortName]; ok {
		return &g, nil
	}
	return nil, nil
}

func (f *fakePlatform) SetAttachMenu(_ context.Context, m botstore.AttachMenu) ([]int64, error) {
	f.writes++
	f.menu = &m
	return []int64{1, 2}, nil
}

func (f *fakePlatform) DeleteAttachMenu(context.Context, int64) ([]int64, error) {
	f.writes++
	if f.menu == nil {
		return nil, botstore.ErrNoAttachMenu
	}
	f.menu = nil
	return []int64{1}, nil
}

func (f *fakePlatform) AttachMenu(context.Context, int64) (*botstore.AttachMenu, error) {
	if f.menu == nil {
		return nil, nil
	}
	m := *f.menu
	return &m, nil
}

func (f *fakePlatform) SetLoginClient(_ context.Context, botId int64, redirects, origins []string, now int32) (string, error) {
	f.writes++
	f.client = &botstore.LoginClient{BotId: botId, RedirectURIs: redirects, Origins: origins, CreatedAt: now}
	f.secret = "secret-" + time.Unix(int64(now), 0).UTC().Format("150405")
	return f.secret, nil
}

func (f *fakePlatform) EditLoginClient(_ context.Context, _ int64, redirects, origins []string) error {
	f.writes++
	if f.client == nil {
		return botstore.ErrNoLoginClient
	}
	f.client.RedirectURIs, f.client.Origins = redirects, origins
	return nil
}

func (f *fakePlatform) DeleteLoginClient(context.Context, int64) error {
	f.writes++
	if f.client == nil {
		return botstore.ErrNoLoginClient
	}
	f.client = nil
	return nil
}

func (f *fakePlatform) LoginClient(context.Context, int64) (*botstore.LoginClient, error) {
	if f.client == nil {
		return nil, nil
	}
	c := *f.client
	return &c, nil
}

// fakeUpload is a file the commands sent to dfs and media.
type fakeUpload struct {
	photo      bool
	name, mime string
	attributes []*mtproto.DocumentAttribute
	size       int
}

// fakeBotServices records the uploads and the calls to biz.
type fakeBotServices struct {
	uploads []fakeUpload
	updates []*userpb.TLUserUpdateBotData
}

func (s *fakeBotServices) UploadPhoto(_ context.Context, image []byte, name string) (int64, error) {
	s.uploads = append(s.uploads, fakeUpload{photo: true, name: name, size: len(image)})
	return int64(100 + len(s.uploads)), nil
}

func (s *fakeBotServices) UploadDocument(_ context.Context, file []byte, name, mimeType string, attributes []*mtproto.DocumentAttribute) (int64, error) {
	s.uploads = append(s.uploads, fakeUpload{name: name, mime: mimeType, attributes: attributes, size: len(file)})
	return int64(100 + len(s.uploads)), nil
}

func (s *fakeBotServices) UpdateBotData(_ context.Context, in *userpb.TLUserUpdateBotData) error {
	s.updates = append(s.updates, in)
	return nil
}

// testFiles writes the files the commands read: a PNG picture, SVG and TGS
// icons and an MP4 animation of 320x240 and 2.5 s.
func testFiles(t *testing.T) (png, svg, tgs, mp4 string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(`{"v":"5.5.2","fr":60,"w":512,"h":512,"layers":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return write("app.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)),
		write("icon.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24H0z"/></svg>`)),
		write("icon.tgs", gz.Bytes()),
		write("anim.mp4", testMP4(0, 320, 240, 1000, 2500))
}

// box makes an ISO BMFF box.
func box(name string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], name)
	return append(b, body...)
}

// testMP4 makes the boxes mp4Video reads: an mvhd of the timescale and
// duration, a sound track of 0 x 0 and a video track of w x h, with boxes
// of the given version.
func testMP4(version byte, w, h, timescale, duration uint32) []byte {
	u32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	u64 := func(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
	var mvhd, tkhdTimes []byte
	if version == 1 {
		mvhd = bytes.Join([][]byte{{1, 0, 0, 0}, u64(0), u64(0), u32(timescale), u64(uint64(duration)), make([]byte, 80)}, nil)
		tkhdTimes = bytes.Join([][]byte{u64(0), u64(0), u32(1), u32(0), u64(uint64(duration))}, nil)
	} else {
		mvhd = bytes.Join([][]byte{{0, 0, 0, 0}, u32(0), u32(0), u32(timescale), u32(duration), make([]byte, 80)}, nil)
		tkhdTimes = bytes.Join([][]byte{u32(0), u32(0), u32(1), u32(0), u32(duration)}, nil)
	}
	tkhd := func(tw, th uint32) []byte {
		return box("tkhd", []byte{version, 0, 0, 3}, tkhdTimes, make([]byte, 8+2+2+2+2+36), u32(tw<<16), u32(th<<16))
	}
	return bytes.Join([][]byte{
		box("ftyp", []byte("isom"), u32(512), []byte("isomavc1")),
		box("moov",
			box("mvhd", mvhd),
			box("trak", tkhd(0, 0), box("mdia")),
			box("trak", tkhd(w, h), box("mdia")),
		),
		box("mdat", make([]byte, 32)),
	}, nil)
}

// Every command of the bot platform refuses the built-in inline bots with
// the error of token and set, before it writes or uploads anything.
func TestPlatformCommandsRefuseBuiltinBots(t *testing.T) {
	png, svg, _, _ := testFiles(t)
	ctx := context.Background()
	for _, b := range inlinebots.All() {
		u := b.Username
		commands := map[string]func(st *fakePlatform, svc *fakeBotServices) error{
			"set -domain": func(st *fakePlatform, svc *fakeBotServices) error {
				return setBot(ctx, st, svc, []string{u, "-domain", "example.com"}, io0())
			},
			"set -main-app": func(st *fakePlatform, svc *fakeBotServices) error {
				return setBot(ctx, st, svc, []string{u, "-main-app", "https://a.example/"}, io0())
			},
			"app list": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAppCommand(ctx, st, svc, []string{"list", u}, io0())
			},
			"app add": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAppCommand(ctx, st, svc, []string{"add", u, "shop", "-title", "Shop", "-url", "https://a.example/", "-photo", png}, io0())
			},
			"app edit": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAppCommand(ctx, st, svc, []string{"edit", u, "shop", "-title", "S"}, io0())
			},
			"app rm": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAppCommand(ctx, st, svc, []string{"rm", u, "shop"}, io0())
			},
			"attach show": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAttachCommand(ctx, st, svc, []string{"show", u}, io0())
			},
			"attach set": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAttachCommand(ctx, st, svc, []string{"set", u, "-name", "Shop", "-url", "https://a.example/", "-icon", "default_static=" + svg}, io0())
			},
			"attach rm": func(st *fakePlatform, svc *fakeBotServices) error {
				return botAttachCommand(ctx, st, svc, []string{"rm", u}, io0())
			},
			"game list": func(st *fakePlatform, svc *fakeBotServices) error {
				return botGameCommand(ctx, st, svc, []string{"list", u}, io0())
			},
			"game add": func(st *fakePlatform, svc *fakeBotServices) error {
				return botGameCommand(ctx, st, svc, []string{"add", u, "race", "-title", "Race", "-photo", png}, io0())
			},
			"game edit": func(st *fakePlatform, svc *fakeBotServices) error {
				return botGameCommand(ctx, st, svc, []string{"edit", u, "race", "-title", "R"}, io0())
			},
			"game rm": func(st *fakePlatform, svc *fakeBotServices) error {
				return botGameCommand(ctx, st, svc, []string{"rm", u, "race"}, io0())
			},
			"login show": func(st *fakePlatform, _ *fakeBotServices) error {
				return botLoginCommand(ctx, st, []string{"show", u}, time.Now, io0())
			},
			"login set": func(st *fakePlatform, _ *fakeBotServices) error {
				return botLoginCommand(ctx, st, []string{"set", u, "-redirect", "https://a.example/cb"}, time.Now, io0())
			},
			"login edit": func(st *fakePlatform, _ *fakeBotServices) error {
				return botLoginCommand(ctx, st, []string{"edit", u, "-origin", "off"}, time.Now, io0())
			},
			"login rm": func(st *fakePlatform, _ *fakeBotServices) error {
				return botLoginCommand(ctx, st, []string{"rm", u}, time.Now, io0())
			},
		}
		for name, run := range commands {
			st, svc := newFakePlatform(), &fakeBotServices{}
			if err := run(st, svc); !errors.Is(err, errBuiltinBot) {
				t.Errorf("@%s %s: %v, want the built-in bot refused", u, name, err)
			}
			if st.writes != 0 || len(svc.uploads) != 0 || len(svc.updates) != 0 {
				t.Errorf("@%s %s: %d writes, %d uploads, %d calls to biz", u, name, st.writes, len(svc.uploads), len(svc.updates))
			}
		}
	}
}

func io0() *bytes.Buffer { return &bytes.Buffer{} }

func TestSetBotDomainAndMainApp(t *testing.T) {
	ctx := context.Background()
	st, svc := newFakePlatform(), &fakeBotServices{}
	if err := setBot(ctx, st, svc, []string{"shopbot", "-domain", "Shop.Example.com"}, io0()); err != nil {
		t.Fatal(err)
	}
	if st.domain != "shop.example.com" || len(svc.updates) != 0 {
		t.Errorf("domain %q, %d calls to biz: the domain is not in the user object", st.domain, len(svc.updates))
	}
	if err := setBot(ctx, st, svc, []string{"shopbot", "-domain", "https://shop.example.com/"}, io0()); err == nil {
		t.Error("a domain with a scheme is taken")
	}
	if err := setBot(ctx, st, svc, []string{"shopbot", "-main-app", "https://app.example.com/", "-privacy", "off"}, io0()); err != nil {
		t.Fatal(err)
	}
	if st.mainApp != "https://app.example.com/" || len(svc.updates) != 1 ||
		!mtproto.FromBool(svc.updates[0].GetBotHasMainApp()) || !mtproto.FromBool(svc.updates[0].GetBotChatHistory()) {
		t.Errorf("main app %q, updates %v", st.mainApp, svc.updates)
	}
	if err := setBot(ctx, st, svc, []string{"shopbot", "-main-app", "off", "-domain", "off"}, io0()); err != nil {
		t.Fatal(err)
	}
	if st.mainApp != "" || st.domain != "" || len(svc.updates) != 2 || mtproto.FromBool(svc.updates[1].GetBotHasMainApp()) {
		t.Errorf("after off: main app %q, domain %q, updates %v", st.mainApp, st.domain, svc.updates)
	}
}

func TestBotApp(t *testing.T) {
	png, _, _, mp4 := testFiles(t)
	ctx := context.Background()
	st, svc := newFakePlatform(), &fakeBotServices{}
	var out bytes.Buffer
	err := botAppCommand(ctx, st, svc, []string{"add", "shopbot", "shop", "-title", "Shop", "-url", "https://shop.example.com/app",
		"-photo", png, "-animation", mp4, "-write-access", "on"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	a := st.apps["shop"]
	if a.BotId != testBotId || a.Title != "Shop" || a.URL != "https://shop.example.com/app" || !a.RequestWriteAccess ||
		a.PhotoId != 101 || a.DocumentId != 102 {
		t.Errorf("app %+v", a)
	}
	if len(svc.uploads) != 2 || !svc.uploads[0].photo || svc.uploads[1].mime != "video/mp4" {
		t.Fatalf("uploads %+v", svc.uploads)
	}
	attrs := map[string]*mtproto.DocumentAttribute{}
	for _, at := range svc.uploads[1].attributes {
		attrs[at.GetPredicateName()] = at
	}
	if v := attrs[mtproto.Predicate_documentAttributeVideo]; v.GetW() != 320 || v.GetH() != 240 || v.GetDuration() != 2.5 {
		t.Errorf("video attribute %v", v)
	}
	if attrs[mtproto.Predicate_documentAttributeAnimated] == nil {
		t.Error("no animated attribute")
	}
	if len(svc.updates) != 1 || svc.updates[0].GetBotId() != testBotId {
		t.Errorf("the cached bot is not refreshed: %v", svc.updates)
	}
	if !strings.Contains(out.String(), "https://pluma.chat/shopbot/shop") {
		t.Errorf("no link in %q", out.String())
	}

	// edit changes what is given and keeps the rest; -animation off drops
	// the animation.
	if err = botAppCommand(ctx, st, svc, []string{"edit", "shopbot", "shop", "-title", "Shop 2", "-animation", "off"}, io0()); err != nil {
		t.Fatal(err)
	}
	if a = st.apps["shop"]; a.Title != "Shop 2" || a.URL != "https://shop.example.com/app" || a.PhotoId != 101 || a.DocumentId != 0 || !a.RequestWriteAccess {
		t.Errorf("edited app %+v", a)
	}
	if len(svc.uploads) != 2 {
		t.Errorf("edit uploaded %d files", len(svc.uploads)-2)
	}
	if err = botAppCommand(ctx, st, svc, []string{"add", "shopbot", "other", "-title", "O", "-url", "https://shop.example.com/", "-photo", mp4}, io0()); err == nil {
		t.Error("an MP4 is taken for a photo")
	}
	if err = botAppCommand(ctx, st, svc, []string{"edit", "shopbot", "missing", "-title", "M"}, io0()); err == nil {
		t.Error("an app that is not there is edited")
	}
	if err = botAppCommand(ctx, st, svc, []string{"rm", "shopbot", "shop"}, io0()); err != nil || len(st.apps) != 0 {
		t.Errorf("rm: %v, %d apps", err, len(st.apps))
	}
}

func TestBotGame(t *testing.T) {
	png, _, _, _ := testFiles(t)
	ctx := context.Background()
	st, svc := newFakePlatform(), &fakeBotServices{}
	var out bytes.Buffer
	if err := botGameCommand(ctx, st, svc, []string{"add", "shopbot", "race", "-title", "Race", "-description", "Fast", "-photo", png}, &out); err != nil {
		t.Fatal(err)
	}
	if g := st.games["race"]; g.Title != "Race" || g.Description != "Fast" || g.PhotoId != 101 || g.BotId != testBotId {
		t.Errorf("game %+v", g)
	}
	if len(svc.updates) != 0 {
		t.Error("a game refreshed the cached bot: it is not part of the bot's info")
	}
	if !strings.Contains(out.String(), "https://pluma.chat/shopbot?game=race") {
		t.Errorf("no link in %q", out.String())
	}
	if err := botGameCommand(ctx, st, svc, []string{"add", "shopbot", "race2", "-title", "Race"}, io0()); !errors.Is(err, errUsage) {
		t.Errorf("a game without a photo: %v", err)
	}
}

func TestBotAttach(t *testing.T) {
	_, svg, tgs, _ := testFiles(t)
	ctx := context.Background()
	st, svc := newFakePlatform(), &fakeBotServices{}
	if err := botAttachCommand(ctx, st, svc, []string{"set", "shopbot", "-icon", "default_static=" + svg}, io0()); err == nil {
		t.Error("a new entry without -name and -url is made")
	}
	err := botAttachCommand(ctx, st, svc, []string{"set", "shopbot", "-name", "Shop", "-url", "https://shop.example.com/menu",
		"-peers", "pm,chat", "-icon", "default_static=" + svg, "-icon", "android_animated=" + tgs,
		"-color", "android_animated:light_icon=#1a2b3c"}, io0())
	if err != nil {
		t.Fatal(err)
	}
	m := st.menu
	if m == nil || m.ShortName != "Shop" || !m.ShowInAttachMenu || m.ShowInSideMenu ||
		m.PeerTypes != botstore.AttachPeerPM|botstore.AttachPeerChat || len(m.Icons) != 2 {
		t.Fatalf("entry %+v", m)
	}
	if m.Icons[1].Name != "android_animated" || len(m.Icons[1].Colors) != 1 || m.Icons[1].Colors[0].Color != 0x1a2b3c {
		t.Errorf("icon %+v", m.Icons[1])
	}
	if svc.uploads[0].mime != svgMimeType || svc.uploads[0].name != "icon.svg" ||
		svc.uploads[1].mime != tgsMimeType || svc.uploads[1].name != "icon.tgs" {
		t.Errorf("uploads %+v", svc.uploads)
	}
	if len(svc.updates) != 1 || !mtproto.FromBool(svc.updates[0].GetBotAttachMenu()) {
		t.Errorf("bot_attach_menu is not set: %v", svc.updates)
	}

	// A second set changes only what it names.
	if err = botAttachCommand(ctx, st, svc, []string{"set", "shopbot", "-side-menu", "on", "-no-icon", "android_animated"}, io0()); err != nil {
		t.Fatal(err)
	}
	if m = st.menu; m.URL != "https://shop.example.com/menu" || !m.ShowInSideMenu || len(m.Icons) != 1 || len(svc.uploads) != 2 {
		t.Errorf("entry after the second set %+v, %d uploads", m, len(svc.uploads))
	}
	if err = botAttachCommand(ctx, st, svc, []string{"set", "shopbot", "-icon", "default_static=" + tgs}, io0()); err == nil {
		t.Error("a TGS is taken for a static icon")
	}
	if err = botAttachCommand(ctx, st, svc, []string{"set", "shopbot", "-icon", "ios_animated=" + svg}, io0()); err == nil {
		t.Error("an SVG is taken for an animated icon")
	}
	if err = botAttachCommand(ctx, st, svc, []string{"rm", "shopbot"}, io0()); err != nil || st.menu != nil {
		t.Fatalf("rm: %v", err)
	}
	if last := svc.updates[len(svc.updates)-1]; last.GetBotAttachMenu() == nil || mtproto.FromBool(last.GetBotAttachMenu()) {
		t.Errorf("bot_attach_menu is not cleared: %v", last)
	}
}

func TestBotLogin(t *testing.T) {
	ctx := context.Background()
	st := newFakePlatform()
	now := func() time.Time { return time.Date(2026, 11, 16, 12, 30, 45, 0, time.UTC) }
	var out bytes.Buffer
	err := botLoginCommand(ctx, st, []string{"set", "shopbot", "-redirect", "https://shop.example.com/cb", "-origin", "https://shop.example.com"}, now, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "client_secret: "+st.secret+"\n") || !strings.Contains(out.String(), "client_id: 136907800\n") {
		t.Errorf("output %q", out.String())
	}
	if st.client.CreatedAt != int32(now().Unix()) {
		t.Errorf("created at %d", st.client.CreatedAt)
	}
	secret := st.secret
	out.Reset()
	if err = botLoginCommand(ctx, st, []string{"edit", "shopbot", "-origin", "off"}, now, &out); err != nil {
		t.Fatal(err)
	}
	if len(st.client.Origins) != 0 || len(st.client.RedirectURIs) != 1 || st.secret != secret || strings.Contains(out.String(), secret) {
		t.Errorf("edit: %+v, output %q", st.client, out.String())
	}
	if err = botLoginCommand(ctx, st, []string{"set", "shopbot"}, now, io0()); !errors.Is(err, errUsage) {
		t.Errorf("set without a redirect address: %v", err)
	}
	if err = botLoginCommand(ctx, st, []string{"rm", "shopbot"}, now, io0()); err != nil || st.client != nil {
		t.Errorf("rm: %v", err)
	}
}

func TestMP4Video(t *testing.T) {
	for _, version := range []byte{0, 1} {
		w, h, s, err := mp4Video(testMP4(version, 640, 360, 600, 1500))
		if err != nil || w != 640 || h != 360 || s != 2.5 {
			t.Errorf("version %d: %dx%d %.2f s, %v", version, w, h, s, err)
		}
	}
	if _, _, _, err := mp4Video([]byte("not a video at all")); err == nil {
		t.Error("garbage is a video")
	}
	broken := testMP4(0, 640, 360, 600, 1500)
	binary.BigEndian.PutUint32(broken[24:], 1<<30) // the size of moov past the end
	if _, _, _, err := mp4Video(broken); err == nil {
		t.Error("a box past the end of the file is read")
	}
}

func TestParseAttachSettings(t *testing.T) {
	if v, err := parseAttachPeers("all"); err != nil || v != botstore.AttachPeerAll {
		t.Errorf("all: %d, %v", v, err)
	}
	if v, err := parseAttachPeers("same_bot,broadcast"); err != nil || v != botstore.AttachPeerSameBotPM|botstore.AttachPeerBroadcast {
		t.Errorf("list: %d, %v", v, err)
	}
	if _, err := parseAttachPeers("pm,group"); err == nil {
		t.Error("an unknown peer type is taken")
	}
	if v, err := parseRGB("#ff8000"); err != nil || v != 0xff8000 {
		t.Errorf("color: %x, %v", v, err)
	}
	for _, bad := range []string{"ff8000", "#ff800", "#gg8000"} {
		if _, err := parseRGB(bad); err == nil {
			t.Errorf("color %q is taken", bad)
		}
	}
}

func TestPrintWebAppKey(t *testing.T) {
	seed := strings.Repeat("ab", 32)
	raw, _ := hex.DecodeString(seed)
	want := hex.EncodeToString(ed25519.NewKeyFromSeed(raw).Public().(ed25519.PublicKey))
	var out bytes.Buffer
	if err := printWebAppKey(webappdata.Config{SigningKey: seed}, &out); err != nil || out.String() != want+"\n" {
		t.Errorf("key %q, %v; want %s", out.String(), err, want)
	}
	if err := printWebAppKey(webappdata.Config{}, io0()); err == nil {
		t.Error("no key set, and nothing is said")
	}
	if err := printWebAppKey(webappdata.Config{SigningKey: "abc"}, io0()); err == nil {
		t.Error("a bad key is taken")
	}
}
