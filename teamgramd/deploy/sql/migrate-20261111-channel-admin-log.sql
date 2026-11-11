-- The recent actions of channels and supergroups (task 078,
-- channels.getAdminLog): one row per action, kept for 48 hours (pkg/adminlog).
-- kind is the filter bit of the action, text what a search finds it by,
-- action the channelAdminLogEventAction in protobuf JSON.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_admin_log` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  `kind` bigint(20) NOT NULL,
  `text` varchar(512) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `action` mediumblob NOT NULL,
  PRIMARY KEY (`id`),
  KEY `channel_id` (`channel_id`, `id`),
  KEY `channel_date` (`channel_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
