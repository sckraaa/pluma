-- Moderation (task 025, ADR 0040): reports of users, the restrictions the
-- decisions impose, and the people of the internal panel.
--
-- moderation_reports: a report with what it was about at that moment
-- (snapshot, JSON of pkg/moderation.Snapshot). dedup_key is set while the
-- report is open: the same report again changes the open one. A decided
-- report has none, so a later report of the same thing is a new one.
-- effects_stage counts the steps of the decision that are done
-- (pkg/moderation.Service): the worker of the panel goes on from there.
--
-- moderation_restrictions: 1 limited (PEER_FLOOD to strangers,
-- USER_BANNED_IN_CHANNEL in groups), 2 banned. until_date 0 is no end; a
-- passed one is read as none, nothing removes the row.
--
-- admin_audit is append-only: the panel's database user has only SELECT and
-- INSERT on it (docs/ops/admin-panel.md).
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `moderation_reports` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `reporter_user_id` bigint(20) NOT NULL,
  `kind` tinyint(4) NOT NULL,
  `peer_type` int(11) NOT NULL DEFAULT '0',
  `peer_id` bigint(20) NOT NULL DEFAULT '0',
  `target_user_id` bigint(20) NOT NULL DEFAULT '0',
  `reason` varchar(32) NOT NULL DEFAULT '',
  `comment` varchar(1024) NOT NULL DEFAULT '',
  `snapshot` mediumtext NOT NULL,
  `dedup_key` char(64) DEFAULT NULL,
  `status` tinyint(4) NOT NULL DEFAULT '0',
  `decision` varchar(16) NOT NULL DEFAULT '',
  `decided_by` varchar(255) NOT NULL DEFAULT '',
  `decision_reason` varchar(512) NOT NULL DEFAULT '',
  `decision_until` int(11) NOT NULL DEFAULT '0',
  `decided_at` bigint(20) NOT NULL DEFAULT '0',
  `effects_stage` tinyint(4) NOT NULL DEFAULT '0',
  `effects_total` tinyint(4) NOT NULL DEFAULT '0',
  `effects_attempts` int(11) NOT NULL DEFAULT '0',
  `effects_error` varchar(512) NOT NULL DEFAULT '',
  `effects_claim_until` bigint(20) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `dedup` (`dedup_key`),
  KEY `queue` (`status`, `id`),
  KEY `effects` (`status`, `effects_stage`, `effects_total`),
  KEY `reporter` (`reporter_user_id`, `created_at`),
  KEY `target` (`target_user_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `moderation_restrictions` (
  `user_id` bigint(20) NOT NULL,
  `kind` tinyint(4) NOT NULL,
  `until_date` int(11) NOT NULL DEFAULT '0',
  `report_id` bigint(20) NOT NULL DEFAULT '0',
  `reason` varchar(512) NOT NULL DEFAULT '',
  `created_by` varchar(255) NOT NULL DEFAULT '',
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `admin_users` (
  `login` varchar(255) NOT NULL,
  `role` tinyint(4) NOT NULL,
  `disabled` tinyint(1) NOT NULL DEFAULT '0',
  `created_by` varchar(255) NOT NULL DEFAULT '',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`login`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `admin_passkeys` (
  `credential_id` varbinary(255) NOT NULL,
  `login` varchar(255) NOT NULL,
  `public_x` varbinary(32) NOT NULL,
  `public_y` varbinary(32) NOT NULL,
  `sign_count` bigint(20) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`credential_id`),
  KEY `login` (`login`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `admin_sessions` (
  `token_hash` char(64) NOT NULL,
  `login` varchar(255) NOT NULL,
  `expires_at` bigint(20) NOT NULL,
  PRIMARY KEY (`token_hash`),
  KEY `login` (`login`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `admin_challenges` (
  `challenge` char(64) NOT NULL,
  `login` varchar(255) NOT NULL,
  `purpose` varchar(16) NOT NULL,
  `digest` char(64) NOT NULL DEFAULT '',
  `expires_at` bigint(20) NOT NULL,
  PRIMARY KEY (`challenge`),
  KEY `expires` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- users.about: 140 characters of about_length_limit_premium did not fit in
-- varchar(128).
SET @ddl = IF((SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'about') < 140,
  'ALTER TABLE `users` MODIFY `about` varchar(140) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''''',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `admin_audit` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `at` bigint(20) NOT NULL,
  `actor` varchar(255) NOT NULL,
  `action` varchar(32) NOT NULL,
  `target` varchar(300) NOT NULL DEFAULT '',
  `detail` varchar(1024) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `actor` (`actor`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
