-- Sticker and emoji sets made by users (task 084, ADR 0053). They live
-- beside the catalog's sets in catalog_sticker_sets (source 'user'), found
-- by the same id and short name, with the account that made them
-- (owner_id; 0 for a set nobody here made) and the document of the set's
-- own thumbnail (thumb_document_id, 0 for none). An import of the catalog
-- and a set fetched on demand never replace a set a user made.
--
-- user_sticker_set_documents: the stickers of those sets, each a document
-- of its own made from the one the user gave (source_id), so that a
-- sticker names its set (stickers.removeStickerFromSet and the like take
-- the sticker alone) and a document given twice is one sticker. A sticker
-- taken out of its set stays here (removed = 1): messages that carry it
-- still show it, and a custom emoji is still fetched by its id.
--
-- user_featured_sticker_reads: the trending sets an account has seen
-- (messages.readFeaturedStickers); the rest of the trending sets are its
-- unread ones.
--
-- media_attached_stickers: the stickers put on an uploaded photo or video
-- (the stickers of inputMediaUploadedPhoto and inputMediaUploadedDocument),
-- in their order, by the id of the photo or the document
-- (messages.getAttachedStickers).
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'catalog_sticker_sets' AND COLUMN_NAME = 'owner_id') = 0,
  'ALTER TABLE `catalog_sticker_sets` ADD `owner_id` bigint(20) NOT NULL DEFAULT ''0'' AFTER `source`, ADD `thumb_document_id` bigint(20) NOT NULL DEFAULT ''0'' AFTER `owner_id`, ADD KEY `owner` (`owner_id`, `set_id`)',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `user_sticker_set_documents` (
  `document_id` bigint(20) NOT NULL,
  `set_id` bigint(20) NOT NULL,
  `source_id` bigint(20) NOT NULL DEFAULT '0',
  `removed` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`document_id`),
  KEY `set_id` (`set_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_featured_sticker_reads` (
  `user_id` bigint(20) NOT NULL,
  `set_id` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `set_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `media_attached_stickers` (
  `media_id` bigint(20) NOT NULL,
  `pos` int(11) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  PRIMARY KEY (`media_id`, `pos`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
