-- Task 105: Telegram Business for every account (ADR 0067, pkg/business).
-- The statements are CREATE TABLE IF NOT EXISTS and nothing else: running
-- the file again changes nothing. The quick replies (the service msg) and
-- the qts queue of bots have files of their own.

-- The Business part of the profile of an account: the work hours, the
-- location and the intro (account.updateBusinessWorkHours,
-- account.updateBusinessLocation, account.updateBusinessIntro,
-- userFull.business_*). A row holds at least one of the three; the row of
-- an account that removed all three is deleted.
CREATE TABLE IF NOT EXISTS `business_profiles` (
  `user_id` bigint NOT NULL,
  `hours_timezone` varchar(64) NOT NULL DEFAULT '' COMMENT 'an id of help.getTimezonesList; empty: no work hours',
  `hours_intervals` varchar(1024) NOT NULL DEFAULT '' COMMENT 'JSON [[start_minute, end_minute], ...], normalized',
  `location_set` tinyint(1) NOT NULL DEFAULT 0,
  `location_address` varchar(512) NOT NULL DEFAULT '' COMMENT 'up to 96 characters',
  `location_has_geo` tinyint(1) NOT NULL DEFAULT 0,
  `location_lat` double NOT NULL DEFAULT 0,
  `location_long` double NOT NULL DEFAULT 0,
  `intro_set` tinyint(1) NOT NULL DEFAULT 0,
  `intro_title` varchar(256) NOT NULL DEFAULT '',
  `intro_description` varchar(1024) NOT NULL DEFAULT '',
  `intro_sticker_id` bigint NOT NULL DEFAULT 0 COMMENT 'the document of the sticker; 0: none',
  `updated_at` bigint NOT NULL COMMENT 'unix ms',
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The automatic replies of an account (account.updateBusinessGreetingMessage,
-- account.updateBusinessAwayMessage): one row per kind that is on.
CREATE TABLE IF NOT EXISTS `business_auto_reply_settings` (
  `user_id` bigint NOT NULL,
  `kind` tinyint NOT NULL COMMENT '1: greeting, 2: away',
  `shortcut_id` int NOT NULL COMMENT 'the quick reply shortcut sent',
  `recipients` varchar(4096) NOT NULL COMMENT 'JSON of pkg/business.Recipients',
  `no_activity_days` int NOT NULL DEFAULT 0 COMMENT 'greeting: 7, 14, 21 or 28',
  `schedule` tinyint NOT NULL DEFAULT 0 COMMENT 'away: 1 always, 2 outside the work hours, 3 custom',
  `schedule_start` int NOT NULL DEFAULT 0 COMMENT 'away, custom: unix time',
  `schedule_end` int NOT NULL DEFAULT 0 COMMENT 'away, custom: unix time',
  `offline_only` tinyint(1) NOT NULL DEFAULT 0,
  `enabled_at` int NOT NULL COMMENT 'unix time the kind was first turned on: the chats begun before are the existing ones',
  PRIMARY KEY (`user_id`, `kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The automatic replies to send: a row is written in the box transaction of
-- the owner that takes the message which triggers it, and the worker of
-- the service msg sends it and deletes it. One row per triggering message
-- and kind: the event applied again finds the row.
CREATE TABLE IF NOT EXISTS `business_auto_replies` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL COMMENT 'the owner, who sends the reply',
  `peer_id` bigint NOT NULL COMMENT 'the user the reply goes to',
  `kind` tinyint NOT NULL COMMENT '1: greeting, 2: away',
  `trigger_box_id` int NOT NULL COMMENT 'the id of the incoming message in the box of the owner',
  `shortcut_id` int NOT NULL,
  `claim_until` bigint NOT NULL DEFAULT 0 COMMENT 'unix ms the lease of a worker ends; 0: free',
  `attempts` int NOT NULL DEFAULT 0,
  `created_at` bigint NOT NULL COMMENT 'unix ms',
  PRIMARY KEY (`id`),
  UNIQUE KEY `trigger` (`user_id`, `peer_id`, `kind`, `trigger_box_id`),
  KEY `due` (`claim_until`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- When an automatic reply of a kind last went to a user.
CREATE TABLE IF NOT EXISTS `business_auto_reply_log` (
  `user_id` bigint NOT NULL,
  `peer_id` bigint NOT NULL,
  `kind` tinyint NOT NULL,
  `sent_at` int NOT NULL COMMENT 'unix time',
  PRIMARY KEY (`user_id`, `peer_id`, `kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The chat links of an account (account.createBusinessChatLink and the
-- methods beside it): https://pluma.chat/m/<slug>. The slug is compared
-- byte for byte: it is case-sensitive.
CREATE TABLE IF NOT EXISTS `business_chat_links` (
  `slug` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `user_id` bigint NOT NULL,
  `message` text NOT NULL,
  `entities` mediumtext NOT NULL COMMENT 'JSON of the entities of message; empty: none',
  `title` varchar(256) NOT NULL DEFAULT '',
  `views` int NOT NULL DEFAULT 0,
  `created_at` bigint NOT NULL COMMENT 'unix ms: the order of the list',
  PRIMARY KEY (`slug`),
  KEY `user` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The connected bot of an account (account.updateConnectedBot): one per
-- account. connection_id is the opaque id the bot acts by; it stays while
-- the settings change.
CREATE TABLE IF NOT EXISTS `business_bot_connections` (
  `user_id` bigint NOT NULL,
  `bot_id` bigint NOT NULL,
  `connection_id` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `rights` int NOT NULL COMMENT 'the bits of businessBotRights as the client sent them',
  `recipients` varchar(8192) NOT NULL COMMENT 'JSON of pkg/business.Recipients',
  `created_at` int NOT NULL COMMENT 'unix time',
  `updated_at` int NOT NULL COMMENT 'unix time',
  PRIMARY KEY (`user_id`),
  UNIQUE KEY `connection` (`connection_id`),
  KEY `bot` (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The chats in which the owner paused the connected bot
-- (account.toggleConnectedBotPaused).
CREATE TABLE IF NOT EXISTS `business_bot_paused_peers` (
  `user_id` bigint NOT NULL,
  `peer_id` bigint NOT NULL,
  `paused_at` int NOT NULL COMMENT 'unix time',
  PRIMARY KEY (`user_id`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
