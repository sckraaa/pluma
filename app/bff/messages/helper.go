/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2022 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package messages_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/svc"
	"github.com/teamgram/teamgram-server/app/bff/messages/plugin"
)

type (
	Config = config.Config
)

// New returns the messages.* methods; stories answers for the stories a
// message shares or replies to (nil: none are served).
func New(c Config, plugin plugin.MessagesPlugin, stories plugin.StoryPlugin) *service.Service {
	return service.New(svc.NewServiceContext(c, plugin, stories))
}
