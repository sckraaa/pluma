-- Task 102: Pluma's CDN-DC (ADR 0065). The home keeps the keys, tokens and
-- hashes of the files it pushes to the CDN node, the state of every copy on
-- a node and the state of the nodes (pkg/cdn). The statements are CREATE
-- TABLE IF NOT EXISTS and nothing else: running the file again changes
-- nothing. Times are unix milliseconds.

-- One row per file that upload.getFile was asked for with cdn_supported:
-- a document, a size of a photo or a secret chat file (pkg/cdn.Key). The
-- key, IV and token are set once, before the first push, and are the same
-- for every node (ADR 0065, p. 3).
CREATE TABLE IF NOT EXISTS `cdn_files` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `file_kind` tinyint NOT NULL COMMENT '1 document, 2 photo, 3 encrypted (secret chat)',
  `file_id` bigint NOT NULL,
  `thumb_size` varbinary(8) NOT NULL DEFAULT '' COMMENT 'the size of a photo, compared byte for byte; empty for the other kinds',
  `size` bigint NOT NULL DEFAULT 0 COMMENT 'bytes; 0: not yet known',
  `eligible` tinyint NOT NULL DEFAULT 1 COMMENT '0: below CDN_MIN_FILE_SIZE, never pushed',
  `file_token` varbinary(32) NULL COMMENT 'random, set with the key',
  `aes_key` binary(32) NULL,
  `aes_iv` binary(16) NULL COMMENT 'the last 4 bytes are zero',
  `hashes` mediumblob NULL COMMENT 'SHA-256 of the 128 KB plaintext parts, 32 bytes each',
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_file` (`file_kind`, `file_id`, `thumb_size`),
  UNIQUE KEY `uk_token` (`file_token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- A copy of a file on a node: the push queue (claimed by lease) and what is
-- ready to redirect to.
CREATE TABLE IF NOT EXISTS `cdn_replicas` (
  `file_row_id` bigint NOT NULL COMMENT 'cdn_files.id',
  `dc_id` int NOT NULL,
  `state` tinyint NOT NULL COMMENT '0 queued, 1 pushing, 2 ready, 3 absent, 4 failed',
  `priority` tinyint NOT NULL DEFAULT 0 COMMENT '1: asked by upload.reuploadCdnFile',
  `lease_until` bigint NOT NULL DEFAULT 0,
  `attempts` int NOT NULL DEFAULT 0 COMMENT 'claims since the copy was last queued anew',
  `next_try_at` bigint NOT NULL DEFAULT 0,
  `last_error` varchar(64) NOT NULL DEFAULT '',
  `ready_at` bigint NOT NULL DEFAULT 0,
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`file_row_id`, `dc_id`),
  KEY `k_queue` (`dc_id`, `state`, `priority`, `next_try_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- A CDN node as the home last saw it, and the keys of its request tokens
-- (ADR 0065, p. 4), which the home makes and hands to the node.
CREATE TABLE IF NOT EXISTS `cdn_nodes` (
  `dc_id` int NOT NULL,
  `public_key` text NULL COMMENT 'PKCS#1 PEM of the node''s RSA key',
  `fingerprint` bigint NOT NULL DEFAULT 0,
  `boot_id` varbinary(16) NULL,
  `healthy_until` bigint NOT NULL DEFAULT 0,
  `unhealthy_reason` varchar(32) NOT NULL DEFAULT '',
  `egress_month` char(7) NOT NULL DEFAULT '' COMMENT 'YYYY-MM of egress_bytes',
  `egress_bytes` bigint NOT NULL DEFAULT 0,
  `cache_used` bigint NOT NULL DEFAULT 0,
  `cache_cap` bigint NOT NULL DEFAULT 0,
  `rt_key` binary(32) NULL COMMENT 'request_token key in force',
  `rt_key_prev` binary(32) NULL COMMENT 'the one before, still accepted',
  `rt_key_at` bigint NOT NULL DEFAULT 0,
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`dc_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
