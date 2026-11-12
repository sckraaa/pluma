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
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
)

// Service is mtproto.RPCGroupCallsServer: every method of the service is
// served here, those of stage 1 (task 093) by core, the rest with
// ErrEnterpriseIsBlocked (core/not_served.go).
type Service struct {
	svcCtx *svc.ServiceContext
}

func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// PhoneCreateGroupCall
// phone.createGroupCall#48cdc6d8 flags:# rtmp_stream:flags.2?true peer:InputPeer random_id:int title:flags.0?string schedule_date:flags.1?int = Updates;
func (s *Service) PhoneCreateGroupCall(ctx context.Context, request *mtproto.TLPhoneCreateGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.createGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneCreateGroupCall(request)
	core.CountError("phone.createGroupCall", err)
	return r, err
}

// PhoneJoinGroupCall
// phone.joinGroupCall#8fb53057 flags:# muted:flags.0?true video_stopped:flags.2?true call:InputGroupCall join_as:InputPeer invite_hash:flags.1?string public_key:flags.3?int256 block:flags.3?bytes params:DataJSON = Updates;
func (s *Service) PhoneJoinGroupCall(ctx context.Context, request *mtproto.TLPhoneJoinGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.joinGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneJoinGroupCall(request)
	core.CountError("phone.joinGroupCall", err)
	return r, err
}

// PhoneLeaveGroupCall
// phone.leaveGroupCall#500377f9 call:InputGroupCall source:int = Updates;
func (s *Service) PhoneLeaveGroupCall(ctx context.Context, request *mtproto.TLPhoneLeaveGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.leaveGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneLeaveGroupCall(request)
	core.CountError("phone.leaveGroupCall", err)
	return r, err
}

// PhoneInviteToGroupCall
// phone.inviteToGroupCall#7b393160 call:InputGroupCall users:Vector<InputUser> = Updates;
func (s *Service) PhoneInviteToGroupCall(ctx context.Context, request *mtproto.TLPhoneInviteToGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.inviteToGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneInviteToGroupCall(request)
	core.CountError("phone.inviteToGroupCall", err)
	return r, err
}

// PhoneDiscardGroupCall
// phone.discardGroupCall#7a777135 call:InputGroupCall = Updates;
func (s *Service) PhoneDiscardGroupCall(ctx context.Context, request *mtproto.TLPhoneDiscardGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.discardGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneDiscardGroupCall(request)
	core.CountError("phone.discardGroupCall", err)
	return r, err
}

// PhoneToggleGroupCallSettings
// phone.toggleGroupCallSettings#974392f2 flags:# reset_invite_hash:flags.1?true call:InputGroupCall join_muted:flags.0?Bool messages_enabled:flags.2?Bool send_paid_messages_stars:flags.3?long = Updates;
func (s *Service) PhoneToggleGroupCallSettings(ctx context.Context, request *mtproto.TLPhoneToggleGroupCallSettings) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.toggleGroupCallSettings - metadata: {%s}", c.MD)
	r, err := c.PhoneToggleGroupCallSettings(request)
	core.CountError("phone.toggleGroupCallSettings", err)
	return r, err
}

// PhoneGetGroupCall
// phone.getGroupCall#41845db call:InputGroupCall limit:int = phone.GroupCall;
func (s *Service) PhoneGetGroupCall(ctx context.Context, request *mtproto.TLPhoneGetGroupCall) (*mtproto.Phone_GroupCall, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCall(request)
	core.CountError("phone.getGroupCall", err)
	return r, err
}

// PhoneGetGroupParticipants
// phone.getGroupParticipants#c558d8ab call:InputGroupCall ids:Vector<InputPeer> sources:Vector<int> offset:string limit:int = phone.GroupParticipants;
func (s *Service) PhoneGetGroupParticipants(ctx context.Context, request *mtproto.TLPhoneGetGroupParticipants) (*mtproto.Phone_GroupParticipants, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupParticipants - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupParticipants(request)
	core.CountError("phone.getGroupParticipants", err)
	return r, err
}

// PhoneCheckGroupCall
// phone.checkGroupCall#b59cf977 call:InputGroupCall sources:Vector<int> = Vector<int>;
func (s *Service) PhoneCheckGroupCall(ctx context.Context, request *mtproto.TLPhoneCheckGroupCall) (*mtproto.Vector_Int, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.checkGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneCheckGroupCall(request)
	core.CountError("phone.checkGroupCall", err)
	return r, err
}

// PhoneToggleGroupCallRecord
func (s *Service) PhoneToggleGroupCallRecord(ctx context.Context, request *mtproto.TLPhoneToggleGroupCallRecord) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.toggleGroupCallRecord - metadata: {%s}", c.MD)
	r, err := c.PhoneToggleGroupCallRecord(request)
	core.CountError("phone.toggleGroupCallRecord", err)
	return r, err
}

// PhoneEditGroupCallParticipant
// phone.editGroupCallParticipant#a5273abf flags:# call:InputGroupCall participant:InputPeer muted:flags.0?Bool volume:flags.1?int raise_hand:flags.2?Bool video_stopped:flags.3?Bool video_paused:flags.4?Bool presentation_paused:flags.5?Bool = Updates;
func (s *Service) PhoneEditGroupCallParticipant(ctx context.Context, request *mtproto.TLPhoneEditGroupCallParticipant) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.editGroupCallParticipant - metadata: {%s}", c.MD)
	r, err := c.PhoneEditGroupCallParticipant(request)
	core.CountError("phone.editGroupCallParticipant", err)
	return r, err
}

// PhoneEditGroupCallTitle
// phone.editGroupCallTitle#1ca6ac0a call:InputGroupCall title:string = Updates;
func (s *Service) PhoneEditGroupCallTitle(ctx context.Context, request *mtproto.TLPhoneEditGroupCallTitle) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.editGroupCallTitle - metadata: {%s}", c.MD)
	r, err := c.PhoneEditGroupCallTitle(request)
	core.CountError("phone.editGroupCallTitle", err)
	return r, err
}

// PhoneGetGroupCallJoinAs
// phone.getGroupCallJoinAs#ef7c213a peer:InputPeer = phone.JoinAsPeers;
func (s *Service) PhoneGetGroupCallJoinAs(ctx context.Context, request *mtproto.TLPhoneGetGroupCallJoinAs) (*mtproto.Phone_JoinAsPeers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCallJoinAs - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCallJoinAs(request)
	core.CountError("phone.getGroupCallJoinAs", err)
	return r, err
}

// PhoneExportGroupCallInvite
// phone.exportGroupCallInvite#e6aa647f flags:# can_self_unmute:flags.0?true call:InputGroupCall = phone.ExportedGroupCallInvite;
func (s *Service) PhoneExportGroupCallInvite(ctx context.Context, request *mtproto.TLPhoneExportGroupCallInvite) (*mtproto.Phone_ExportedGroupCallInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.exportGroupCallInvite - metadata: {%s}", c.MD)
	r, err := c.PhoneExportGroupCallInvite(request)
	core.CountError("phone.exportGroupCallInvite", err)
	return r, err
}

// PhoneToggleGroupCallStartSubscription
// phone.toggleGroupCallStartSubscription#219c34e6 call:InputGroupCall subscribed:Bool = Updates;
func (s *Service) PhoneToggleGroupCallStartSubscription(ctx context.Context, request *mtproto.TLPhoneToggleGroupCallStartSubscription) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.toggleGroupCallStartSubscription - metadata: {%s}", c.MD)
	r, err := c.PhoneToggleGroupCallStartSubscription(request)
	core.CountError("phone.toggleGroupCallStartSubscription", err)
	return r, err
}

// PhoneStartScheduledGroupCall
// phone.startScheduledGroupCall#5680e342 call:InputGroupCall = Updates;
func (s *Service) PhoneStartScheduledGroupCall(ctx context.Context, request *mtproto.TLPhoneStartScheduledGroupCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.startScheduledGroupCall - metadata: {%s}", c.MD)
	r, err := c.PhoneStartScheduledGroupCall(request)
	core.CountError("phone.startScheduledGroupCall", err)
	return r, err
}

// PhoneSaveDefaultGroupCallJoinAs
// phone.saveDefaultGroupCallJoinAs#575e1f8c peer:InputPeer join_as:InputPeer = Bool;
func (s *Service) PhoneSaveDefaultGroupCallJoinAs(ctx context.Context, request *mtproto.TLPhoneSaveDefaultGroupCallJoinAs) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.saveDefaultGroupCallJoinAs - metadata: {%s}", c.MD)
	r, err := c.PhoneSaveDefaultGroupCallJoinAs(request)
	core.CountError("phone.saveDefaultGroupCallJoinAs", err)
	return r, err
}

// PhoneJoinGroupCallPresentation
// phone.joinGroupCallPresentation#cbea6bc4 call:InputGroupCall params:DataJSON = Updates;
func (s *Service) PhoneJoinGroupCallPresentation(ctx context.Context, request *mtproto.TLPhoneJoinGroupCallPresentation) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.joinGroupCallPresentation - metadata: {%s}", c.MD)
	r, err := c.PhoneJoinGroupCallPresentation(request)
	core.CountError("phone.joinGroupCallPresentation", err)
	return r, err
}

// PhoneLeaveGroupCallPresentation
// phone.leaveGroupCallPresentation#1c50d144 call:InputGroupCall = Updates;
func (s *Service) PhoneLeaveGroupCallPresentation(ctx context.Context, request *mtproto.TLPhoneLeaveGroupCallPresentation) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.leaveGroupCallPresentation - metadata: {%s}", c.MD)
	r, err := c.PhoneLeaveGroupCallPresentation(request)
	core.CountError("phone.leaveGroupCallPresentation", err)
	return r, err
}

// PhoneGetGroupCallStreamChannels
func (s *Service) PhoneGetGroupCallStreamChannels(ctx context.Context, request *mtproto.TLPhoneGetGroupCallStreamChannels) (*mtproto.Phone_GroupCallStreamChannels, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCallStreamChannels - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCallStreamChannels(request)
	core.CountError("phone.getGroupCallStreamChannels", err)
	return r, err
}

// PhoneGetGroupCallStreamRtmpUrl
func (s *Service) PhoneGetGroupCallStreamRtmpUrl(ctx context.Context, request *mtproto.TLPhoneGetGroupCallStreamRtmpUrl) (*mtproto.Phone_GroupCallStreamRtmpUrl, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCallStreamRtmpUrl - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCallStreamRtmpUrl(request)
	core.CountError("phone.getGroupCallStreamRtmpUrl", err)
	return r, err
}

// PhoneSendGroupCallMessageB1D11410
func (s *Service) PhoneSendGroupCallMessageB1D11410(ctx context.Context, request *mtproto.TLPhoneSendGroupCallMessageB1D11410) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.sendGroupCallMessage - metadata: {%s}", c.MD)
	r, err := c.PhoneSendGroupCallMessageB1D11410(request)
	core.CountError("phone.sendGroupCallMessage", err)
	return r, err
}

// PhoneDeleteGroupCallMessages
func (s *Service) PhoneDeleteGroupCallMessages(ctx context.Context, request *mtproto.TLPhoneDeleteGroupCallMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.deleteGroupCallMessages - metadata: {%s}", c.MD)
	r, err := c.PhoneDeleteGroupCallMessages(request)
	core.CountError("phone.deleteGroupCallMessages", err)
	return r, err
}

// PhoneDeleteGroupCallParticipantMessages
func (s *Service) PhoneDeleteGroupCallParticipantMessages(ctx context.Context, request *mtproto.TLPhoneDeleteGroupCallParticipantMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.deleteGroupCallParticipantMessages - metadata: {%s}", c.MD)
	r, err := c.PhoneDeleteGroupCallParticipantMessages(request)
	core.CountError("phone.deleteGroupCallParticipantMessages", err)
	return r, err
}

// PhoneGetGroupCallStars
func (s *Service) PhoneGetGroupCallStars(ctx context.Context, request *mtproto.TLPhoneGetGroupCallStars) (*mtproto.Phone_GroupCallStars, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCallStars - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCallStars(request)
	core.CountError("phone.getGroupCallStars", err)
	return r, err
}

// PhoneSaveDefaultSendAs
func (s *Service) PhoneSaveDefaultSendAs(ctx context.Context, request *mtproto.TLPhoneSaveDefaultSendAs) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.saveDefaultSendAs - metadata: {%s}", c.MD)
	r, err := c.PhoneSaveDefaultSendAs(request)
	core.CountError("phone.saveDefaultSendAs", err)
	return r, err
}

// PhoneSendGroupCallMessage87893014
func (s *Service) PhoneSendGroupCallMessage87893014(ctx context.Context, request *mtproto.TLPhoneSendGroupCallMessage87893014) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.sendGroupCallMessage - metadata: {%s}", c.MD)
	r, err := c.PhoneSendGroupCallMessage87893014(request)
	core.CountError("phone.sendGroupCallMessage", err)
	return r, err
}
