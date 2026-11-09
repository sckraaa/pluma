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
	"github.com/teamgram/proto/mtproto"
)

// PhoneGetCallConfig: the options of the call library. Android and iOS pass
// the answer to tgcalls as they get it (VoIPService.java:4542,
// ManagedVoipConfigurationUpdates.swift); "{}" is the library's defaults.
func (c *VoipCallsCore) PhoneGetCallConfig(in *mtproto.TLPhoneGetCallConfig) (*mtproto.DataJSON, error) {
	_ = in
	data := c.svcCtx.Config.Calls.ClientConfig
	if data == "" {
		data = "{}"
	}
	return mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: data}).To_DataJSON(), nil
}
