-- Releases of the client apps (task 054, ADR 0035): one row per signed build
-- CI hands to the releases service. The current release of a platform is its
-- latest build not withdrawn; the site lists it and help.getAppUpdate offers
-- it. version is what the client compares (Android 12.9.1.N, iOS the version
-- of Telegram, desktop the integer AppVersion); build is CI's rising number.
-- document_id is the APK in dfs for Android, 0 for the others. files lists
-- the stored files (name, kind, size, sha256, arch) as JSON.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `app_releases` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `platform` varchar(16) COLLATE utf8mb4_unicode_ci NOT NULL,
  `version` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `build` int(11) NOT NULL,
  `document_id` bigint(20) NOT NULL DEFAULT '0',
  `files` json NOT NULL,
  `notes_en` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `notes_ru` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `can_not_skip` tinyint(1) NOT NULL DEFAULT '0',
  `withdrawn` tinyint(1) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `platform_build` (`platform`, `build`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Releases of the client apps';
