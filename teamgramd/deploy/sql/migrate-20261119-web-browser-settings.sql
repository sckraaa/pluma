-- Task 098: the in-app browser settings of an account
-- (account.getWebBrowserSettings and the methods that change them,
-- pkg/webbrowser). The statements are CREATE TABLE IF NOT EXISTS and
-- nothing else: running the file again changes nothing.

-- One row per account that changed its settings or its exceptions. No row,
-- or a row with flags_set = 0, is the defaults (pkg/webbrowser.Defaults):
-- a write of the exceptions makes the row only to lock it, so that two
-- writes of one account never pass the limit of a list together.
CREATE TABLE IF NOT EXISTS `user_web_browser_settings` (
  `user_id` bigint NOT NULL,
  `open_external_browser` tinyint(1) NOT NULL,
  `display_close_button` tinyint(1) NOT NULL,
  `flags_set` tinyint(1) NOT NULL DEFAULT 0 COMMENT '1: the two flags are the account''s; 0: the defaults apply',
  `updated_at` bigint NOT NULL COMMENT 'unix ms',
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The exceptions: a domain is in one of the two lists. The domain is
-- compared byte for byte (it is kept in lower case): the accent- and
-- width-insensitive collation of the table would make two hosts one.
CREATE TABLE IF NOT EXISTS `user_web_browser_exceptions` (
  `user_id` bigint NOT NULL,
  `domain` varchar(253) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `url` varchar(1024) NOT NULL COMMENT 'the address the exception was made from, as the client sent it',
  `open_external` tinyint(1) NOT NULL COMMENT '1: external_exceptions, 0: inapp_exceptions',
  `created_at` bigint NOT NULL COMMENT 'unix ms: the order of the list',
  PRIMARY KEY (`user_id`, `domain`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
