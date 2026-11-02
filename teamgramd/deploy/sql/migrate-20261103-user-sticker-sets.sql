-- The sticker, mask and custom emoji sets an account installed (task 030):
-- by kind (0 stickers, 1 masks, 2 custom emoji), each kind in its own order
-- (order2, the highest first, as messages.getAllStickers lists them), with
-- the date it was installed and, for an archived one, the date it was
-- archived (archived_at, 0 for an installed one). The sets themselves are
-- the catalog's (catalog_sticker_sets).
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `user_sticker_sets` (
  `user_id` bigint(20) NOT NULL,
  `set_id` bigint(20) NOT NULL,
  `kind` tinyint(4) NOT NULL,
  `order2` bigint(20) NOT NULL,
  `installed_at` bigint(20) NOT NULL,
  `archived_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`, `set_id`),
  KEY `user_kind` (`user_id`, `kind`, `archived_at`, `order2`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
