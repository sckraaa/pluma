-- The antispam of supergroups and the autotranslation of channels (task 082,
-- ADR 0050):
--   channels.antispam            the supergroup's antispam is on
--                                (channels.toggleAntiSpam);
--   channels.autotranslation     the broadcast channel is translated for all
--                                (channels.toggleAutotranslation);
--   channel_antispam_deletions   the messages the antispam deleted, by which
--                                rule, and whether an admin reported the
--                                deletion as wrong
--                                (channels.reportAntiSpamFalsePositive);
--   channel_antispam_exempt      the members the antispam no longer checks
--                                in a group, after such a report;
--   antispam_recent_posts        what each user posted to supergroups in the
--                                last minutes, as a hash of its content (no
--                                text): the same post in several groups.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'antispam') = 0,
  'ALTER TABLE `channels` ADD COLUMN `antispam` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'autotranslation') = 0,
  'ALTER TABLE `channels` ADD COLUMN `autotranslation` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_antispam_deletions` (
  `channel_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `sender_user_id` bigint(20) NOT NULL,
  `rule` int(11) NOT NULL,
  `rules_version` int(11) NOT NULL,
  `date` bigint(20) NOT NULL,
  `reported` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`channel_id`, `message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `channel_antispam_exempt` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `antispam_recent_posts` (
  `user_id` bigint(20) NOT NULL,
  `content_hash` binary(32) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `content_hash`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
