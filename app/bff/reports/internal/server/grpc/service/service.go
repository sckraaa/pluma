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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/reports/internal/svc"
)

// Service is the gRPC server of RPCReports.
type Service struct {
	svcCtx *svc.ServiceContext
}

// New returns the service.
func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// MessagesReportFC78AF9B
// messages.report#fc78af9b peer:InputPeer id:Vector<int> option:bytes message:string = ReportResult;
func (s *Service) MessagesReportFC78AF9B(ctx context.Context, request *mtproto.TLMessagesReportFC78AF9B) (*mtproto.ReportResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.report#fc78af9b - metadata: {%s}", c.MD)
	return c.MessagesReportFC78AF9B(request)
}

// MessagesReport8953AB4E
// messages.report#8953ab4e peer:InputPeer id:Vector<int> reason:ReportReason message:string = Bool;
func (s *Service) MessagesReport8953AB4E(ctx context.Context, request *mtproto.TLMessagesReport8953AB4E) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.report#8953ab4e - metadata: {%s}", c.MD)
	return c.MessagesReport8953AB4E(request)
}

// AccountReportPeer
// account.reportPeer#c5ba3d86 peer:InputPeer reason:ReportReason message:string = Bool;
func (s *Service) AccountReportPeer(ctx context.Context, request *mtproto.TLAccountReportPeer) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.reportPeer - metadata: {%s}", c.MD)
	return c.AccountReportPeer(request)
}

// AccountReportProfilePhoto
// account.reportProfilePhoto#fa8cc6f5 peer:InputPeer photo_id:InputPhoto reason:ReportReason message:string = Bool;
func (s *Service) AccountReportProfilePhoto(ctx context.Context, request *mtproto.TLAccountReportProfilePhoto) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.reportProfilePhoto - metadata: {%s}", c.MD)
	return c.AccountReportProfilePhoto(request)
}

// MessagesReportSpam
// messages.reportSpam#cf1592db peer:InputPeer = Bool;
func (s *Service) MessagesReportSpam(ctx context.Context, request *mtproto.TLMessagesReportSpam) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.reportSpam - metadata: {%s}", c.MD)
	return c.MessagesReportSpam(request)
}

// ChannelsReportSpam
// channels.reportSpam#f44a8315 channel:InputChannel participant:InputPeer id:Vector<int> = Bool;
func (s *Service) ChannelsReportSpam(ctx context.Context, request *mtproto.TLChannelsReportSpam) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.reportSpam - metadata: {%s}", c.MD)
	return c.ChannelsReportSpam(request)
}

// MessagesReportEncryptedSpam is not served: Pluma has no secret chats.
// messages.reportEncryptedSpam#4b0c8c0f peer:InputEncryptedChat = Bool;
func (s *Service) MessagesReportEncryptedSpam(ctx context.Context, request *mtproto.TLMessagesReportEncryptedSpam) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesReportReadMetrics is not served.
// messages.reportReadMetrics#4067c5e6 peer:InputPeer metrics:Vector<InputMessageReadMetric> = Bool;
func (s *Service) MessagesReportReadMetrics(ctx context.Context, request *mtproto.TLMessagesReportReadMetrics) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesReportMusicListen is not served.
// messages.reportMusicListen#ddbcd819 id:InputDocument listened_duration:int = Bool;
func (s *Service) MessagesReportMusicListen(ctx context.Context, request *mtproto.TLMessagesReportMusicListen) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
