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

package langpackclient

import (
	"context"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/zrpc"
)

var _ *mtproto.Bool

type LangpackClient interface {
	LangpackGetLangPack(ctx context.Context, in *mtproto.TLLangpackGetLangPack) (*mtproto.LangPackDifference, error)
	LangpackGetStrings(ctx context.Context, in *mtproto.TLLangpackGetStrings) (*mtproto.Vector_LangPackString, error)
	LangpackGetDifference(ctx context.Context, in *mtproto.TLLangpackGetDifference) (*mtproto.LangPackDifference, error)
	LangpackGetLanguages(ctx context.Context, in *mtproto.TLLangpackGetLanguages) (*mtproto.Vector_LangPackLanguage, error)
	LangpackGetLanguage(ctx context.Context, in *mtproto.TLLangpackGetLanguage) (*mtproto.LangPackLanguage, error)
}

type defaultLangpackClient struct {
	cli zrpc.Client
}

func NewLangpackClient(cli zrpc.Client) LangpackClient {
	return &defaultLangpackClient{
		cli: cli,
	}
}

// LangpackGetLangPack
// langpack.getLangPack#f2f2330a lang_pack:string lang_code:string = LangPackDifference;
// langpack.getLangPack#9ab5c58e lang_code:string = LangPackDifference;
func (m *defaultLangpackClient) LangpackGetLangPack(ctx context.Context, in *mtproto.TLLangpackGetLangPack) (*mtproto.LangPackDifference, error) {
	client := mtproto.NewRPCLangpackClient(m.cli.Conn())
	return client.LangpackGetLangPack(ctx, in)
}

// LangpackGetStrings
// langpack.getStrings#efea3803 lang_pack:string lang_code:string keys:Vector<string> = Vector<LangPackString>;
// langpack.getStrings#2e1ee318 lang_code:string keys:Vector<string> = Vector<LangPackString>;
func (m *defaultLangpackClient) LangpackGetStrings(ctx context.Context, in *mtproto.TLLangpackGetStrings) (*mtproto.Vector_LangPackString, error) {
	client := mtproto.NewRPCLangpackClient(m.cli.Conn())
	return client.LangpackGetStrings(ctx, in)
}

// LangpackGetDifference
// langpack.getDifference#cd984aa5 lang_pack:string lang_code:string from_version:int = LangPackDifference;
func (m *defaultLangpackClient) LangpackGetDifference(ctx context.Context, in *mtproto.TLLangpackGetDifference) (*mtproto.LangPackDifference, error) {
	client := mtproto.NewRPCLangpackClient(m.cli.Conn())
	return client.LangpackGetDifference(ctx, in)
}

// LangpackGetLanguages
// langpack.getLanguages#42c6978f lang_pack:string = Vector<LangPackLanguage>;
// langpack.getLanguages#800fd57d = Vector<LangPackLanguage>;
func (m *defaultLangpackClient) LangpackGetLanguages(ctx context.Context, in *mtproto.TLLangpackGetLanguages) (*mtproto.Vector_LangPackLanguage, error) {
	client := mtproto.NewRPCLangpackClient(m.cli.Conn())
	return client.LangpackGetLanguages(ctx, in)
}

// LangpackGetLanguage
// langpack.getLanguage#6a596502 lang_pack:string lang_code:string = LangPackLanguage;
func (m *defaultLangpackClient) LangpackGetLanguage(ctx context.Context, in *mtproto.TLLangpackGetLanguage) (*mtproto.LangPackLanguage, error) {
	client := mtproto.NewRPCLangpackClient(m.cli.Conn())
	return client.LangpackGetLanguage(ctx, in)
}
