-- Closed registration (task 001): only phones on this list may request a
-- login code, sign in or sign up (bff, pkg/phoneallowlist). Managed with
-- tools/pluma-admin: allowlist add|remove|list.
-- Phones are normalized digits without '+', as in users.phone.

CREATE TABLE IF NOT EXISTS `phone_allowlist` (
  `phone` varchar(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  `note` varchar(128) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`phone`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Phones allowed to log in or sign up';

-- Existing regular accounts keep their access. Bots, service accounts
-- (777000), test and deleted accounts are not added.
INSERT IGNORE INTO `phone_allowlist` (`phone`, `note`)
SELECT `phone`, 'existing account' FROM `users`
WHERE `user_type` = 2 AND `is_bot` = 0 AND `deleted` = 0 AND `phone` <> '';
