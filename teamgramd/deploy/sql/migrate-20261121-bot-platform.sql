-- The bots platform (task 106, ADR 0068): login on sites, mini apps, the
-- attachment menu, games, prepared messages and widgets.
--
-- bots.login_domain: the one site domain the bot logs users in on
-- (keyboardButtonUrlAuth, url_auth_domains), lower case, '' for none; no two
-- bots share one (pkg/botstore keeps that under the login_domain index).
-- bots.platform_version: raised on every change of the bot's mini apps or
-- attachment menu, for the hashes of messages.getAttachMenuBots.
--
-- bot_secret_keys: the keys derived from the bot's token (pkg/botkeys),
-- sealed with BOT_SECRET_ENCRYPTION_KEY; token_hash is bots.token of the
-- token they come from.
-- bot_login_clients: the OpenID Connect client of a bot; secret_hash is the
-- SHA-256 of client_secret, hex; redirect_uris and origins are JSON arrays.
-- web_authorizations: the sites a user logged in on (account.getWebAuthorizations),
-- one per user, bot and domain; hash is the random id the clients see.
-- bot_write_allowed: the users who let a bot write to them, and the first
-- way they did (1 domain, 2 app, 3 attach menu, 4 request from a mini app).
-- bot_apps, bot_app_launches: the direct link mini apps of bots and the
-- users who opened each (botApp.inactive); version is botApp.hash.
-- bot_attach_menus, bot_attach_menu_installs: the attachment menu entry of
-- a bot (peer_types: 1 same bot pm, 2 bot pm, 4 pm, 8 chat, 16 broadcast;
-- icons: JSON [{name, document_id, colors}]) and the users who added it.
-- bot_games, game_scores: the games of bots and the scores by game, chat
-- and user; the chat is chat_type with chat_id (a chat or channel) or the
-- pair chat_id < chat_id2 of a private chat.
-- bot_prepared: prepared inline messages (kind 1) and keyboard buttons
-- (kind 2) a bot made for a user's mini app, until expire_at.
-- bot_widgets: shared widgets: the zlib HTML and the public data.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'bots' AND COLUMN_NAME = 'login_domain') = 0,
  'ALTER TABLE `bots` ADD COLUMN `login_domain` varchar(253) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'bots' AND COLUMN_NAME = 'platform_version') = 0,
  'ALTER TABLE `bots` ADD COLUMN `platform_version` int(11) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'bots' AND INDEX_NAME = 'login_domain') = 0,
  'ALTER TABLE `bots` ADD KEY `login_domain` (`login_domain`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `bot_secret_keys` (
  `bot_id` bigint(20) NOT NULL,
  `token_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `webapp_key` varbinary(96) NOT NULL,
  `login_key` varbinary(96) NOT NULL,
  `updated_at` int(11) NOT NULL,
  PRIMARY KEY (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_login_clients` (
  `bot_id` bigint(20) NOT NULL,
  `secret_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `redirect_uris` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `origins` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `created_at` int(11) NOT NULL,
  PRIMARY KEY (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `web_authorizations` (
  `hash` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `domain` varchar(253) COLLATE utf8mb4_unicode_ci NOT NULL,
  `browser` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL,
  `platform` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `ip` varchar(45) COLLATE utf8mb4_unicode_ci NOT NULL,
  `region` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL,
  `write_allowed` tinyint(1) NOT NULL DEFAULT '0',
  `date_created` int(11) NOT NULL,
  `date_active` int(11) NOT NULL,
  PRIMARY KEY (`hash`),
  UNIQUE KEY `user_bot_domain` (`user_id`, `bot_id`, `domain`),
  KEY `user` (`user_id`, `date_active`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_write_allowed` (
  `user_id` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `source` tinyint(4) NOT NULL,
  `allowed_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_apps` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `short_name` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `description` varchar(512) COLLATE utf8mb4_unicode_ci NOT NULL,
  `url` varchar(1024) COLLATE utf8mb4_unicode_ci NOT NULL,
  `photo_id` bigint(20) NOT NULL,
  `document_id` bigint(20) NOT NULL DEFAULT '0',
  `request_write_access` tinyint(1) NOT NULL DEFAULT '0',
  `version` int(11) NOT NULL DEFAULT '1',
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `bot_short` (`bot_id`, `short_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_app_launches` (
  `user_id` bigint(20) NOT NULL,
  `app_id` bigint(20) NOT NULL,
  `launched_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `app_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_attach_menus` (
  `bot_id` bigint(20) NOT NULL,
  `short_name` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `url` varchar(1024) COLLATE utf8mb4_unicode_ci NOT NULL,
  `show_in_attach_menu` tinyint(1) NOT NULL,
  `show_in_side_menu` tinyint(1) NOT NULL,
  `request_write_access` tinyint(1) NOT NULL,
  `has_settings` tinyint(1) NOT NULL,
  `peer_types` int(11) NOT NULL,
  `icons` text COLLATE utf8mb4_unicode_ci NOT NULL,
  PRIMARY KEY (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_attach_menu_installs` (
  `user_id` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `installed_at` int(11) NOT NULL,
  PRIMARY KEY (`user_id`, `bot_id`),
  KEY `bot` (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_games` (
  `id` bigint(20) NOT NULL,
  `access_hash` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `short_name` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  `description` varchar(512) COLLATE utf8mb4_unicode_ci NOT NULL,
  `photo_id` bigint(20) NOT NULL,
  `document_id` bigint(20) NOT NULL DEFAULT '0',
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `bot_short` (`bot_id`, `short_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `game_scores` (
  `game_id` bigint(20) NOT NULL,
  `chat_type` tinyint(4) NOT NULL,
  `chat_id` bigint(20) NOT NULL,
  `chat_id2` bigint(20) NOT NULL DEFAULT '0',
  `user_id` bigint(20) NOT NULL,
  `score` int(11) NOT NULL,
  `updated_at` int(11) NOT NULL,
  PRIMARY KEY (`game_id`, `chat_type`, `chat_id`, `chat_id2`, `user_id`),
  KEY `ranking` (`game_id`, `chat_type`, `chat_id`, `chat_id2`, `score`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_prepared` (
  `id` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `kind` tinyint(4) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `payload` mediumblob NOT NULL,
  `expire_at` int(11) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `expire` (`expire_at`),
  KEY `bot_user` (`bot_id`, `user_id`, `expire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `bot_widgets` (
  `widget_id` bigint(20) NOT NULL,
  `bot_id` bigint(20) NOT NULL,
  `payload` mediumblob NOT NULL,
  `width` int(11) NOT NULL,
  `height` int(11) NOT NULL,
  `allow_queries` tinyint(1) NOT NULL,
  `public_data` mediumtext COLLATE utf8mb4_unicode_ci NOT NULL,
  `public_hash` bigint(20) NOT NULL,
  `period` int(11) NOT NULL,
  `updated_at` int(11) NOT NULL,
  PRIMARY KEY (`widget_id`),
  KEY `bot` (`bot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
