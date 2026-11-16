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
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerlists"
)

// AccountUpdateBusinessIntro
// account.updateBusinessIntro#a614d034 flags:# intro:flags.0?InputBusinessIntro = Bool;
//
// Sets the intro of the account, what the empty chat with it shows to
// others in place of the default: a title, a description and maybe a
// sticker (tdesktop data/business/data_business_info.cpp:108-128). Without
// intro, or with nothing in it, it removes the intro (an intro with
// neither text nor sticker is none, tdesktop ChatIntro::operator bool,
// data/business/data_business_common.h:211-223). The texts are held to
// intro_title_length_limit and intro_description_length_limit of the app
// config, in UTF-16 code units as the clients count them: TITLE_INVALID,
// DESCRIPTION_INVALID (Telegram publishes no error of this method; the
// names are this server's). The sticker must be a sticker document the
// client may name (its access hash), DOCUMENT_INVALID otherwise.
func (c *BusinessCore) AccountUpdateBusinessIntro(in *mtproto.TLAccountUpdateBusinessIntro) (*mtproto.Bool, error) {
	const method = "account.updateBusinessIntro"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	intro, err := c.introOf(method, in.GetIntro())
	if err != nil {
		return nil, err
	}
	if err := c.saveProfile(method, func(tx *sqlx.Tx, now int64) error {
		return business.SetIntroTx(tx, c.MD.UserId, intro, now)
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// introOf checks in and returns it as it is kept, nil for no intro.
func (c *BusinessCore) introOf(method string, in *mtproto.InputBusinessIntro) (*business.Intro, error) {
	if in == nil {
		return nil, nil
	}
	title, description := in.GetTitle(), in.GetDescription()
	if !utf8.ValidString(title) || business.UTF16Length(title) > business.IntroTitleLengthLimit {
		err := mtproto.ErrTitleInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if !utf8.ValidString(description) || business.UTF16Length(description) > business.IntroDescriptionLengthLimit {
		err := mtproto.ErrDescriptionInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	intro := &business.Intro{Title: title, Description: description}
	switch s := in.GetSticker(); s.GetPredicateName() {
	case "", mtproto.Predicate_inputDocumentEmpty:
	case mtproto.Predicate_inputDocument:
		doc, err := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: s.GetId()})
		if err != nil {
			c.Logger.Errorf("%s - media: %v", method, err)
			return nil, err
		}
		if stickerlists.StickerAttribute(doc) == nil || doc.GetAccessHash() != s.GetAccessHash() {
			err = mtproto.ErrDocumentInvalid
			c.Logger.Errorf("%s - document %d: %v", method, s.GetId(), err)
			return nil, err
		}
		intro.StickerID = s.GetId()
	default:
		err := mtproto.ErrDocumentInvalid
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if intro.Title == "" && intro.Description == "" && intro.StickerID == 0 {
		return nil, nil
	}
	return intro, nil
}
