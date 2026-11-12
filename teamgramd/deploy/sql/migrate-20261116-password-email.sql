-- The recovery email of the cloud password (task 091, ADR 0058, p. 6), in
-- the row of the password, so that whatever removes the password removes
-- the email too:
--   user_passwords.email              the confirmed recovery email; '' if none;
--   user_passwords.email_unconfirmed  the address waiting for its code; '' if none;
--   user_passwords.email_set_at       when email was confirmed (unix seconds);
--   user_passwords.awaits_email       1: a first password set with a recovery
--                                     email, not in force until the email is
--                                     confirmed (account.confirmPasswordEmail).
-- The codes never reach MySQL: they live in Redis as HMACs for 30 minutes.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_passwords' AND COLUMN_NAME = 'email') = 0,
  'ALTER TABLE `user_passwords` ADD COLUMN `email` varchar(254) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_passwords' AND COLUMN_NAME = 'email_unconfirmed') = 0,
  'ALTER TABLE `user_passwords` ADD COLUMN `email_unconfirmed` varchar(254) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '''', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_passwords' AND COLUMN_NAME = 'email_set_at') = 0,
  'ALTER TABLE `user_passwords` ADD COLUMN `email_set_at` bigint(20) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_passwords' AND COLUMN_NAME = 'awaits_email') = 0,
  'ALTER TABLE `user_passwords` ADD COLUMN `awaits_email` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
