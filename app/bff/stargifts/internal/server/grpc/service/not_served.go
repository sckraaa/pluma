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
)

// The methods of mtproto.RPCGifts the server does not serve: buying,
// upgrading, converting, moving and selling star gifts, which need payments
// (ADR 0016). They answer ERR_ENTERPRISE_IS_BLOCKED (400), as before the
// service was served here: a client gives up on a 400, while it retries an
// internal error.

func (s *Service) PaymentsGetStarGifts(ctx context.Context, request *mtproto.TLPaymentsGetStarGifts) (*mtproto.Payments_StarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsSaveStarGift(ctx context.Context, request *mtproto.TLPaymentsSaveStarGift) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsConvertStarGift(ctx context.Context, request *mtproto.TLPaymentsConvertStarGift) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftUpgradePreview(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftUpgradePreview) (*mtproto.Payments_StarGiftUpgradePreview, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsUpgradeStarGift(ctx context.Context, request *mtproto.TLPaymentsUpgradeStarGift) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsTransferStarGift(ctx context.Context, request *mtproto.TLPaymentsTransferStarGift) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetUniqueStarGift(ctx context.Context, request *mtproto.TLPaymentsGetUniqueStarGift) (*mtproto.Payments_UniqueStarGift, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetSavedStarGift(ctx context.Context, request *mtproto.TLPaymentsGetSavedStarGift) (*mtproto.Payments_SavedStarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftWithdrawalUrl(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftWithdrawalUrl) (*mtproto.Payments_StarGiftWithdrawalUrl, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsToggleChatStarGiftNotifications(ctx context.Context, request *mtproto.TLPaymentsToggleChatStarGiftNotifications) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsToggleStarGiftsPinnedToTop(ctx context.Context, request *mtproto.TLPaymentsToggleStarGiftsPinnedToTop) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetResaleStarGifts(ctx context.Context, request *mtproto.TLPaymentsGetResaleStarGifts) (*mtproto.Payments_ResaleStarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsUpdateStarGiftPrice(ctx context.Context, request *mtproto.TLPaymentsUpdateStarGiftPrice) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetUniqueStarGiftValueInfo(ctx context.Context, request *mtproto.TLPaymentsGetUniqueStarGiftValueInfo) (*mtproto.Payments_UniqueStarGiftValueInfo, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsCheckCanSendGift(ctx context.Context, request *mtproto.TLPaymentsCheckCanSendGift) (*mtproto.Payments_CheckCanSendGiftResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftAuctionState(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftAuctionState) (*mtproto.Payments_StarGiftAuctionState, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftAuctionAcquiredGifts(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftAuctionAcquiredGifts) (*mtproto.Payments_StarGiftAuctionAcquiredGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftActiveAuctions(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftActiveAuctions) (*mtproto.Payments_StarGiftActiveAuctions, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsResolveStarGiftOffer(ctx context.Context, request *mtproto.TLPaymentsResolveStarGiftOffer) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsSendStarGiftOffer(ctx context.Context, request *mtproto.TLPaymentsSendStarGiftOffer) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetStarGiftUpgradeAttributes(ctx context.Context, request *mtproto.TLPaymentsGetStarGiftUpgradeAttributes) (*mtproto.Payments_StarGiftUpgradeAttributes, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetCraftStarGifts(ctx context.Context, request *mtproto.TLPaymentsGetCraftStarGifts) (*mtproto.Payments_SavedStarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsCraftStarGift(ctx context.Context, request *mtproto.TLPaymentsCraftStarGift) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetUserStarGifts(ctx context.Context, request *mtproto.TLPaymentsGetUserStarGifts) (*mtproto.Payments_UserStarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (s *Service) PaymentsGetUserStarGift(ctx context.Context, request *mtproto.TLPaymentsGetUserStarGift) (*mtproto.Payments_UserStarGifts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
