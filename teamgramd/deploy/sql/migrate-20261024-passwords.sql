-- 2FA passwords (task 008, https://core.telegram.org/api/srp): the SRP
-- salts and verifier v = g^x mod p of each user's password, never the
-- password itself (pkg/twofa). Dates are unix seconds; 0 is none.
--
-- pending_reset_date: account.resetPassword asked to remove the password,
-- which happens once this date has passed (account.declinePasswordReset
-- cancels it).
-- pending_delete_date: account.deleteAccount without the password asked to
-- delete the account, which it does once this date has passed
-- (2FA_CONFIRM_WAIT_X until then; the right password cancels it).
--
-- Safe to run again: the table is created once.
CREATE TABLE IF NOT EXISTS `user_passwords` (
  `user_id` bigint(20) NOT NULL,
  `salt1` varbinary(255) NOT NULL,
  `salt2` varbinary(255) NOT NULL,
  `verifier` varbinary(256) NOT NULL,
  `hint` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',
  `set_at` bigint(20) NOT NULL,
  `pending_reset_date` bigint(20) NOT NULL DEFAULT '0',
  `pending_delete_date` bigint(20) NOT NULL DEFAULT '0',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='2FA passwords (SRP verifiers)';
