-- Task 107: the HTTPS CDN (ADR 0071). The CDN node of task 102 is gone:
-- the files are prepared for the origin of the home (their hashes), not
-- pushed to a node, and the home keeps the health and the monthly traffic
-- of each delivery (pkg/cdn). Times are unix milliseconds.
--
-- Safe on an empty and on a populated database, and safe to run again:
-- tables are created if missing, the column is renamed only while it has
-- its old name, and the table of the nodes is dropped if it is there.

-- The last check of each delivery (pkg/cdn health): the redirects go to a
-- delivery only while healthy_until is ahead.
CREATE TABLE IF NOT EXISTS `cdn_deliveries` (
  `dc_id` int NOT NULL COMMENT 'the delivery, dc_id of upload.fileCdnRedirect (201...)',
  `healthy_until` bigint NOT NULL DEFAULT 0,
  `unhealthy_reason` varchar(32) NOT NULL DEFAULT '',
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`dc_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The traffic of each delivery in a calendar month (UTC), against its
-- budget (CDN_GCORE_MONTHLY_GB).
CREATE TABLE IF NOT EXISTS `cdn_delivery_usage` (
  `dc_id` int NOT NULL,
  `month` char(7) NOT NULL COMMENT 'YYYY-MM, UTC',
  `origin_bytes` bigint NOT NULL DEFAULT 0 COMMENT 'served by the home origin, exact',
  `edge_bytes_est` bigint NOT NULL DEFAULT 0 COMMENT 'home-side estimate of what the CDN sent to clients',
  PRIMARY KEY (`dc_id`, `month`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- cdn_replicas: a copy on a node becomes the preparation of a file for a
-- target, dc_id becomes target_id (1: the home origin). The key and the
-- index k_queue follow the column. Guarded like
-- migrate-20261031-read-dates.sql.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'cdn_replicas' AND COLUMN_NAME = 'dc_id') = 1,
  'ALTER TABLE `cdn_replicas` CHANGE COLUMN `dc_id` `target_id` int NOT NULL COMMENT ''1: the home origin''',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

-- The CDN node of task 102 never ran in production.
DROP TABLE IF EXISTS `cdn_nodes`;
