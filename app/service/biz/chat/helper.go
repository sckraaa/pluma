/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package chat_helper

import (
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dao/mysql_dao"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/plugin"
)

type (
	Config = config.Config
	Topics = config.Topics
	// UserSource looks up the users of the operator's own updates with
	// seq: biz user.
	UserSource = dao.UserSource
)

// MaxBoxOpTime is the longest a box operation of the service takes: the
// deadline of the server must cover it.
const MaxBoxOpTime = dao.MaxBoxOpTime

type (
	ChatParticipantsDAO = mysql_dao.ChatParticipantsDAO
	ChatParticipantsDO  = dataobject.ChatParticipantsDO
)

var (
	NewChatParticipantsDAO = mysql_dao.NewChatParticipantsDAO
)

// New returns the chat service; users looks up the users of the operator's
// own updates with seq (the biz server's user service).
func New(c Config, plugin plugin.ChatPlugin, users UserSource) *service.Service {
	return service.New(svc.NewServiceContext(c, plugin, users))
}
