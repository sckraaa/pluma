-- Login codes through a Telegram account (task 073): the registration switch.
--
-- registration_settings: one row (id = 1); is_open = 1 lets anyone ask for a
-- login code, is_open = 0 (or no row) keeps login to the phones of
-- phone_allowlist. Safe to run again.

CREATE TABLE IF NOT EXISTS `registration_settings` (
  `id` tinyint(4) NOT NULL DEFAULT '1',
  `is_open` tinyint(4) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
