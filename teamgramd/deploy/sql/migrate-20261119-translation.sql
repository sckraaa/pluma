-- Task 101: translation of messages by the server's own model
-- (messages.translateText, messages.togglePeerTranslations; ADR 0064,
-- pkg/translate). The statements are CREATE TABLE IF NOT EXISTS and
-- nothing else: running the file again changes nothing.

-- One row per translated text: the key is the text, its entities, the
-- target language, the model and the profile, so a post read by many and
-- every forward of a message are translated once; an edited message is
-- another text and another key. Rows unused for CacheTTL are swept.
CREATE TABLE IF NOT EXISTS `translation_cache` (
  `cache_key` binary(32) NOT NULL COMMENT 'SHA-256, pkg/translate.CacheKey',
  `to_lang` varchar(16) NOT NULL COMMENT 'the language of the profile, not the client''s spelling',
  `model` varchar(64) NOT NULL,
  `text` mediumtext NOT NULL,
  `entities` mediumblob NOT NULL COMMENT 'protobuf of the entities of the translation',
  `markup` tinyint NOT NULL DEFAULT 1 COMMENT '1 tags kept, 2 fallback: formatting lost',
  `created_at` bigint NOT NULL COMMENT 'unix ms',
  `used_at` bigint NOT NULL COMMENT 'unix ms, updated at most once an hour',
  PRIMARY KEY (`cache_key`),
  KEY `idx_used` (`used_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- messages.togglePeerTranslations: the translation bar of a chat hidden
-- by its reader; translations_disabled of userFull, chatFull, channelFull.
-- Only hidden bars have a row: showing the bar again deletes it.
CREATE TABLE IF NOT EXISTS `peer_translation_settings` (
  `user_id` bigint NOT NULL,
  `peer_type` tinyint NOT NULL COMMENT 'mtproto.PEER_USER, PEER_CHAT or PEER_CHANNEL',
  `peer_id` bigint NOT NULL,
  `disabled` tinyint(1) NOT NULL,
  `updated_at` bigint NOT NULL COMMENT 'unix ms',
  PRIMARY KEY (`user_id`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
