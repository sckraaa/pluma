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
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// bot game: the games of a bot (task 106, ADR 0068, p. 7). The bot sends one
// with inputMediaGame(inputGameShortName); its address comes from the bot's
// answer to the game button. game.photo is a Photo, not optional (TL game),
// and game.document an animation shown in its place. A game is not part of
// the bot's info, so its changes leave the cached bot alone.

// botGames is what bot game changes: pkg/botstore.
type botGames interface {
	botLookup
	AddGame(ctx context.Context, g botstore.Game) (botstore.Game, error)
	EditGame(ctx context.Context, g botstore.Game) (botstore.Game, error)
	DeleteGame(ctx context.Context, botId int64, shortName string) error
	Games(ctx context.Context, botId int64) ([]botstore.Game, error)
	GameByShortName(ctx context.Context, botId int64, shortName string) (*botstore.Game, error)
}

// botGameCommand: game list|add|edit|rm <bot> [<short name> flags...].
func botGameCommand(ctx context.Context, store botGames, services botServices, args []string, stdout io.Writer) error {
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
		return listGames(ctx, store, id, args[1], stdout)
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
		if err = store.DeleteGame(ctx, id, shortName); err != nil {
			return describeGameError(err)
		}
		fmt.Fprintf(stdout, "bot %d: game %s removed; the messages with it stay, and so do its scores.\n", id, shortName)
		return nil
	}

	flags := flag.NewFlagSet("bot game", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	title := flags.String("title", "", "the title of the game, 1 to 64 characters")
	description := flags.String("description", "", "the description of the game, up to 512 characters")
	photo := flags.String("photo", "", "a JPEG or PNG picture of the game")
	animation := flags.String("animation", "", "an MP4 animation shown in place of the picture; off removes it (edit)")
	if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	given := givenFlags(flags)
	id, err := editableBot(ctx, store, username)
	if err != nil {
		return err
	}
	var game botstore.Game
	if args[0] == "add" {
		if *title == "" || *photo == "" || *animation == settingOff {
			return errUsage
		}
		game = botstore.Game{BotId: id, ShortName: shortName}
	} else {
		if len(given) == 0 {
			return errUsage
		}
		old, err := store.GameByShortName(ctx, id, shortName)
		if err != nil {
			return err
		}
		if old == nil {
			return describeGameError(botstore.ErrNoGame)
		}
		game = *old
	}
	if given["title"] {
		game.Title = *title
	}
	if given["description"] {
		game.Description = *description
	}
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
		if game.PhotoId, err = services.UploadPhoto(ctx, photoData, "game.jpg"); err != nil {
			return err
		}
	}
	switch {
	case animationData != nil:
		if game.DocumentId, err = services.UploadDocument(ctx, animationData, animationFileName, "video/mp4", animationAttributes); err != nil {
			return err
		}
	case given["animation"]:
		game.DocumentId = 0
	}
	var saved botstore.Game
	if args[0] == "add" {
		saved, err = store.AddGame(ctx, game)
	} else {
		saved, err = store.EditGame(ctx, game)
	}
	if err != nil {
		return describeGameError(err)
	}
	fmt.Fprintf(stdout, "bot %d: game %s (id %d): %s\n", id, saved.ShortName, saved.Id, gameLink(username, saved.ShortName))
	return nil
}

// gameLink is the link that shares a game: t.me/<bot>?game=<short name>
// (core.telegram.org/api/links, "Game links") on Pluma's host (ADR 0036).
func gameLink(username, shortName string) string {
	return "https://pluma.chat/" + username + "?game=" + shortName
}

func listGames(ctx context.Context, store botGames, botId int64, username string, stdout io.Writer) error {
	games, err := store.Games(ctx, botId)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SHORT NAME\tTITLE\tANIMATION\tLINK")
	for _, g := range games {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", g.ShortName, g.Title, yesNo(g.DocumentId != 0), gameLink(username, g.ShortName))
	}
	return w.Flush()
}

func describeGameError(err error) error {
	if errors.Is(err, botstore.ErrNoGame) {
		return errors.New("the bot has no game of this short name")
	}
	return describeAppError(err)
}
