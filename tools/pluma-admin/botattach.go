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
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// bot attach: the attachment menu entry of a bot (task 106, ADR 0068,
// p. 5). Telegram gives it to chosen bots only, and so does the
// administrator here; users add the bot with a startattach link or from
// the bot's profile (messages.toggleBotInAttachMenu).
//
// The icons are documents: the *_static ones SVG, the *_animated ones
// Lottie animations in TGS. tdesktop draws default_static with QSvgRenderer
// (inline_bots/bot_attach_web_view.cpp:178, 916); Android draws a document
// as SVG when its name ends in .svg and as Lottie when its type is
// application/x-tgsticker (messenger/ImageLoader.java:3216-3223), and takes
// android_animated for the animated tab icon
// (ui/Components/glass/GlassTabView.java:302).

// Limits of an icon file and of the animation a TGS file unpacks to. Ours:
// Telegram documents none for these icons.
const (
	maxIconBytes     = 64 << 10
	maxLottieUnpack  = 4 << 20
	svgIconFileName  = "icon.svg"
	tgsIconFileName  = "icon.tgs"
	svgMimeType      = "image/svg+xml"
	tgsMimeType      = "application/x-tgsticker"
	staticIconSuffix = "_static"
	animIconSuffix   = "_animated"
)

// attachPeerName is a name of -peers and its bit.
type attachPeerName struct {
	name string
	bit  int32
}

// attachPeerNames are the names -peers takes, one per bit of
// botstore.AttachPeer* (TL AttachMenuPeerType).
var attachPeerNames = []attachPeerName{
	{"same_bot", botstore.AttachPeerSameBotPM},
	{"bot", botstore.AttachPeerBotPM},
	{"pm", botstore.AttachPeerPM},
	{"chat", botstore.AttachPeerChat},
	{"broadcast", botstore.AttachPeerBroadcast},
}

// botAttachMenus is what bot attach changes: pkg/botstore.
type botAttachMenus interface {
	botLookup
	SetAttachMenu(ctx context.Context, m botstore.AttachMenu) ([]int64, error)
	DeleteAttachMenu(ctx context.Context, botId int64) ([]int64, error)
	AttachMenu(ctx context.Context, botId int64) (*botstore.AttachMenu, error)
}

// botAttachCommand: attach show|set|rm <bot> [flags...].
func botAttachCommand(ctx context.Context, store botAttachMenus, services botServices, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	switch args[0] {
	case "show", "rm":
		if len(args) != 2 {
			return errUsage
		}
	case "set":
	default:
		return errUsage
	}
	if args[0] == "set" {
		return setAttachMenu(ctx, store, services, args[1], args[2:], stdout)
	}
	id, err := editableBot(ctx, store, args[1])
	if err != nil {
		return err
	}
	if args[0] == "show" {
		m, err := store.AttachMenu(ctx, id)
		if err != nil {
			return err
		}
		if m == nil {
			fmt.Fprintf(stdout, "bot %d has no attachment menu entry.\n", id)
			return nil
		}
		printAttachMenu(stdout, id, args[1], m)
		return nil
	}
	// The flag goes first: a failure after it leaves an entry nobody is
	// offered to add, and running rm again finishes the work; the other
	// order could leave the clients offering an entry that is gone, with
	// nothing left for rm to do.
	if err = services.UpdateBotData(ctx, &userpb.TLUserUpdateBotData{BotId: id, BotAttachMenu: mtproto.BoolFalse}); err != nil {
		return err
	}
	installed, err := store.DeleteAttachMenu(ctx, id)
	if errors.Is(err, botstore.ErrNoAttachMenu) {
		return errors.New("the bot has no attachment menu entry")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bot %d: attachment menu entry removed, from the menus of %d users as well.\n", id, len(installed))
	return nil
}

func setAttachMenu(ctx context.Context, store botAttachMenus, services botServices, username string, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("bot attach set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "the name of the entry in the menu (attachMenuBot.short_name)")
	url := flags.String("url", "", "the https address of the mini app the entry opens")
	attachMenu := flags.String("attach-menu", "", "on: the entry is in the attachment menu (on for a new entry)")
	sideMenu := flags.String("side-menu", "", "on: the entry is in the side menu (off for a new entry)")
	peers := flags.String("peers", "", "the chats the entry is offered in: all or a list of same_bot,bot,pm,chat,broadcast (all for a new entry)")
	writeAccess := flags.String("write-access", "", "on: adding the bot asks the user to let it write to him")
	settings := flags.String("settings", "", "on: the app has a settings button")
	var icons, noIcons, colors listFlag
	flags.Var(&icons, "icon", "<icon name>=<file>: an SVG for *_static, a TGS for *_animated; repeatable")
	flags.Var(&noIcons, "no-icon", "<icon name>: removes the icon; repeatable")
	flags.Var(&colors, "color", "<icon name>:<color name>=#rrggbb, or =off to remove; repeatable")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	given := givenFlags(flags)
	if len(given) == 0 {
		return errUsage
	}
	id, err := editableBot(ctx, store, username)
	if err != nil {
		return err
	}
	old, err := store.AttachMenu(ctx, id)
	if err != nil {
		return err
	}
	m := botstore.AttachMenu{BotId: id, ShowInAttachMenu: true, PeerTypes: botstore.AttachPeerAll}
	if old != nil {
		m = *old
		m.Icons = slices.Clone(old.Icons)
	} else if *name == "" || *url == "" {
		return errors.New("a new entry needs -name and -url")
	}
	if given["name"] {
		m.ShortName = *name
	}
	if given["url"] {
		m.URL = *url
	}
	for _, sw := range []struct {
		flag, value string
		field       *bool
	}{
		{"attach-menu", *attachMenu, &m.ShowInAttachMenu},
		{"side-menu", *sideMenu, &m.ShowInSideMenu},
		{"write-access", *writeAccess, &m.RequestWriteAccess},
		{"settings", *settings, &m.HasSettings},
	} {
		if given[sw.flag] {
			if *sw.field, err = onOff(sw.value); err != nil {
				return err
			}
		}
	}
	if given["peers"] {
		if m.PeerTypes, err = parseAttachPeers(*peers); err != nil {
			return err
		}
	}
	for _, n := range noIcons {
		i := slices.IndexFunc(m.Icons, func(icon botstore.AttachIcon) bool { return icon.Name == n })
		if i < 0 {
			return fmt.Errorf("the entry has no icon %s", n)
		}
		m.Icons = slices.Delete(m.Icons, i, i+1)
	}
	// Every file is read and checked before the first upload.
	type upload struct {
		name string
		data []byte
	}
	var uploads []upload
	for _, v := range icons {
		iconName, path, ok := strings.Cut(v, "=")
		if !ok || iconName == "" || path == "" {
			return errUsage
		}
		data, err := readIcon(iconName, path)
		if err != nil {
			return err
		}
		uploads = append(uploads, upload{iconName, data})
	}
	for _, u := range uploads {
		docId, err := uploadIcon(ctx, services, u.name, u.data)
		if err != nil {
			return fmt.Errorf("icon %s: %w", u.name, err)
		}
		if i := slices.IndexFunc(m.Icons, func(icon botstore.AttachIcon) bool { return icon.Name == u.name }); i >= 0 {
			m.Icons[i].DocumentId = docId // its colors stay
		} else {
			m.Icons = append(m.Icons, botstore.AttachIcon{Name: u.name, DocumentId: docId})
		}
	}
	for _, v := range colors {
		if err := setIconColor(&m, v); err != nil {
			return err
		}
	}
	installed, err := store.SetAttachMenu(ctx, m)
	switch {
	case errors.Is(err, botstore.ErrShortNameInvalid):
		return errors.New("the name must be 1 to 64 letters, digits and _")
	case err != nil:
		return err
	}
	// bot_attach_menu tells the clients the bot has an entry to add, for
	// the side menu as well: Android asks for the entry on a link of a bot
	// with the flag and offers to add it when it is in either menu
	// (ui/LaunchActivity.java:4474-4482).
	if err = services.UpdateBotData(ctx, &userpb.TLUserUpdateBotData{BotId: id, BotAttachMenu: mtproto.BoolTrue}); err != nil {
		return fmt.Errorf("the entry is saved, but the cached bot is not refreshed (run the command again): %w", err)
	}
	fmt.Fprintf(stdout, "bot %d: attachment menu entry saved; %d users have it in their menus.\n", id, len(installed))
	fmt.Fprintf(stdout, "Adding link: https://pluma.chat/%s?startattach\n", username)
	return nil
}

// parseAttachPeers reads -peers: all, or names of attachPeerNames.
func parseAttachPeers(v string) (int32, error) {
	if v == "all" {
		return botstore.AttachPeerAll, nil
	}
	var bits int32
	for _, n := range strings.Split(v, ",") {
		i := slices.IndexFunc(attachPeerNames, func(p attachPeerName) bool { return p.name == strings.TrimSpace(n) })
		if i < 0 {
			return 0, fmt.Errorf("peer type %q: all, or a list of same_bot, bot, pm, chat, broadcast", n)
		}
		bits |= attachPeerNames[i].bit
	}
	return bits, nil
}

// peerNames prints the bits of peer types.
func peerNames(bits int32) string {
	var names []string
	for _, p := range attachPeerNames {
		if bits&p.bit != 0 {
			names = append(names, p.name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ",")
}

// setIconColor applies one -color: <icon>:<color>=#rrggbb or =off.
func setIconColor(m *botstore.AttachMenu, v string) error {
	target, value, ok := strings.Cut(v, "=")
	if !ok {
		return errUsage
	}
	iconName, colorName, ok := strings.Cut(target, ":")
	if !ok || iconName == "" || colorName == "" {
		return errUsage
	}
	i := slices.IndexFunc(m.Icons, func(icon botstore.AttachIcon) bool { return icon.Name == iconName })
	if i < 0 {
		return fmt.Errorf("the entry has no icon %s to color", iconName)
	}
	icon := &m.Icons[i]
	j := slices.IndexFunc(icon.Colors, func(c botstore.AttachIconColor) bool { return c.Name == colorName })
	if value == settingOff {
		if j < 0 {
			return fmt.Errorf("icon %s has no color %s", iconName, colorName)
		}
		icon.Colors = slices.Delete(slices.Clone(icon.Colors), j, j+1)
		return nil
	}
	rgb, err := parseRGB(value)
	if err != nil {
		return err
	}
	if j >= 0 {
		icon.Colors = slices.Clone(icon.Colors)
		icon.Colors[j].Color = rgb
	} else {
		icon.Colors = append(slices.Clone(icon.Colors), botstore.AttachIconColor{Name: colorName, Color: rgb})
	}
	return nil
}

// parseRGB reads #rrggbb into the int of attachMenuBotIconColor.color.
func parseRGB(v string) (int32, error) {
	if len(v) != 7 || v[0] != '#' {
		return 0, fmt.Errorf("color %q: #rrggbb", v)
	}
	n, err := strconv.ParseUint(v[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("color %q: #rrggbb", v)
	}
	return int32(n), nil
}

// readIcon reads the file of an icon and checks it is of the kind its name
// asks for.
func readIcon(name, path string) ([]byte, error) {
	data, err := readFile(path, maxIconBytes, "icon")
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasSuffix(name, staticIconSuffix):
		if !isSVG(data) {
			return nil, fmt.Errorf("icon %s: %s is not an SVG image", name, path)
		}
	case strings.HasSuffix(name, animIconSuffix):
		if !isTGS(data) {
			return nil, fmt.Errorf("icon %s: %s is not a TGS animation (gzip of Lottie JSON)", name, path)
		}
	default:
		return nil, fmt.Errorf("icon %s: the names end in %s or %s (docs/ops/bots.md)", name, staticIconSuffix, animIconSuffix)
	}
	return data, nil
}

func uploadIcon(ctx context.Context, services botServices, name string, data []byte) (int64, error) {
	fileName, mimeType := svgIconFileName, svgMimeType
	if strings.HasSuffix(name, animIconSuffix) {
		fileName, mimeType = tgsIconFileName, tgsMimeType
	}
	return services.UploadDocument(ctx, data, fileName, mimeType, []*mtproto.DocumentAttribute{
		mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: fileName}).To_DocumentAttribute(),
	})
}

// isSVG tells UTF-8 XML whose root element is svg.
func isSVG(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local == "svg"
		}
	}
}

// isTGS tells a gzip stream of a JSON object (a Lottie animation) that
// unpacks to at most maxLottieUnpack bytes.
func isTGS(data []byte) bool {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return false
	}
	defer r.Close()
	unpacked, err := io.ReadAll(io.LimitReader(r, maxLottieUnpack+1))
	if err != nil || len(unpacked) > maxLottieUnpack {
		return false
	}
	var anim map[string]json.RawMessage
	return json.Unmarshal(unpacked, &anim) == nil && anim["v"] != nil
}

func printAttachMenu(stdout io.Writer, id int64, username string, m *botstore.AttachMenu) {
	fmt.Fprintf(stdout, "bot %d: %s\n", id, m.ShortName)
	fmt.Fprintf(stdout, "  url: %s\n", m.URL)
	fmt.Fprintf(stdout, "  attachment menu: %s, side menu: %s, peers: %s\n", yesNo(m.ShowInAttachMenu), yesNo(m.ShowInSideMenu), peerNames(m.PeerTypes))
	fmt.Fprintf(stdout, "  write access: %s, settings: %s\n", yesNo(m.RequestWriteAccess), yesNo(m.HasSettings))
	for _, icon := range m.Icons {
		var cs []string
		for _, c := range icon.Colors {
			cs = append(cs, fmt.Sprintf("%s=#%06x", c.Name, uint32(c.Color)&0xffffff))
		}
		fmt.Fprintf(stdout, "  icon %s: document %d %s\n", icon.Name, icon.DocumentId, strings.Join(cs, " "))
	}
	fmt.Fprintf(stdout, "  adding link: https://pluma.chat/%s?startattach\n", username)
}
