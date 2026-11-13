-- Group calls of basic groups, supergroups and channels (task 093, ADR 0059).
-- The media live in the SFU only; these tables are the state the clients see.
--
--   group_calls                    one row per call. live is 1 until the call
--                                  is discarded and NULL after, so that a peer
--                                  has at most one call that is not over.
--                                  version grows by one with every join and
--                                  leave (pkg/groupcalls) and is never 0.
--                                  posted: 1 the start (or the schedule) is in
--                                  the history, 2 the end is.
--   group_call_participants        who is in a call and was: a row is kept
--                                  after its peer leaves (in_call 0, no
--                                  sources), so that what an admin set holds
--                                  when the peer returns. The peer is the
--                                  account itself (joining as a channel is
--                                  not offered, ADR 0060); user_id repeats
--                                  it. One account is in a call once.
--   group_call_viewer_prefs        what a user set for another participant for
--                                  himself only: muted_by_you and the volume.
--   group_call_subscriptions       who waits for a scheduled call to start.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `group_calls` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `peer_type` tinyint(4) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `creator_id` bigint(20) NOT NULL,
  `random_id` int(11) NOT NULL,
  `title` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `join_muted` tinyint(1) NOT NULL DEFAULT '0',
  `version` int(11) NOT NULL DEFAULT '1',
  `schedule_date` int(11) NOT NULL DEFAULT '0',
  `date` int(11) NOT NULL,
  `start_date` int(11) NOT NULL DEFAULT '0',
  `end_date` int(11) NOT NULL DEFAULT '0',
  `duration` int(11) NOT NULL DEFAULT '0',
  `live` tinyint(1) DEFAULT '1',
  `listener_hash` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `speaker_hash` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `sfu_node` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `posted` tinyint(4) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `live_peer` (`peer_type`, `peer_id`, `live`),
  UNIQUE KEY `creator_random` (`creator_id`, `peer_type`, `peer_id`, `random_id`),
  KEY `peer_calls` (`peer_type`, `peer_id`, `date`),
  KEY `live_calls` (`live`, `end_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Group calls of chats and channels';

CREATE TABLE IF NOT EXISTS `group_call_participants` (
  `call_id` bigint(20) NOT NULL,
  `peer_type` tinyint(4) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `auth_key_id` bigint(20) NOT NULL DEFAULT '0',
  `in_call` tinyint(1) NOT NULL DEFAULT '0',
  `source` bigint(20) DEFAULT NULL,
  `presentation_source` bigint(20) DEFAULT NULL,
  `muted` tinyint(1) NOT NULL DEFAULT '0',
  `can_self_unmute` tinyint(1) NOT NULL DEFAULT '1',
  `volume` int(11) NOT NULL DEFAULT '0',
  `raise_hand_rating` bigint(20) NOT NULL DEFAULT '0',
  `video_joined` tinyint(1) NOT NULL DEFAULT '0',
  `video_stopped` tinyint(1) NOT NULL DEFAULT '1',
  `video_paused` tinyint(1) NOT NULL DEFAULT '0',
  `video` varchar(2048) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `presentation` varchar(2048) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `presentation_paused` tinyint(1) NOT NULL DEFAULT '0',
  `date` int(11) NOT NULL DEFAULT '0',
  `active_date` int(11) NOT NULL DEFAULT '0',
  `seen_at` bigint(20) NOT NULL DEFAULT '0',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`call_id`, `peer_type`, `peer_id`),
  UNIQUE KEY `call_source` (`call_id`, `source`),
  UNIQUE KEY `call_presentation_source` (`call_id`, `presentation_source`),
  KEY `call_users` (`call_id`, `user_id`, `in_call`),
  KEY `user_calls` (`user_id`, `in_call`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Participants of group calls';

CREATE TABLE IF NOT EXISTS `group_call_viewer_prefs` (
  `call_id` bigint(20) NOT NULL,
  `viewer_id` bigint(20) NOT NULL,
  `peer_type` tinyint(4) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `muted` tinyint(1) NOT NULL DEFAULT '0',
  `volume` int(11) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`call_id`, `viewer_id`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='What a viewer set for a participant of a group call';

CREATE TABLE IF NOT EXISTS `group_call_subscriptions` (
  `call_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`call_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Who waits for a scheduled group call';
