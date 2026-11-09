// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package server

import (
	"flag"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	account_helper "github.com/teamgram/teamgram-server/app/bff/account"
	authorization_helper "github.com/teamgram/teamgram-server/app/bff/authorization"
	autodownload_helper "github.com/teamgram/teamgram-server/app/bff/autodownload"
	"github.com/teamgram/teamgram-server/app/bff/bff/internal/config"
	catalog_helper "github.com/teamgram/teamgram-server/app/bff/catalog"
	channels_helper "github.com/teamgram/teamgram-server/app/bff/channels"
	chatinvites_helper "github.com/teamgram/teamgram-server/app/bff/chatinvites"
	chats_helper "github.com/teamgram/teamgram-server/app/bff/chats"
	configuration_helper "github.com/teamgram/teamgram-server/app/bff/configuration"
	contacts_helper "github.com/teamgram/teamgram-server/app/bff/contacts"
	dialogs_helper "github.com/teamgram/teamgram-server/app/bff/dialogs"
	drafts_helper "github.com/teamgram/teamgram-server/app/bff/drafts"
	files_helper "github.com/teamgram/teamgram-server/app/bff/files"
	folders_helper "github.com/teamgram/teamgram-server/app/bff/folders"
	gifs_helper "github.com/teamgram/teamgram-server/app/bff/gifs"
	langpackhelper "github.com/teamgram/teamgram-server/app/bff/langpack"
	messages_helper "github.com/teamgram/teamgram-server/app/bff/messages"
	miscellaneous_helper "github.com/teamgram/teamgram-server/app/bff/miscellaneous"
	notification_helper "github.com/teamgram/teamgram-server/app/bff/notification"
	nsfw_helper "github.com/teamgram/teamgram-server/app/bff/nsfw"
	passkeyhelper "github.com/teamgram/teamgram-server/app/bff/passkey"
	passport_helper "github.com/teamgram/teamgram-server/app/bff/passport"
	premium_helper "github.com/teamgram/teamgram-server/app/bff/premium"
	privacysettingshelper "github.com/teamgram/teamgram-server/app/bff/privacysettings"
	qrcode_helper "github.com/teamgram/teamgram-server/app/bff/qrcode"
	reports_helper "github.com/teamgram/teamgram-server/app/bff/reports"
	savedmessagedialogshelper "github.com/teamgram/teamgram-server/app/bff/savedmessagedialogs"
	scheduledmessages_helper "github.com/teamgram/teamgram-server/app/bff/scheduledmessages"
	sponsoredmessages_helper "github.com/teamgram/teamgram-server/app/bff/sponsoredmessages"
	stargifts_helper "github.com/teamgram/teamgram-server/app/bff/stargifts"
	stories_helper "github.com/teamgram/teamgram-server/app/bff/stories"
	tos_helper "github.com/teamgram/teamgram-server/app/bff/tos"
	twofa_helper "github.com/teamgram/teamgram-server/app/bff/twofa"
	updates_helper "github.com/teamgram/teamgram-server/app/bff/updates"
	userchannelprofileshelper "github.com/teamgram/teamgram-server/app/bff/userchannelprofiles"
	usernames_helper "github.com/teamgram/teamgram-server/app/bff/usernames"
	users_helper "github.com/teamgram/teamgram-server/app/bff/users"
	webbrowserhelper "github.com/teamgram/teamgram-server/app/bff/webbrowser"
	webpage_helper "github.com/teamgram/teamgram-server/app/bff/webpage"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

var configFile = flag.String("f", "etc/bff.yaml", "the config file")

type Server struct {
	grpcSrv *zrpc.RpcServer
}

func New() *Server {
	return new(Server)
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets from the environment (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)

	// dialog methods of biz run box transactions (pkg/userbox): the client
	// deadline must outlive them.
	if err := userbox.CheckRPCTimeout("BizServiceClient.Timeout", c.BizServiceClient.Timeout, dialog.MaxBoxOpTime); err != nil {
		return err
	}
	// ctx := svc.NewServiceContext(c)
	// s.grpcSrv = grpc.New(ctx, c.RpcServerConf)

	// Pushes are rows of mq_outbox in bff's database: Kafka is not on the
	// path of a request.
	syncClient := sync_client.OutboxConf{DSN: c.Mysql.DSN, Topic: c.SyncClient.Topic}

	s.grpcSrv = zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		// reports_helper: the moderation queue (task 025, ADR 0040).
		mtproto.RegisterRPCReportsServer(
			grpcServer,
			reports_helper.New(reports_helper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
				UserClient:    c.BizServiceClient,
				MessageClient: c.BizServiceClient,
				ChannelClient: c.BizServiceClient,
			}))

		// tos_helper
		mtproto.RegisterRPCTosServer(
			grpcServer,
			tos_helper.New(tos_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// configuration_helper: also the promo data, the suggestions of the
		// account.
		configuration := configuration_helper.New(configuration_helper.Config{
			RpcServerConf:     c.RpcServerConf,
			Mysql:             c.Mysql,
			UserClient:        c.BizServiceClient,
			AuthsessionClient: c.AuthSessionClient,
			MediaClient:       c.MediaClient,
			ReleasesURL:       c.ReleasesURL,
		})
		mtproto.RegisterRPCConfigurationServer(grpcServer, configuration)
		mtproto.RegisterRPCPromoDataServer(grpcServer, configuration)

		// langpackhelper: the language packs of the clients (task 021).
		mtproto.RegisterRPCLangpackServer(
			grpcServer,
			langpackhelper.New(langpackhelper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
			}))

		// qrcode_helper
		mtproto.RegisterRPCQrCodeServer(
			grpcServer,
			qrcode_helper.New(
				qrcode_helper.Config{
					RpcServerConf:     c.RpcServerConf,
					KV:                c.KV,
					Mysql:             c.Mysql,
					UserClient:        c.BizServiceClient,
					AuthSessionClient: c.AuthSessionClient,
					SyncClient:        syncClient,
					MsgClient:         c.MsgClient,
				}))

		// miscellaneous_helper
		mtproto.RegisterRPCMiscellaneousServer(
			grpcServer,
			miscellaneous_helper.New(miscellaneous_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// authorization_helper
		mtproto.RegisterRPCAuthorizationServer(
			grpcServer,
			authorization_helper.New(
				authorization_helper.Config{
					RpcServerConf:     c.RpcServerConf,
					KV:                c.KV,
					Mysql:             c.Mysql,
					LoginCode:         c.LoginCode,
					UserClient:        c.BizServiceClient,
					AuthsessionClient: c.AuthSessionClient,
					ChatClient:        c.BizServiceClient,
					StatusClient:      c.StatusClient,
					SyncClient:        syncClient,
					MsgClient:         c.MsgClient,
				},
				nil))

		// premium_helper
		mtproto.RegisterRPCPremiumServer(
			grpcServer,
			premium_helper.New(premium_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// scheduledmessages_helper (task 061)
		mtproto.RegisterRPCScheduledMessagesServer(
			grpcServer,
			scheduledmessages_helper.New(scheduledmessages_helper.Config{
				RpcServerConf: c.RpcServerConf,
				MsgClient:     c.MsgClient,
			}))

		// chatinvites_helper
		mtproto.RegisterRPCChatInvitesServer(
			grpcServer,
			chatinvites_helper.New(chatinvites_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UserClient:    c.BizServiceClient,
				ChatClient:    c.BizServiceClient,
				MsgClient:     c.MsgClient,
				SyncClient:    syncClient,
				ChannelClient: c.BizServiceClient,
			}))

		// chats_helper
		mtproto.RegisterRPCChatsServer(
			grpcServer,
			chats_helper.New(chats_helper.Config{
				RpcServerConf:     c.RpcServerConf,
				KV:                c.KV,
				Mysql:             c.Mysql,
				UserClient:        c.BizServiceClient,
				ChatClient:        c.BizServiceClient,
				MsgClient:         c.MsgClient,
				DialogClient:      c.BizServiceClient,
				SyncClient:        syncClient,
				MediaClient:       c.MediaClient,
				AuthsessionClient: c.AuthSessionClient,
				IdgenClient:       c.IdgenClient,
				MessageClient:     c.BizServiceClient,
				ChannelClient:     c.BizServiceClient,
			}))

		// files_helper
		mtproto.RegisterRPCFilesServer(
			grpcServer,
			files_helper.New(files_helper.Config{
				RpcServerConf: c.RpcServerConf,
				DfsClient:     c.DfsClient,
				UserClient:    c.BizServiceClient,
				MediaClient:   c.MediaClient,
			}, nil, nil))

		// passport_helper
		mtproto.RegisterRPCPassportServer(
			grpcServer,
			passport_helper.New(passport_helper.Config{
				RpcServerConf:     c.RpcServerConf,
				AuthsessionClient: c.AuthSessionClient,
				UserClient:        c.BizServiceClient,
			}))

		// updates_helper
		mtproto.RegisterRPCUpdatesServer(
			grpcServer,
			updates_helper.New(updates_helper.Config{
				RpcServerConf:     c.RpcServerConf,
				UpdatesClient:     c.BizServiceClient,
				UserClient:        c.BizServiceClient,
				ChatClient:        c.BizServiceClient,
				ChannelClient:     c.BizServiceClient,
				DialogClient:      c.BizServiceClient,
				AuthsessionClient: c.AuthSessionClient,
			}))

		// contacts_helper
		mtproto.RegisterRPCContactsServer(
			grpcServer,
			contacts_helper.New(
				contacts_helper.Config{
					RpcServerConf: c.RpcServerConf,
					UserClient:    c.BizServiceClient,
					ChatClient:    c.BizServiceClient,
					SyncClient:    syncClient,
					ChannelClient: c.BizServiceClient,
				},
				nil))

		// dialogs_helper
		mtproto.RegisterRPCDialogsServer(
			grpcServer,
			dialogs_helper.New(dialogs_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UpdatesClient: c.BizServiceClient,
				UserClient:    c.BizServiceClient,
				ChatClient:    c.BizServiceClient,
				DialogClient:  c.BizServiceClient,
				SyncClient:    syncClient,
				MessageClient: c.BizServiceClient,
				ChannelClient: c.BizServiceClient,
				MsgClient:     c.MsgClient,
			}, nil))

		// folders_helper: the folders of chats and their tags.
		folders := folders_helper.New(folders_helper.Config{
			RpcServerConf: c.RpcServerConf,
			DialogClient:  c.BizServiceClient,
			UserClient:    c.BizServiceClient,
			ChatClient:    c.BizServiceClient,
			SyncClient:    syncClient,
			MsgClient:     c.MsgClient,
			ChannelClient: c.BizServiceClient,
		})
		mtproto.RegisterRPCFoldersServer(grpcServer, folders)
		mtproto.RegisterRPCFolderTagsServer(grpcServer, folders)

		// webpage_helper: the cards of links (task 023).
		mtproto.RegisterRPCWebPageServer(
			grpcServer,
			webpage_helper.New(webpage_helper.Config{
				RpcServerConf: c.RpcServerConf,
				Mysql:         c.Mysql,
				DfsClient:     c.DfsClient,
				MediaClient:   c.MediaClient,
			}))

		// catalog_helper: the catalog imported from Telegram (task 024):
		// reactions, stickers, emoji, colors, themes, wallpapers.
		catalog := catalog_helper.New(catalog_helper.Config{
			RpcServerConf: c.RpcServerConf,
			Mysql:         c.Mysql,
			MediaClient:   c.MediaClient,
			DfsClient:     c.DfsClient,
			TelegramBot:   c.TelegramBot,
			SyncTopic:     c.SyncClient.Topic,
			MsgClient:     c.MsgClient,
			MessageClient: c.BizServiceClient,
			UserClient:    c.BizServiceClient,
			ChatClient:    c.BizServiceClient,
			ChannelClient: c.BizServiceClient,
			DialogClient:  c.BizServiceClient,
		})
		mtproto.RegisterRPCReactionsServer(grpcServer, catalog)
		mtproto.RegisterRPCStickersServer(grpcServer, catalog)
		mtproto.RegisterRPCCustomEmojisServer(grpcServer, catalog)
		mtproto.RegisterRPCEmojiServer(grpcServer, catalog)
		mtproto.RegisterRPCEmojiCategoriesServer(grpcServer, catalog)
		mtproto.RegisterRPCEmojiStatusServer(grpcServer, catalog)
		mtproto.RegisterRPCAccentColorsServer(grpcServer, catalog)
		mtproto.RegisterRPCThemesServer(grpcServer, catalog)
		mtproto.RegisterRPCWallpapersServer(grpcServer, catalog)
		mtproto.RegisterRPCTimezonesServer(grpcServer, catalog)
		mtproto.RegisterRPCMessageEffectsServer(grpcServer, catalog)
		mtproto.RegisterRPCSavedMessageTagsServer(grpcServer, catalog)

		// channels_helper: channels and supergroups (ADR 0034); the methods
		// not served yet answer ERR_ENTERPRISE_IS_BLOCKED.
		// The same module serves the boosts of channels (RPCBoosts).
		channels := channels_helper.New(channels_helper.Config{
			RpcServerConf: c.RpcServerConf,
			KV:            c.KV,
			Mysql:         c.Mysql,
			MediaClient:   c.MediaClient,
			ChannelClient: c.BizServiceClient,
			UserClient:    c.BizServiceClient,
			ChatClient:    c.BizServiceClient,
			MsgClient:     c.MsgClient,

			AuthsessionClient: c.AuthSessionClient,
		})
		mtproto.RegisterRPCChannelsServer(grpcServer, channels)
		mtproto.RegisterRPCBoostsServer(grpcServer, channels)
		// channels.getLeftChannels, of the takeout API (task 041).
		mtproto.RegisterRPCTakeoutServer(grpcServer, channels)
		// The threads of replies: comments of channel posts (task 048).
		mtproto.RegisterRPCMessageThreadsServer(grpcServer, channels)
		// The forums and topics of supergroups (task 060).
		mtproto.RegisterRPCForumsServer(grpcServer, channels)

		// stories_helper: the stories (none yet, the list answers so).
		mtproto.RegisterRPCStoriesServer(
			grpcServer,
			stories_helper.New(stories_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// gifs_helper: the saved GIFs.
		mtproto.RegisterRPCGifsServer(
			grpcServer,
			gifs_helper.New(gifs_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UserClient:    c.BizServiceClient,
				MediaClient:   c.MediaClient,
			}))

		// stargifts_helper: the star gifts (none without payments).
		mtproto.RegisterRPCGiftsServer(
			grpcServer,
			stargifts_helper.New(stargifts_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UserClient:    c.BizServiceClient,
			}))

		// drafts_helper
		mtproto.RegisterRPCDraftsServer(
			grpcServer,
			drafts_helper.New(drafts_helper.Config{
				RpcServerConf: c.RpcServerConf,
				DialogClient:  c.BizServiceClient,
				UserClient:    c.BizServiceClient,
				SyncClient:    syncClient,
				ChatClient:    c.BizServiceClient,
				ChannelClient: c.BizServiceClient,
			}, nil))

		// autodownload_helper
		mtproto.RegisterRPCAutoDownloadServer(
			grpcServer,
			autodownload_helper.New(autodownload_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// messages_helper
		messagesService := messages_helper.New(messages_helper.Config{
			RpcServerConf: c.RpcServerConf,
			UserClient:    c.BizServiceClient,
			ChatClient:    c.BizServiceClient,
			MsgClient:     c.MsgClient,
			DialogClient:  c.BizServiceClient,
			IdgenClient:   c.IdgenClient,
			MessageClient: c.BizServiceClient,
			MediaClient:   c.MediaClient,
			UpdatesClient: c.BizServiceClient,
			SyncClient:    syncClient,
			Mysql:         c.Mysql,
			DfsClient:     c.DfsClient,
			ChannelClient: c.BizServiceClient,
		}, nil)
		mtproto.RegisterRPCMessagesServer(grpcServer, messagesService)
		// pluma: task 062
		mtproto.RegisterRPCPollsServer(grpcServer, messagesService)
		mtproto.RegisterRPCTodoListsServer(grpcServer, messagesService)

		// notification_helper; it answers messages.reportMessagesDelivery of
		// the pushes too (task 009), and the reaction notification settings
		// (task 020).
		notificationService := notification_helper.New(notification_helper.Config{
			RpcServerConf:           c.RpcServerConf,
			UserClient:              c.BizServiceClient,
			ChatClient:              c.BizServiceClient,
			SyncClient:              syncClient,
			Mysql:                   c.Mysql,
			PushSecretEncryptionKey: c.PushSecretEncryptionKey,
		}, nil)
		mtproto.RegisterRPCNotificationServer(grpcServer, notificationService)
		mtproto.RegisterRPCGatewayVerificationMessagesServer(grpcServer, notificationService)
		mtproto.RegisterRPCReactionNotificationServer(grpcServer, notificationService)

		// users_helper
		mtproto.RegisterRPCUsersServer(
			grpcServer,
			users_helper.New(
				users_helper.Config{
					RpcServerConf: c.RpcServerConf,
					UserClient:    c.BizServiceClient,
					ChatClient:    c.BizServiceClient,
					DialogClient:  c.BizServiceClient,
					MediaClient:   c.MediaClient,
					MsgClient:     c.MsgClient,
				},
				nil,
				nil,
				appearance.NewStore(sqlx.NewMySQL(&c.Mysql), nil, "")))

		// nsfw_helper
		mtproto.RegisterRPCNsfwServer(
			grpcServer,
			nsfw_helper.New(nsfw_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UserClient:    c.BizServiceClient,
			}))

		// sponsoredmessages_helper
		mtproto.RegisterRPCSponsoredMessagesServer(
			grpcServer,
			sponsoredmessages_helper.New(sponsoredmessages_helper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		// account_helper
		mtproto.RegisterRPCAccountServer(
			grpcServer,
			account_helper.New(
				account_helper.Config{
					RpcServerConf:     c.RpcServerConf,
					KV:                c.KV,
					Mysql:             c.Mysql,
					LoginCode:         c.LoginCode,
					UserClient:        c.BizServiceClient,
					AuthsessionClient: c.AuthSessionClient,
					ChatClient:        c.BizServiceClient,
					SyncClient:        syncClient,
				},
				nil))

		// twofa_helper: the 2FA password and its settings (task 008).
		mtproto.RegisterRPCTwoFaServer(
			grpcServer,
			twofa_helper.New(twofa_helper.Config{
				RpcServerConf:     c.RpcServerConf,
				KV:                c.KV,
				Mysql:             c.Mysql,
				UserClient:        c.BizServiceClient,
				AuthsessionClient: c.AuthSessionClient,
				MsgClient:         c.MsgClient,
			}))

		// usernames_helper
		mtproto.RegisterRPCUsernamesServer(
			grpcServer,
			usernames_helper.New(usernames_helper.Config{
				RpcServerConf: c.RpcServerConf,
				UserClient:    c.BizServiceClient,
				ChatClient:    c.BizServiceClient,
				SyncClient:    syncClient,
				ChannelClient: c.BizServiceClient,
			}, nil))

		// privacysettingshelper
		mtproto.RegisterRPCPrivacySettingsServer(
			grpcServer,
			privacysettingshelper.New(privacysettingshelper.Config{
				RpcServerConf:     c.RpcServerConf,
				UserClient:        c.BizServiceClient,
				AuthsessionClient: c.AuthSessionClient,
				ChatClient:        c.BizServiceClient,
				DialogClient:      c.BizServiceClient,
				SyncClient:        syncClient,
			}))

		// savedmessagedialogshelper
		mtproto.RegisterRPCSavedMessageDialogsServer(
			grpcServer,
			savedmessagedialogshelper.New(savedmessagedialogshelper.Config{
				RpcServerConf: c.RpcServerConf,
				UpdatesClient: c.BizServiceClient,
				UserClient:    c.BizServiceClient,
				ChatClient:    c.BizServiceClient,
				DialogClient:  c.BizServiceClient,
				SyncClient:    syncClient,
				MessageClient: c.BizServiceClient,
				MsgClient:     c.MsgClient,
			}))

		// userchannelprofileshelper
		mtproto.RegisterRPCUserChannelProfilesServer(
			grpcServer,
			userchannelprofileshelper.New(userchannelprofileshelper.Config{
				RpcServerConf: c.RpcServerConf,
				MediaClient:   c.MediaClient,
				UserClient:    c.BizServiceClient,
				MsgClient:     c.MsgClient,
				SyncClient:    syncClient,
				KV:            c.KV,
			}))

		mtproto.RegisterRPCPasskeyServer(
			grpcServer,
			passkeyhelper.New(passkeyhelper.Config{
				RpcServerConf: c.RpcServerConf,
			}))

		mtproto.RegisterRPCWebBrowserServer(
			grpcServer,
			webbrowserhelper.New(webbrowserhelper.Config{
				RpcServerConf: c.RpcServerConf,
			}))
	})

	// logx.Must(err)

	go func() {
		s.grpcSrv.Start()
	}()
	return nil
}

func (s *Server) RunLoop() {
}

func (s *Server) Destroy() {
	s.grpcSrv.Stop()
}
