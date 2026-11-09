-- Wallpapers and cloud themes (task 065): what accounts upload and choose on
-- top of the catalog.
--
-- wallpapers: uploaded wallpapers, the serialized wallPaper in data.
-- user_wallpapers: the list of an account: saved wallpapers with its settings,
--   and removed preinstalled ones (a row with removed = 1 and no data).
-- chat_wallpapers: the wallpaper of a private chat for one of its sides; for
--   a wallpaper the other side forced on it (for_both), overridden = 1 and
--   prev_data what it had before.
-- themes, user_themes: cloud themes and the list of an account (state 1
--   saved, 2 a removed preinstalled one) and installs.
-- channels.wallpaper and channels.theme_emoticon: the look of a channel.
--
-- New tables and new columns only: safe on an empty and on a populated
-- database, and safe to run again.

CREATE TABLE IF NOT EXISTS `wallpapers` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `slug` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `creator_id` bigint(20) NOT NULL,
  `for_chat` tinyint(1) NOT NULL DEFAULT '0',
  `data` mediumblob NOT NULL,
  `created_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `slug` (`slug`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_wallpapers` (
  `user_id` bigint(20) NOT NULL,
  `wallpaper_id` bigint(20) NOT NULL,
  `removed` tinyint(1) NOT NULL DEFAULT '0',
  `data` mediumblob,
  `order2` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`, `wallpaper_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `chat_wallpapers` (
  `user_id` bigint(20) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `data` mediumblob,
  `overridden` tinyint(1) NOT NULL DEFAULT '0',
  `prev_data` mediumblob,
  `updated_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `themes` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `slug` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `creator_id` bigint(20) NOT NULL,
  `title` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `data` mediumblob NOT NULL,
  `installs_count` int(11) NOT NULL DEFAULT '0',
  `updated_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `slug` (`slug`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_themes` (
  `user_id` bigint(20) NOT NULL,
  `theme_id` bigint(20) NOT NULL,
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `installed` tinyint(1) NOT NULL DEFAULT '0',
  `saved_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`, `theme_id`),
  KEY `theme_id` (`theme_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'wallpaper') = 0,
  'ALTER TABLE `channels` ADD COLUMN `wallpaper` mediumblob NULL, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'theme_emoticon') = 0,
  'ALTER TABLE `channels` ADD COLUMN `theme_emoticon` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
