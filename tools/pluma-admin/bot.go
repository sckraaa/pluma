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
	"crypto/md5" //nolint:gosec // MTProto's md5_checksum of an uploaded file, a check of the upload, not security
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// botConfig is the part of the bff config bot needs: the database of bots,
// the user service for the flags (it owns the cache of users), authsession
// and sync to end the sessions of a replaced token, the key the keys of a
// new token are sealed under (task 106, ADR 0068, p. 3), dfs and media for
// the pictures of apps, games and the attachment menu, and the key that
// signs the data of mini apps.
type botConfig struct {
	Mysql             sqlx.Config
	BizServiceClient  zrpc.RpcClientConf
	AuthSessionClient zrpc.RpcClientConf
	MediaClient       zrpc.RpcClientConf
	DfsClient         zrpc.RpcClientConf
	SyncClient        struct{ Topic string }
	BotSecrets        botkeys.Config    `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	WebApp            webappdata.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// noBotSecrets is printed after a token issued without
// BOT_SECRET_ENCRYPTION_KEY.
const noBotSecrets = "BOT_SECRET_ENCRYPTION_KEY is not set: the keys of the token are not stored, so the bot's mini apps and login stay off (docs/ops/bots.md)"

// botCommand runs bot create, list, token and set (task 070, ADR 0043) and
// the settings of the bot platform: app, attach, game, login and
// webapp-key (task 106, ADR 0068, p. 2).
func botCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	var c botConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	if args[1] == "webapp-key" {
		if len(args) != 2 {
			return errUsage
		}
		return printWebAppKey(c.WebApp, stdout)
	}
	sealer, err := c.BotSecrets.Sealer()
	if err != nil {
		return err
	}
	db := sqlx.NewMySQL(&c.Mysql)
	store := botstore.New(db).WithSecrets(sealer)
	services := &rpcBotServices{conf: &c}
	ctx, cancel := context.WithTimeout(context.Background(), botCommandTimeout)
	defer cancel()

	switch args[1] {
	case "list":
		if len(args) != 2 {
			return errUsage
		}
		return listBots(ctx, store, stdout)
	case "create":
		if err := createBot(ctx, db, store, args[2:], stdout); err != nil {
			return err
		}
		if sealer == nil {
			fmt.Fprintln(stdout, noBotSecrets)
		}
		return nil
	case "token":
		if len(args) != 3 {
			return errUsage
		}
		id, err := editableBot(ctx, store, args[2])
		if err != nil {
			return err
		}
		token, err := store.NewToken(ctx, id)
		if err != nil {
			return err
		}
		// Printed first: it cannot be shown again, and a failed revocation
		// is repaired by running the command once more.
		fmt.Fprintln(stdout, token)
		if sealer == nil {
			fmt.Fprintln(stdout, noBotSecrets)
		}
		runner := &effects.Runner{
			Authsession: authsession_client.NewAuthsessionClient(rpcx.GetCachedRpcClient(c.AuthSessionClient)),
			Sync:        sync_client.MustNewSyncOutboxClient(sync_client.OutboxConf{DSN: c.Mysql.DSN, Topic: c.SyncClient.Topic}),
		}
		if err = runner.Revoke(ctx, id); err != nil {
			return fmt.Errorf("the token is replaced, but the sessions of the bot are not ended: %w", err)
		}
		return nil
	case "set":
		return setBot(ctx, store, services, args[2:], stdout)
	case "app":
		return botAppCommand(ctx, store, services, args[2:], stdout)
	case "attach":
		return botAttachCommand(ctx, store, services, args[2:], stdout)
	case "game":
		return botGameCommand(ctx, store, services, args[2:], stdout)
	case "login":
		return botLoginCommand(ctx, store, args[2:], time.Now, stdout)
	}
	return errUsage
}

// botCommandTimeout bounds one command: a few small files to upload at
// most.
const botCommandTimeout = 2 * time.Minute

// printWebAppKey prints the public key that checks the signature of the
// data of mini apps (WEBAPP_SIGNING_KEY), in hex, as Telegram publishes
// its own (core.telegram.org/bots/webapps, "Validating data for
// Third-Party Use"): the developers of bots check initData with it.
func printWebAppKey(c webappdata.Config, stdout io.Writer) error {
	signer, err := c.Signer()
	if err != nil {
		return err
	}
	if signer == nil {
		return errors.New("WEBAPP_SIGNING_KEY is not set: mini apps do not open and there is no key to print (docs/ops/bots.md)")
	}
	fmt.Fprintln(stdout, signer.PublicKeyHex())
	return nil
}

// botLookup finds a bot by its username: pkg/botstore.
type botLookup interface {
	ByUsername(ctx context.Context, username string) (int64, error)
}

func botByUsername(ctx context.Context, store botLookup, username string) (int64, error) {
	id, err := store.ByUsername(ctx, username)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("no bot %s", username)
	}
	return id, nil
}

// errBuiltinBot is every command that changes a bot or shows its settings,
// for a bot of pkg/inlinebots: the server answers for it and its account is
// z_pluma-inline-bots.sql's (ADR 0062).
var errBuiltinBot = errors.New("built-in bot, managed by the server")

// editableBot is botByUsername for the commands that change a bot or show
// its settings: a built-in bot is refused.
func editableBot(ctx context.Context, store botLookup, username string) (int64, error) {
	id, err := botByUsername(ctx, store, username)
	if err != nil {
		return 0, err
	}
	if err = checkEditable(id); err != nil {
		return 0, fmt.Errorf("%s: %w", username, err)
	}
	return id, nil
}

func checkEditable(id int64) error {
	if inlinebots.IsBuiltin(id) {
		return errBuiltinBot
	}
	return nil
}

func listBots(ctx context.Context, store *botstore.Store, stdout io.Writer) error {
	bots, err := store.List(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSERNAME\tNAME\tOWNER")
	for _, b := range bots {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\n", b.Id, b.Username, b.Name, b.Creator)
	}
	return w.Flush()
}

func createBot(ctx context.Context, db *sqlx.DB, store *botstore.Store, args []string, stdout io.Writer) error {
	// Flags after the name.
	var rest, nameParts []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && i > 0 {
			rest = args[i:]
			break
		}
		nameParts = append(nameParts, args[i])
	}
	if len(nameParts) < 2 {
		return errUsage
	}
	username, name := nameParts[0], strings.Join(nameParts[1:], " ")
	flags := flag.NewFlagSet("bot create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	owner := flags.String("owner", "", "the phone of the account that may edit the bot")
	if err := flags.Parse(rest); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	if name == "" || len([]rune(name)) > botstore.MaxName {
		return errors.New("the name must be 1 to 64 characters")
	}
	creator := int64(0)
	if *owner != "" {
		phone, err := normalize(*owner)
		if err != nil {
			return err
		}
		var ids []int64
		if err = db.QueryRowsPartial(ctx, &ids, "SELECT id FROM users WHERE phone = ? AND deleted = 0", phone); err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("+%s has no account", phone)
		}
		creator = ids[0]
	}
	id, token, err := store.Create(ctx, username, name, creator)
	switch {
	case errors.Is(err, botstore.ErrUsernameTaken):
		return fmt.Errorf("username %s is taken", username)
	case errors.Is(err, botstore.ErrUsernameInvalid):
		return fmt.Errorf("username %s: 5 to 32 letters, digits and _, starts with a letter, ends in bot", username)
	case err != nil:
		return err
	}
	fmt.Fprintf(stdout, "bot %d: %s\n", id, token)
	return nil
}

// botSettings is what bot set changes beyond the flags of the user: the
// login domain and the main mini app (pkg/botstore).
type botSettings interface {
	botLookup
	SetLoginDomain(ctx context.Context, botId int64, domain string) error
	SetMainApp(ctx context.Context, botId int64, url string) error
}

// settingOff is the value of -domain and -main-app that removes the
// setting: neither a domain of two labels nor an https address.
const settingOff = "off"

func setBot(ctx context.Context, store botSettings, services botServices, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	flags := flag.NewFlagSet("bot set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	privacy := flags.String("privacy", "", "on: the bot gets only the messages meant for it in groups")
	groups := flags.String("groups", "", "off: the bot cannot be added to groups")
	inlineGeo := flags.String("inline-geo", "", "on: the bot gets the location of the user of an inline query")
	inline := flags.String("inline", "", "the placeholder of the inline mode; a space turns inline off")
	domain := flags.String("domain", "", "the site the bot logs users in to; off unlinks it")
	mainApp := flags.String("main-app", "", "the https address of the bot's main mini app; off removes it")
	businessMode := flags.String("business", "", "on: accounts may connect the bot as their business bot; off disconnects it from all of them")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	id, err := editableBot(ctx, store, args[0])
	if err != nil {
		return err
	}
	in := &userpb.TLUserUpdateBotData{BotId: id}
	on := func(v string) (*mtproto.Bool, error) {
		switch v {
		case "":
			return nil, nil
		case "on":
			return mtproto.BoolTrue, nil
		case "off":
			return mtproto.BoolFalse, nil
		}
		return nil, errUsage
	}
	flagsSet := false
	if *privacy != "" {
		// Privacy mode on is bot_chat_history off.
		v, err := on(*privacy)
		if err != nil {
			return err
		}
		in.BotChatHistory = mtproto.ToBool(!mtproto.FromBool(v))
		flagsSet = true
	}
	if *groups != "" {
		v, err := on(*groups)
		if err != nil {
			return err
		}
		in.BotNochats = mtproto.ToBool(!mtproto.FromBool(v))
		flagsSet = true
	}
	if *inlineGeo != "" {
		if in.BotInlineGeo, err = on(*inlineGeo); err != nil {
			return err
		}
		flagsSet = true
	}
	if *inline != "" {
		placeholder := *inline
		if strings.TrimSpace(placeholder) == "" {
			placeholder = ""
		}
		in.BotInlinePlaceholder = wrapperspb.String(placeholder)
		flagsSet = true
	}
	if *businessMode != "" {
		// user.bot_business: the clients offer only such a bot for
		// Telegram Business (tdesktop settings/business/settings_chatbots.cpp:342,
		// Android ui/Business/ChatbotsActivity.java:565), and
		// account.updateConnectedBot refuses others (BOT_BUSINESS_MISSING).
		if in.BotBusiness, err = on(*businessMode); err != nil {
			return err
		}
		flagsSet = true
	}
	if *domain == "" && *mainApp == "" && !flagsSet {
		return errUsage
	}
	// The settings of pkg/botstore go first: they are checked there, and
	// the call to biz below drops the cached user after them.
	if *domain != "" {
		d := *domain
		if d == settingOff {
			d = ""
		}
		switch err := store.SetLoginDomain(ctx, id, d); {
		case errors.Is(err, botstore.ErrLoginDomainInvalid):
			return fmt.Errorf("domain %s: a host name such as example.com, without scheme, port or path", *domain)
		case errors.Is(err, botstore.ErrLoginDomainTaken):
			return fmt.Errorf("domain %s belongs to another bot", d)
		case err != nil:
			return err
		}
		if d == "" {
			fmt.Fprintf(stdout, "bot %d: no login domain.\n", id)
		} else {
			fmt.Fprintf(stdout, "bot %d: login domain %s.\n", id, strings.ToLower(d))
		}
	}
	if *mainApp != "" {
		u := *mainApp
		if u == settingOff {
			u = ""
		}
		if err := store.SetMainApp(ctx, id, u); err != nil {
			return describeAppError(err)
		}
		// bot_has_main_app is a flag of the user (user.bot_has_main_app,
		// read by Android ui/ProfileActivity.java:10988), so the cached
		// user is dropped through biz with it.
		in.BotHasMainApp = mtproto.ToBool(u != "")
		flagsSet = true
	}
	if !flagsSet {
		// The login domain is not in the user object: url_auth_domains
		// of the app config picks it up.
		return nil
	}
	if err := services.UpdateBotData(ctx, in); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bot %d updated.\n", id)
	if in.BotBusiness != nil && !mtproto.FromBool(in.BotBusiness) {
		// After the flag: no account can connect the bot again meanwhile.
		// A failure here is repaired by running the command once more.
		n, err := services.DisconnectBusiness(ctx, id)
		if err != nil {
			return fmt.Errorf("the business mode is off, but the bot is still connected to some accounts: %w", err)
		}
		fmt.Fprintf(stdout, "bot %d: disconnected from %d accounts.\n", id, n)
	}
	return nil
}

// botServices are the services the settings of the bot platform go
// through: dfs and media make the pictures, as a client's upload does
// (ADR 0068, p. 2: the way of servicephoto.go), and biz user owns the flags
// of the bot and the cache of users.
type botServices interface {
	// UploadPhoto makes a photo of a JPEG or PNG image and returns its id.
	UploadPhoto(ctx context.Context, image []byte, name string) (int64, error)
	// UploadDocument makes a document of the file and returns its id.
	UploadDocument(ctx context.Context, file []byte, name, mimeType string, attributes []*mtproto.DocumentAttribute) (int64, error)
	// UpdateBotData changes the flags of the bot, raises its
	// bot_info_version and drops the cached user (user.updateBotData).
	UpdateBotData(ctx context.Context, in *userpb.TLUserUpdateBotData) error
	// DisconnectBusiness disconnects the bot from every account it is the
	// business bot of (pkg/business) and returns how many there were.
	DisconnectBusiness(ctx context.Context, botId int64) (int, error)
}

// rpcBotServices are the services of the config; the clients are made on
// first use, so the commands that only read the database need none of
// them.
type rpcBotServices struct {
	conf  *botConfig
	dfs   dfs_client.DfsClient
	media media_client.MediaClient
	user  user_client.UserClient
}

func (s *rpcBotServices) files() {
	if s.dfs == nil {
		s.dfs = dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(s.conf.DfsClient))
		s.media = media_client.NewMediaClient(rpcx.GetCachedRpcClient(s.conf.MediaClient))
	}
}

// send writes the parts of data to dfs as a new anonymous owner, one id for
// this upload alone, as service-photo does, and returns the owner and the
// file as a client names it.
func (s *rpcBotServices) send(ctx context.Context, data []byte, name string) (int64, *mtproto.InputFile, error) {
	s.files()
	owner, err := randomId()
	if err != nil {
		return 0, nil, err
	}
	fileId, err := randomId()
	if err != nil {
		return 0, nil, err
	}
	parts := int32(0)
	for off := 0; off < len(data); off += photoPartSize {
		end := min(off+photoPartSize, len(data))
		if _, err := s.dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{
			Creator: owner, FileId: fileId, FilePart: parts, Bytes: data[off:end],
		}); err != nil {
			return 0, nil, fmt.Errorf("dfs: part %d: %w", parts, err)
		}
		parts++
	}
	if len(data) > bigFileSize {
		return owner, mtproto.MakeTLInputFileBig(&mtproto.InputFile{Id_INT64: fileId, Parts: parts, Name: name}).To_InputFile(), nil
	}
	sum := md5.Sum(data) //nolint:gosec // inputFile.md5_checksum, which dfs checks the parts against
	return owner, mtproto.MakeTLInputFile(&mtproto.InputFile{
		Id_INT64: fileId, Parts: parts, Name: name, Md5Checksum: hex.EncodeToString(sum[:]),
	}).To_InputFile(), nil
}

// UploadPhoto: media answers with the photo even when it could not record
// it (see importchat_media.go), so the photo is read back before its id is
// used.
func (s *rpcBotServices) UploadPhoto(ctx context.Context, image []byte, name string) (int64, error) {
	owner, file, err := s.send(ctx, image, name)
	if err != nil {
		return 0, err
	}
	photo, err := s.media.MediaUploadPhotoFile(ctx, &media.TLMediaUploadPhotoFile{OwnerId: owner, File: file})
	if err != nil {
		return 0, fmt.Errorf("media: %w", err)
	}
	if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
		return 0, errors.New("media: no photo made of the file")
	}
	stored, err := s.media.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: photo.GetId()})
	if err != nil {
		return 0, fmt.Errorf("media: %w", err)
	}
	if stored.GetPredicateName() != mtproto.Predicate_photo || len(stored.GetSizes()) == 0 {
		return 0, fmt.Errorf("media: photo %d was not recorded", photo.GetId())
	}
	return photo.GetId(), nil
}

// UploadDocument reads the document back, as UploadPhoto does the photo.
func (s *rpcBotServices) UploadDocument(ctx context.Context, data []byte, name, mimeType string, attributes []*mtproto.DocumentAttribute) (int64, error) {
	owner, file, err := s.send(ctx, data, name)
	if err != nil {
		return 0, err
	}
	made, err := s.media.MediaUploadedDocumentMedia(ctx, &media.TLMediaUploadedDocumentMedia{
		OwnerId: owner,
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File: file, MimeType: mimeType, Attributes: attributes,
		}).To_InputMedia(),
	})
	if err != nil {
		return 0, fmt.Errorf("media: %w", err)
	}
	if made.GetPredicateName() != mtproto.Predicate_messageMediaDocument || made.GetDocument().GetId() == 0 {
		return 0, errors.New("media: no document made of the file")
	}
	id := made.GetDocument().GetId()
	stored, err := s.media.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: id})
	if err != nil {
		return 0, fmt.Errorf("media: %w", err)
	}
	if stored.GetPredicateName() != mtproto.Predicate_document {
		return 0, fmt.Errorf("media: document %d was not recorded", id)
	}
	if size := stored.GetSize2_INT64(); size != int64(len(data)) {
		return 0, fmt.Errorf("media: document %d is recorded with %d bytes, the file has %d", id, size, len(data))
	}
	return id, nil
}

func (s *rpcBotServices) UpdateBotData(ctx context.Context, in *userpb.TLUserUpdateBotData) error {
	if s.user == nil {
		s.user = user_client.NewUserClient(rpcx.GetCachedRpcClient(s.conf.BizServiceClient))
	}
	_, err := s.user.UserUpdateBotData(ctx, in)
	return err
}

// DisconnectBusiness removes the connections of the bot account by
// account, each in a box transaction of the account, as
// account.updateConnectedBot does; the bot is told of each (part 7 of task
// 105, updateBotBusinessConnect with disabled). A connection the account
// changed to another bot meanwhile stays.
func (s *rpcBotServices) DisconnectBusiness(ctx context.Context, botId int64) (int, error) {
	store := business.NewStore(sqlx.NewMySQL(&s.conf.Mysql))
	accounts, err := store.AccountsOfBot(ctx, botId)
	if err != nil || len(accounts) == 0 {
		return 0, err
	}
	boxDB, err := userbox.Open(s.conf.Mysql.DSN)
	if err != nil {
		return 0, err
	}
	defer boxDB.Close()
	n := 0
	for _, userId := range accounts {
		var removed *business.Connection
		if _, err = userbox.Run(ctx, boxDB, userId, func(b *userbox.Box) error {
			var err error
			removed, err = business.DisconnectTx(b.Tx(), userId, botId)
			return err
		}); err != nil {
			return n, fmt.Errorf("account %d: %w", userId, err)
		}
		if removed != nil {
			n++
		}
	}
	return n, nil
}

// readFile reads a file of 1 byte to limit bytes; what names the file in
// the errors.
func readFile(path string, limit int64, what string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %s is not a file", what, path)
	}
	if st.Size() == 0 || st.Size() > limit {
		return nil, fmt.Errorf("%s %s: 1 byte to %d KiB", what, path, limit>>10)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return nil, fmt.Errorf("%s %s changed while it was read", what, path)
	}
	return data, nil
}

// onOff reads the value of a switch: on or off.
func onOff(v string) (bool, error) {
	switch v {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, errUsage
}

// listFlag is a flag that may be given several times.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// givenFlags returns the names of the flags given on the command line.
func givenFlags(flags *flag.FlagSet) map[string]bool {
	given := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { given[f.Name] = true })
	return given
}

// yesNo prints a switch.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
