-- An update of the seq journal (ADR 0020) that one authorization must not
-- get (ADR 0022, task 019): updateNewAuthorization of a login goes to the
-- other sessions of the account, not to the new one. 0 is every
-- authorization, as for every row written before.
--
-- Safe on an empty and on a populated database, and safe to run again: the
-- column is added only when it is missing.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_seq_updates' AND COLUMN_NAME = 'except_auth_key_id') = 0,
  'ALTER TABLE `user_seq_updates` ADD `except_auth_key_id` BIGINT NOT NULL DEFAULT 0 AFTER `update_data`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
