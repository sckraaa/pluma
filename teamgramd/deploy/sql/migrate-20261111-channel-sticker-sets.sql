-- The sticker set and the emoji set a supergroup chose (task 078,
-- channels.setStickers, channels.setEmojiStickers): one row per kind, from
-- the catalog of sets (task 024).
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_sticker_sets` (
  `channel_id` bigint(20) NOT NULL,
  `emoji` tinyint(1) NOT NULL,
  `set_id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `emoji`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
