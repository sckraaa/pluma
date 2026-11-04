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

package core

import (
	"strconv"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// Boosts (task 044): Pluma has no Premium boosts, so a channel has none and
// its level is 0, which is its real state. Android asks the status of every
// channel it opens and shows an error bulletin when the answer is an error
// (ChatActivity.java:22397-22428, ChannelBoostsController.java:58-60 at
// aac1efcb), and then the account's own boosts (getMyBoosts). Giving and
// listing boosts is not served (ERR_ENTERPRISE_IS_BLOCKED, 400, which the
// clients do not retry).

// nextLevelBoosts is the boosts of the next level a channel at level 0 is
// shown: without it Android takes the channel for one at its highest level
// and draws "Level -1" (ChannelBoostsController.java:73,
// LimitReachedBottomSheet.java:2206, LimitPreviewView.java:631-634 at
// aac1efcb). One boost for level 1 is the least that shows a level not yet
// reached; Telegram's thresholds are not verified.
const nextLevelBoosts = 1

// PremiumGetBoostsStatus
// premium.getBoostsStatus#42f1f61 peer:InputPeer = premium.BoostsStatus;
//
// The boosts of a channel the account may read: none, level 0. boost_url is
// the link Android opens boosts by: t.me/boost/<username> or
// t.me/boost?c=<id> (LaunchActivity.java:2119-2125 at aac1efcb), on this
// server's link host. Another peer is PEER_ID_INVALID.
func (c *ChannelsCore) PremiumGetBoostsStatus(in *mtproto.TLPremiumGetBoostsStatus) (*mtproto.Premium_BoostsStatus, error) {
	p := in.GetPeer()
	if p.GetPredicateName() != mtproto.Predicate_inputPeerChannel {
		return nil, mtproto.ErrPeerIdInvalid
	}
	mc, err := c.mutableChannel(mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: p.GetChannelId(), AccessHash: p.GetAccessHash()}).To_InputChannel())
	if err != nil {
		return nil, err
	}
	if err = readable(mc, c.MD.UserId); err != nil {
		return nil, err
	}
	url := "https://" + env2.TDotMe + "/boost?c=" + strconv.FormatInt(mc.GetChannel().GetId(), 10)
	if name := mc.GetChannel().GetUsername(); name != "" {
		url = "https://" + env2.TDotMe + "/boost/" + name
	}
	return mtproto.MakeTLPremiumBoostsStatus(&mtproto.Premium_BoostsStatus{
		Level:              0,
		CurrentLevelBoosts: 0,
		Boosts:             0,
		NextLevelBoosts:    mtproto.MakeFlagsInt32(nextLevelBoosts),
		BoostUrl:           url,
	}).To_Premium_BoostsStatus(), nil
}

// PremiumGetMyBoosts
// premium.getMyBoosts#be77b4a = premium.MyBoosts;
//
// The account's boost slots: none, as Pluma has no Premium boosts.
func (c *ChannelsCore) PremiumGetMyBoosts(in *mtproto.TLPremiumGetMyBoosts) (*mtproto.Premium_MyBoosts, error) {
	return mtproto.MakeTLPremiumMyBoosts(&mtproto.Premium_MyBoosts{
		MyBoosts: []*mtproto.MyBoost{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}).To_Premium_MyBoosts(), nil
}

// ChannelsSetBoostsToUnblockRestrictions is not served: channels.setBoostsToUnblockRestrictions.
func (c *ChannelsCore) ChannelsSetBoostsToUnblockRestrictions(in *mtproto.TLChannelsSetBoostsToUnblockRestrictions) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// PremiumGetBoostsList is not served: premium.getBoostsList.
func (c *ChannelsCore) PremiumGetBoostsList(in *mtproto.TLPremiumGetBoostsList) (*mtproto.Premium_BoostsList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// PremiumApplyBoost is not served: premium.applyBoost.
func (c *ChannelsCore) PremiumApplyBoost(in *mtproto.TLPremiumApplyBoost) (*mtproto.Premium_MyBoosts, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// PremiumGetUserBoosts is not served: premium.getUserBoosts.
func (c *ChannelsCore) PremiumGetUserBoosts(in *mtproto.TLPremiumGetUserBoosts) (*mtproto.Premium_BoostsList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StoriesGetBoostsStatus is not served: stories.getBoostsStatus.
func (c *ChannelsCore) StoriesGetBoostsStatus(in *mtproto.TLStoriesGetBoostsStatus) (*mtproto.Stories_BoostsStatus, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StoriesGetBoostersList is not served: stories.getBoostersList.
func (c *ChannelsCore) StoriesGetBoostersList(in *mtproto.TLStoriesGetBoostersList) (*mtproto.Stories_BoostersList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StoriesCanApplyBoost is not served: stories.canApplyBoost.
func (c *ChannelsCore) StoriesCanApplyBoost(in *mtproto.TLStoriesCanApplyBoost) (*mtproto.Stories_CanApplyBoostResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StoriesApplyBoost is not served: stories.applyBoost.
func (c *ChannelsCore) StoriesApplyBoost(in *mtproto.TLStoriesApplyBoost) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
