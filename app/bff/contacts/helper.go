/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2022 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package contacts_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/contacts/internal/svc"
	"github.com/teamgram/teamgram-server/app/bff/contacts/plugin"
)

type (
	Config = config.Config
)

// New returns the contacts.* methods; stories serves the blocklist of
// stories (my_stories_from), nil where the stories are not served.
func New(c Config, plugin plugin.ContactsPlugin, stories plugin.StoriesBlocklist) *service.Service {
	ctx := svc.NewServiceContext(c, plugin)
	ctx.Stories = stories
	return service.New(ctx)
}
