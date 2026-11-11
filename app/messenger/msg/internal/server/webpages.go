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

package server

import (
	"context"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/config"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/pkg/webpage"
)

// sendWait is how long a message with a link waits for its card. Clients
// ask for the card while the user types (messages.getWebPagePreview), so
// it is usually stored already; a page slower than this goes without a
// card, and its fetch goes on for the next message with the link.
const sendWait = 4 * time.Second

// msgPlugin is the plugin of the msg service: the cards of links from
// pkg/webpage (task 023) and the users of a text's @usernames.
type msgPlugin struct {
	pages *webpage.Service
	users user_client.UserClient
}

func newMsgPlugin(c config.Config) *msgPlugin {
	return &msgPlugin{
		pages: webpage.NewService(sqlx.NewMySQL(&c.Mysql), webpage.RPCPhotos{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		}),
		users: user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
	}
}

// UsernameResolveUsername resolves an @username of a message to the peer it
// names, nil for a name nobody holds. The entity of an @username is made on
// the server, and an incoming message that mentions the account has the
// mentioned flag (core.telegram.org/api/mentions): the messageEntityMention
// of a user carries the user, and the message is marked mentioned for that
// user (mtproto.CheckHasMention). An error leaves the name unresolved: the
// entity stays, the message goes.
func (p *msgPlugin) UsernameResolveUsername(ctx context.Context, in *user.TLUserResolveUsername) (*mtproto.Peer, error) {
	peer, err := p.users.UserResolveUsername(ctx, in)
	if err != nil {
		// USERNAME_NOT_OCCUPIED for most: a name in a text is not always
		// someone's. The mention stays an entity without a user.
		return nil, nil //nolint:nilerr // an unresolved name is no error of the send
	}
	return peer, nil
}

// GetWebpagePreview returns the card of a message's link, nil if the page
// has none (webPageEmpty: the message goes as plain text) or none came in
// time.
func (p *msgPlugin) GetWebpagePreview(ctx context.Context, link string) (*mtproto.WebPage, error) {
	ctx, cancel := context.WithTimeout(ctx, sendWait)
	defer cancel()
	page, err := p.pages.Preview(ctx, link)
	if err != nil || page.GetPredicateName() != mtproto.Predicate_webPage {
		return nil, nil
	}
	return page, nil
}
