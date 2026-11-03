// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
)

// video_upload_maxbitrate of the presets, in percent of the best quality a
// client offers for a video it compresses before sending. The values are the
// built-in defaults of the official Android client (DownloadController:
// defaultLow/defaultMedium/defaultHigh, field 13: 50, 100, 100). 0, sent
// before, is not "no limit": iOS maps it to its 480p preset
// (LegacyAttachmentMenu.defaultVideoPresetForContext), and Android picks its
// lowest compression step for a preset that carries it.
const (
	videoUploadMaxBitrateLow    = 50
	videoUploadMaxBitrateMedium = 100
	videoUploadMaxBitrateHigh   = 100
)

// AccountGetAutoDownloadSettings
// account.getAutoDownloadSettings#56da0b3f = account.AutoDownloadSettings;
func (c *AutoDownloadCore) AccountGetAutoDownloadSettings(in *mtproto.TLAccountGetAutoDownloadSettings) (*mtproto.Account_AutoDownloadSettings, error) {
	makeAutoDownloadSettings := func(disabled, videoPreloadLarge, audioPreloadNext, phonecallsLessData bool, photoSizeMax, videoSizeMax, fileSizeMax, videoUploadMaxBitrate int32) *mtproto.AutoDownloadSettings {
		return mtproto.MakeTLAutoDownloadSettings(&mtproto.AutoDownloadSettings{
			Disabled:              disabled,
			VideoPreloadLarge:     videoPreloadLarge,
			AudioPreloadNext:      audioPreloadNext,
			PhonecallsLessData:    phonecallsLessData,
			PhotoSizeMax:          photoSizeMax,
			VideoSizeMax_INT32:    videoSizeMax,
			VideoSizeMax_INT64:    int64(videoSizeMax),
			FileSizeMax_INT32:     fileSizeMax,
			FileSizeMax_INT64:     int64(fileSizeMax),
			VideoUploadMaxbitrate: videoUploadMaxBitrate,
		}).To_AutoDownloadSettings()
	}

	return mtproto.MakeTLAccountAutoDownloadSettings(&mtproto.Account_AutoDownloadSettings{
		Low: makeAutoDownloadSettings(
			false,
			true,
			true,
			true,
			1048576,
			512000,
			512000,
			videoUploadMaxBitrateLow),
		Medium: makeAutoDownloadSettings(
			false,
			true,
			true,
			false,
			1048576,
			10485760,
			1048576,
			videoUploadMaxBitrateMedium),
		High: makeAutoDownloadSettings(
			false,
			true,
			true,
			false,
			1048576,
			15728640,
			3145728,
			videoUploadMaxBitrateHigh),
	}).To_Account_AutoDownloadSettings(), nil
}
