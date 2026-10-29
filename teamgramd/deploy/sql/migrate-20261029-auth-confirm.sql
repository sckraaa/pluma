-- Settings of an authorization (task 019), as account.getAuthorizations and
-- account.changeAuthorizationSettings have them:
--   unconfirmed                  a new login while the user had other
--                                sessions; confirmed by
--                                account.changeAuthorizationSettings or
--                                authorization_autoconfirm_period seconds
--                                after date_created;
--   encrypted_requests_disabled  the session takes no new secret chats;
--   call_requests_disabled       the session takes no calls.
--
-- Safe on an empty and on a populated database, and safe to run again: a
-- column is added only when it is missing. Existing authorizations stay
-- confirmed.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'auth_users' AND COLUMN_NAME = 'unconfirmed') = 0,
  'ALTER TABLE `auth_users` ADD `unconfirmed` TINYINT(1) NOT NULL DEFAULT 0 AFTER `android_push_session_id`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'auth_users' AND COLUMN_NAME = 'encrypted_requests_disabled') = 0,
  'ALTER TABLE `auth_users` ADD `encrypted_requests_disabled` TINYINT(1) NOT NULL DEFAULT 0 AFTER `unconfirmed`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'auth_users' AND COLUMN_NAME = 'call_requests_disabled') = 0,
  'ALTER TABLE `auth_users` ADD `call_requests_disabled` TINYINT(1) NOT NULL DEFAULT 0 AFTER `encrypted_requests_disabled`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
