-- Task 099, part C: the media the built-in inline bots (@gif, @pic,
-- pkg/inlinebots) import from the open web, and the sends of @gif's GIFs
-- (pkg/inlinemedia, ADR 0062). The statements are CREATE TABLE IF NOT
-- EXISTS and nothing else: running the file again changes nothing.

-- One row per medium a provider found and the server imported into dfs:
-- the next query that finds it again gets the stored photo or document
-- without a fetch. source_key is sha256(provider, 0x00, the provider's id
-- of the medium or its URL)[:16]; the address itself is not kept. The row
-- is written after the file: a failure between the two leaves a file with
-- no row (inline_media_orphans_total), and the next query imports again.
-- Imported files are not deleted (dfs has no deletion); last_served_at is
-- for a later collection of the ones nobody is shown.
CREATE TABLE IF NOT EXISTS `inline_media` (
  `provider` varchar(16) NOT NULL COMMENT 'searxng, klipy',
  `source_key` binary(16) NOT NULL COMMENT 'sha256(provider, 0x00, id or URL)[:16]',
  `kind` tinyint NOT NULL COMMENT '1 photo, 2 gif',
  `media_id` bigint NOT NULL COMMENT 'the id of the photo or of the document',
  `provider_ref` varchar(128) NOT NULL DEFAULT '' COMMENT 'the provider''s id, for revalidation',
  `bytes` int NOT NULL COMMENT 'the size of the file stored',
  `created_at` bigint NOT NULL COMMENT 'unix seconds',
  `last_served_at` bigint NOT NULL COMMENT 'unix seconds',
  PRIMARY KEY (`provider`, `source_key`),
  KEY `served` (`last_served_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The GIFs sent from @gif's results, per document and UTC day: the popular
-- GIFs of @gif's empty query are the sends of the last 30 days. Older rows
-- are deleted by the code that counts the popular ones.
CREATE TABLE IF NOT EXISTS `inline_gif_sends` (
  `document_id` bigint NOT NULL,
  `day` date NOT NULL COMMENT 'UTC',
  `sends` int NOT NULL,
  PRIMARY KEY (`document_id`, `day`),
  KEY `day` (`day`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
