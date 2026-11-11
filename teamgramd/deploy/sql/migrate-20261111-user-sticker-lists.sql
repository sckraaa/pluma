-- The recently used and the faved stickers of an account (task 079): by
-- list (0 the recent stickers, 1 the recent stickers attached to photos and
-- videos, 2 the faved stickers), each the newest first by order2, with the
-- date the sticker was used or faved (used_at, the dates of
-- messages.recentStickers). A list holds at most stickers_recent_limit
-- (help.getConfig) or stickers_faved_limit_default (help.getAppConfig)
-- stickers. Every change goes in a box transaction of the account with
-- updateRecentStickers or updateFavedStickers (ADR 0020).
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `user_sticker_lists` (
  `user_id` bigint(20) NOT NULL,
  `list` tinyint(4) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  `order2` bigint(20) NOT NULL,
  `used_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `list`, `document_id`),
  KEY `user_list_order` (`user_id`, `list`, `order2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
