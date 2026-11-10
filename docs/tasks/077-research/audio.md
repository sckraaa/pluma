# Call quality: audio

Read from the client sources pinned in upstream.env of each client. `Verified: no` marks what was not confirmed from a source.

## Calls are switched off, so no audio path exists yet
- Today: Empty CALLS_ICE_SERVERS makes phone.requestCall answer CALLS_NOT_CONFIGURED. Repo docs say coturn is not deployed and no real call has been made. The live server's .env was not read.
- Where: teamgram-server origin/master app/bff/voipcalls/internal/core/core.go:45; docs/project/STATUS.md:21; docs/tasks/068-calls-1to1.md:3,37; docs/ops/calls.md
- Change via: infrastructure
- Change: Deploy coturn and set CALLS_ICE_SERVERS and CALLS_TURN_SECRET. Capture a baseline first: MOS, loss, RTT and mouth-to-ear delay on stock settings.
- Effect: Precondition for everything below. Without a baseline, 'better than Telegram' cannot be shown.
- Risk: None for audio. TURN location directly sets RTT and loss, which is the network lens.
- Verified: yes

## Which implementation runs: server picks the library version
- Today: Negotiate takes the greatest common version compared as strings, so 9.0.0 ranks first, then 8.0.0, 7.0.0, 5.0.0, 2.7.7, 19.0.0, 18.0.0, 14.0.0, 13.0.0, 12.0.0, 11.0.0, 10.0.0. Any native pair gets 9.0.0 = InstanceV2Impl. A pair with Web K gets 13.0.0 (Web K offers only that). CORRECTED: that is not 'same audio' in both directions. The native side sends V2Impl 60 ms; Web K sends from the browser with no ptime in its fmtp and its own processing. Registry: 2.7.7, 5.0.0 InstanceImpl; 7, 8, 9, 12, 13 InstanceV2Impl; 10, 11 InstanceV2ReferenceImpl; 14 Compat (iOS only); 18, 19 Pump (iOS, Android aarch64; not tdesktop).
- Where: teamgram-server pkg/phonecalls/phonecalls.go:135-140; tgcalls/v2/InstanceV2Impl.cpp:2376-2384; tgcalls/v2/InstanceV2ReferenceImpl.cpp:1737-1742; android 13.2.1 jni/voip/org_telegram_messenger_voip_Instance.cpp:31-36; tdesktop calls_call.cpp:73-75; iOS submodules/TgVoipWebrtc/Sources/OngoingCallThreadLocalContext.mm:1852-1859; tweb p2P/getCallProtocol.ts (CALL_PROTOCOL_LIBRARY_VERSIONS); tweb p2P/utils.ts:357-358; android VoIPService.java:3564,3601-3602
- Change via: server
- Change: Optional, audio-only calls between native clients: prefer 11.0.0 in Negotiate. Keep 9.0.0 for video and for anything with Web K. Never pick 2.7.7 or 5.0.0.
- Effect: The only server-side lever on audio. Reference sets no ptime, so Opus uses the 20 ms default; the jitter floor is 0 instead of 50 ms; Android keeps fixed-digital AGC because the MinimizeResampling trial is absent. Bitrate cap stays 32*1024. Because of the FEC finding below, 20 ms also lets in-band FEC repair a whole lost packet. Not measured.
- Risk: Android: VoIPService.java:3564 compares "2.7.7" with the version as strings, so 11.0.0 sorts below. An outgoing camera is destroyed, and CLOSED OPEN POINT: :3601-3602 then sets isVideoAvailable false and notifies listeners, so Android loses video availability for that call. The server must therefore never choose 11.0.0 for a call that may want video. Reference skips isTcp servers (:658). 20 ms adds about 13.3 kbit/s of headers at 50 B per packet; that 50 B is an estimate (see Opus packet time). Needs a real test; a client patch of 60 to 20 is cleaner.
- Verified: yes

## InstanceImpl (2.7.7, 5.0.0) audio, for the record
- Today: Opus 48 kHz, min 6, start 8, max 32 kbit/s, ptime 120 ms, in-band FEC, field trial WebRTC-Audio-Allocation/min:32kbps,max:32kbps. 16 kbit/s under data saving.
- Where: tgcalls/MediaManager.cpp:293-301, 379-394, 864-870
- Change via: not possible
- Change: Nothing; negotiation never chooses these for current clients.
- Effect: None.
- Risk: None.
- Verified: yes

## phone.getCallConfig JSON
- Today: Server answers {} (no ClientConfig set under teamgramd). No key reaches 1:1 audio. Android reads use_system_ns, use_system_aec, voip_enable_stun_marking, hangup_ui_timeout and enable_{vp8,vp9,h264,h265}_{encoder,decoder}. use_system_* only set Config.enableAEC/enableNS, which only legacy reads; nothing calls WebRtcAudioUtils.setWebRtcBased*, so the JSON cannot switch platform and software effects either. Android's native setGlobalServerConfig is commented out. iOS stores it as serializedData. tdesktop never requests it.
- Where: android Instance.java:451-463; VoIPService.java:3525-3529, 4636-4646; org_telegram_messenger_voip_Instance.cpp:893-895, 1014-1016; tgcalls/legacy/InstanceImplLegacy.cpp:130-138; iOS submodules/TelegramCore/Sources/State/ManagedVoipConfigurationUpdates.swift:9-18; OngoingCallContext.swift:944; teamgram-server app/bff/voipcalls/internal/core/phone.getCallConfig_handler.go:22-31
- Change via: not possible
- Change: Do not expect audio tuning from this JSON. The handler comment saying Android and iOS pass it to tgcalls is wrong for current releases; correct it in its own change.
- Effect: None on audio.
- Risk: Spending time here gains nothing.
- Verified: yes

## phone.phoneCall custom_parameters
- Today: Server never sends it (phonecalls.go:250-261), though the generated type has the field. Android, iOS and Web K read it; tdesktop 7.4.1 does not pass customParameters. All keys read by V2Impl and the networking code are network or signaling. There is no audio key.
- Where: tgcalls/v2/InstanceV2Impl.cpp:949-960, 994, 1011; tgcalls/v2/NativeNetworkingImpl.cpp:522-669; android VoIPService.java:3529; iOS OngoingCallThreadLocalContext.mm:2001-2023; tdesktop calls_call.cpp:1158-1169; tweb callInstance.ts:813-820; pinned tgcalls@545f5ae v2/CustomParameters.h; teamgram-server third_party/teamgram-proto/mtproto/codec_schema.tl.pb.go:185236
- Change via: client patch
- Change: Make this the tuning channel: patch V2Impl to read audio_max_bitrate_kbps, audio_ptime_ms, audio_jitter_min_ms, audio_min_bitrate_kbps; add .customParameters to tdesktop's Config; have the server fill the field per side and per network.
- Effect: After one client release, the audio values below become server settings. Matches ADR 0004 'one tgcalls patch, limits from the server'.
- Risk: ADDED: getCustomParameterInt returns 0 for an absent or out-of-range key, so a real value of 0 (jitter floor 0) cannot be told from absent; check key presence or encode differently. ADDED: field trials are process-global and InitFieldTrialsFromString stores the raw pointer (field_trial.cc:163-170), so a per-call trial string needs storage that outlives the call. Values need clamping in the client. iOS 12.9.2 tgcalls would need the patch too.
- Verified: yes

## Opus packet time
- Today: 60 ms in V2Impl, set on the sender only. Reference sets none, so 20 ms.
- Where: tgcalls/v2/InstanceV2Impl.cpp:173; webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:131-146, 530-535; webrtc/api/audio_codecs/opus/audio_encoder_opus_config.h:24; webrtc/audio/audio_send_stream.cc:158-159, 873-875
- Change via: client patch
- Change: kCodecParamPTime 60 to 20 on good and lossy networks. Keep 40 or 60 as a server-selected value only for uplinks that cannot carry the headers.
- Effect: About 40 ms less packetization delay, and in-band FEC can repair a whole lost packet instead of 20 of its 60 ms (see the FEC entry). Likely the largest audible gain per line changed. Not measured.
- Risk: About +13.3 kbit/s of headers, three times the packet rate. CORRECTED citation: the 50 B constant at audio_send_stream.cc:866 is the legacy branch, which is off because WebRTC-Audio-LegacyOverhead is unset (:158-159). The real path adds actual overhead to the constraints (:873-875) and the encoder subtracts it (audio_encoder_opus.cc:530-535), so Opus payload stays 32768 and the wire rate rises. Real overhead also includes RTP extensions and TURN, so 50 B is a floor-level estimate. Sender-local, no interop problem.
- Verified: yes

## Opus bitrate: min, start, max
- Today: Effectively fixed at 32768 bit/s: setMaxBitrate(32*1024), then UpdateAllowedBitrateRange sets min = max = target. Audio is registered with the allocator because transport-cc is negotiated on it. CLOSED OPEN POINT: the observer is added with enforce_min_bitrate true, and OnBitrateUpdated clamps again, so a BWE below min still gives audio its min. Audio keeps pushing about 32.8 kbit/s plus headers into a congested link. Web K caps the same 32*1024.
- Where: tgcalls/v2/InstanceV2Impl.cpp:2162-2165, 240-255, 942-947; tgcalls/v2/ContentNegotiation.cpp:258; tgcalls/v2/InstanceV2ReferenceImpl.cpp:719; webrtc/media/engine/webrtc_voice_engine.cc:194-224, 1105-1126; webrtc/audio/audio_send_stream.cc:491-502, 817-823, 842-856; webrtc/call/bitrate_allocator.cc:197-198; tweb callInstance.ts:91, 1461-1472; opus 12.9.1 jni/opus/src/opus_encoder.c:156-159, 1368-1389
- Change via: client patch
- Change: Add WebRTC-Audio-Allocation/min:16kbps,max:48kbps to the V2Impl trial string so BWE moves Opus inside a range, and raise the cap to match. CHANGED from 64: prefer about 48 kbit/s. Same cap in Reference and Web K. adaptive_ptime is not a route on Android: it needs the audio network adaptor, and WEBRTC_ENABLE_PROTOBUF is not among Android's build defines.
- Effect: Bad networks can drop instead of pushing a fixed rate; clean networks get headroom. The downward step is what serves bad networks. Audible gain above 32 on mono speech is probably modest; not measured.
- Risk: ADDED: near 64 kbit/s Opus can leave SILK/hybrid. With voice_est 115 in VoIP the CELT threshold works out to about 61.6 kbit/s plus or minus 4, and CELT-only frames carry no LBRR until reported loss forces SILK back (:1388). The trial min/max also override the setMaxBitrate cap in the constraints (audio_send_stream.cc:849-852). Below about 16 kbit/s FEC thresholds bite (opus_encoder.c:162-168, 720-745). Video shares the budget, so check video after. Opus figures come from bundled copies, not the exact linked libopus.a.
- Verified: yes

## Jitter buffer (NetEq)
- Today: Receiver V2Impl: fast_accelerate true, min delay 50 ms, max 200 packets (engine default). Reference: fast_accelerate true, min delay 0. Checked whether 60 ms packets make the floor inert: they do not by that route. The packet-length floor in TargetLevelMs applies only when enable_stable_delay_mode is false, and it defaults to true.
- Where: tgcalls/v2/InstanceV2Impl.cpp:338-339; tgcalls/v2/InstanceV2ReferenceImpl.cpp:654; webrtc/media/engine/webrtc_voice_engine.cc:440-442, 581-591; webrtc/modules/audio_coding/neteq/decision_logic.cc:192-200; decision_logic.h:159; delay_manager.cc:97-105; audio/channel_receive.cc:81-83
- Change via: client patch
- Change: audio_jitter_buffer_min_delay_ms 50 to 0 or 20, together with 20 ms ptime; NetEq's adaptive target still grows under jitter.
- Effect: Up to 50 ms less mouth-to-ear delay on stable paths. Not measured.
- Risk: Receiver-local. Less cushion at call start on jittery mobile links (start target is 80 ms, delay_manager.cc:34); test on a shaped network and keep it server-selectable.
- Verified: yes

## NetEq adaptation trials (missed)
- Today: Not set by tgcalls, so defaults apply: quantile 0.95, forget_factor 0.983, reorder optimizer on, ms_per_loss_percent 20.
- Where: webrtc/modules/audio_coding/neteq/delay_manager.h:34-41; delay_manager.cc:48-58; decision_logic.cc:68-77; tgcalls/v2/InstanceV2Impl.cpp:942-947
- Change via: client patch
- Change: Add WebRTC-Audio-NetEqDelayManagerConfig to the trial string only if measurement on shaped networks shows late-loss or excess delay.
- Effect: Trades delay against late packets for bad networks. Size unknown.
- Risk: Tuning without a baseline can worsen both. Process-global. Lower priority than ptime and floor.
- Verified: yes

## In-band FEC: how much a lost packet can be repaired
- Today: useinbandfec=1 on V2Impl. CORRECTED and now read rather than recalled: at about 32 kbit/s mono VoIP Opus runs hybrid at fullband, and hybrid cannot use frames over 20 ms, so a 60 ms packet is three 20 ms frames. FEC is decoded only from the first frame of the next packet. It therefore repairs only the last 20 ms of a lost 60 ms packet, and 40 ms goes to concealment. At 20 ms ptime one lost packet is fully repairable. The trial WebRTC-Audio-OpusMinPacketLossRate has no reader in this WebRTC.
- Where: tgcalls/v2/InstanceV2Impl.cpp:169-183; opus 12.9.1 jni/opus/src/opus_encoder.c:127-132, 1388-1389, 1563-1569, 1596, 720-745; jni/opus/src/opus_decoder.c:672-697; same logic in iOS OpusKit opus_encoder.c:1377, 1552; webrtc/modules/audio_coding/codecs/opus/opus_interface.cc:811-812, 746-760; audio_decoder_opus.cc:46-53
- Change via: client patch
- Change: Move to 20 ms frames (see Opus packet time). Optionally floor the projected loss rate so FEC is on before the first loss report.
- Effect: This is the main reason 60 ms is weak on lossy links. Repair per single loss goes from a third of the packet to all of it. Perceptual size not measured.
- Risk: LBRR takes bits from the primary inside the same budget. Android links a prebuilt libopus.a from an empty submodule (22244de), so the two copies read stand in for it; NetEq's use of the FEC frame was read but not run.
- Verified: yes

## Loss feedback delay to the Opus encoder (missed)
- Today: The encoder learns loss only from RTCP report blocks. The receive stream sets no report interval, so audio's 5000 ms default applies, randomized to 2.5 to 7.5 s, then an exponential smoother. Until the first report showing loss, FEC is off (decide_fec returns 0 at loss 0).
- Where: webrtc/audio/channel_send.cc:567-570; webrtc/audio/channel_receive.cc:575-581; webrtc/modules/rtp_rtcp/source/rtcp_sender.cc:58, 149-150, 722; webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:69, 321-340, 478-487; opus_encoder.c:720-723
- Change via: client patch
- Change: Set a shorter report interval on the audio receive RTP module (about 1 s). This is a patch of WebRTC's channel_receive, or a loss floor in the encoder instead.
- Effect: FEC starts seconds earlier at the onset of loss, which matters on mobile handovers. Not measured.
- Risk: Small extra RTCP traffic. Needs each client's WebRTC copy patched; iOS WebRTC and tg_owt were not read.
- Verified: yes

## RED
- Today: RED exists in the engine codec list but tgcalls strips it: only opus survives negotiation and the outgoing channel. Default redundancy is one earlier frame unless WebRTC-Audio-Red-For-Opus/Enabled-N is set.
- Where: tgcalls/v2/ContentNegotiation.cpp:221-227; tgcalls/v2/InstanceV2Impl.cpp:168-183; webrtc/media/engine/webrtc_voice_engine.cc:764-768, 1380, 1442-1454, 2151-2152; webrtc/audio/audio_send_stream.cc:618-625; webrtc/modules/audio_coding/codecs/red/audio_encoder_copy_red.cc:36-51, 238-243
- Change via: client patch
- Change: Stop filtering RED, place it before opus with fmtp pt/pt, and set Enabled-2 if bursts are the target. Only after 20 ms frames and a measured need.
- Effect: CORRECTED: with default level 1 RED repairs a single lost packet, which 20 ms FEC already does; a burst needs level 2 or more. Its gain over FEC is full-quality rather than LBRR-quality repair. Size not measured.
- Risk: The encoder wrapper forwards the target to Opus unchanged, so RED bytes come on top of the allocation. Needs all clients. Carrying the empty-key fmtp through tgcalls signaling and the receive path under ChannelManager was not exercised.
- Verified: no

## Audio NACK (missed)
- Today: Off. The engine advertises only transport-cc on audio, while the send and receive paths honor nack if the codec carries it.
- Where: webrtc/media/engine/webrtc_voice_engine.cc:96, 285, 743-744, 1397, 2206-2210
- Change via: client patch
- Change: Evaluate adding nack feedback on audio for low-RTT paths.
- Effect: Could repair losses within the jitter budget. Benefit on long international paths is doubtful; not measured.
- Risk: Whether NetEq requests retransmission usefully in this build was not traced. Lower priority than FEC at 20 ms.
- Verified: no

## DTX
- Today: Off: usedtx is not set on any 1:1 path (dtx_enabled defaults false).
- Where: webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:252; webrtc/api/audio_codecs/opus/audio_encoder_opus_config.cc:45; tgcalls/v2/InstanceV2Impl.cpp:172-173
- Change via: client patch
- Change: Leave off unless data use matters; quality is not improved by it.
- Effect: Saves traffic in silence only.
- Risk: Noise pumping and slower reaction at speech onset (from memory, unverified).
- Verified: yes

## Sample rate, channels, codec
- Today: Opus only, 48 kHz RTP, mono: the stereo param is never set, so num_channels = 1 and application is VoIP, even though the payload type says 2 channels.
- Where: webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:114-121, 208-213, 248-257; tgcalls/v2/ContentNegotiation.cpp:221-227; tgcalls/v2/InstanceV2Impl.cpp:1135-1136
- Change via: not possible
- Change: Keep mono and Opus for speech.
- Effect: None needed.
- Risk: None.
- Verified: yes

## Opus encoder complexity on phones
- Today: 5 on Android and iOS, 9 on desktop (build-time constant in WebRTC). iOS and desktop inferred from the Android copy's branches.
- Where: webrtc/api/audio_codecs/opus/audio_encoder_opus_config.cc:17-21, 41; opus_encoder.c:1140-1142
- Change via: client patch
- Change: Raise kDefaultComplexity to 9 or 10 for mobile in the WebRTC copies.
- Effect: Better coding at the same bitrate on phones; size not measured.
- Risk: CPU and battery; check on low-end Android. ADDED: at 7 or above (10 in fixed-point builds) libopus runs its signal analysis, which changes voice_est and so the mode and bandwidth decisions; retest those.
- Verified: yes

## Android audio I/O layer (missed)
- Today: Always Java AudioRecord and AudioTrack. isDeviceBlacklistedForOpenSLESUsage is hard-coded to return true, which makes low-latency playout false; AAudio is disabled in Java and its build flag is absent. Android supplies no custom device module, so kPlatformDefaultAudio falls to kAndroidJavaAudio.
- Where: android src/main/java/org/webrtc/voiceengine/WebRtcAudioManager.java:45, 215-223, 277-282; webrtc/modules/audio_device/android/audio_manager.cc:195-202, 218-223; webrtc/modules/audio_device/audio_device_impl.cc:217-234; tgcalls/v2/InstanceV2Impl.cpp:2172-2196
- Change via: client patch
- Change: Test Java input plus OpenSL ES output (kAndroidJavaInputAndOpenSLESOutputAudio) on devices that report low latency. Java input keeps platform AEC and NS, which OpenSL input does not implement (audio_device_template.h:372-374).
- Effect: Possibly lower playout delay on Android. Size not measured; the amount is an assumption until a device test.
- Risk: The hard-coded true suggests device problems upstream, reason unknown. Device-by-device testing required.
- Verified: yes

## Noise suppression
- Today: CORRECTED: V2Impl asks for noise_suppression true, but on Android the engine replaces software NS with the platform effect whenever the device reports one. WebRTC classic NS at High runs on Android only without a usable platform NS, and on desktop (inferred; lib_webrtc is empty and tg_owt unread). iOS forces engine NS off and relies on VPIO. RNNoise is wired only in group calls. Web K already applies RNNoise in calls.
- Where: tgcalls/v2/InstanceV2Impl.cpp:153-154, 1143-1144; tgcalls/group/GroupInstanceCustomImpl.cpp:68-69, 720-731, 764-769, 2145-2152; webrtc/media/engine/webrtc_voice_engine.cc:432-437, 564-575, 622-627; webrtc/modules/audio_device/android/audio_device_template.h:407-416; android WebRtcAudioEffects.java:150-155; tweb lib/calls/noiseSuppression/noiseSuppression.ts:89,98
- Change via: client patch
- Change: Move the group AudioCapturePostProcessor with RNNoise into V2Impl via builder.SetCapturePostProcessing, with a user switch as in groups. On Android also decide platform NS on or off (WebRtcAudioUtils.setWebRtcBasedNoiseSuppressor).
- Effect: Consistent noise handling across phones instead of whatever the vendor effect does, using code already in the client. A real point of difference. Superiority over classic NS is not measured here for native; the Web K code comment cites its own evaluation.
- Risk: Stacking RNNoise on a vendor NS leaves artifacts, as the Web K comment notes for the browser's NS. CPU on weak phones; order against AEC; test iOS with VPIO.
- Verified: yes

## Echo cancellation
- Today: Android: built-in AEC if the device reports it, otherwise software in mobile mode. Desktop: software AEC (inferred, device module unread). iOS: engine AEC off, VPIO does it. Config.enableAEC is ignored by V2.
- Where: tgcalls/v2/InstanceV2Impl.cpp:153; webrtc/media/engine/webrtc_voice_engine.cc:484-501, 529-545, 601-604; android org/webrtc/voiceengine/WebRtcAudioEffects.java:141
- Change via: client patch
- Change: Consider mobile_mode false on Android when built-in AEC is unavailable or known bad, with a device list.
- Effect: Could help speakerphone on devices without working hardware AEC. Size unknown.
- Risk: CPU. Needs real phones. Lower priority than the items above.
- Verified: yes

## Automatic gain control
- Today: Android and iOS under V2Impl: engine AGC off, because the trial WebRTC-Audio-MinimizeResamplingOnMobile forces it off. Reference has no such trial and passes empty AudioOptions, so Android keeps the engine default, fixed-digital. Desktop code path selects adaptive analog, but whether it can act depends on microphone volume support in lib_webrtc's device module, which is an empty submodule here and was not read.
- Where: tgcalls/v2/InstanceV2Impl.cpp:944; tgcalls/v2/InstanceV2ReferenceImpl.cpp:383-386, 706; webrtc/media/engine/webrtc_voice_engine.cc:430-431, 504-527, 606-616
- Change via: client patch
- Change: Remove that trial on Android, or test gain_controller2 in the APM config.
- Effect: Steadier level from quiet or distant Android microphones.
- Risk: Extra resampling on mobile, and possible noise pumping; the voice-communication source may already apply device AGC. Test on devices.
- Verified: yes

## Pump core 18.0.0, 19.0.0 and Compat 14.0.0 as a route to better audio
- Today: ReferenceCallCore at 545f5ae has kAudioMaxBitrateBps 32*1024 and no ptime, jitter or audio processing terms. Not on tdesktop; Pump on Android only for aarch64; Compat only on iOS. Compat's audio values were not opened in this check.
- Where: pinned tgcalls@545f5ae v2wasm/ReferenceCallCore.cpp (kAudioMaxBitrateBps; no line numbers, fetched excerpt); android org_telegram_messenger_voip_Instance.cpp:31-36; iOS OngoingCallThreadLocalContext.mm:1855-1859
- Change via: not possible
- Change: Do not select these. The server cannot ship a call core.
- Effect: No evidence of audio gain over 11.0.0, and worse platform coverage.
- Risk: None if left alone.
- Verified: no

## Notes
Bottom line stands: the server alone can barely touch 1:1 audio. getCallConfig and custom_parameters carry no audio key on any client. The only server lever is the library version (9.0.0 today; 11.0.0 for audio-only, which removes Android video availability for that call and is untested). Beating Telegram on audio needs the client patch ADR 0004 plans.

What this review changed:
- New and most important: at 60 ms and about 32 kbit/s Opus is hybrid, so a packet is three 20 ms frames and FEC repairs only the last 20 ms of a lost packet. This replaces the earlier 'from memory' note. Opus sources do exist locally (jni/opus in 12.9.1, iOS OpusKit), contrary to the earlier note; the exact linked libopus.a was still not read.
- Corrected: Android NS uses the platform effect when available. RED level 1 does not repair bursts. The 50 B citation was the legacy branch that is off. 'Same audio' with Web K is true only for the native sender. Bitrate cap suggestion moved from 64 to about 48 kbit/s because of the CELT threshold near 61.6.
- Closed open points: BitrateAllocator enforces audio's min (enforce_min_bitrate true). Android sets isVideoAvailable false on 11.0.0.
- Tried and failed to refute: the 50 ms jitter floor is not masked by 60 ms packets, because stable delay mode is on by default.
- Added: RTCP loss feedback delay (2.5 to 7.5 s), Android forced to Java audio layer, NetEq trials, audio NACK.
- Marked verified:false: RED, audio NACK, Pump/Compat row.

Suggested order: (1) deploy TURN and measure a baseline; (2) one tgcalls patch reading audio_* from custom_parameters, plus customParameters in tdesktop; (3) ptime 20 and jitter floor 0 to 20; (4) Opus range 16 to 48 kbit/s and faster loss feedback; (5) RNNoise in 1:1 with a decision on Android platform NS; (6) mobile complexity, Android audio layer, AGC, AEC; (7) RED only if measurement shows a need.

Correction to docs/audit/06 (line 19): it says 1:1 voice is '<=32 kbit/s'. In the code path read the range collapses, so Opus sits at 32768 bit/s and does not adapt. The comment in phone.getCallConfig_handler.go:22-24 is wrong for current clients.

Sources and limits:
- Path prefixes: tgcalls/ = ~/pluma-work/reference-src/android/TMessagesProj/jni/voip/tgcalls (Android 12.9.1 copy); webrtc/ = ~/pluma-work/reference-src-new/android/TMessagesProj/jni/voip/webrtc (Android 13.2.1); opus_encoder.c and opus_decoder.c = ~/pluma-work/reference-src/android/TMessagesProj/jni/opus/src unless named otherwise.
- tgcalls submodules in reference-src-new are empty. Pinned commits (Android 545f5ae) were checked only through fetched excerpts for CustomParameters.h and ReferenceCallCore.cpp, without line numbers. 12.9.1 line numbers may have shifted.
- iOS WebRTC, tg_owt and lib_webrtc were not read (lib_webrtc is empty). Engine behaviour on iOS and desktop is inferred from the Android copy.
- All effect sizes are engineering estimates. No call has been made or measured on this system.
- Read only: no repository files changed.
