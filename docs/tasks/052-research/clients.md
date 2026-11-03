# 052. How the official clients compress media before sending

Sources, read at the releases the Pluma client repos pin:

| Client | Checkout | Version | Pluma repo |
|---|---|---|---|
| Android | `/Users/sckr.a/pluma-work/reference-src/android` (DrKLO/Telegram `aac1efcb`) | 12.9.1, `gradle.properties:16-17` | `~/pluma-android`, `upstream.env` pins the same ref |
| iOS | `/Users/sckr.a/pluma-work/reference-src/ios` (`6ad963e5`) | 12.9.2, `versions.json` | `sckraaa/pluma-ios`, `upstream.env` `UPSTREAM_TAG=release-12.9.2` |
| Desktop | `/Users/sckr.a/pluma-work/reference-src/tdesktop` (`a1e89e1`) | 7.0.9, `core/version.h:25-26` | `sckraaa/pluma-desktop` (no local clone; patch mechanism not read) |
| Web K | `/Users/sckr.a/pluma-work/reference-src/tweb` (`346844bf`) | 2.2 build 676 | `~/pluma-web`, `upstream.env` pins the same ref |

Paths below are relative to `TMessagesProj/src/main/java/org/telegram/` (Android), `submodules/` (iOS), `Telegram/SourceFiles/` (tdesktop) and `src/` (tweb). Everything not read from source is marked UNVERIFIED.

## Summary

| | Re-encodes video before sending | User's quality choice | Top quality | Original possible |
|---|---|---|---|---|
| Android | yes (MediaCodec, H.264) | dot slider in PhotoViewer, 1-4 steps | source size at a capped bitrate (6.8 Mbit/s at 1080p) | only "send as file" |
| iOS | yes (AVFoundation, H.264 High) | 240p-1080p slider in the media editor | 1080p, 6.6 Mbit/s | only "send as file" |
| tdesktop | no, uploads the original | none for video | original | always |
| Web K | no, except `.mov` HEVC, GIF->mp4 and edited videos | quality slider only in the media editor | original | always |

So the quality of a video in Pluma today is decided by the Android and iOS encoders. tdesktop and Web K already send originals; they need no video patch.

## Android

### Bitrate formula: `messenger/MediaController.java`

- `makeVideoBitrate(originalHeight, originalWidth, originalBitrate, height, width)` (6766-6797) picks a tier by the shorter output side:

| tier (min side) | maxBitrate | compressFactor | minCompressFactor |
|---|---|---|---|
| >= 1080 | 6,800,000 | 1.0 | 1.0 |
| >= 720 | 2,600,000 | 1.0 | 1.0 |
| >= 480 | 1,000,000 | 0.75 | 0.9 |
| < 480 | 750,000 | 0.6 | 0.7 |

  `remeasured = originalBitrate / min(oh/h, ow/w) * compressFactor`; `minBitrate = minCompressFactor * 2,260,000 * (w*h / 921,600)`. If the source bitrate is below `minBitrate` the result is `remeasured`, otherwise `clamp(remeasured, minBitrate, maxBitrate)`.
- `VIDEO_BITRATE_1080/720/480/360` constants (922-925) are not used anywhere.
- `getVideoBitrateWithFactor(f) = f*2000f*1000f*1.13f` (6832-6834).

Resulting bitrates for a 1920x1080 source at 16 Mbit/s:

| output | Telegram Android |
|---|---|
| 478x270 | 750 kbit/s |
| 640x360 | 750 kbit/s |
| 852x480 | 1.0 Mbit/s |
| 1280x720 | 2.6 Mbit/s |
| 1920x1080 (from a 4K source) | 6.8 Mbit/s |

### Encoder: `messenger/video/MediaCodecVideoConvertor.java`

- `video/avc`; HEVC only for stories; VP9 for stickers (111). No `KEY_PROFILE` is set, so the device default profile is used (UNVERIFIED which).
- **`bitrate = Math.min(originalBitrate, bitrate);`** (427-429): the result never exceeds the source bitrate. This is what keeps a raised table from inflating already-small files.
- `KEY_BIT_RATE`, `max-bitrate`, `KEY_I_FRAME_INTERVAL = 1` (474-483); CBR only for avatars (477-480).
- Audio is copied when it is AAC; otherwise `video/AudioRecoder.java` re-encodes AAC 44.1 kHz stereo at `DEFAULT_BIT_RATE = 128000` (21-23, 65).
- Frame rate: 0 -> 25, >59 -> 59 (`MediaController.java:6662-6666`); above 40 fps with a short side <= 480 -> 30 (6674-6676).

### Quality slider: `ui/PhotoViewer.java`

- `QualityChooseView` (21047-21186) opens from `compressItem`; the button shows SD/HD with 240...4K (`ui/Components/VideoCompressButton.java:63-96`).
- `updateCompressionsCount` (21707-21718): >1280 -> 4 steps, >854 -> 3, >640 -> 2, else 1.
- `calculateResultVideoSize` (21403-21438): step 0 -> 480 long side, 1 -> 854, 2 -> 1280, 3 -> 1920; the last step at scale >= 1 keeps the source size.
- `updateWidthHeightBitrateForCompression` (21452-21484): `makeVideoBitrate`, or the source bitrate when the size is unchanged.
- `selectCompression` (21689-21705): **`int maxCompression = 2;`** (21696) caps both the default and the restored saved choice (`compress_video_%d`, saved in `didChangedCompressionLevel` 21233-21243) at step 2. A user who picks 1080p gets 720p preselected next time.

### Sending without the editor: `messenger/SendMessagesHelper.java`

- `createCompressionSettings` (11679-11777): step = `round(DownloadController.getMaxVideoBitrate()/(100f/count))` (11727-11731); compresses only if the file is under 1000 MB and the step is not the last or the long side > 1280 (11733-11734); `maxSize` per step 432 / 640 / 848 / **1280** (11736-11749).
- "Send as file" sets `videoEditedInfo = null`: no compression (10939-10941).

### Server lever: `video_upload_maxbitrate`

- `DownloadController.getMaxVideoBitrate()` (461-470) returns the percentage of the current preset; `Preset.set(TL_autoDownloadSettings)` copies `settings.video_upload_maxbitrate` (167). Built-in defaults: low 50, medium 100, high 100 (field 13 of `defaultLow/defaultMedium/defaultHigh`, 261-263).
- The "send in high quality" switches (`ui/Components/ChatAttachAlertPhotoLayout.java:3323-3352`, `PhotoViewer.java:7620-7630`) are for photos only.

### Round videos and GIFs

- `ui/Components/InstantCameraView.java`: H.264, 30 fps, I-frame every 1 s (2111-2114); size `roundVideoSize`, bitrate `roundVideoBitrate*1024` (2317-2326); AAC mono `roundAudioBitrate*1024` (3201-3206).
- Defaults (`messenger/MessagesController.java:1633-1635`): diameter 384, video 1000 (1,024,000 bit/s), audio 64; overridden by the app config key `round_video_encoding {diameter, video_bitrate, audio_bitrate}` (3258-3295). A server lever, no client patch needed.
- Muted video sent as GIF: bitrate -1 -> 921,600 in the convertor (424-426), 854 long side (`PhotoViewer.java:9818-9820`).
- Avatar video: 800 px, 2.6 / 2.2 / 1.56 Mbit/s by duration (21288-21295, 21460-21474).

## iOS

### Presets: two tables that must be patched together

- Enum `TGMediaVideoConversionPreset` (`LegacyComponents/PublicHeaders/LegacyComponents/TGVideoEditAdjustments.h:6-21`).
- Legacy table, `LegacyComponents/Sources/TGMediaVideoConverter.m`: `maximumSizeForPreset` 1269-1302, `_videoBitrateKbpsForPreset` 1408-1445, `_audioBitrateKbpsForPreset` 1447-1479, `_audioChannelsCountForPreset` 1481-1510. Used by conversion without adjustments, the size estimate and the slider.
- Modern table, `TelegramUI/Components/MediaEditor/Sources/MediaEditorValues.swift` `enum MediaQualityPreset` (129-228). Used by `MediaEditorVideoExport`, i.e. by the normal send path (`alwaysUseModernPipeline` is true unless the app config key `ios_killswitch_disable_modern_video_pipeline` is set, `TelegramAccountAuxiliaryMethods.swift:24-28, 37-41`).

| preset | label (`TGPhotoQualityController.m:895-911`) | max side | video kbit/s | audio kbit/s x channels |
|---|---|---|---|---|
| CompressedVeryLow | 240p | 480 | 400 | 32 x 1 |
| CompressedLow | 360p | 640 | 700 | 32 x 1 |
| CompressedMedium | 480p | 848 | 1600 | 64 x 2 |
| CompressedHigh | 720p | 1280 | 3000 | 64 x 2 |
| CompressedVeryHigh | 1080p | 1920 | 6600 | 64 x 2 |
| Animation | - | 848 | 900 | none |
| VideoMessage | - | 384 legacy / 400 modern | 1000 | 64 |
| Profile* | - | 720-800 | 1100-2400 | none |

Bitrates are fixed per preset, independent of the source bitrate (unlike Android).

### Encoder

- Legacy `videoSettingsForPreset` (`TGMediaVideoConverter.m:1328-1406`): H.264 High AutoLevel, CABAC, `AVVideoAverageBitRateKey`, BT.709, `useH265 = false` (1353); fps capped at 30 except VeryHigh (388-390).
- Modern `recommendedVideoExportConfiguration` (`MediaEditorValues.swift:1929-2050`): with a preset, size = source aspect-fitted to the preset's max side via `targetSize` (1911-1927, 16-px width blocks); bitrate and audio from the preset; `useHEVC = false` (1967-1983). `aspectFitted` can upscale (`Display/Source/UIKitUtils.swift:517-520`); whether an unedited small source is really upscaled under a high default preset is UNVERIFIED.

### How the user picks quality and the default

- Toolbar quality tab (`TGPhotoToolbarView.m:644-653`) opens `TGPhotoQualityController` (240p-1080p, max = `bestAvailablePresetForDimensions - 1`, `TGPhotoQualityController.m:135-138`); the choice is saved to `TG_preferredVideoPreset_v0` (`TGPhotoEditorController.m:2070-2077`).
- Opening the attachment menu overwrites that key with `defaultVideoPresetForContext` (`LegacyMediaPickerUI/Sources/LegacyAttachmentMenu.swift:312-313`, `LegacyMediaPickers.swift:84-85`), so the default is what the server says:
- `defaultVideoPresetForContext` (`LegacyAttachmentMenu.swift:14-53`): `videoUploadMaxbitrate` of the high preset on Wi-Fi, of the medium one otherwise; `v*5/100`: **0 -> Medium (480p)**, 1 VeryLow, 2 Low, 3 Medium, 4 High, 5 VeryHigh. Pluma's server sent 0 for every preset (`account.getAutoDownloadSettings`), so every Pluma iOS user started at 480p, 1.6 Mbit/s. Fixed on the server in this task (100 -> VeryHigh, capped by the source size in the picker).
- "Send as file" uses `.passthrough` (`LegacyMediaPickers.swift:907`; `FetchVideoMediaResource.swift:311-320`).

### Round videos and GIFs

- `Camera/Sources/CameraOutput.swift:369-383`: 400x400 H.264 High at `AVVideoAverageBitRateKey: 1000 * 1000`; send with `.videoMessage` (`VideoMessageCameraScreen.swift:1983`).
- GIF -> mp4: `TGGifConverter.m:103`, `AVVideoAverageBitRateKey: @(500000)`; "send as GIF" uses Animation (848 px, 900 kbit/s, `TGMediaVideoConverter.m:133`).

## tdesktop

- Videos are uploaded as they are: `FileLoadTask::process` (`storage/localimageloader.cpp:678-1043`) only reads them (`media/clip/media_clip_reader.cpp:966-1006`); `filedata` is set only for photos (`localimageloader.cpp:941`), documents and videos go as the original file (1025-1030). There is no encoder in the send path.
- Photos (`localimageloader.cpp`): long side 1280, or 2560 in "High Quality" (`PhotoSideLimit`, 205-212; the toggle is in the send menu, `menu/menu_send.cpp:780-786`, offered only for photos larger than 1280, `ui/chat/attach/attach_prepare.cpp:27, 202-208`); `ComputePhotoJpegBytes` (174-201) keeps an original JPEG that was not downscaled and is <= 4 bits per pixel, otherwise re-encodes at quality 87, progressive (195-196).
- Video thumbnail: first frame, <= 320 px, JPEG 87 (48-49, 63-95), uploaded only for files > 5 MB or unusual MIME types (97-123).
- GIF files are uploaded as is with `documentAttributeAnimated` (926-927); the server converts them (Pluma: dfs `uploadGifDocumentMedia`).
- Round video recorder (`ui/controls/round_video_recorder.cpp:29-35`): 400x400, 2 Mbit/s, AAC 64 kbit/s.
- Playback of server qualities: `DocumentData::setVideoQualities` / `chooseQuality` (`data/data_document.cpp:546-706`) from `messageMediaDocument.alt_documents` (`history/history_item.cpp:356-367`), default "start with SD" 480 (`core/core_settings.cpp:26, 163`), automatic switching (`media/streaming/media_streaming_document.cpp:186-244`). No HLS.

## Web K

- Videos are uploaded as they are (`components/popups/newMedia.tsx:1403-1445`), except: QuickTime `.mov` (H.264 remuxed, HEVC transcoded to H.264/AAC when <= 100 MB, `helpers/movToVideo.ts:41-114`); GIF -> silent mp4 (`helpers/gifToVideo.ts`); videos edited in the media editor (`components/mediaEditor/finalRender/calcCodecAndBitrate.ts:3-23`: 14 Mbit/s at 720p30, 20 Mbit/s at 1080p, scaled by pixels; quality slider 240-1080, `components/mediaEditor/utils.ts:107-127`). These are already well above Telegram's mobile encoders.
- Photos: `scaleImageForTelegram` (`newMedia.tsx:1110-1163`): up to 2560, JPEG quality 1.0 (0.9 for heavy PNG/BMP or > 6 MB); otherwise the original bytes.
- Round video (`helpers/videoRecorder/nativeVideoRecorder.ts:153-157`): 400x400, 1.2 Mbit/s, 64 kbit/s audio.
- Server qualities: plays `alt_documents` through HLS playlists (`application/x-mpegurl` documents named `mtproto:<id>`, `lib/hls/*`, `createHlsVideoSource.ts:13-42`); without them there is no quality menu.

## What Telegram does with uploaded videos on its side

- Sourced: core.telegram.org/api/files: for big channels "Telegram will automatically convert them, generating multiple versions in multiple qualities and formats", delivered in `messageMediaDocument.alt_documents`; such messages are sent asynchronously, like scheduled ones; `video_ignore_alt_documents` in the client config turns them off.
- Inferred from client parsers only (UNVERIFIED as server behaviour): the alternatives are H.264 (and sometimes AV1, `video_codec`) mp4 documents, one HLS playlist per quality named `mtproto:<mp4 id>`, and storyboard previews (tdesktop `data/data_document.cpp:91-99, 589-607`; Android `ui/Components/VideoPlayer.java:824-1068`; iOS `TelegramUniversalVideoContent/Sources/HLSVideoContent.swift:77-114, 410`; tweb `lib/hls/*`).
- Whether Telegram re-encodes ordinary uploads to private chats: UNVERIFIED; the clients' design (sending originals from tdesktop and Web K, 480p default in tdesktop playback) suggests the original is stored.

## Proposed Pluma tables

Goal from the owner: at every resolution, a visibly higher bitrate than Telegram. Proposal: about 1.8-2x Telegram's numbers, H.264 only (every client decodes it in hardware), audio raised where it is low.

| output | Telegram Android | Telegram iOS | Pluma (proposed) |
|---|---|---|---|
| 240p | 0.75 Mbit/s (<480 tier) | 0.4 Mbit/s, audio 32 mono | 0.8 Mbit/s, audio 64 x 2 |
| 360p | 0.75 Mbit/s (<480 tier) | 0.7 Mbit/s, audio 32 mono | 1.5 Mbit/s, audio 64 x 2 |
| 480p | 1.0 Mbit/s | 1.6 Mbit/s, audio 64 x 2 | 2.5 Mbit/s, audio 128 x 2 |
| 720p | 2.6 Mbit/s | 3.0 Mbit/s, audio 64 x 2 | 5.0 Mbit/s, audio 128 x 2 |
| 1080p | 6.8 Mbit/s | 6.6 Mbit/s, audio 64 x 2 | 12 Mbit/s, audio 128 x 2 |
| GIF / animation | 0.92 Mbit/s | 0.9 Mbit/s (0.5 for GIF files) | 1.8 Mbit/s (1.0 for GIF files) |
| round video | 1.0 Mbit/s, 384 px | 1.0 Mbit/s, 400 px | 2.0 Mbit/s, 400 px, audio 96 (Android: by server app config) |

File size check: one minute at 720p / 5 Mbit/s is about 38 MB, at 1080p / 12 Mbit/s about 90 MB, both under the 2000 MB upload limit and acceptable for ~100 users. Android keeps `Math.min(originalBitrate, bitrate)` (`MediaCodecVideoConvertor.java:427-429`), so a small source is never inflated.

## Patch plan (exact edit points)

### Android (`~/pluma-android/ci/patch.py`, `tree.replace(rel, old, new)`; each `old` below occurs once)

`TMessagesProj/src/main/java/org/telegram/messenger/MediaController.java`:
- `maxBitrate = 6800_000;` -> `maxBitrate = 12000_000;` (6771)
- `maxBitrate = 2600_000;` -> `maxBitrate = 5000_000;` (6775)
- `maxBitrate = 1000_000;` -> `maxBitrate = 2500_000;` (6779)
- `maxBitrate = 750_000;` -> `maxBitrate = 1500_000;` (6783)
- `            compressFactor = 0.75f;\n            minCompressFactor = 0.9f;` -> `1.0f` / `1.0f`; `            compressFactor = 0.6f;\n            minCompressFactor = 0.7f;` -> `1.0f` / `1.0f` (the low tiers stop cutting the source bitrate).
- Optionally `return (int) (f * 2000f * 1000f * 1.13f);` (6833) -> `f * 4000f * 1000f * 1.13f` (size estimates only).

`.../ui/PhotoViewer.java`: `int maxCompression = 2;` (21696) -> `int maxCompression = compressionsCount - 1;` so that a 1080p choice is remembered. Decision to confirm: this also makes 1080p the default for sources above 1280 px when no choice was saved.

`.../messenger/SendMessagesHelper.java`: `maxSize = 1280.0f;` (11747) -> `maxSize = 1920.0f;` (path without the editor, e.g. sharing from another app).

`.../messenger/video/MediaCodecVideoConvertor.java`: GIF bitrate `921600` at 424-426 -> `1843200` (read the exact text before writing the patch; the report gives only the line range).

`.../messenger/video/AudioRecoder.java`: `private final int DEFAULT_BIT_RATE = 128000;` (22): keep; 128 kbit/s stereo AAC is already transparent for speech and music in a phone video.

Round videos: no patch; the server sends `round_video_encoding` in `help.getAppConfig` (`MessagesController.java:3258-3295`).

### iOS (`sckraaa/pluma-ios ci/patch.py`, `Tree.replace` / `replace_lines`)

`submodules/LegacyComponents/Sources/TGMediaVideoConverter.m` (`_videoBitrateKbpsForPreset`, each once): `return 6600;` -> `return 12000;` (1425), `return 3000;` -> `return 5000;` (1422), `return 1600;` -> `return 2500;` (1419), `return 700;` -> `return 1500;` (1416), `return 400;` -> `return 800;` (1413), `return 900;` -> `return 1800;` (1443). Audio (`_audioBitrateKbpsForPreset` 1447-1479): `return 64;` occurs 4 times and `return 32;` twice, so edit by context with `replace_lines` (the blank lines inside the switch carry 12 trailing spaces).

`submodules/TelegramUI/Components/MediaEditor/Sources/MediaEditorValues.swift` (`MediaQualityPreset`): the same numbers, `            return 6600\n` etc. (each once); `return 1000` occurs twice (videoMessage and sticker) — change only the videoMessage one by context.

`submodules/Camera/Sources/CameraOutput.swift`: `AVVideoAverageBitRateKey: 1000 * 1000,` (once) -> `2000 * 1000` (round video recording).

`submodules/LegacyComponents/Sources/TGGifConverter.m:103`: `@(500000)` -> `@(1000000)`.

Default preset: no patch; `video_upload_maxbitrate = 100` from the server now selects VeryHigh (`LegacyAttachmentMenu.swift:14-53`). Check on a device that an unedited 720p source is sent at 720p, not upscaled (see the UNVERIFIED note above).

Optional, later: a 1440p/4K preset needs a new enum value, a label and a slider step in both tables and the UI; not proposed now.

### Desktop (`sckraaa/pluma-desktop`)

Video: nothing to change, originals are sent. Photos (if the owner wants photos above Telegram too): `storage/localimageloader.cpp` `PhotoSideLimit` (205-212) default 2560 instead of 1280, quality 87 -> 92 at 195-196. The patch mechanism of the repo was not read (no local clone).

### Web K (`~/pluma-web/ci/patch.py`, `tree.replace`)

Video: nothing to change. Optional: round video `helpers/videoRecorder/nativeVideoRecorder.ts:153-157` 1.2 -> 2 Mbit/s.
