# Call quality: video

Read from the client sources pinned in upstream.env of each client. `Verified: no` marks what was not confirmed from a source.

## Camera send cap in 1:1 (negotiated engine InstanceV2Impl, library 9.0.0, or 13.0.0 with Web K)
- Today: 1000*1024 = 1,024,000 bps, set as encodings[0].max_bitrate_bps. It is a literal; no config or custom parameter reaches it.
- Where: Old copy TG/v2/InstanceV2Impl.cpp:2162-2168, applied by setMaxBitrate :659-668, called at :1190, :1586, :2033. Raw reads at the pins, with line numbers: tgcalls@545f5ae :2136-2141 (Android), @1c236c09 :2145-2150 (tdesktop), @c1de0882 :2134-2139 (iOS).
- Change via: client patch
- Change: Patch adjustBitratePreferences in tgcalls: raise the literal (audit C3 proposes 2.5-4 Mbit/s), or read a new integer key from _customParameters (member at @1c236c09 :2204; helper getCustomParameterInt at v2/CustomParameters.h:38). The cap is applied by the sender only, so no signaling change is needed.
- Effect: Removes the only explicit limit on the camera. WebRTC's own default for 1280x720 is 2500 kbps (WR/video/config/encoder_stream_factory.cc:79-96), so the stock cap is 40% of it. How much sharper the picture gets is a judgement until measured; the earlier wording that 720p is starved at 1 Mbit/s was not supported by a read source. The bandwidth estimator still limits it on bad links.
- Risk: More data on mobile and through TURN, and the engine ignores network type and data saving today (see that knob), so a raised cap applies on cellular too. A server-driven value also needs tdesktop to forward custom_parameters.
- Verified: yes

## Screen-share send cap in 1:1
- Today: No explicit cap. adjustBitratePreferences touches only the audio and camera channels; setMaxBitrate is never called on _outgoingScreencastChannel. WebRTC's default applies by frame size: 2500 kbps above 960x540, 2000 up to 960x540, 1700 up to 640x480, with a 1200 floor for screens. This corrects docs/audit/06-call-quality.md lines 19 and 64, which say the screen is also limited to 1-1.2 Mbit/s.
- Where: TG/v2/InstanceV2Impl.cpp:2162-2168; the pins show the same body (@545f5ae :2136-2141, @1c236c09 :2145-2150, @c1de0882 :2134-2139), and a grep of each pin finds _outgoingScreencastChannel used only with setVideoCapture. WR/media/engine/webrtc_video_engine.cc:2158; WR/video/config/encoder_stream_factory.cc:79-96, 158-165, 186-199
- Change via: client patch
- Change: Add _outgoingScreencastChannel->setMaxBitrate(...) in adjustBitratePreferences, for example 4-6 Mbit/s, ideally from the same server-driven parameter. It already runs at the end of channel creation (:1586).
- Effect: Sharper 1080p text and motion once capture limits are also raised. Audit C3 targets 1080p at 15-30 fps.
- Risk: Software encoder load on desktop at higher rates. Inferred from reading the code, not observed in a running call.
- Verified: yes

## Library version the server negotiates, which selects the media engine
- Today: Negotiate picks the greatest common version compared as strings, so 9.0.0 between native clients (the string 9.0.0 sorts above 13.0.0, 18.0.0 and 19.0.0) and 13.0.0 with Web K, whose only version is 13.0.0. Both run InstanceV2Impl: 1000 kbps camera, working screen share.
- Where: teamgram-server origin/master pkg/phonecalls/phonecalls.go:124-148; tweb src/lib/calls/p2P/getCallProtocol.ts:13; TG/v2/InstanceV2Impl.cpp:2376-2382 (same list at @545f5ae); TG/v2/InstanceV2ReferenceImpl.cpp GetVersions (10.0.0, 11.0.0), :1329, :1378, at @1c236c09 :1548-1549, :1600, :718-720; registries in tdesktop calls_call.cpp:73-75, Android org_telegram_messenger_voip_Instance.cpp:31-35, iOS OngoingCallThreadLocalContext.mm:1852-1859; @545f5ae v2/InstanceV2CompatImpl.cpp:1615; v2wasm/InstanceV2PumpImpl.cpp:95-102; v2wasm/ReferenceCallCore.cpp:20, 322-327
- Change via: server
- Change: Could force 10.0.0 or 11.0.0 (InstanceV2ReferenceImpl, 1200*1024). Not recommended: this is the only video-related setting the server can pick, and it is a poor trade.
- Effect: Camera cap rises 20%, from 1,024,000 to 1,228,800 bps.
- Risk: Breaks screen sharing: the Reference setVideoCapture has an empty branch for screen capture. Compat 14.0.0 and core 18.0.0/19.0.0 also add no track for a screen. Reference skips isTcp servers. The earlier note that it drops non-IP hosts is out of date at @1c236c09, where network_reference_allow_hostname_ice_servers defaults to true (:714). tdesktop registers none of the 14/18/19 engines.
- Verified: yes

## phone.getCallConfig JSON as a way to control video
- Today: No video effect on any native client. Android parses enable_vp8/vp9/h264/h265_encoder and _decoder keys, but a grep over all of TMessagesProj/src and jni/voip finds them only in Instance.java (the org.webrtc factories do not read them), and the native setGlobalServerConfig body is commented out. iOS applyServerConfig is empty. tdesktop UpdateConfig is empty. The server answers {} by default.
- Where: reference-src-new: android .../voip/Instance.java:438-463; android jni/voip/org_telegram_messenger_voip_Instance.cpp:1014-1016; android org/webrtc/HardwareVideoEncoderFactory.java and MediaCodecVideoDecoderFactory.java (no reference); ios submodules/TgVoipWebrtc/Sources/OngoingCallThreadLocalContext.mm:1824; tdesktop calls_call.cpp:1723; teamgram-server app/bff/voipcalls/internal/core/phone.getCallConfig_handler.go:25
- Change via: not possible
- Change: None on stock clients. A patched client could read this JSON, but custom_parameters on the call is the better channel because it is per call.
- Effect: None today for video.
- Risk: Spending time tuning keys that nothing consumes. The handler's comment says Android and iOS pass the answer to tgcalls; on Android only the Java side reads it, and on iOS it is discarded.
- Verified: yes

## phoneCall.custom_parameters (per-call JSON from the server)
- Today: The server does not send it (no CustomParameters in app/bff/voipcalls or pkg/phonecalls). Android and iOS pass it to tgcalls; tdesktop's tgcalls::Config omits it; Web K already has a reader. The engine reads only network_* keys: network_kcp_experiment, network_signaling_nosctp, network_standalone_reflectors, network_use_mtproto, network_use_default_route, network_enable_shared_socket, network_skip_initial_ping, network_reflector_resolve_remote_candidate_ip, and at @1c236c09 also network_sctp_* and network_reference_allow_hostname_ice_servers. No key touches bitrate, codec, resolution or fps.
- Where: TG/v2/InstanceV2Impl.cpp:949-958; @1c236c09 v2/InstanceV2Impl.cpp:938-947, 983-991, v2/NativeNetworkingImpl.cpp:514-661, v2/InstanceV2ReferenceImpl.cpp:714; android VoIPService.java:3529; ios TelegramCallsUI/Sources/PresentationCall.swift:1040 (inline_conference only), :1225, OngoingCallThreadLocalContext.mm:2001-2023; tdesktop calls_call.cpp:1158-1169; tweb src/lib/calls/callInstance.ts:813-824
- Change via: client patch
- Change: Make this the transport for video limits: the server sends for example {video_max_bitrate_kbps, screen_max_bitrate_kbps, start_bitrate_kbps}; tgcalls reads them; tdesktop adds .customParameters to its Config; Web K uses getCustomParam.
- Effect: Limits become tunable per call, country or network without a client release, as ADR 0004 intends.
- Risk: Unknown keys are ignored by the map lookups read here. The three clients pin three different tgcalls commits, so the patch has to land on each or the pins have to be unified.
- Verified: yes

## Start bitrate and first seconds of the camera
- Today: Start 300 kbps (WebRTC default; tgcalls sets nothing in CallConfig and sends no x-google-start-bitrate). WebRTC itself calls SetSdpBitrateParameters with start -1 when the codec has no such parameter. Below 500 kbps a 720p frame counts as too large: up to 4 initial frames are dropped and resolution is stepped down, for the camera only, since screen share has no quality scaler. The estimator probes at 3x and 6x the start, so the rate itself recovers quickly; the resolution step-down is the slower part. Encoder minimum 30 kbps. The legacy 64/400/2000 values reach only MediaManager.
- Where: WR/api/transport/bitrate_settings.h:41-45; WR/media/engine/webrtc_media_engine.cc:161-184; WR/media/engine/webrtc_video_engine.cc:910, 1251-1273; WR/video/video_stream_encoder.cc:1815, 2334-2362; WR/video/adaptation/video_stream_encoder_resource_manager.cc:231-244, 489-497; WR/modules/congestion_controller/goog_cc/probe_controller.cc:87-88; WR/rtc_base/experiments/min_video_bitrate_experiment.cc:22; TG/MediaManager.cpp:786-788, 860-861; TG/v2/InstanceV2Impl.cpp:1163-1165, 474-478
- Change via: client patch
- Change: Start at 500 kbps or more where the network allows (800-1200 kbps on Wi-Fi is a target to measure). CallConfig.bitrate_config covers only the start of the call; a camera turned on mid-call needs x-google-start-bitrate on the codec in OutgoingVideoChannel (:476-478 copy payload parameters), which WebRTC applies when the send codec changes.
- Effect: Avoids the initial frame drops and the resolution step-down at 720p, in addition to more bits at the start. Read from the code, not measured. This replaces the earlier explanation of a slow ramp from 300 kbps.
- Risk: Early loss on weak links if too high. A choice by network type needs setNetworkType implemented first (see that knob). The server cannot inject this through signaling: signaling goes end to end between clients.
- Verified: yes

## Video codecs actually used, per client pair
- Today: The sender orders H.265, then H.264, then the rest, for camera and screen alike. The preferredVideoCodecs setting (VP9 on Android) reaches only the legacy Manager. Android: hardware plus software through DefaultVideoEncoderFactory, H.264 High enabled. iOS: VideoToolbox H.264 and H.265 where available. tdesktop: software only, built-in factory, with libvpx VP8/VP9 and OpenH264; no H.265 in its encoder or decoder factories and no AV1 (libaom and dav1d are not linked or defined at the pinned tg_owt). So any call with Desktop runs H.264 at best, encoded in software.
- Where: TG/v2/InstanceV2Impl.cpp:485-504 (@545f5ae :477-479); TG/Manager.cpp:110; android org_telegram_messenger_voip_Instance.cpp:900-901; TG/platform/android/AndroidInterface.cpp:64-82; android org/webrtc/DefaultVideoEncoderFactory.java; @c1de0882 platform/darwin/TGRTCDefaultVideoEncoderFactory.mm; @1c236c09 platform/tdesktop/DesktopInterface.cpp:16-22; tdesktop Telegram/build/prepare/prepare.py:1862 (tg_owt 020feec5); tg_owt@020feec5 src/media/engine/internal_encoder_factory.cc, internal_decoder_factory.cc, cmake/libwebrtcbuild.cmake:18-26; tweb src/lib/calls/callInstance.ts:2411 (H.264 ahead of VP8)
- Change via: client patch
- Change: Keep hardware H.265/H.264 for mobile cameras. A hardware encoder for Desktop would be a separate piece of work in tg_owt.
- Effect: Sets the ceiling for Desktop: it pays CPU for every bitrate and resolution raise.
- Risk: The codec differs per client pair, so measurements must be made per pair.
- Verified: yes

## Codec choice for screen share
- Today: Screen uses the same H.265-then-H.264 order as the camera. Desktop has VP9 available in software; AV1 is not an option there.
- Where: TG/v2/InstanceV2Impl.cpp:485-504; tg_owt@020feec5 src/media/engine/internal_encoder_factory.cc
- Change via: client patch
- Change: Try VP9 first for the screen channel, separately from the camera, and compare with OpenH264 on text.
- Effect: Unsupported claim: that VP9 gives better text per bit here was neither read nor measured. VP9 would also keep ULPFEC usable (see the loss-recovery knob).
- Risk: CPU on desktop; whether every receiver decodes VP9 in hardware was not read.
- Verified: no

## Loss recovery for video on bad and distant links (missing from the first list)
- Today: NACK and RTX only with H.264/H.265. RED+ULPFEC is advertised but WebRTC disables it when NACK is on and the codec has no picture ID, which covers H.264 and H.265; only VP8/VP9 keep it. FlexFEC needs field trials WebRTC-FlexFEC-03 and WebRTC-FlexFEC-03-Advertised, and tgcalls sets neither. A lost packet therefore costs at least one round trip.
- Where: WR/call/rtp_video_sender.cc:60-72, 103-114; WR/media/engine/webrtc_video_engine.cc:126-127, 195-207, 1057-1058; WR/pc/media_session.cc:186; @1c236c09 v2/InstanceV2Impl.cpp:932-935; receive side copies ssrc groups at TG/v2/InstanceV2Impl.cpp:800-809
- Change via: client patch
- Change: Test the two FlexFEC field trials in the tgcalls string on a lossy, high-RTT link, or prefer a codec that keeps ULPFEC there. The negotiation and receive path for FlexFEC exists in shape but has not been followed end to end.
- Effect: Directly targets freezes across countries, the owner's stated case. Size of the gain is not measured.
- Risk: FlexFEC is behind an experiment flag in this WebRTC; it spends bandwidth on redundancy; field trials are process-wide.
- Verified: yes

## Network type and data saving in the 1:1 engine (missing from the first list)
- Today: InstanceV2Impl ignores both: setNetworkType and setRequestedVideoAspect are empty, and dataSaving and enableHighBitrateVideo do not appear in the file. Android and iOS still pass them. tdesktop passes dataSaving Never and does not call setNetworkType.
- Where: TG/v2/InstanceV2Impl.cpp:2038-2043; @545f5ae v2/InstanceV2Impl.cpp:2012-2017; grep of @1c236c09 v2/InstanceV2Impl.cpp for dataSaving; android VoIPService.java:3523, 3529; android org_telegram_messenger_voip_Instance.cpp:1009; ios OngoingCallThreadLocalContext.mm:2001-2023; tdesktop calls_call.cpp:1163
- Change via: client patch
- Change: Implement setNetworkType and the data-saving setting in tgcalls, and pick the cap from them, for example separate Wi-Fi and cellular values in custom_parameters.
- Effect: A precondition for raising caps safely, and for a start bitrate that depends on the network.
- Risk: Without it, higher caps apply on mobile data, and the user's data-saving setting has no effect on video, as it has none today.
- Verified: yes

## Camera capture resolution and fps
- Today: Android 1280x720 at 30. iOS prefers a 1280x720 format at 30, then adaptVideoSource requests 25 fps. tdesktop prefers 1280x720 at 30, and its adaptVideoSource is empty, as is Android's. Web K ideal 720p30, max 1080p30. The encoder fps ceiling is 60, so capture is the real limit.
- Where: android .../voip/VideoCapturerDevice.java:31-33; TG/platform/darwin/VideoCapturerInterfaceImpl.mm:154-166, 222; @c1de0882 VideoCaptureInterfaceImpl.cpp:129, 131 (old copy :127, :130), platform/darwin/VideoCameraCapturer.mm:175-198, 550; TG/platform/darwin/DarwinInterface.mm:120-122; TG/platform/android/AndroidInterface.cpp:101; @1c236c09 platform/tdesktop/VideoCameraCapturer.cpp:21-23, 127-129, DesktopInterface.cpp adaptVideoSource; tweb src/lib/calls/helpers/getVideoConstraints.ts; WR/media/base/media_constants.cc:135
- Change via: client patch
- Change: Change 25 to 30 in VideoCaptureInterfaceImpl.cpp (lines 129 and 131 at the iOS pin). Add 1080p capture on capable devices only after the bitrate cap is raised and the H.264 level question is settled.
- Effect: iPhone sends smoother motion, matching the other platforms. 1080p helps only with enough bitrate.
- Risk: Thermals and battery on phones. The same file also serves group calls, so the change reaches them.
- Verified: yes

## H.264 level for 1080p (missing from the first list)
- Today: Android and the WebRTC built-in factories advertise H.264 at level 3.1; iOS maps the negotiated level to a VideoToolbox profile. Level 3.1 is defined for up to about 720p (from memory of H.264 Annex A, not read). tdesktop already sends 1920x1080 screens under this nominal level.
- Where: android org/webrtc/VideoCodecInfo.java:29-33; android org/webrtc/MediaCodecUtils.java:83-91; WR/modules/video_coding/codecs/h264/h264.cc:93-103; @c1de0882 platform/darwin/TGRTCVideoEncoderH264.mm:181-203
- Change via: client patch
- Change: Before enabling 1080p on phones, test whether VideoToolbox and Android hardware encoders accept it at the negotiated level; advertise a higher level if they do not.
- Effect: Could block or distort the planned 1080p step when H.264 is the negotiated codec.
- Risk: Actual encoder behaviour was not read or observed.
- Verified: no

## Screen capture path, resolution and fps per platform
- Today: tdesktop: aspectSize 1280x720 at 24 fps; frames are halved when at least 2x that size in either dimension, so a 2560x1440 display is sent at 1280x720, while 1920x1080 stays full size. Android: MediaProjection at 30 fps, long side reduced to about 1000 px or fewer. iOS: ReplayKit extension throttles to 30 fps with no scaling; the 25 fps adaptation fires from the camera's rotation callback, which the custom capturer does not have, so it was not found on this path. Web K: max 1080p30, contentHint text.
- Where: @1c236c09 platform/tdesktop/VideoCapturerInterfaceImpl.cpp:48-51, desktop_capturer/DesktopCaptureSource.h:36, desktop_capturer/DesktopCaptureSourceHelper.cpp:157-161; android VideoCapturerDevice.java:86-131, 171; ios OngoingCallThreadLocalContext.mm:1532; ios Telegram/BroadcastUpload/BroadcastUploadExtension.swift:234-254; TG/platform/darwin/VideoCapturerInterfaceImpl.mm:218-222; TG/VideoCaptureInterfaceImpl.cpp:69, 108; tweb src/lib/calls/helpers/getScreenConstraints.ts, getScreenStream.ts:4
- Change via: client patch
- Change: tdesktop: aspectSize 1920x1080 and fps 30, so only 4K displays are halved. Android: raise the 1000/970 limits in getScreenCaptureSize to about 1920. Do this together with the screen bitrate cap.
- Effect: Readable text on 1440p desktops and on Android.
- Risk: Halving is the only scaling, so with aspectSize 1920x1080 a 2560x1440 display goes out at its full 3.7 megapixels: four times today's pixels, at 30 fps instead of 24, on a software encoder, and with the 2500 kbps default unless the screen cap is raised. The same capturer may serve group calls; not followed.
- Verified: yes

## Simulcast and SVC in 1:1
- Today: None: a single layer, one SSRC plus RTX. Android wraps the encoder in SimulcastEncoderAdapter but with one stream.
- Where: TG/v2/ContentNegotiation.cpp:295-296; TG/platform/android/AndroidInterface.cpp:54, 81; WR/media/engine/webrtc_video_engine.cc:2118
- Change via: client patch
- Change: Leave as is. With one receiver the sender adapts directly from the feedback. Temporal layers could be considered later through scalability_mode.
- Effect: None expected from simulcast in 1:1.
- Risk: Complexity with no receiver to select layers for.
- Verified: yes

## Adaptation under congestion and CPU load
- Today: Camera MAINTAIN_FRAMERATE, so resolution falls first, driven by the quality scaler (iOS H.264 QP thresholds 28 and 39). Screen MAINTAIN_RESOLUTION, so fps falls first (Web K matches). CPU adaptation on, no suspend below minimum. Field trials are a fixed string with nothing for video.
- Where: WR/media/engine/webrtc_video_engine.cc:910, 1820-1855; TG/v2/InstanceV2Impl.cpp:466, 552-554, 942-947; @c1de0882 platform/darwin/TGRTCVideoEncoderH264.mm:66-67; WR/media/base/media_config.h:31, 40; tweb src/lib/calls/callInstance.ts:1467, helpers/updateSenderParameters.ts:28
- Change via: client patch
- Change: Extend the field-trial string, for example WebRTC-Video-BalancedDegradation for the camera, with values chosen from measurements.
- Effect: Possibly a better trade-off between sharpness and smoothness on poor networks; needs measurement.
- Risk: The WebRTC source itself notes BALANCED is not tuned for all codecs (webrtc_video_engine.cc:1850). Field trials are process-wide.
- Verified: yes

## Camera and screen at the same time in 1:1
- Today: Mutually exclusive: starting one removes the other channel.
- Where: TG/v2/InstanceV2Impl.cpp:1967-2015
- Change via: client patch
- Change: Keep both channels and extend UI and remote-state handling on all clients.
- Effect: Discord-style camera beside the shared screen.
- Risk: Large change across engine and four UIs, and receivers merge the two states today (:1824-1827). Not needed for picture quality.
- Verified: yes

## Web K video limits
- Today: No maxBitrate on the 1:1 video or screen sender; only audio is capped at 32*1024 (the one 2500000 in streamManager.ts:299 is commented out). The browser's default applies, and that default was not read.
- Where: tweb src/lib/calls/callInstance.ts:91, 1465-1473; src/lib/calls/streamManager.ts:299
- Change via: client patch
- Change: Set encodings[0].maxBitrate through the existing updateSenderParameters helper from the same server value as native.
- Effect: Same limits on all four clients; a TypeScript-only change.
- Risk: Chromium source is not local, so whether this raises or lowers the rate today is unknown.
- Verified: no

## ICE and TURN placement as it affects video
- Today: Server-settable through CALLS_ICE_SERVERS. p2p_allowed is computed from the two users' privacy settings, so it is not a free operator setting. Calls are off until coturn is deployed. The docs' coturn config sets no per-session bandwidth cap. Video through the relay is limited by the path and the node's capacity, not by a video setting.
- Where: teamgram-server origin/master docs/ops/calls.md:9, 12, 26-32; docs/project/STATUS.md:21; app/bff/voipcalls/internal/core/phone.requestCall_handler.go:99
- Change via: infrastructure
- Change: Deploy coturn near users and check its bandwidth headroom for higher caps. Measure relay share per country as ADR 0004 plans.
- Effect: Needed for any call at all, and for relayed calls to reach raised bitrates.
- Risk: Raising caps to 3-4 Mbit/s roughly triples relay traffic per video call, which affects any TURN budget.
- Verified: yes

## Notes
Result of the critique: the list mostly holds. I opened the cited lines and read the three tgcalls pins and tg_owt directly as raw files, so most "through fetch" caveats are gone and those entries now have real line numbers.

Corrected:
- Start bitrate: the real cost of 300 kbps is not a slow rate ramp, since the estimator probes at 3x and 6x. At under 500 kbps WebRTC drops initial 720p frames and steps resolution down, for the camera only. Also, WebRTC does call SetSdpBitrateParameters itself, with start -1, and CallConfig covers only the start of the call, not a camera turned on later.
- Codecs: tg_owt is now read. Desktop has VP8, VP9 and OpenH264 in software, no H.265 and no AV1, so the AV1 suggestion is removed. Split into a verified entry (what runs today) and an unverified one (VP9 for screen).
- Camera cap: "720p is starved" and "largest gain" were unsupported and are reworded.
- Screen capture: on iOS the limit found is the extension's 30 fps throttle; the 25 fps path was not found for the custom capturer. On tdesktop the proposed aspectSize sends 1440p at full size, four times today's pixels, on a software encoder.
- Library version: "drops non-IP hosts" is out of date at the tdesktop pin.
- custom_parameters: the key list is extended; Web K already has getCustomParam.
- iOS 25 fps line numbers are 129 and 131 at the pin.
- p2p_allowed comes from user privacy, not from the operator.

Tried to refute and could not: phone.getCallConfig has no video effect. A grep over all Android sources, including org.webrtc, finds those keys only in Instance.java.

Added: loss recovery (no FEC at all on H.264/H.265, only retransmission, which matters most for the owner's cross-country case); network type and data saving ignored by the engine (a precondition for raising caps safely); H.264 level 3.1 versus 1080p (verified:false).

Bottom line unchanged: the server cannot set any video parameter on stock clients. Suggested order: (1) setNetworkType and data saving, with camera and screen caps driven by custom_parameters; (2) tdesktop and Android screen limits; (3) start bitrate of at least 500 kbps; (4) iOS 25 to 30 fps; (5) FlexFEC, screen codec and field trials, only after measurements on a lossy, high-RTT link.

Reading rule for the field: verified:true means today, where and the mechanism were read. Effect statements are reasoning from code unless they cite a source; all numbers are targets. No real call has run (STATUS:21).

Source limits:
- The tgcalls submodules under reference-src-new are empty. Lines marked TG are from ~/pluma-work/reference-src/android/TMessagesProj/jni/voip/tgcalls (older Android 12.9.1); lines marked @commit are raw reads from github.com/TelegramMessenger/tgcalls at that pin (545f5ae Android, 1c236c09 tdesktop, c1de0882 iOS).
- WR means ~/pluma-work/reference-src-new/android/TMessagesProj/jni/voip/webrtc. tdesktop's and iOS's own WebRTC trees were read only for the factories named; the adaptation, FEC and probing lines are from Android's copy.
- The three pins differ, so one patch must be carried onto each or the pins unified.
- Group calls are outside this list.

Nothing in the repository was changed.
