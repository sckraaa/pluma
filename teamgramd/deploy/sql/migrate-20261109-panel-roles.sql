-- The panel has one person, the owner (task 072, ADR 0042): admin_users is
-- the list of logins, without roles. Runs with the panel's new image: the
-- old one reads the column.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'admin_users' AND COLUMN_NAME = 'role') > 0,
  'ALTER TABLE `admin_users` DROP COLUMN `role`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
