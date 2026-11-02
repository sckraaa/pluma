-- The catalog from Telegram (task 024, ADR 0028): what every account gets
-- alike, imported with ids of this server.
--
-- catalog_answers: the answers of methods ready to send, mtproto objects
-- in protobuf, by method and argument (a language, a theme format; '' for
-- none), with the hash the method compares (int or long by method).
-- catalog_sticker_sets: the sticker and emoji sets, found by id, by
-- short_name and, for the sets clients ask for by kind
-- (inputStickerSetAnimatedEmoji and the like), by that kind.
-- catalog_documents: the document each imported one became, by a key of
-- what makes it that document (SHA-256 of its file and of the set it
-- belongs to), so that importing again gives the same ids; a set keeps its
-- id by short_name. By document_id it tells the documents of the catalog,
-- which anyone may fetch by id (messages.getCustomEmojiDocuments), from
-- the documents of users, which nobody may.
--
-- catalog_ids: the id of this server each set, theme and wallpaper has, by
-- what names it (a set's short name, a slug), written as soon as it is
-- given, so that a document stored by an import that then failed refers to
-- the set it will have.
--
-- Arguments, kinds and slugs compare byte by byte (utf8mb4_bin): the
-- unicode collation takes every emoji of the dice for one, and slugs
-- differ by case; short names compare without case, as Telegram's.
--
-- An import writes all of a catalog in one transaction. Safe on an empty
-- and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `catalog_answers` (
  `method` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `arg` varchar(96) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `hash` bigint(20) NOT NULL DEFAULT '0',
  `data` mediumblob NOT NULL,
  `imported_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`method`, `arg`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `catalog_sticker_sets` (
  `set_id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `short_name` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `kind` varchar(64) COLLATE utf8mb4_bin DEFAULT NULL,
  `hash` int(11) NOT NULL DEFAULT '0',
  `data` mediumblob NOT NULL,
  `imported_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`set_id`),
  UNIQUE KEY `short_name` (`short_name`),
  UNIQUE KEY `kind` (`kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `catalog_documents` (
  `doc_key` binary(32) NOT NULL,
  `document_id` bigint(20) NOT NULL,
  PRIMARY KEY (`doc_key`),
  KEY `document_id` (`document_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `catalog_ids` (
  `kind` varchar(16) COLLATE utf8mb4_bin NOT NULL,
  `name` varchar(128) COLLATE utf8mb4_bin NOT NULL,
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  PRIMARY KEY (`kind`, `name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
