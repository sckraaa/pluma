/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2025 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/premium/internal/core"
)

// HelpGetPremiumPromo
// help.getPremiumPromo#b81b93d4 = help.PremiumPromo;
func (s *Service) HelpGetPremiumPromo(ctx context.Context, request *mtproto.TLHelpGetPremiumPromo) (*mtproto.Help_PremiumPromo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.getPremiumPromo - metadata: {%s}", c.MD)

	r, err := c.HelpGetPremiumPromo(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PaymentsAssignAppStoreTransaction
// payments.assignAppStoreTransaction#80ed747d receipt:bytes purpose:InputStorePaymentPurpose = Updates;
func (s *Service) PaymentsAssignAppStoreTransaction(ctx context.Context, request *mtproto.TLPaymentsAssignAppStoreTransaction) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("payments.assignAppStoreTransaction - metadata: {%s}", c.MD)

	r, err := c.PaymentsAssignAppStoreTransaction(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PaymentsAssignPlayMarketTransaction
// payments.assignPlayMarketTransaction#dffd50d3 receipt:DataJSON purpose:InputStorePaymentPurpose = Updates;
func (s *Service) PaymentsAssignPlayMarketTransaction(ctx context.Context, request *mtproto.TLPaymentsAssignPlayMarketTransaction) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("payments.assignPlayMarketTransaction - metadata: {%s}", c.MD)

	r, err := c.PaymentsAssignPlayMarketTransaction(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PaymentsCanPurchaseStore
// payments.canPurchaseStore#4fdc5ea7 purpose:InputStorePaymentPurpose = Bool;
func (s *Service) PaymentsCanPurchaseStore(ctx context.Context, request *mtproto.TLPaymentsCanPurchaseStore) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("payments.canPurchaseStore - metadata: {%s}", c.MD)

	r, err := c.PaymentsCanPurchaseStore(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PaymentsCanPurchasePremium
// payments.canPurchasePremium#9fc19eb6 purpose:InputStorePaymentPurpose = Bool;
func (s *Service) PaymentsCanPurchasePremium(ctx context.Context, request *mtproto.TLPaymentsCanPurchasePremium) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("payments.canPurchasePremium - metadata: {%s}", c.MD)

	r, err := c.PaymentsCanPurchasePremium(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
