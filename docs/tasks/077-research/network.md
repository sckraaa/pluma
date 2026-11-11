# Call quality: network

Read from the client sources pinned in upstream.env of each client. `Verified: no` marks what was not confirmed from a source.

## Any relay at all (coturn plus CALLS_ICE_SERVERS)
- Today: Not deployed. An empty list means phone.requestCall answers CALLS_NOT_CONFIGURED; no real call has ever run (STATUS.md, entry 068). iOS needs a non-empty list because it takes connections.first!; phone.confirmCall already ends the call if no server resolves.
- Where: teamgram origin/master docs/ops/calls.md:3,13; pkg/phonecalls/ice/ice.go:155; app/bff/voipcalls/internal/core/phone.confirmCall_handler.go (comment before conns := c.connections); ios/submodules/TelegramCore/Sources/State/CallSessionManager.swift:1658
- Change via: infrastructure
- Change: Deploy coturn and set CALLS_ICE_SERVERS and CALLS_TURN_SECRET. Topology is in notes.
- Effect: Precondition for every other item. Web K keeps only phoneConnectionWebrtc (tweb/src/lib/calls/callInstance.ts:736), so TURN is the one relay type all four clients share.
- Risk: An open relay next to MySQL/Redis if peers are not restricted (see the coturn item).
- Verified: yes

## Which relay wins: the order of the connections list
- Today: Static config order, and the same list for both sides. Each TURN server gets relative_priority counting down from the list length, so the first entry has the highest candidate priority. Order of comparison: connection state (writable, receiving), then on the controlled side nomination, then network preference and cost (a relay pair on Wi-Fi can beat one on cellular), then pair priority, and RTT only on an exact tie. Clients do not choose the fastest relay. On native versions 7-13 every relayed path is relay to relay (see the skip_relay item), so each side's own first entry decides its own entry point.
- Where: webrtc p2p/client/basic_port_allocator.cc:1579-1581; p2p/base/turn_port.cc:891; api/candidate.cc:149; p2p/base/basic_ice_controller.cc:534-539,651-668,693-725,732-762; teamgram pkg/phonecalls/ice/ice.go:155; app/bff/voipcalls/internal/core/phone.confirmCall_handler.go; phone.acceptCall_handler.go
- Change via: server
- Change: Give Connections a per-side input (country via the geoip service) and sort the nearest relay first, separately for admin and participant. Correction: computing two lists in phone.confirmCall is not enough. That handler runs in the admin's session, so c.MD.ClientAddr is the admin's address; phone.acceptCall does not record the participant's. Save the participant's address or country at acceptCall (new column, migration), then build both lists at confirmCall, including the repeat-confirm path.
- Effect: Each client enters at its nearest relay, and a cross-country call rides relay to relay between data centres. This is the closest stock equivalent of Discord's edge entry. Gain not measured.
- Risk: A wrong GeoIP result (for example a client on VPN) only costs latency, since the other relays stay as lower-priority candidates. Needs one shared TURN secret or a per-node secret in ice.Servers (one secret field today).
- Verified: yes

## Second UDP port per relay (UDP 443 next to 3478)
- Today: Docs show 3478 only. phoneConnectionWebrtc is UDP on every client, but the port is free: Android passes it as is, and two entries with the same host and different ports parse fine.
- Where: android jni/voip/org_telegram_messenger_voip_Instance.cpp:958-965; tdesktop calls_call.cpp:174-182; teamgram pkg/phonecalls/ice/ice.go:60-100
- Change via: server
- Change: Add host:443:turn entries after host:3478:stun+turn; on the node redirect UDP 443 to 3478 with nftables. The current coturn sample says DTLS is not started unless dtls is set, so tls-listening-port=443 alone should not open UDP 443 and the redirect does not collide with it; confirm on the installed version.
- Effect: Passes networks that allow only web ports, with no client change. Each entry adds allocations and pairs, so keep the list to about 4 entries.
- Risk: FOCI 2026 reports Russian UDP filtering across all ports, so this may not help there (seen only as a search summary, not re-read). Needs a field test.
- Verified: yes

## p2p_allowed per call
- Today: Set only from the two users' PHONE_P2P privacy, at phone.requestCall. When false, tgcalls disables UDP and STUN ports and removes reflexive candidates; Web K uses iceTransportPolicy relay. When true, a direct pair that is writable and receiving beats relay on the same network: srflx type preference is 100, relay UDP is 2. A direct pair that stops receiving does lose to a receiving relay pair, so only a degraded but still alive direct path stays selected.
- Where: teamgram app/bff/voipcalls/internal/core/phone.requestCall_handler.go:99; pkg/phonecalls/phonecalls.go:251; tgcalls v2/NativeNetworkingImpl.cpp:604-608; tweb/src/lib/calls/callInstance.ts:1487; webrtc p2p/base/p2p_constants.h:119-122; p2p/base/basic_ice_controller.cc:651-668
- Change via: server
- Change: AND the privacy result with a per-country-pair policy from config, once measurements show relay is better. Correction: requestCall runs before the participant's address is known, so the policy must be evaluated at phone.confirmCall and stored there (Store.Confirm), not at the current assignment.
- Effect: Removes bad but alive direct paths. This is the only server-side way to override the P2P preference; dropping the stun flag would not, because every UDP TURN also acts as STUN (basic_port_allocator.cc:1734-1742).
- Risk: Doubles relay traffic for those calls. The flag is per call, not per side. Turning it on without measurements could make good direct calls worse.
- Verified: yes

## TURN over TCP and TLS on 443
- Today: Impossible on stock clients. The TL type has no tcp flag; Android, iOS and tdesktop never set isTcp for webrtc entries. NativeNetworkingImpl knows only PROTO_TCP or PROTO_UDP, at all three pinned tgcalls commits (545f5ae, c1de088, 1c236c0). Web K builds only turn:host:port. On a UDP-blocked network a call fails. The engine underneath supports PROTO_TLS, and TCP relay is unaffected by PORTALLOCATOR_DISABLE_TCP.
- Where: pkg/catalog/tl/api.tl:1054; android jni/voip/org_telegram_messenger_voip_Instance.cpp:958-965; ios/submodules/TelegramVoip/Sources/OngoingCallContext.swift:100; tdesktop calls_call.cpp:174-182; tgcalls v2/NativeNetworkingImpl.cpp:626; tweb/src/lib/calls/p2P/utils.ts:204; webrtc p2p/base/turn_port.cc:411-417, p2p/client/basic_port_allocator.cc:1510, p2p/base/basic_packet_socket_factory.cc:152, rtc_base/ssl_roots.h
- Change via: client patch
- Change: tgcalls: add isTls to RtcServer (Instance.h:48-56), map it to PROTO_TLS at NativeNetworkingImpl.cpp:626, set tls_cert_policy on the config (ReflectorRelayPortFactory passes it on). Signal it by a port convention inside tgcalls (audit C4) or a new flag in the three native mappings. Web K: emit turns:host:443?transport=tcp. Certificate: the TL field carries an IP, so SNI and name checking need a hostname from elsewhere. The bundled root store has no ISRG root (0 matches in ssl_roots.h, generated 2023), so a Let's Encrypt certificate fails under the default SECURE policy; use a certificate pinned through tls_cert_verifier, or a CA that is in the bundle. Versions 18/19 need the same change separately (the core skips isTcp servers, ReferenceCallCore).
- Effect: The largest reachability gain. Relay preference is TLS 0, TCP 1, UDP 2, so these candidates are used only when UDP relay fails and do not degrade good networks.
- Risk: TCP head-of-line blocking raises latency on loss, so it is a last resort. With INSECURE_NO_CHECK a middlebox can read the per-call TURN credential but not media. Needs a test on all clients.
- Verified: yes

## Own Telegram reflector (phoneConnection, tcp flag)
- Today: Not built, no open server exists (audit 04). iOS removes TCP reflectors from media and uses the first only for signalling; tdesktop and Android do use them as PROTO_TCP (Android sets username reflector in Instance.java:236). Web K ignores phoneConnection. Reflector candidates only route inside the same server id, so cross-region relay to relay is out.
- Where: ios/submodules/TelegramVoip/Sources/OngoingCallContext.swift:1019-1024; tdesktop calls_call.cpp:120-146; android src/main/java/org/telegram/messenger/voip/Instance.java:235-237; tgcalls v2/ReflectorPort.cpp:593, v2/ReflectorRelayPortFactory.cpp:20; tweb/src/lib/calls/callInstance.ts:736
- Change via: server
- Change: Do not build it. Corrects audit 06 S3, which calls it the TCP fallback without a client patch: it would not cover iOS or Web.
- Effect: Avoids 1-2 weeks of work (audit's estimate) for partial coverage.
- Risk: None in skipping it, given the clients are patched anyway.
- Verified: yes

## phoneCall.custom_parameters network_* flags
- Today: Server sends none. Android and iOS pass the JSON to tgcalls; tdesktop does not. Correction: network_skip_initial_ping is not needed on desktop, because tdesktop's pinned tgcalls (1c236c0) sets presume_writable_when_fully_relayed unconditionally; it is conditional at 545f5ae (Android) and absent at c1de088 (iOS). The list also missed network_use_default_route (one adapter only) and network_enable_shared_socket (host, srflx and UDP relay on one socket, so one NAT binding).
- Where: tgcalls v2/NativeNetworkingImpl.cpp:588-594,647-649; webrtc p2p/client/basic_port_allocator.cc:1626-1629; webrtc p2p/base/basic_ice_controller.cc:PresumedWritable; android VoIPService.java:3529; ios OngoingCallContext.swift:1096; tdesktop calls_call.cpp:1155-1170; github TelegramMessenger/tgcalls@1c236c0 tgcalls/v2/NativeNetworkingImpl.cpp (read through a fetch summary, no line number)
- Change via: server
- Change: network_skip_initial_ping can be set on all calls, not only relay-only ones: the presumption applies only to a pair that is relay on both ends, and direct pairs still win when they become writable. Test network_enable_shared_socket on carrier NAT. Do not use network_use_default_route: it gives up the second interface that handover relies on. Patching tdesktop to read custom_parameters (audit C6) is still needed for later server-tunable patches.
- Effect: Slightly faster media start on Android. DTLS still has to complete over the path, so expect about a round trip at most. Not measured.
- Risk: Early packets may be lost before the TURN permission exists. Shared socket changes NAT behaviour and needs a field test.
- Verified: yes

## phone.getCallConfig JSON
- Today: "{}". For the V2 engine it carries no ICE or relay setting. Android reads codec toggles, AEC/NS, hangup timeout and voip_enable_stun_marking; tgcalls only feeds it to the legacy engine.
- Where: android src/main/java/org/telegram/messenger/voip/Instance.java:449-466; tgcalls legacy/InstanceImplLegacy.cpp:349; teamgram app/bff/voipcalls/internal/core/phone.getCallConfig_handler.go:25
- Change via: not possible
- Change: No transport gain here. Leave voip_enable_stun_marking absent or false for Android; iOS is a separate source (next item).
- Effect: None on transport.
- Risk: None.
- Verified: yes

## iOS STUN marking (missed; corrects the earlier 'keep it off')
- Today: On iOS it is ON today. shouldEnableStunMarking returns true when the app config lacks the key, and teamgramd/bin/app_config.json has no voip_enable_stun_marking. Each iOS TURN message then carries SOFTWARE="Telegram ". Android defaults to false from getCallConfig.
- Where: ios/submodules/TelegramCallsUI/Sources/PresentationCallManager.swift:21-29,362; tgcalls TurnCustomizerImpl.cpp:14; android Instance.java:455; teamgram teamgramd/bin/app_config.json
- Change via: server
- Change: Add "voip_enable_stun_marking": false to help.getAppConfig (app_config.json).
- Effect: Removes a literal application signature from iOS TURN traffic, one line, before the first real call.
- Risk: None found. It does not hide the STUN magic cookie, so it is not a defence against STUN filtering by itself.
- Verified: yes

## Library version negotiation
- Today: Server picks the string-greatest common version: 9.0.0 between native clients, 13.0.0 with Web K. Correction: the version list above was from Android 12.9.1. Current clients also offer 18.0.0 (native core) and 19.0.0 (WASM core) through InstanceV2PumpImpl: iOS always, Android only on aarch64, tdesktop not at all. These are a different stack on PeerConnection: ICE restart on failed (caller only, at most every 5 s), renomination, no skip_relay trial, the same 20 s watchdog and the same 32 kbit/s and 1200 kbit/s caps. 9.0.0 still wins because "9" sorts above "19". 9.0.0 signalling has no retransmission; 12/13 use SCTP.
- Where: teamgram pkg/phonecalls/phonecalls.go:136; ios/submodules/TgVoipWebrtc/Sources/OngoingCallThreadLocalContext.mm:1852-1859; android jni/voip/org_telegram_messenger_voip_Instance.cpp:31-36; tdesktop calls_call.cpp:73-75; tgcalls v2/InstanceV2Impl.cpp:88-99,1011; v2/ExternalSignalingConnection.cpp; android VoIPService.java:3564; github TelegramMessenger/tgcalls@545f5ae tgcalls/v2wasm/{InstanceV2PumpImpl,ReferenceCallCore,CallCoreHost}.cpp (read through a fetch summary, no line numbers)
- Change via: client patch
- Change: Keep 9.0.0 until tested, but do not close this: the choice is a server edit in Negotiate, while 18.0.0 sorts below 2.7.7 and so turns off the camera on Android outgoing calls unless VoIPService.java:3564 is patched. Candidate policy after a test: numeric comparison preferring 18.0.0 when both sides offer it, falling back to 9.0.0 for desktop and 32-bit Android. The same line already drops the camera on Android outgoing video to Web K at 13.0.0. Avoid 10/11 (skip isTcp servers).
- Effect: 18.0.0 is the only existing way to get ICE restart on native clients without writing it. Quality difference not measured.
- Risk: A newer, less tested path here, and client patches for TCP and timeouts would have to land in two stacks. What real Telegram selects is unverified; if it picks 18/19, Pluma runs an older stack than Telegram on mobile.
- Verified: yes

## Relay failover speed
- Today: IceConfig sets only continual gathering, likely-pair priority and regather. Defaults apply: a connection stops receiving after 2.5 s, backup pairs ping every 25 s, stable pairs every 2.5 s, switching delay 1 s. Correction: 25 s is not the failover time. Once the selected pair is weak, all pairs are pinged at the weak rate, so a switch comes about 2.5 s plus a round trip, up to 1 s more. A silent path turns unreliable after 5 failed pings and 5 s; 15 s is only the final timeout.
- Where: tgcalls v2/NativeNetworkingImpl.cpp:642-650; webrtc p2p/base/p2p_constants.cc:47-49,61-63,69-70; p2p/base/basic_ice_controller.cc:244-261,293-304; p2p/base/connection.cc:943-967
- Change via: client patch
- Change: Set receiving_timeout about 1000-1200 ms together with stable_writable_connection_ping_interval at or below half of it, and a shorter backup_connection_ping_interval, driven from custom_parameters. Values are judgement and need loss-lab tuning.
- Effect: Shorter audio gaps when a path dies, mostly from receiving_timeout.
- Risk: A receiving_timeout below the 2.5 s ping interval, without changing that interval, marks the pair weak whenever the peer sends no media (mute, DTX), which causes flapping. More STUN traffic on mobile.
- Verified: yes

## Path choice by loss and RTT
- Today: Priority first, RTT only as a tie-break with a 10 ms margin. Loss rate is never an input; only complete ping failure (write state, receiving state) is. On the controlled side, nomination outranks priority, so the caller decides the route once both pairs are healthy.
- Where: webrtc p2p/base/basic_ice_controller.cc:16,495-520,534-539,693-725; p2p/base/connection.cc:943-958; tgcalls v2/NativeNetworkingImpl.cpp:660
- Change via: client patch
- Change: A custom ICE controller that demotes a selected pair with high measured RTT or ping loss. It only has effect on the caller's side. Do after failover tuning.
- Effect: Would be a real advantage over stock tgcalls on lossy direct paths. Size unknown.
- Risk: Most invasive item; touches libwebrtc behaviour on three builds.
- Verified: yes

## Call survival during an outage
- Today: Hard-coded: 20 s without connection sets Failed, which ends the call on clients. Same value at all three pinned commits and in the 18/19 core (kConnectionFailureTimeoutMs). V2 does not read receiveTimeout or initializationTimeout, so call_packet_timeout_ms (10000 here) has no effect on it.
- Where: tgcalls v2/NativeNetworkingImpl.cpp:848-854, v2/InstanceV2Impl.cpp:1851; tdesktop calls_call.cpp:1318-1323; teamgram teamgramd/bin/config.json:43; github tgcalls@545f5ae v2wasm/ReferenceCallCore.cpp
- Change via: client patch
- Change: Make maxTimeout a custom parameter, for example 45-60 s after the call has connected; keep 20 s before first connect.
- Effect: Ordinary handovers, tunnels and lifts survive. A block of about 10 minutes, as reported for UDP in Russia, does not.
- Risk: The user sits in Reconnecting longer before a clean failure.
- Verified: yes

## Network change handling
- Today: V2 (7-13) never does an ICE restart: no restart path in InstanceV2Impl or NativeNetworkingImpl, confirmed at 545f5ae. It relies on continual gathering plus new candidates through signalling; Android and iOS have a real network monitor. setNetworkType is an empty function. Web K restarts on its own side; the 18/19 core restarts on failed, caller only.
- Where: tgcalls v2/NativeNetworkingImpl.cpp:521,643,722; v2/InstanceV2Impl.cpp:2041; platform/android/AndroidInterface.cpp:150; platform/darwin/DarwinInterface.mm:62; tweb/src/lib/calls/p2P/connectionRecovery.ts:21-30; tweb/src/lib/calls/callInstance.ts:1785; github tgcalls@545f5ae v2wasm/ReferenceCallCore.cpp
- Change via: client patch
- Change: On a network change, trigger immediate candidate exchange and, if needed, an ICE restart over the existing InitialSetup signalling. Adopt only after measuring Wi-Fi to LTE handover, and compare with 18.0.0 first.
- Effect: Faster Wi-Fi to LTE recovery. Depends on signalling arriving (see next item).
- Risk: Needs both peers patched and a glare rule; Web K's design is a template.
- Verified: yes

## Signalling lost while a device is offline (missed)
- Today: phone.sendSignalingData uses toAuthIfOnline: if the peer device is not online at that instant the bytes are dropped silently, and the result of pushTo is ignored. With 9.0.0 ExternalSignalingConnection sends each message once, with no retransmission, and V2 has no ICE restart. During a handover, exactly when the peer's MTProto link flaps, new candidates can be lost for good.
- Where: teamgram app/bff/voipcalls/internal/core/phone.sendSignalingData_handler.go:43; app/bff/voipcalls/internal/core/core.go:150-157; tgcalls v2/ExternalSignalingConnection.cpp; v2/InstanceV2Impl.cpp:1011,1030
- Change via: server
- Change: Keep a small bounded per-call, per-device queue (for example 30 s, 64 KB, in order) and flush it when the device comes back; the existing 2 s sweep already notices returning devices. Only for active calls.
- Effect: Candidates survive a short disconnect, with no client change. Directly serves the bad-network goal. Size not measured.
- Risk: Replayed old candidates are harmless to ICE but the queue must be dropped at call end. How often a device is really reported offline during a handover is not measured.
- Verified: yes

## skip_relay_to_non_relay_connections (missed)
- Today: tgcalls V2 hard-codes this field trial, so a relay candidate never pairs with a non-relay one. Every relayed path is relay to relay: both sides must reach a TURN server, even if the other side has a public address, and one server carries each packet in and out twice. The 18/19 core does not set it.
- Where: tgcalls v2/InstanceV2Impl.cpp:946; webrtc p2p/base/p2p_transport_channel.cc:1415-1423; github tgcalls@545f5ae v2wasm/CallCoreHost.cpp
- Change via: client patch
- Change: Leave it for now; do not design on one-sided relay. Removing the trial string would allow relay to srflx, one hop shorter. Decide only with route data.
- Effect: Explains the relay load and makes per-side nearest entry work as described. Removal might cut latency where only one side is blocked; size unknown.
- Risk: Telegram set it deliberately, reason not documented; more candidate pairs.
- Verified: yes

## coturn settings
- Today: docs/ops/calls.md has the minimum: 3478 only, no TLS, no peer restriction, no quotas, no metrics. no-tcp-relay there is correct.
- Where: teamgram docs/ops/calls.md:18-31; pkg/phonecalls/ice/ice.go (username); coturn examples/etc/turnserver.conf; tgcalls v2/InstanceV2Impl.cpp:946
- Change via: infrastructure
- Change: Add: denied-peer-ip for RFC1918, link-local and CGNAT; no-multicast-peers; no-software-attribute; tls-listening-port=443 with cert and pkey; prometheus; generous user-quota; total-quota and bps-capacity as a safety cap. Correction: never deny the relay's own public address, or the other relays': native relayed calls are relay to relay, so on one node the peer is that node's own address and such a rule kills every relayed call. no-cli is not in the current sample (cli is off by default), so skip it.
- Effect: Safe relay and the data needed to know the relayed share. Correction on quota: the username is per call and shared by both parties, so count allocations for both sides: entries x networks x families x 2. Set user-quota to at least about 32.
- Risk: A low user-quota silently removes candidates. TURN credentials stay valid 48 h, past the end of the call.
- Verified: yes

## Cloudflare TURN as a second relay outside Russia
- Today: In ADR 0004 but not implemented. Read on the Cloudflare page: anycast; UDP 3478 and 443, TCP 3478 and 80, TLS 5349 and 443; per allocation about 50-100 Mbps and 5-10 kpps, and a limit on new unique IPs per second. Per-call credentials come from audit 06 only (the page read does not describe them). Behaviour for relay to relay traffic is not documented there.
- Where: developers.cloudflare.com/realtime/turn/; teamgram docs/adr/0004-calls-architecture.md:16; docs/audit/06-call-quality.md S2
- Change via: server
- Change: Add a credential fetch and list it after own coturn for clients outside Russia. Resolve to IP literals, as ice.go already does. Test a call with one side on Cloudflare and the other on coturn first.
- Effect: Global entry near the user with no fleet. Reliability backup for the EU node.
- Risk: External dependency and an extra failure path at call start; needs a short timeout. Not useful in Russia per the audit.
- Verified: no

## Measuring routes
- Today: Server sets neither need_rating nor need_debug, so it gets no per-call route data; tgcalls does record route p2p or turn. Correction: Android does upload. The commented block in onTgVoipPreStop is dead code; the live path is VoIPDebugToSend.done, called with phoneCall.need_debug. tdesktop and iOS also handle it.
- Where: tgcalls v2/NativeNetworkingImpl.cpp:987-992; android VoIPService.java:1798-1806; android VoIPDebugToSend.java:52-64; tdesktop calls_call.cpp:885; ios OngoingCallContext.swift:1309; teamgram docs/tasks/068-calls-1to1.md:44
- Change via: server
- Change: Enable coturn Prometheus first; then set need_debug on a sample of calls in phoneCallDiscarded. saveCallDebug must answer true: on false Android goes on to upload a full log through saveCallLog, which is not stored.
- Effect: ADR 0004 makes every further step conditional on measurements; without this they cannot be taken.
- Risk: Debug logs must pass the log audit rules; they contain IPs.
- Verified: yes

## Notes
WHAT CHANGED IN THIS REVIEW. Every local citation was opened. Most entries hold. Real corrections: (1) iOS STUN marking is ON today, the opposite of "keep it off", and one line in app_config.json fixes it. (2) Current clients offer library versions 18.0.0 and 19.0.0, a different stack with ICE restart, which the list missed. (3) New: signalling is dropped when the peer device is offline, with no retransmission at 9.0.0; a server-only fix exists. (4) New: skip_relay_to_non_relay makes every native relayed call relay to relay, so a denied-peer-ip rule covering the node's own IP would kill relayed calls. (5) Per-side lists and per-country p2p_allowed need the participant's address saved at phone.acceptCall; confirmCall only sees the admin. (6) network_skip_initial_ping is already always on in tdesktop. (7) Failover is about 2.5-3.5 s, not governed by the 25 s backup ping, and receiving_timeout below the ping interval flaps on muted calls. (8) Android does upload debug logs. (9) Let's Encrypt is not in the bundled TLS roots. (10) coturn user-quota is shared by both parties.

SOURCE GAP, CORRECTED. The submodules are empty locally, but TelegramMessenger/tgcalls is public and the three pinned commits are fetchable. I read NativeNetworkingImpl.cpp at 545f5ae (Android), c1de088 (iOS) and 1c236c0 (tdesktop), plus InstanceV2Impl.cpp and v2wasm/{InstanceV2PumpImpl,ReferenceCallCore,CallCoreHost}.cpp at 545f5ae. These reads went through a fetch tool that summarizes, so they have quotes but no line numbers; re-read the raw files before writing a patch. Unprefixed "tgcalls" citations remain Android 12.9.1 at ~/pluma-work/reference-src/android/TMessagesProj/jni/voip/tgcalls. For the V2 transport (IceConfig, PROTO mapping, 20 s timeout, field trial, no restart) the current code matched. Not checked: InstanceV2ReferenceImpl at current commits (audit 06 notes a hostname change there). "webrtc" = reference-src-new/android/TMessagesProj/jni/voip/webrtc only. iOS (ali-fareed/webrtc) and tdesktop (tg_owt) build other libwebrtc copies that I did not read, so the ICE-controller details are confirmed for Android only. The earlier list did not state this.

Nothing was run; no call has ever been made on this server, so every effect is a reading of code or a judgement, none measured. Production was not inspected.

HOW IT WORKS TODAY (read). Server gives one list of phoneConnectionWebrtc to both sides. Each client gathers host, srflx (every UDP TURN also acts as STUN, webrtc basic_port_allocator.cc:1734-1742) and relay candidates from every entry (NO_PRUNE, NativeNetworkingImpl.cpp:634). Native pairs direct with direct and relay with relay only. ICE takes the best healthy pair: direct before relay on the same network, then the earlier-listed relay. Signalling goes through phone.sendSignalingData, so it survives UDP blocks but not a moment when the peer's device is offline.

WHAT LIMITS QUALITY, IN ORDER (judgement on ranking): 1) no relay deployed; 2) UDP-only relay, so UDP-blocked networks get no call; 3) lost signalling plus no ICE restart during handover; 4) one identical list, no nearest entry; 5) direct path wins while alive even when worse; 6) slow failover and a hard 20 s death. Server-only gains: relay deployment, stun marking off, signalling queue, nearest-first lists, UDP 443, per-country p2p_allowed, measurement. Client patches: TCP/TLS, failover, 20 s timeout, loss-aware path choice, version 18 on Android.

TOPOLOGY FOR ABOUT 100 USERS (judgement unless marked).
- EU node: a small dedicated VM, not the main CX33: its own IP gives a free 443 for TLS, and media stays off the MTProto host's CPU (audit 06). Public IP on the interface, host networking, no NAT, because native relayed calls on one node are always hairpinned through that node's own relay address. listening-port=3478 (UDP and TCP), tls-listening-port=443, nftables redirect UDP 443 to 3478. The coturn sample confirms TLS and plain can share a port (read).
- Second EU relay at another provider, or Cloudflare TURN, listed after the first: failover comes free because all entries are gathered.
- Russia: a node at a large Russian provider, decided up front only if Russian users are a real share (audit 06). Do not reuse neo_mirror. Owner's call on provider, money, and exposure: a Russian relay sees IP metadata but not media content. Use a per-node TURN secret; ice.Servers needs a small change for that.
- Lists: Russian client gets RU first then EU; others EU first, no RU. At most 4 entries per client.
- Sizing: with today's 1:1 limit of about 1-1.2 Mbit/s video (audit 06), a relayed video call is about 2.5 Mbit/s each way per relay, consistent with relay to relay; 20 at once is about 50 Mbit/s. A 2 vCPU VM suffices. At 4 Mbit/s, about 200 Mbit/s.

RUSSIA, CONFIDENCE. Audit 06 records TCP/UDP freezing after about 16 KB to foreign data centres and mobile whitelists, partly from forum data; not re-read here. Search results say STUN can be detected by signature and dropped ([Meduza](https://meduza.io/cards/v-rossii-perestayut-rabotat-zvonki-v-votsape-i-telegrame-chto-delat-chtoby-ostavatsya-na-svyazi-v-etih-messendzherah), [Habr](https://habr.com/en/articles/971480), secondary) and that UDP filtering spans all ports ([FOCI 2026](https://www.petsymposium.org/foci/2026/foci-2026-0010.pdf)); seen only as a search summary. If true, plain TURN and STUN share the same magic cookie, which is an argument for TLS 443 inside Russia. Unverified; needs a test on MTS, MegaFon, Beeline and T2 with real SIMs. The RU-to-EU relay leg is unmeasured.

NOT VERIFIED: what real Telegram returns in connections (count, TCP flags, regions) and which library version it selects between mobile clients. The project's Web K probe can capture connections, though only for a Web K call, which is limited to 13.0.0. A claim of beating Telegram on transport is not supportable until then. Note that today's selection of 9.0.0 may put Pluma behind Telegram's newer stack on mobile.

Other sources: [coturn turnserver.conf](https://raw.githubusercontent.com/coturn/coturn/master/examples/etc/turnserver.conf), [Cloudflare TURN](https://developers.cloudflare.com/realtime/turn/), [tgcalls](https://github.com/TelegramMessenger/tgcalls). No repository files were changed.
