-- How an account looks and its small settings (task 084, ADR 0052).
--
-- 1. users.color and users.profile_color: -1 is none, as channels have it
--    (migrate-20261111-channel-look.sql). Until now 0 stood for none, and 0
--    is a color of its own (the first of help.getPeerColors), which nobody
--    could choose: account.updateColor was not served. The rows that hold 0
--    without a pattern were therefore never chosen and become -1; the
--    default of the columns becomes -1 with them. The default tells whether
--    the step was done, so running the file again changes nothing: a color 0
--    chosen later stays.
-- 2. user_recent_emoji_statuses: the emoji statuses an account used, the
--    newest first by order2 (account.getRecentEmojiStatuses).
-- 3. user_saved_ringtones: the notification sounds an account saved, the
--    newest first by order2 (account.getSavedRingtones).
-- 4. user_contact_notes: the private note an account keeps on a contact
--    (contacts.updateContactNote, userFull.note); entities is the
--    Vector<MessageEntity> of the note as protobuf.
-- 5. contact_tokens: the temporary links to a profile
--    (contacts.exportContactToken); a row is of no use after expires.
-- 6. user_autodownload_settings: what an account saved as its auto-download
--    settings (account.saveAutoDownloadSettings) under each of the three
--    names: preset 0 low, 1 medium, 2 high; settings is the
--    AutoDownloadSettings as protobuf. The presets everybody gets
--    (account.getAutoDownloadSettings) do not come from here.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @unset = (SELECT COUNT(*) FROM information_schema.COLUMNS
              WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'color'
                AND COLUMN_DEFAULT = '0');

SET @dml = IF(@unset = 1,
  'UPDATE `users` SET `color` = -1 WHERE `color` = 0 AND `color_background_emoji_id` = 0',
  'DO 0');
PREPARE dml FROM @dml;
EXECUTE dml;
DEALLOCATE PREPARE dml;

SET @dml = IF(@unset = 1,
  'UPDATE `users` SET `profile_color` = -1 WHERE `profile_color` = 0 AND `profile_color_background_emoji_id` = 0',
  'DO 0');
PREPARE dml FROM @dml;
EXECUTE dml;
DEALLOCATE PREPARE dml;

-- The defaults last: they mark the step as done.
SET @ddl = IF(@unset = 1,
  'ALTER TABLE `users` ALTER COLUMN `profile_color` SET DEFAULT ''-1'', ALTER COLUMN `color` SET DEFAULT ''-1''',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `user_recent_emoji_statuses` (
  `user_id` bigint(20) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  `order2` bigint(20) NOT NULL,
  `used_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `document_id`),
  KEY `user_order` (`user_id`, `order2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_saved_ringtones` (
  `user_id` bigint(20) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  `order2` bigint(20) NOT NULL,
  `saved_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `document_id`),
  KEY `user_order` (`user_id`, `order2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_contact_notes` (
  `user_id` bigint(20) NOT NULL,
  `contact_id` bigint(20) NOT NULL,
  `note` varchar(1024) COLLATE utf8mb4_unicode_ci NOT NULL,
  `entities` blob NOT NULL,
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `contact_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `contact_tokens` (
  `token` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `expires` bigint(20) NOT NULL,
  PRIMARY KEY (`token`),
  KEY `user_expires` (`user_id`, `expires`),
  KEY `expires` (`expires`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_autodownload_settings` (
  `user_id` bigint(20) NOT NULL,
  `preset` tinyint(4) NOT NULL,
  `settings` blob NOT NULL,
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `preset`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
