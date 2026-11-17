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
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// The photos of the built-in inline bots (task 111): a magnifier over "GIF"
// for @gif and over a picture for @pic, in the colours of the service
// account's photo. The .svg files next to them are their sources. They are
// JPEGs, as servicePhoto and as every client uploads a profile photo: dfs
// encodes the photo's sizes in the format of the uploaded file's name.
//
//go:embed inline-gif-photo.jpg
var inlineGifPhoto []byte

//go:embed inline-pic-photo.jpg
var inlinePicPhoto []byte

// inlineBotPhoto returns the photo of a bot of the registry and the name of
// its file.
func inlineBotPhoto(b inlinebots.Bot) ([]byte, string, bool) {
	switch b.Id {
	case inlinebots.Gif.Id:
		return inlineGifPhoto, "gif.jpg", true
	case inlinebots.Pic.Id:
		return inlinePicPhoto, "pic.jpg", true
	}
	return nil, "", false
}

// inlinePhotoServices are the services of service-photo and the account
// read from biz, to see whether it has a photo.
type inlinePhotoServices interface {
	photoServices
	Account(ctx context.Context, id int64) (*mtproto.ImmutableUser, error)
}

func (s *rpcPhotoServices) Account(ctx context.Context, id int64) (*mtproto.ImmutableUser, error) {
	return s.user.UserGetImmutableUser(ctx, &userpb.TLUserGetImmutableUser{Id: id})
}

// inlineBotsCommand: inline-bots photos [-replace] gives the built-in inline
// bots their photos, the way service-photo does for the service account:
// the photo becomes the main one, biz drops the cached user and sends
// updateUser to everyone who has the bot among their chats, and the clients
// load the bot again, its name with it. A bot that has a photo keeps it, so
// the command can run on every deploy; -replace sets the built-in photo over
// it, the old one staying in the bot's album. It talks to dfs, media and
// biz, so it runs in the bff container.
func inlineBotsCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 || args[1] != "photos" {
		return errUsage
	}
	flags := flag.NewFlagSet("inline-bots photos", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	replace := flags.Bool("replace", false, "set the built-in photo over the bot's own")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	services, err := newPhotoServices(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return setInlineBotPhotos(ctx, services, *replace, stdout)
}

// setInlineBotPhotos sets the photo of every bot of the registry that has
// none, or of every bot with replace. A bot that fails does not stop the
// others; the errors of all are returned.
func setInlineBotPhotos(ctx context.Context, s inlinePhotoServices, replace bool, stdout io.Writer) error {
	var errs []error
	for _, b := range inlinebots.All() {
		image, name, ok := inlineBotPhoto(b)
		if !ok {
			errs = append(errs, fmt.Errorf("@%s: no built-in photo", b.Username))
			continue
		}
		u, err := s.Account(ctx, b.Id)
		if err != nil {
			errs = append(errs, fmt.Errorf("@%s: biz: %w", b.Username, err))
			continue
		}
		switch acc := u.GetUser(); {
		case acc == nil || acc.GetBot() == nil || acc.GetDeleted():
			// The account is not made (its username was taken) or was
			// changed by hand: docs/ops/inline-bots.md, «Если имя бота занято».
			errs = append(errs, fmt.Errorf("@%s: account %d is not a bot in order; see docs/ops/inline-bots.md", b.Username, b.Id))
			continue
		case acc.GetProfilePhoto().GetId() != 0 && !replace:
			fmt.Fprintf(stdout, "@%s (%d) has photo %d, kept.\n", b.Username, b.Id, acc.GetProfilePhoto().GetId())
			continue
		}
		photoId, err := setProfilePhoto(ctx, s, b.Id, image, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("@%s: %w", b.Username, err))
			continue
		}
		fmt.Fprintf(stdout, "@%s (%d) has photo %d now.\n", b.Username, b.Id, photoId)
	}
	return errors.Join(errs...)
}
