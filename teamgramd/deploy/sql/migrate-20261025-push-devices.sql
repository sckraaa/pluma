-- Devices that receive push notifications (task 009, ADR 0017,
-- pkg/pushdevices): one row per authorization and token type, as
-- account.registerDevice registered it. The upstream table `devices` is not
-- used.
--
-- token_hash is SHA-256 of token: tokens (a web push subscription is JSON)
-- are too long for a unique key.
-- secret is sealed with AES-256-GCM on PUSH_SECRET_ENCRYPTION_KEY: a version
-- byte, the nonce, the ciphertext and the tag. Empty for a web push device
-- without encryption.
-- invalidated: the provider rejected the token; the next registration of it
-- gets TOKEN_WAS_INVALIDATED.
-- Dates are unix seconds.
--
-- Safe to run again: the table is created once.
CREATE TABLE IF NOT EXISTS `push_devices` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) NOT NULL,
  `auth_key_id` bigint(20) NOT NULL,
  `token_type` int(11) NOT NULL,
  `token` varchar(4096) COLLATE utf8mb4_unicode_ci NOT NULL,
  `token_hash` binary(32) NOT NULL,
  `app_sandbox` tinyint(1) NOT NULL DEFAULT '0',
  `no_muted` tinyint(1) NOT NULL DEFAULT '0',
  `secret` varbinary(512) NOT NULL DEFAULT '',
  `other_uids` varchar(2048) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `lang_code` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `locked_period` int(11) NOT NULL DEFAULT '0',
  `invalidated` tinyint(1) NOT NULL DEFAULT '0',
  `created_at` bigint(20) NOT NULL,
  `updated_at` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `authorization` (`auth_key_id`, `token_type`),
  UNIQUE KEY `token` (`user_id`, `token_type`, `token_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='push notification devices';
