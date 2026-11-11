# Call quality: group

Read from the client sources pinned in upstream.env of each client. `Verified: no` marks what was not confirmed from a source.

## SFU existence and group-call API on the server
- Today: No group calls. Every group method is `missing` in api-coverage, including joinGroupCall, joinGroupCallPresentation, checkGroupCall, getGroupCall, getGroupParticipants. No SFU in the repo. 1:1 is off in production (CALLS_NOT_CONFIGURED, no TURN) and has never carried a real call.
- Where: teamgram-server origin/master docs/project/api-coverage.md (phone.* rows); docs/project/STATUS.md (068 entry); docs/adr/0004-calls-architecture.md:3 (status proposed)
- Change via: server
- Change: Build the phone.* group handlers and a media service. Design in notes.
- Effect: Precondition for everything else in this list.
- Risk: Large subproject. Size in notes is an estimate.
- Verified: yes

## ICE candidates in the join response (the only network path of a native group call)
- Today: Native takes candidates only from transport.candidates, and the field is mandatory. No STUN or TURN: SetConfiguration({}, {}, ...), confirmed at 12.9.1, Desktop pin 1c236c09 and Android 13.2.1 pin 545f5ae5. IPv6 enabled. TCP not disabled in the allocator. tcptype is parsed but not passed to cricket::Candidate. Web prints candidates into SDP with no tcptype either.
- Where: tgcalls/group/GroupNetworkManager.cpp:390-398; GroupJoinPayloadInternal.cpp:88-90,159; GroupInstanceCustomImpl.cpp:3695-3715; webrtc/p2p/base/tcp_port.cc:137-142; tweb src/lib/calls/sdpBuilder.ts:29-36
- Change via: server
- Change: Return per-client candidates: SFU UDP v4 and v6 on one muxed port, a TCP candidate on 443 with non-zero port, and regional L4 forwarders chosen by GeoIP.
- Effect: Reachability and a closer entry point across countries, with no client patch. The code path accepts it; the effect is not measured.
- Risk: ICE-TCP has never been run against real tgcalls or Chrome here. Head-of-line blocking, so fallback only. Forwarders need per-client symmetric mapping, and the SFU, not the client, decides the path for native (see the ICE role and path migration entries).
- Verified: no

## TURN for group calls
- Today: Not possible in stock clients: empty server lists in CustomImpl, `iceServers: []` in Web, and no ICE server assignment found in GroupInstanceReferenceImpl at e3069322 or c1de0882 (grep of the raw file).
- Where: GroupNetworkManager.cpp:398; tweb src/lib/calls/groupCallConnectionInstance.ts:160; tgcalls e3069322 and c1de0882 tgcalls/group/GroupInstanceReferenceImpl.cpp
- Change via: client patch
- Change: Pass TURN servers from new join-response fields into SetConfiguration, and into iceServers on Web.
- Effect: A path through networks where UDP and raw TCP to the SFU fail.
- Risk: New join JSON field, a patch in three tgcalls copies plus tweb (the earlier list omitted tweb). ICE-TCP plus forwarders may make it unnecessary; measure first.
- Verified: yes

## Video codec of the whole call (payload-types)
- Today: Native ignores server ids and rtp-hdrexts and only checks whether a payload named H264 is listed. Listing it puts H264 first, then VP8, then VP9. Ids are assigned locally from 100 in the order VP8, VP9, H264. AV1 is filtered out. Receive codecs come from the ENCODER factory list, so a client with no H264 encoder configures no H264 decoder. The join payload carries no codec information. Web, unlike native, uses the server's ids in its SDP.
- Where: GroupInstanceCustomImpl.cpp:219-226, 299-350, 1539-1546, 2158, 2897, 2972-3004; GroupJoinPayloadInternal.cpp serialize; tweb sdpBuilder.ts:185-186, 221; go_sfu sfu.go:273 lists H264
- Change via: server
- Change: Choose whether `H264` appears in video.payload-types. VP8 is the safe uniform choice: it is always first, so 100/101 on every client that has it, and the id list sent for Web must match that.
- Effect: Corrected: on Android hardware H264 has the same vendor gate as hardware VP8 (QCOM, Exynos), so H264 is not a general hardware win there. On iOS a VideoToolbox gain is from memory, unverified.
- Risk: Higher than first stated. With H264 listed: a sender picking H264 cannot be decoded by a receiver whose encoder list lacks it, and H264's id shifts with VP9 presence (102 vs 104), with nothing in the join for the SFU to remap by. Recommend VP8 until tested across all four clients.
- Verified: yes

## Codec preference flags in app config
- Today: Desktop reads confcall_use_vp8; iOS reads a DIFFERENT key, ios_calls_prioritize_vp8 (the earlier list had this wrong). Both only push VP8 to the front. Desktop applies it to all group calls and the screen instance. No Android key found. Nothing selects VP9 or H264.
- Where: reference-src-new/tdesktop/Telegram/SourceFiles/main/main_app_config.cpp:160-161; calls/group/calls_group_call.cpp:2999-3004, 3187; reference-src-new/ios/submodules/TelegramCallsUI/Sources/PresentationGroupCall.swift:1880-1882; ios/submodules/TgVoipWebrtc/Sources/OngoingCallThreadLocalContext.mm:2671-2673
- Change via: server
- Change: Set both keys in help.getAppConfig only if H264 is listed and proves unreliable.
- Effect: Forces interoperability; does not raise quality.
- Risk: Low. Android cannot be forced this way, so it does not make listing H264 safe.
- Verified: yes

## SFU retransmission (NACK/RTX) for video
- Today: Clients negotiate nack, nack pli, ccm fir and rtx on every video payload. go_sfu reads only PLI and FIR from RTCP; no NACK handling. Correction: it does have an RTX ring buffer, used only for probing padding.
- Where: GroupInstanceCustomImpl.cpp:155-170, 2905-2945; tgcalls 1c236c09 tools/go_sfu/participant.go:452,457; tools/go_sfu/bandwidth.go:88-126; ~/go/pkg/mod/github.com/pion/interceptor@v0.1.44
- Change via: server
- Change: Per-receiver RTX cache answering NACK; send NACK upstream to publishers; rate-limit PLI.
- Effect: The largest server-only gain for video on lossy networks.
- Risk: Cache memory and retransmit storms; needs a per-receiver budget.
- Verified: yes

## Downlink bandwidth estimation and layer selection in the SFU
- Today: The receiver answers with transport-cc when a packet carries extension 3; if it does not, WebRTC falls back to receive-side estimation, so REMB absence is conditional on the SFU stamping extension 3. go_sfu's REMB selector never reaches PROBING_UP in live runs. Correction: go_sfu already generates TWCC to senders (100 ms). Its open note about the ramp is on the SENDER side: GCC reaches layer 1 only, and it suspects missing probing or the 1052 kbit/s cap. server_sources creates a receive channel for a probing SSRC; go_sfu never fills it and uses field name server_ssrcs.
- Where: webrtc/modules/congestion_controller/receive_side_congestion_controller.cc:87-102; GroupJoinPayloadInternal.cpp:251-257; GroupInstanceCustomImpl.cpp:3681-3682, 3734-3770; tgcalls 1c236c09 tools/go_sfu/CLAUDE.md:88 and REMB-free Fallback section; tools/go_sfu/sfu.go:62
- Change via: server
- Change: Stamp extension 3 on forwarded packets; run GCC per receiver from TWCC; pace output; probe on the server_sources SSRC; choose the simulcast layer per receiver; keep TWCC to publishers.
- Effect: Each viewer gets the best layer its link holds: the Discord-style adaptation in ADR 0004.
- Risk: The hardest part of the SFU; needs loss and delay emulation. Whether senders reach layer 2 under the stock 1052 kbit/s cap is an open question upstream.
- Verified: yes

## Sender layer control (SenderVideoConstraints)
- Today: Default idealHeight 720. For 3-layer camera: layers on at >=180, >=360, >=720. Downgrade delayed 2 s, upgrade immediate. 2-layer screen share has no `active` handling at all, so constraints do not affect it.
- Where: GroupInstanceCustomImpl.cpp:3193-3228, 2432-2441, 2454-2466, 2469-2478, 4293; tweb videoSendParameters.ts:37-52
- Change via: server
- Change: SFU sends the highest height any viewer wants, so unused TOP layers stop encoding.
- Effect: Corrected: saves sender uplink and CPU only. It cannot switch off low layers while keeping high ones, and low layers are capped at 60 and 110 kbit/s, so freed bandwidth does not improve what is still watched.
- Risk: Low. Raise before a viewer needs it, given encoder start time.
- Verified: yes

## Active-speaker forwarding
- Today: Client keeps at most 11 incoming audio channels including the dummy one; a new SSRC waits until one is silent for 1 s. go_sfu forwards all audio. Senders mark voice activity in the audio-level extension. Correction: packets of an unknown SSRC are NOT delivered later: the buffer's delivery code is commented out, confirmed at Android pin 545f5ae5, so speech before the channel exists is lost.
- Where: GroupInstanceCustomImpl.cpp:3148-3155, 3345-3350, 3855-3880; GroupNetworkManager.cpp:36, 211, 309; tgcalls 545f5ae5 GroupInstanceCustomImpl.cpp:2759; tdesktop calls_group_call.cpp:3282-3287, 3291-3310
- Change via: server
- Change: Forward the top N speakers (3-5) by level and VAD bit, with hangover. Deliver participant SSRCs through updateGroupCallParticipants before the first forwarded packet, so the client resolves locally instead of by RPC.
- Effect: Less downlink on bad networks, and no speaker locked out by the channel limit.
- Risk: First-syllable loss if the switch is slow, worse for a speaker whose SSRC the client has not yet seen.
- Verified: yes

## Video participant limits
- Today: groupcall_video_participants_max in app config and unmuted_video_limit on groupCall are server-set.
- Where: reference-src-new/android/TMessagesProj/src/main/java/org/telegram/messenger/MessagesController.java:3172; ios/submodules/AccountContext/Sources/PresentationCallManager.swift:200; tdesktop calls_group_call.cpp:2775, 2802; tdesktop mtproto/scheme/api.tl:1450
- Change via: server
- Change: Set limits sized to SFU egress, for example 30.
- Effect: Product limit only; avoids overload.
- Risk: Low.
- Verified: yes

## Group audio bitrate (native)
- Today: Fixed 32 kbit/s. Android and Desktop never pass outgoingAudioBitrateKbit; iOS reads voice_chat_send_bitrate. In CustomImpl the field trial overrides the allocator range, Opus defines no bitrate range of its own, and the transport cap is 32000 with no video. Same at 1c236c09, e3069322, 545f5ae5.
- Where: GroupInstanceImpl.h:176; GroupInstanceCustomImpl.cpp:2028, 2565-2567, 3042-3046; webrtc/audio/audio_send_stream.cc:499-501, 846-855; Instance.cpp descriptor (no such field); ios PresentationGroupCall.swift:1854-1857
- Change via: client patch
- Change: Remove the field-trial cap and the 32000 limit; adaptive 24-64 kbit/s from a new join-response field.
- Effect: Clearly better timbre: 32 against Discord's 64 (Discord figure from docs/audit/06-call-quality.md:13, which marks its source unverifiable).
- Risk: Raising voice_chat_send_bitrate alone is clamped back on CustomImpl; by code reading, not tested on a device.
- Verified: yes

## Group audio frame size (native)
- Today: Opus ptime 120 ms in CustomImpl, confirmed at all three pins. The encoder picks the next supported length >= ptime, so it is honoured. One lost packet is 120 ms of speech.
- Where: GroupInstanceCustomImpl.cpp:1409, 2568; webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:131-137; tgcalls 545f5ae5 GroupInstanceCustomImpl.cpp:829, 1975; e3069322:1576, 2736
- Change via: client patch
- Change: 20 ms frames (or 40), FEC retained.
- Effect: About 100 ms less mouth-to-ear delay and much smaller loss bursts (estimate from audit 06). The most audible single change after the network path.
- Risk: Packet rate up about 6x; bounded by active-speaker forwarding.
- Verified: yes

## Receive jitter buffer floor (missed)
- Today: Every incoming audio channel sets audio_jitter_buffer_min_delay_ms=50 and fast_accelerate.
- Where: GroupInstanceCustomImpl.cpp:1398-1399; tgcalls 545f5ae5 GroupInstanceCustomImpl.cpp:819
- Change via: client patch
- Change: Revisit the floor together with shorter frames, for example 0-20 ms on good links.
- Effect: Up to about 50 ms less delay, in the same patch as ptime (estimate).
- Risk: More underruns on jittery links if lowered unconditionally.
- Verified: yes

## Audio loss recovery (NACK / RED)
- Today: Native send description has transport-cc and in-band FEC; the receive description has only FEC, no transport-cc param (the earlier list merged them). No nack, no RED. WebRTC-Audio-OpusMinPacketLossRate is 1%.
- Where: GroupInstanceCustomImpl.cpp:1411-1413, 2029, 2570-2576
- Change via: client patch
- Change: Add RED to native send and receive descriptions and let the SFU forward it; or negotiate audio NACK.
- Effect: Survives bursts FEC does not; large on bad mobile networks.
- Risk: Needs a coordinated native rollout. For Web see the join-response SDP entry.
- Verified: yes

## SFU RTCP receiver reports set the sender's Opus FEC (missed)
- Today: The native sender feeds fraction_lost from RTCP report blocks into Opus packet-loss percent. Without reports from the SFU it stays at the 1% floor; go_sfu's documented feedback is TWCC.
- Where: webrtc/audio/channel_send.cc:567-571; webrtc/modules/audio_coding/codecs/opus/audio_encoder_opus.cc:478-488, 730-737; GroupInstanceCustomImpl.cpp:2029
- Change via: server
- Change: Send accurate per-publisher RR for audio SSRCs.
- Effect: Server-only: FEC strength follows real uplink loss, with no client patch.
- Risk: At fixed 32 kbit/s more FEC takes bits from voice. Not run; go_sfu's RR behaviour was not read beyond its notes.
- Verified: yes

## ICE role per client type (missed; corrects the notes)
- Today: CustomImpl is hard-coded CONTROLLED, so the SFU must be controlling for it. PeerConnection clients (Web and the iOS reference engine) are not: go_sfu takes an iceControlling flag and is controlled for them. The join payload says nothing about the engine.
- Where: GroupNetworkManager.cpp:419-421; tgcalls 1c236c09 tools/go_sfu/sfu.go:148-150, participant.go:221-232; GroupJoinPayloadInternal.cpp serialize; tweb groupCallsController.ts:570, sdpBuilder.ts:89
- Change via: server
- Change: Decide the role per participant from the first inbound binding request (ICE-CONTROLLED or ICE-CONTROLLING).
- Effect: Without it either native or Web fails to connect.
- Risk: pion fixes the role at Dial or Accept, so the SFU must see that request first. Test on all four clients. tweb comments the SFU is ICE-lite yet the a=ice-lite line is commented out, so Chrome's real role needs a live check.
- Verified: yes

## Path migration on network change (missed)
- Today: Native regathers continually, but as the controlled side it depends on the SFU to nominate. pion's standard nomination sets a pair only if none is selected; switching needs renomination. Renomination is optional in pion and uses attribute 0xC001, which libwebrtc accepts. go_sfu uses a plain Lite agent with no renomination.
- Where: GroupNetworkManager.cpp:407-410; webrtc/p2p/base/connection.cc:643-659; webrtc/api/transport/stun.h:758; ~/go/pkg/mod/github.com/pion/ice/v4@v4.2.5/selection.go (controllingSelector.HandleSuccessResponse), renomination.go:18; tgcalls 1c236c09 tools/go_sfu/participant.go:127-130
- Change via: server
- Change: Enable renomination on controlling agents, or send to the latest authenticated source address.
- Effect: Wi-Fi to LTE without a rejoin: central to bad-network quality and absent from the first list.
- Risk: Not run. Automatic renomination could flap between forwarder and direct paths.
- Verified: yes

## Web media parameters come from the join response (missed)
- Today: Web builds its answer SDP from the server's audio and video payload-types, including fmtp parameters and rtcp-fb. The server can therefore set Opus parameters and list red for Web without a patch. Web layer caps stay local; Web has no total cap equivalent to 1052 kbit/s.
- Where: tweb src/lib/calls/sdpBuilder.ts:185-186, 221-240; types.d.ts:61-64; groupCallConnectionInstance.ts:495-497; helpers/videoSendParameters.ts:37-52
- Change via: server
- Change: Send audio.payload-types tuned for Web: opus with useinbandfec, a bitrate parameter, optionally red.
- Effect: Possibly better Web audio with no patch.
- Risk: How Chrome reacts to each fmtp value or to red in the answer is from memory, not read or tested.
- Verified: no

## iOS PeerConnection group engine switch
- Today: ios_calls_group_reference_impl turns on GroupInstanceReferenceImpl. Corrected at the pinned commit: at e3069322 (iOS 12.9.2, now read) it sets minptime=10 and useinbandfec with NO ptime, which is WebRTC's 20 ms default; only c1de0882 (iOS 13.2) sets ptime 60, maxptime 120. Sender max_bitrate is the configured kbit x 1024 at both. No Audio-Allocation trial in that file.
- Where: tgcalls e3069322 tgcalls/group/GroupInstanceReferenceImpl.cpp:612, 938-939; c1de0882 same file:862, 1223-1241; reference-src/ios/submodules/TelegramCallsUI/Sources/PresentationGroupCall.swift:1867; reference-src-new/ios same file:1887-1897; Android Instance.cpp (reference-src-new):632; calls_group_call.cpp:3148
- Change via: server
- Change: Set ios_calls_group_reference_impl=1 and voice_chat_send_bitrate=48-64 for iOS only, after a test.
- Effect: On the shipped 12.9.2: 20 ms frames (6x shorter) and a higher bitrate with no patch. On 13.2: 60 ms.
- Risk: Opt-in upstream engine, never run here. It is a PeerConnection client, so it needs the other ICE role. Its video path was not read. Behaviour differs between iOS versions and from other platforms.
- Verified: yes

## Group video bitrate ceilings
- Today: Total send cap (1020+32)*1000 = 1.052 Mbit/s, plus SDP bandwidth 1.3 Mbit/s. Camera layers max 60/110/900 (sum 1070, above the 1020 left after audio). Start 400 kbit/s. minOutgoingVideoBitrateKbit is dead code: settings.min is overwritten from preferences.
- Where: GroupInstanceCustomImpl.cpp:3032-3050, 2454-2466, 2375, 2386, 1557, 1598; webrtc/pc/channel.cc:110; tgcalls 545f5ae5 GroupInstanceCustomImpl.cpp:2444-2453; tweb videoSendParameters.ts:37-52
- Change via: client patch
- Change: Join-response fields for layer and total caps, stock defaults. Targets about 150/500/2000 kbit/s, total 3-4 Mbit/s. Change the 1.3 Mbit/s set_bandwidth too.
- Effect: The main visible step above Telegram for webcams.
- Risk: Only after NACK and BWE are in the SFU, otherwise bad networks get worse (audit 06 section 9).
- Verified: yes

## Screen share resolution, fps, bitrate
- Today: Separate instance with 2 layers, 100 kbit/s and 1 Mbit/s. Correction: that instance has its own Call and its own 1.052 Mbit/s cap, so camera plus screen can reach about 2.1 Mbit/s with two competing estimators. Desktop capture aspect 1280x720 at 24 fps (confirmed at 1c236c09). Android captures at screen size, 30 fps; iOS accepts up to 30 fps. is_screencast is off on iOS only.
- Where: GroupInstanceCustomImpl.cpp:2314-2318, 2432-2441, 3036-3037, 3544-3548; tgcalls 1c236c09 tgcalls/platform/tdesktop/VideoCapturerInterfaceImpl.cpp:49-50; VideoCapturerDevice.java:33, 171; ios BroadcastUploadExtension.swift:237; webrtc/media/engine/webrtc_video_engine.cc:1839-1845
- Change via: client patch
- Change: Desktop capture 1920x1080 at 30 fps; cap 4-6 Mbit/s; top layer 3-5 Mbit/s. Resolution-first degradation already applies wherever is_screencast is set; only iOS needs that change.
- Effect: Readable text: Go Live level. Where Telegram loses most visibly.
- Risk: Encoder CPU on weak desktops.
- Verified: yes

## Camera capture
- Today: 1280x720 at 30 fps on Android and Desktop. Receiver constraints stop at 720. iOS camera capture not read.
- Where: VideoCapturerDevice.java:31-33; tgcalls 1c236c09 tgcalls/platform/tdesktop/VideoCameraCapturer.cpp:21-23; GroupInstanceCustomImpl.cpp:3375-3400
- Change via: client patch
- Change: Keep 720p first; 1080p only after bitrate work, with a new quality tier.
- Effect: Modest. 720p at 2 Mbit/s already beats 720p at 900 kbit/s.
- Risk: The protocol has three named qualities; a fourth touches clients.
- Verified: yes

## VP8 temporal layers
- Today: WebRTC-VP8ConferenceTemporalLayers/1/: one temporal layer (WebRTC default is 3).
- Where: GroupInstanceCustomImpl.cpp:2031; webrtc/video/config/simulcast.cc:55, 134-151
- Change via: client patch
- Change: Use 2-3 temporal layers.
- Effect: SFU gains a fine adaptation step without switching layer or requesting a keyframe.
- Risk: SFU must parse VP8 descriptors; H264 path gains nothing.
- Verified: yes

## phone.getCallConfig and phoneConnection for group calls
- Today: No effect. GroupInstanceDescriptor has no custom-parameter or server-config input. Tried to refute through Android: enable_vp8/h264_* are parsed from the config but nothing reads them, and getGlobalServerConfig is used only in the private-call path.
- Where: GroupInstanceImpl.h:156-190; reference-src-new/android/.../voip/Instance.java:456-463; VoIPService.java:3524; teamgram-server app/bff/voipcalls/internal/core/phone.getCallConfig_handler.go:25
- Change via: not possible
- Change: Use the join response and app config instead.
- Effect: None.
- Risk: None.
- Verified: yes

## TCP candidate of type ssltcp (missed)
- Today: libwebrtc accepts protocol `ssltcp` from a remote candidate and opens the TCP connection through a fake-TLS socket. Native passes candidate.protocol through unchanged.
- Where: webrtc/p2p/base/tcp_port.cc:274-275, 585-586; webrtc/p2p/base/basic_packet_socket_factory.cc:157-160; GroupInstanceCustomImpl.cpp:3701-3703
- Change via: server
- Change: Offer an extra ssltcp candidate on 443 and implement the pseudo-TLS handshake on the SFU.
- Effect: Traffic on 443 that begins like TLS, for networks that drop non-TLS on that port. Whether it passes real DPI is unknown.
- Risk: Static fake handshake is likely easy to fingerprint (from memory). Native only. Measure before building.
- Verified: no

## Rejoin speed through phone.checkGroupCall (missed; corrects the notes)
- Today: Not a general keepalive on Desktop: it runs only in state Connecting, every 4 s. Android polls by short-poll. The client rejoins only if the server drops its SSRC from the answer.
- Where: tdesktop calls_group_call.cpp:55, 3741-3759; reference-src-new/android/.../voip/VoIPService.java:2855-2870
- Change via: server
- Change: When the SFU restarts or a transport dies, remove those SSRCs from checkGroupCall's answer at once.
- Effect: Recovery in roughly one 4 s tick instead of a stuck Connecting state.
- Risk: Dropping too eagerly causes needless rejoins during brief outages.
- Verified: yes

## Broadcast (stream) mode
- Today: A join response with stream:true switches the client to polling segments through upload.getFile, with buffering of segment plus buffer duration (the earlier citation of VideoStreamingPart was only a signature check; the exact delay was not read).
- Where: tdesktop calls_group_call.cpp:116-123, 3203-3225; VoIPService.java:1719; tgcalls/group/StreamingMediaContext.cpp:288
- Change via: server
- Change: Never return stream:true for calls. Skip stream channels and segment generation until live streams are a product goal.
- Effect: Removes a large piece of work and keeps latency low at 100 users or fewer.
- Risk: RTMP and live streams stay unavailable; their methods keep ErrEnterpriseIsBlocked.
- Verified: yes

## Conference (E2E) calls
- Today: One video layer. Corrected: the 1.8 Mbit/s is only the layer value; native is still under the 1.052 Mbit/s total, since adjustBitratePreferences has no conference branch (Web has no total cap). Not fully opaque: VP8 and H264 frames keep a plaintext header. Audio level in the RTP extension may be quantized to 0/106/127.
- Where: GroupInstanceCustomImpl.cpp:1275-1285, 2036, 2481, 3032-3039, 3544; GroupNetworkManager.cpp:70-80; tweb videoSendParameters.ts:43, 50, 57-60; api.tl:3130
- Change via: server
- Change: Separate later phase: phone.createConferenceCall plus chain blocks. The same SFU can carry it.
- Effect: Encrypted 1:1-to-group upgrade. Less adaptation, since there is no simulcast.
- Risk: With one layer the SFU can only forward or drop. Raising the native cap needs the same client patch.
- Verified: yes

## Notes
REVIEW RESULT
Opened the cited lines. Facts changed: iOS app-config key for VP8; iOS reference engine at its pin; effect of SenderVideoConstraints; H264 effect and risk; go_sfu's TWCC and RTX; screen-share cap per instance; conference cap and opacity; audio receive description; degradation already resolution-first; unknown-SSRC buffering; ICE role; checkGroupCall. Added: RR-driven FEC, ICE role, path migration, Web SDP from the server, jitter floor, ssltcp, rejoin. Tried and failed to refute: getCallConfig having no group effect, the 32 kbit/s clamp, ptime 120, the 10-channel limit, no TURN.

SOURCES AND THEIR LIMITS
- Line numbers without a commit are the Android 12.9.1 copy at ~/pluma-work/reference-src/android/TMessagesProj/jni/voip (tgcalls/ and webrtc/). tgcalls submodules are empty in reference-src-new.
- Pins now resolved: Android 13.2.1 = 545f5ae5 (git submodule status), Desktop 7.4.1 = 1c236c09, iOS 12.9.2 = e3069322, iOS 13.2 = c1de0882. Read raw from TelegramMessenger/tgcalls with curl piped to grep, not through a summarising fetch. Audio caps, ptime, trial and the 10-channel limit match at the pins I grepped. Only those lines were compared, not whole files.
- ICE remote mode differs: FULL at 1c236c09 and 545f5ae5, LITE in the 12.9.1 copy (GroupNetworkManager.cpp:421). The SFU must be tested against both.
- Nothing was run against a real client. "verified" means the code was read, not that behaviour was observed. Work sizes are estimates.
- Voice processing is not a limiter: RNNoise and WebRTC NS already exist (GroupInstanceCustomImpl.cpp:764-768, 2552-2553), with no server knob.

WHAT THE SERVER MUST SPEAK
Join (phone.joinGroupCall#8fb53057, api.tl:3130; joinGroupCallPresentation, api.tl:3146). params JSON from the client (GroupJoinPayloadInternal.cpp serialize): ssrc (signed int32), ufrag, pwd, fingerprints[{hash, fingerprint, setup}], ssrc-groups[{semantics, sources}]. No SDP, no codec list.
Answer: updateGroupCallConnection#b783982 (api.tl:418) in the Updates of the join. params JSON: transport{ufrag, pwd, fingerprints, candidates[{port, protocol, network, generation, id, component, foundation, priority, ip, type}]}, all candidate fields strings and required, plus video{endpoint, payload-types, rtp-hdrexts, server_sources}. Native parses only transport and video, and ignores rtp-hdrexts (hard-coded at GroupInstanceCustomImpl.cpp:3009-3012). Web also needs audio{payload-types, rtp-hdrexts} (tweb types.d.ts:61-64) and uses the ids it receives.
Transport: one ICE + DTLS-SRTP + SCTP per instance; screen share adds a second. Native is ICE controlled and DTLS server (GroupNetworkManager.cpp:419-420, 435). CORRECTED: the SFU is DTLS client for all clients (tweb processMediaSection.ts sets passive), but its ICE role must be per client; tweb's comment there is about DTLS only and does not support "ICE controlling".
RTP: Opus PT 111; native video PT 100-105 assigned locally. Extensions 1 audio-level, 2 abs-send-time, 3 transport-cc, 13 video-orientation. Reduced-size RTCP, rtcp-mux. SFU emits TWCC and RR to senders.
SSRCs: audio forwarded unchanged. CORRECTED: an unknown SSRC with PT 111 triggers a description request and is NOT buffered for later delivery (GroupInstanceCustomImpl.cpp:3148-3155, 3345-3350). Desktop answers from its local participant list and goes to phone.getGroupParticipants (api.tl:3136) only if unknown (calls_group_call.cpp:3282-3310). Video: sink binds to the first SIM SSRC (GroupInstanceCustomImpl.cpp:1569-1572, 1609), so forward any layer as layer-0 SSRC with continuous sequence numbers and timestamps (go_sfu CLAUDE.md confirms).
Data channel: SCTP 5000/5000, client is DCEP opener (GroupNetworkManager.cpp:455-457 passes true; SctpDataChannelProviderInterfaceImpl.cpp:32-37, 107), stream id 0, label "data". Colibri: ReceiverVideoConstraints from client (GroupInstanceCustomImpl.cpp:3367-3423); SenderVideoConstraints and DebugMessage from server.
Recovery: see the checkGroupCall entry; it is not a Desktop keepalive.
Also needed: phone.createGroupCall, getGroupCall, getGroupParticipants, editGroupCallParticipant, leaveGroupCall, leaveGroupCallPresentation, discardGroupCall, inviteToGroupCall, exportGroupCallInvite, getGroupCallJoinAs, toggleGroupCallSettings; updateGroupCall and updateGroupCallParticipants with versioning; chatFull/channelFull.call; service messages.

CAN AN OPEN SFU SIT BEHIND THIS UNCHANGED? No, not as a product.
- mediasoup: IceServer answers 487 to any request with ICE-CONTROLLED (worker/src/RTC/ICE/IceServer.cpp:473-482, branch v3, read raw); tgcalls sends exactly that (webrtc/p2p/base/connection.cc:1045-1049). Its SSRC behaviour was not re-read in this pass.
- LiveKit: earlier reading of protocol@v1.45.3 and downtrack.go not repeated in this pass; treat as carried over. Licence from memory, verified:false.
- Janus: from memory, verified:false.
- Pion libraries directly: yes; go_sfu is this. pion/ice v4 falls back to a full agent for lite with controlling (selection.go:485-495), and go_sfu runs Lite:true (participant.go:130).
- Cloudflare Realtime SFU: incompatible (ADR 0004, inference).

BEST DESIGN
A Pion-based own SFU, one Go service, using go_sfu as a reference. Licence now read: the tgcalls repository is LGPL-3 at its root, so copying go_sfu code brings LGPL obligations; writing our own against the documented wire contract avoids that (legal reading is the owner's call). One muxed UDP port, IPv4 and IPv6, TCP 443, forwarders by GeoIP. ICE role per participant, renomination for path changes. A bff `phone` area talks to it over gRPC. Per receiver: RTX cache, GCC from TWCC, pacer, simulcast selector with layer-0 rewrite, top-N speakers, server_sources probing. Per sender: TWCC, RR, NACK, PLI limit, SenderVideoConstraints. Prometheus metrics by participant and country.
Then one tgcalls patch in three trees plus tweb: a `quality` object in the join response with stock defaults (ptime, audio min/max, jitter floor, layer caps, total cap, screen capture size and fps), field trial removed, optional RED and TURN.

ROUGH SIZE (estimates, one engineer)
- Group TL handlers, storage, updates, versioning, invites: 2-3 weeks.
- SFU baseline (ICE both roles, DTLS, SCTP, Colibri, forwarding, layer-0 rewrite, ICE-TCP, renomination): 2-3 weeks, possibly more for roles and migration.
- NACK/RTX, GCC, pacer, layer selection, speakers, metrics, emulation tests: 3-5 weeks.
- tgcalls patch and web parameters, four-client verification: 2-3 weeks.
- Conference E2E: extra 2-3 weeks, can wait.
Total about 9-14 weeks.

BOTTOM LINE
Server alone can match Telegram and beat it on adaptation, network path, handover and loss response. It cannot beat Telegram's 32 kbit/s, 120 ms audio or the 1.05 Mbit/s video ceiling on Android and Desktop: better sound and picture there needs the client patch, after SFU adaptation (ADR 0004 order). Two partial exceptions, neither tested: iOS through the reference engine (20 ms at 12.9.2, 60 ms at 13.2) and Web audio through server-sent SDP.

CORRECTIONS TO EXISTING DOCS
- Audit 06 omits the set_bandwidth 1.3 Mbit/s cap and the channel limit on incoming audio.
- ADR 0004 "clients do not send REMB" holds only while forwarded packets carry extension 3 (receive_side_congestion_controller.cc:87-102). Transport-cc remains the right choice.
- ADR 0004 assumes go_sfu is "brought to production": its join never fills server_sources and uses a different field name (sfu.go:62), it is UDP4 only (participant.go:128), has no NACK, and has no renomination. TWCC to senders and a layer selector are already there.
- ADR 0004 does not cover the per-client ICE role or path migration.
