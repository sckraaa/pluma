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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	idgen_client "github.com/teamgram/teamgram-server/app/service/idgen/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/tgexport"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/zrpc"
)

// import-chat brings a group of Telegram into Pluma from the export
// Telegram Desktop makes of it (Export chat history, machine-readable JSON,
// with its files), as a new supergroup whose messages are those of the
// Pluma accounts of the same people, with the dates they had (task 083,
// ADR 0051; docs/ops/import-chat.md).
//
//	import-chat accounts                        the Pluma accounts and their ids
//	import-chat map -dir <export> [-out <file>] the mapping to fill: who is who
//	import-chat plan -dir <export> [-mapping <file>] [-missing-media=skip]
//	import-chat run -dir <export> [-mapping <file>] [-missing-media=skip]
//	                (-storage-dir <dir> | -free-bytes <n>) [-batch <n>] [-pause <d>] [-title <t>]
//	import-chat status [-dir <export> | <import id>]
//	import-chat abort <import id>
//
// map and plan read the export and the database and write nothing to the
// server. run stops at the first fault and may be run again: it goes on
// where it stopped. It prints counts and ids, never a text of a message.

// importConfig is the part of the bff config the command needs.
type importConfig struct {
	Mysql            sqlx.Config
	BizServiceClient zrpc.RpcClientConf
	MediaClient      zrpc.RpcClientConf
	DfsClient        zrpc.RpcClientConf
	IdgenClient      zrpc.RpcClientConf
}

// importConnect makes the backend of the config file: the database alone
// for the commands that write nothing, the services as well for the rest.
type importConnect func(configFile string, services bool) (*importBackend, error)

func importChatCommand(args []string, configFile string, stdout io.Writer) error {
	return importChat(context.Background(), args, configFile, stdout, connectImport)
}

func connectImport(configFile string, services bool) (*importBackend, error) {
	var c importConfig
	if err := conf.Load(configFile, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", configFile, err)
	}
	db := &dbImport{db: sqlx.NewMySQL(&c.Mysql)}
	be := &importBackend{accounts: db, media: db, free: freeSpace, waveform: voiceWaveform, sleep: time.Sleep}
	if services {
		be.channel = &rpcImportChannel{cli: channel_client.NewChannelClient(rpcx.GetCachedRpcClient(c.BizServiceClient))}
		be.up = &rpcImportUploader{
			dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		}
		ids := idgen_client.NewIDGenClient2(rpcx.GetCachedRpcClient(c.IdgenClient))
		be.ids = &ids
	}
	return be, nil
}

// freeSpace is the room left on the disk of dir, for one who is not root.
func freeSpace(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// importChat runs import-chat <subcommand>.
func importChat(ctx context.Context, args []string, configFile string, stdout io.Writer, connect importConnect) error {
	if len(args) < 2 {
		return errUsage
	}
	sub := args[1]
	flags := flag.NewFlagSet("import-chat "+sub, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var o importOptions
	var out, missing string
	flags.StringVar(&o.dir, "dir", "", "the export's folder")
	flags.StringVar(&o.mapping, "mapping", "", "the mapping file (default: mapping.json in the export's folder)")
	flags.StringVar(&out, "out", "", "where map writes the mapping (default: mapping.json in the export's folder)")
	flags.StringVar(&missing, "missing-media", "fail", "fail: files missing from the export stop the import; skip: go on without them")
	flags.StringVar(&o.title, "title", "", "the group's title (default: the one of the export)")
	flags.IntVar(&o.batch, "batch", importBatchMax, "records per transaction")
	flags.DurationVar(&o.pause, "pause", 20*time.Millisecond, "pause between transactions")
	flags.StringVar(&o.storageDir, "storage-dir", "", "a directory on the storage's disk, to check its free space")
	flags.Int64Var(&o.freeBytes, "free-bytes", 0, "the free space of the storage, when no directory of it is mounted")
	if err := flags.Parse(args[2:]); err != nil {
		return errUsage
	}
	switch missing {
	case "fail":
	case "skip":
		o.skipMissing = true
	default:
		return errUsage
	}
	if o.batch < 1 || o.batch > importBatchMax || o.pause < 0 || o.freeBytes < 0 {
		return errUsage
	}
	if o.mapping == "" && o.dir != "" {
		o.mapping = filepath.Join(o.dir, "mapping.json")
	}
	needDir := func() error {
		if o.dir == "" || flags.NArg() != 0 {
			return errUsage
		}
		return nil
	}

	switch sub {
	case "accounts":
		if flags.NArg() != 0 {
			return errUsage
		}
		be, err := connect(configFile, false)
		if err != nil {
			return err
		}
		accounts, err := be.accounts.Accounts(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME")
		for _, a := range accounts {
			fmt.Fprintf(tw, "%d\t%s\n", a.Id, a.Name)
		}
		return tw.Flush()
	case "map":
		if err := needDir(); err != nil {
			return err
		}
		if out == "" {
			out = filepath.Join(o.dir, "mapping.json")
		}
		be, err := connect(configFile, false)
		if err != nil {
			return err
		}
		return mapImport(ctx, be, o.dir, out, stdout)
	case "plan":
		if err := needDir(); err != nil {
			return err
		}
		be, err := connect(configFile, false)
		if err != nil {
			return err
		}
		p, err := prepare(ctx, be, o)
		if err != nil {
			return err
		}
		printPlan(stdout, p.plan, p.names)
		if err = planError(p.plan); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "The import can start.")
		return nil
	case "run":
		if err := needDir(); err != nil {
			return err
		}
		be, err := connect(configFile, true)
		if err != nil {
			return err
		}
		return runImport(ctx, be, o, stdout)
	case "status", "abort":
		var id, chat int64
		switch {
		case sub == "status" && o.dir != "" && flags.NArg() == 0:
			ex, err := tgexport.Open(o.dir)
			if err != nil {
				return err
			}
			chat = ex.Header().Id
		case o.dir == "" && flags.NArg() == 1:
			var err error
			if id, err = strconv.ParseInt(flags.Arg(0), 10, 64); err != nil || id <= 0 {
				return errUsage
			}
		default:
			return errUsage
		}
		be, err := connect(configFile, true)
		if err != nil {
			return err
		}
		state, err := be.channel.State(ctx, id, chat)
		if err != nil {
			return err
		}
		if state == nil {
			return errors.New("there is no such import")
		}
		if sub == "abort" {
			if state, err = be.channel.Abort(ctx, state.Id); err != nil {
				return err
			}
		}
		printState(stdout, state)
		return nil
	}
	return errUsage
}

// mapImport writes the mapping of the export for the owner to fill: every
// peer the export names, and the Pluma account whose name is the same when
// there is exactly one. What the owner has filled in an earlier file stays.
func mapImport(ctx context.Context, be *importBackend, dir, out string, w io.Writer) error {
	ex, err := tgexport.Open(dir)
	if err != nil {
		return err
	}
	census, err := tgexport.Scan(ex)
	if err != nil {
		return err
	}
	accounts, err := be.accounts.Accounts(ctx)
	if err != nil {
		return err
	}
	old, err := tgexport.LoadMapping(out)
	if err != nil && !tgexport.IsNotExist(err) {
		return err
	}
	m := tgexport.Suggest(census, accounts, old)
	if err = m.Save(out); err != nil {
		return err
	}
	suggested, open := 0, 0
	for _, a := range m.Authors {
		switch {
		case a.Confirmed || a.Skip:
		case a.Pluma != nil:
			suggested++
		default:
			open++
		}
	}
	fmt.Fprintf(w, "%s: %d peers of chat %d; %d with an account found by name, to confirm; %d to fill in; %d names of service messages.\n",
		out, len(m.Authors), census.Header.Id, suggested, open, len(m.ServiceNames))
	fmt.Fprintln(w, `Set "creator", give every author its "pluma" id and "confirmed": true, then run import-chat plan.`)
	return nil
}

// dbImport reads the accounts and keeps the uploaded files in the database.
type dbImport struct {
	db *sqlx.DB
}

func (d *dbImport) Accounts(ctx context.Context) ([]tgexport.Account, error) {
	var rows []struct {
		Id        int64  `db:"id"`
		FirstName string `db:"first_name"`
		LastName  string `db:"last_name"`
	}
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT id, first_name, last_name FROM users WHERE deleted = 0 ORDER BY id"); err != nil {
		return nil, err
	}
	out := make([]tgexport.Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, tgexport.Account{Id: r.Id, Name: strings.TrimSpace(r.FirstName + " " + r.LastName)})
	}
	return out, nil
}

func (d *dbImport) UploadedBytes(ctx context.Context, sourceChatId int64) (int64, error) {
	var sum []int64
	err := d.db.QueryRowsPartial(ctx, &sum, "SELECT COALESCE(SUM(size), 0) FROM channel_import_media WHERE source_type = ? AND source_chat_id = ?", importSource, sourceChatId)
	if err != nil || len(sum) == 0 {
		return 0, err
	}
	return sum[0], nil
}

func (d *dbImport) Media(ctx context.Context, sourceChatId int64, hash []byte) (*mtproto.MessageMedia, error) {
	var rows []string
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT media_data FROM channel_import_media WHERE source_type = ? AND source_chat_id = ? AND path_hash = ?",
		importSource, sourceChatId, hash); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var m mtproto.MessageMedia
	if err := jsonx.UnmarshalFromString(rows[0], &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (d *dbImport) SaveMedia(ctx context.Context, sourceChatId int64, hash []byte, size int64, m *mtproto.MessageMedia) error {
	made, err := jsonx.MarshalToString(m)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(ctx, "INSERT INTO channel_import_media (source_type, source_chat_id, path_hash, size, media_data, date2) VALUES (?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE size = VALUES(size), media_data = VALUES(media_data), date2 = VALUES(date2)",
		importSource, sourceChatId, hash, size, made, time.Now().Unix())
	return err
}

// rpcImportChannel is biz channel.
type rpcImportChannel struct {
	cli channel_client.ChannelClient
}

func (c *rpcImportChannel) State(ctx context.Context, importId, sourceChatId int64) (*channel.ChannelImport, error) {
	s, err := c.cli.ChannelImportGetState(ctx, &channel.TLChannelImportGetState{ImportId: importId, SourceType: importSource, SourceChatId: sourceChatId})
	if err != nil && strings.Contains(err.Error(), "IMPORT_NOT_FOUND") {
		return nil, nil
	}
	return s, err
}

func (c *rpcImportChannel) Create(ctx context.Context, in *channel.TLChannelImportCreate) (*channel.ChannelImport, error) {
	return c.cli.ChannelImportCreate(ctx, in)
}

func (c *rpcImportChannel) Messages(ctx context.Context, importId, firstSeq int64, batch []*channel.ImportedMessage) (*channel.ChannelImport, error) {
	return c.cli.ChannelImportMessages(ctx, &channel.TLChannelImportMessages{ImportId: importId, FirstSeq: firstSeq, Messages: batch})
}

func (c *rpcImportChannel) Finish(ctx context.Context, importId int64) (*channel.ChannelImport, error) {
	return c.cli.ChannelImportFinish(ctx, &channel.TLChannelImportFinish{ImportId: importId})
}

func (c *rpcImportChannel) Abort(ctx context.Context, importId int64) (*channel.ChannelImport, error) {
	return c.cli.ChannelImportAbort(ctx, &channel.TLChannelImportAbort{ImportId: importId})
}

func (c *rpcImportChannel) Invite(ctx context.Context, creator, channelId int64, users []int64) error {
	_, err := c.cli.ChannelInviteToChannel(ctx, &channel.TLChannelInviteToChannel{SelfId: creator, ChannelId: channelId, Id: users})
	return err
}

// rpcImportUploader is dfs and media.
type rpcImportUploader struct {
	dfs   dfs_client.DfsClient
	media media_client.MediaClient
}

func (u *rpcImportUploader) WritePart(ctx context.Context, owner, fileId int64, part int32, bytes []byte) error {
	_, err := u.dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{Creator: owner, FileId: fileId, FilePart: part, Bytes: bytes})
	return err
}

func (u *rpcImportUploader) UploadPhoto(ctx context.Context, owner int64, file *mtproto.InputFile) (*mtproto.Photo, error) {
	return u.media.MediaUploadPhotoFile(ctx, &media.TLMediaUploadPhotoFile{OwnerId: owner, File: file})
}

func (u *rpcImportUploader) UploadDocument(ctx context.Context, owner int64, in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	return u.media.MediaUploadedDocumentMedia(ctx, &media.TLMediaUploadedDocumentMedia{OwnerId: owner, Media: in})
}

func (u *rpcImportUploader) Photo(ctx context.Context, id int64) (*mtproto.Photo, error) {
	return u.media.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: id})
}

func (u *rpcImportUploader) Document(ctx context.Context, id int64) (*mtproto.Document, error) {
	return u.media.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: id})
}
