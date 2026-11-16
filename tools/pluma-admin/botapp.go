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
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// bot app: the direct link mini apps of a bot (task 106, ADR 0068, p. 5),
// opened by pluma.chat/<bot>/<short name> through messages.getBotApp and
// messages.requestAppWebView. botApp.photo is a Photo the clients draw, not
// optional (TL botApp), and botApp.document an animation that may stand in
// for it.

// Limits of the pictures of apps and games. Ours: Telegram documents none
// for them.
const (
	maxPictureBytes   = 10 << 20
	maxAnimationBytes = 10 << 20
)

// botApps is what bot app changes: pkg/botstore.
type botApps interface {
	botLookup
	AddApp(ctx context.Context, a botstore.App) (botstore.App, error)
	EditApp(ctx context.Context, a botstore.App) (botstore.App, error)
	DeleteApp(ctx context.Context, botId int64, shortName string) error
	Apps(ctx context.Context, botId int64) ([]botstore.App, error)
	AppByShortName(ctx context.Context, botId int64, shortName string) (*botstore.App, error)
	MainApp(ctx context.Context, botId int64) (string, error)
}

// botAppCommand: app list|add|edit|rm <bot> [<short name> flags...].
func botAppCommand(ctx context.Context, store botApps, services botServices, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return errUsage
		}
		id, err := editableBot(ctx, store, args[1])
		if err != nil {
			return err
		}
		return listApps(ctx, store, id, args[1], stdout)
	case "add", "edit", "rm":
	default:
		return errUsage
	}
	if len(args) < 3 {
		return errUsage
	}
	username, shortName := args[1], args[2]
	if args[0] == "rm" {
		if len(args) != 3 {
			return errUsage
		}
		id, err := editableBot(ctx, store, username)
		if err != nil {
			return err
		}
		if err = store.DeleteApp(ctx, id, shortName); err != nil {
			return describeAppError(err)
		}
		if err = refreshBot(ctx, services, id); err != nil {
			return fmt.Errorf("app %s is removed, but %w", shortName, err)
		}
		fmt.Fprintf(stdout, "bot %d: app %s removed.\n", id, shortName)
		return nil
	}

	flags := flag.NewFlagSet("bot app", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	title := flags.String("title", "", "the title of the app, 1 to 64 characters")
	description := flags.String("description", "", "the description of the app, up to 512 characters")
	url := flags.String("url", "", "the https address of the app")
	photo := flags.String("photo", "", "a JPEG or PNG picture of the app")
	animation := flags.String("animation", "", "an MP4 animation shown in place of the picture; off removes it (edit)")
	writeAccess := flags.String("write-access", "", "on: opening the app asks the user to let the bot write to him")
	if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	given := givenFlags(flags)
	id, err := editableBot(ctx, store, username)
	if err != nil {
		return err
	}
	var app botstore.App
	if args[0] == "add" {
		if *title == "" || *url == "" || *photo == "" || *animation == settingOff {
			return errUsage
		}
		app = botstore.App{BotId: id, ShortName: shortName}
	} else {
		if len(given) == 0 {
			return errUsage
		}
		old, err := store.AppByShortName(ctx, id, shortName)
		if err != nil {
			return err
		}
		if old == nil {
			return describeAppError(botstore.ErrNoApp)
		}
		app = *old
	}
	if given["title"] {
		app.Title = *title
	}
	if given["description"] {
		app.Description = *description
	}
	if given["url"] {
		app.URL = *url
	}
	if given["write-access"] {
		if app.RequestWriteAccess, err = onOff(*writeAccess); err != nil {
			return err
		}
	}
	// The files are checked before anything is uploaded, and uploaded
	// before the app is written: a failed upload leaves the app as it was.
	var photoData, animationData []byte
	if given["photo"] {
		if photoData, err = readPicture(*photo); err != nil {
			return err
		}
	}
	var animationAttributes []*mtproto.DocumentAttribute
	if given["animation"] && *animation != settingOff {
		if animationData, animationAttributes, err = readAnimation(*animation); err != nil {
			return err
		}
	}
	if photoData != nil {
		if app.PhotoId, err = services.UploadPhoto(ctx, photoData, "app.jpg"); err != nil {
			return err
		}
	}
	switch {
	case animationData != nil:
		if app.DocumentId, err = services.UploadDocument(ctx, animationData, animationFileName, "video/mp4", animationAttributes); err != nil {
			return err
		}
	case given["animation"]:
		app.DocumentId = 0
	}
	var saved botstore.App
	if args[0] == "add" {
		saved, err = store.AddApp(ctx, app)
	} else {
		saved, err = store.EditApp(ctx, app)
	}
	if err != nil {
		return describeAppError(err)
	}
	if err = refreshBot(ctx, services, id); err != nil {
		return fmt.Errorf("app %s is saved, but %w", shortName, err)
	}
	fmt.Fprintf(stdout, "bot %d: app %s, version %d: %s\n", id, saved.ShortName, saved.Version, appLink(username, saved.ShortName))
	return nil
}

// refreshBot raises the bot_info_version of the bot through biz, which
// drops the cached user: the changes of the platform raised it in the
// database (botstore.bumpPlatform), and the clients learn of it from the
// user object.
func refreshBot(ctx context.Context, services botServices, botId int64) error {
	if err := services.UpdateBotData(ctx, &userpb.TLUserUpdateBotData{BotId: botId}); err != nil {
		// Running add or rm again would fail on what is already done; an
		// edit of any app of the bot refreshes it as well.
		return fmt.Errorf("the cached bot is not refreshed (bot app edit of any of its apps refreshes it): %w", err)
	}
	return nil
}

// appLink is the direct link of an app: t.me/<bot>/<short name>
// (core.telegram.org/api/links, "Direct mini app links") on Pluma's host
// (ADR 0036).
func appLink(username, shortName string) string {
	return "https://pluma.chat/" + username + "/" + shortName
}

func listApps(ctx context.Context, store botApps, botId int64, username string, stdout io.Writer) error {
	mainURL, err := store.MainApp(ctx, botId)
	if err != nil {
		return err
	}
	apps, err := store.Apps(ctx, botId)
	if err != nil {
		return err
	}
	if mainURL != "" {
		fmt.Fprintf(stdout, "main app: %s (https://pluma.chat/%s?startapp)\n", mainURL, username)
	} else {
		fmt.Fprintln(stdout, "main app: none")
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SHORT NAME\tTITLE\tURL\tWRITE ACCESS\tANIMATION\tVERSION\tLINK")
	for _, a := range apps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", a.ShortName, a.Title, a.URL, yesNo(a.RequestWriteAccess), yesNo(a.DocumentId != 0), a.Version, appLink(username, a.ShortName))
	}
	return w.Flush()
}

func describeAppError(err error) error {
	switch {
	case errors.Is(err, botstore.ErrShortNameInvalid):
		return errors.New("the short name must be 1 to 64 letters, digits and _")
	case errors.Is(err, botstore.ErrShortNameTaken):
		return errors.New("the bot already has one of this short name")
	case errors.Is(err, botstore.ErrNoApp):
		return errors.New("the bot has no app of this short name")
	case errors.Is(err, botstore.ErrNoBot):
		return errors.New("no such bot")
	}
	return err
}

// readPicture reads a JPEG or PNG picture, as media makes photos of.
func readPicture(path string) ([]byte, error) {
	data, err := readFile(path, maxPictureBytes, "picture")
	if err != nil {
		return nil, err
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png":
		return data, nil
	}
	return nil, fmt.Errorf("picture %s is not a JPEG or PNG image", path)
}

// animationFileName names the document of an animation.
const animationFileName = "animation.mp4"

// readAnimation reads an MP4 animation and returns the attributes a client
// sends with one: a video of its size and length, an animation, the name
// (constructor/documentAttributeVideo, documentAttributeAnimated).
func readAnimation(path string) ([]byte, []*mtproto.DocumentAttribute, error) {
	data, err := readFile(path, maxAnimationBytes, "animation")
	if err != nil {
		return nil, nil, err
	}
	w, h, seconds, err := mp4Video(data)
	if err != nil {
		return nil, nil, fmt.Errorf("animation %s: %w", path, err)
	}
	return data, []*mtproto.DocumentAttribute{
		mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{
			SupportsStreaming: true,
			Duration:          seconds, Duration_FLOAT64: seconds, Duration_INT32: int32(seconds),
			W: w, H: h,
		}).To_DocumentAttribute(),
		mtproto.MakeTLDocumentAttributeAnimated(&mtproto.DocumentAttribute{}).To_DocumentAttribute(),
		mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: animationFileName}).To_DocumentAttribute(),
	}, nil
}

var errNotMP4 = errors.New("not an MP4 file with a video track")

// mp4Video returns the size of the first track of an MP4 file that has a
// picture and the length of the file, from the boxes of ISO/IEC 14496-12:
// moov/trak/tkhd (width and height, 16.16 fixed point, after the matrix)
// and moov/mvhd (duration over timescale). Sound tracks have a tkhd of size
// 0 x 0.
func mp4Video(data []byte) (w, h int32, seconds float64, err error) {
	moov, ok := mp4Box(data, "moov")
	if !ok {
		return 0, 0, 0, errNotMP4
	}
	if mvhd, ok := mp4Box(moov, "mvhd"); ok && len(mvhd) >= 4 {
		var timescale uint32
		var duration uint64
		switch {
		case mvhd[0] == 1 && len(mvhd) >= 32:
			timescale, duration = binary.BigEndian.Uint32(mvhd[20:24]), binary.BigEndian.Uint64(mvhd[24:32])
		case mvhd[0] == 0 && len(mvhd) >= 20:
			timescale, duration = binary.BigEndian.Uint32(mvhd[12:16]), uint64(binary.BigEndian.Uint32(mvhd[16:20]))
		}
		if timescale != 0 && duration != ^uint64(0) && duration != uint64(^uint32(0)) {
			seconds = float64(duration) / float64(timescale)
		}
	}
	for rest := moov; len(rest) > 0; {
		name, body, next, ok := mp4Next(rest)
		if !ok {
			break
		}
		rest = next
		if name != "trak" {
			continue
		}
		tkhd, ok := mp4Box(body, "tkhd")
		if !ok || len(tkhd) < 4 {
			continue
		}
		at := 76 // version 0: 4 + 20 bytes of times, id and duration + 52
		if tkhd[0] == 1 {
			at = 88
		}
		if len(tkhd) < at+8 {
			continue
		}
		tw, th := int32(binary.BigEndian.Uint32(tkhd[at:])>>16), int32(binary.BigEndian.Uint32(tkhd[at+4:])>>16)
		if tw > 0 && th > 0 {
			return tw, th, seconds, nil
		}
	}
	return 0, 0, 0, errNotMP4
}

// mp4Box returns the body of the first box of the type among the boxes of
// data.
func mp4Box(data []byte, want string) ([]byte, bool) {
	for len(data) > 0 {
		name, body, next, ok := mp4Next(data)
		if !ok {
			return nil, false
		}
		if name == want {
			return body, true
		}
		data = next
	}
	return nil, false
}

// mp4Next splits the first box off data: its type, its body and the boxes
// after it. A size of 1 is followed by a 64-bit size, 0 runs to the end.
func mp4Next(data []byte) (name string, body, next []byte, ok bool) {
	if len(data) < 8 {
		return "", nil, nil, false
	}
	size, header := uint64(binary.BigEndian.Uint32(data)), uint64(8)
	switch size {
	case 0:
		size = uint64(len(data))
	case 1:
		if len(data) < 16 {
			return "", nil, nil, false
		}
		size, header = binary.BigEndian.Uint64(data[8:16]), 16
	}
	if size < header || size > uint64(len(data)) {
		return "", nil, nil, false
	}
	return string(data[4:8]), data[header:size], data[size:], true
}
