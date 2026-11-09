-- Task 068: one-to-one calls. The state machine of a call and the
-- authorizations that have been told about a call that is ringing.

CREATE TABLE IF NOT EXISTS `phone_calls` (
  `id` bigint NOT NULL,
  `access_hash` bigint NOT NULL,
  `admin_id` bigint NOT NULL COMMENT 'the user who started the call',
  `participant_id` bigint NOT NULL COMMENT 'the user who is called',
  `admin_auth_key_id` bigint NOT NULL COMMENT 'the device that started the call',
  `participant_auth_key_id` bigint NOT NULL DEFAULT 0 COMMENT 'the device that accepted it, 0 before that',
  `random_id` int NOT NULL,
  `video` tinyint(1) NOT NULL DEFAULT 0,
  `p2p_allowed` tinyint(1) NOT NULL DEFAULT 0,
  `state` tinyint NOT NULL COMMENT 'pkg/phonecalls: 1 requested, 2 accepted, 3 active, 4 discarded',
  `g_a_hash` varbinary(32) NOT NULL DEFAULT '',
  `g_b` varbinary(1024) NOT NULL DEFAULT '',
  `g_a` varbinary(1024) NOT NULL DEFAULT '',
  `key_fingerprint` bigint NOT NULL DEFAULT 0,
  `admin_protocol` text NOT NULL,
  `accept_protocol` text NOT NULL,
  `confirm_protocol` text NOT NULL,
  `date` int NOT NULL,
  `receive_date` int NOT NULL DEFAULT 0,
  `start_date` int NOT NULL DEFAULT 0,
  `end_date` int NOT NULL DEFAULT 0,
  `deadline` int NOT NULL DEFAULT 0 COMMENT 'when the sweep ends a call left in this state, 0 when discarded',
  `discard_reason` tinyint NOT NULL DEFAULT 0,
  `duration` int NOT NULL DEFAULT 0,
  `message_posted` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'the service message is in the history',
  `admin_rating` tinyint NOT NULL DEFAULT 0,
  `admin_comment` varchar(1024) NOT NULL DEFAULT '',
  `participant_rating` tinyint NOT NULL DEFAULT 0,
  `participant_comment` varchar(1024) NOT NULL DEFAULT '',
  `debug` mediumtext NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_admin_random` (`admin_id`, `random_id`),
  KEY `idx_participant_state` (`participant_id`, `state`),
  KEY `idx_deadline` (`state`, `deadline`),
  KEY `idx_posted` (`state`, `message_posted`, `end_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- An authorization named here was told about the ringing call: the live
-- update or updates.getDifference. Android declines a second
-- phoneCallRequested as busy, so a call is shown once per authorization.
CREATE TABLE IF NOT EXISTS `phone_call_deliveries` (
  `call_id` bigint NOT NULL,
  `auth_key_id` bigint NOT NULL,
  PRIMARY KEY (`call_id`, `auth_key_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
