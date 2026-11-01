-- Language packs served by langpack.* (task 021, ADR 0025): the languages of
-- each platform pack (android, ios, tdesktop, macos, webk) and their strings,
-- imported from Telegram's own langpack.* answers by pluma-admin langpack
-- import, with "Telegram" replaced by "Pluma" in the values.
--
-- version of a language is the version langpack.getDifference answers with;
-- version of a string is the language version that last changed it. A string
-- that vanished from Telegram's pack stays as a tombstone (deleted = 1) with
-- the version of its removal, so that a difference from any older version
-- lists it as langPackStringDeleted. value is the text of a plain string or
-- the other_value of a pluralized one; upstream_* keep Telegram's text,
-- before the replacement. str_key is compared byte by byte, as the clients
-- compare keys.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `langpack_languages` (
  `lang_pack` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `lang_code` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `native_name` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `plural_code` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `base_lang_code` varchar(64) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `official` tinyint(1) NOT NULL DEFAULT '0',
  `rtl` tinyint(1) NOT NULL DEFAULT '0',
  `beta` tinyint(1) NOT NULL DEFAULT '0',
  `strings_count` int(11) NOT NULL DEFAULT '0',
  `translated_count` int(11) NOT NULL DEFAULT '0',
  `translations_url` varchar(512) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `sort_order` int(11) NOT NULL DEFAULT '0',
  `version` int(11) NOT NULL DEFAULT '0',
  `upstream_version` int(11) NOT NULL DEFAULT '0',
  `updated_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`lang_pack`, `lang_code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `langpack_strings` (
  `lang_pack` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `lang_code` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `str_key` varchar(255) COLLATE utf8mb4_bin NOT NULL,
  `pluralized` tinyint(1) NOT NULL DEFAULT '0',
  `value` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `zero_value` text COLLATE utf8mb4_unicode_ci,
  `one_value` text COLLATE utf8mb4_unicode_ci,
  `two_value` text COLLATE utf8mb4_unicode_ci,
  `few_value` text COLLATE utf8mb4_unicode_ci,
  `many_value` text COLLATE utf8mb4_unicode_ci,
  `upstream_value` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `upstream_zero_value` text COLLATE utf8mb4_unicode_ci,
  `upstream_one_value` text COLLATE utf8mb4_unicode_ci,
  `upstream_two_value` text COLLATE utf8mb4_unicode_ci,
  `upstream_few_value` text COLLATE utf8mb4_unicode_ci,
  `upstream_many_value` text COLLATE utf8mb4_unicode_ci,
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  `version` int(11) NOT NULL DEFAULT '0',
  PRIMARY KEY (`lang_pack`, `lang_code`, `str_key`),
  KEY `lang_version` (`lang_pack`, `lang_code`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
