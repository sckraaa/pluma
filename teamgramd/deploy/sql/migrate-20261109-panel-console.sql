-- The panel as the owner's console (task 072, ADR 0042): tables and columns
-- of the services that the panel's actions change.
--
-- registration_settings: one row (id 1), whether registration is open to
-- every number (is_open 1) or by the list of phone_allowlist (0, or no
-- row). pkg/phoneallowlist reads it on every login request.
--
-- channels.blocked and chats.blocked: the owner of the server blocked it
-- (channel.setBlocked, chat.setBlocked); for a group, deactivated is set
-- with it, so that an unblock can tell a block from a delete.
--
-- admin_broadcasts: the broadcasts of the service account, resumable from
-- cursor_id after a restart.
--
-- admin_audit.target gets an index for the journal of a channel or a user;
-- messages and channel_messages an index on date2 for the count of a day.
--
-- Safe on an empty and on a populated database, and safe to run again.

CREATE TABLE IF NOT EXISTS `registration_settings` (
  `id` tinyint(4) NOT NULL DEFAULT '1',
  `is_open` tinyint(1) NOT NULL DEFAULT '0',
  `updated_by` varchar(255) NOT NULL DEFAULT '',
  `updated_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'blocked') = 0,
  'ALTER TABLE `channels` ADD COLUMN `blocked` tinyint(1) NOT NULL DEFAULT 0 AFTER `deleted`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'chats' AND COLUMN_NAME = 'blocked') = 0,
  'ALTER TABLE `chats` ADD COLUMN `blocked` tinyint(1) NOT NULL DEFAULT 0 AFTER `deactivated`',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `admin_broadcasts` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `text` varchar(8192) NOT NULL,
  `audience` varchar(16) NOT NULL,
  `audience_id` bigint(20) NOT NULL DEFAULT '0',
  `ids` mediumtext NOT NULL,
  `state` tinyint(4) NOT NULL DEFAULT '0',
  `total` bigint(20) NOT NULL DEFAULT '0',
  `sent` bigint(20) NOT NULL DEFAULT '0',
  `failed` bigint(20) NOT NULL DEFAULT '0',
  `cursor_id` bigint(20) NOT NULL DEFAULT '0',
  `last_error` varchar(512) NOT NULL DEFAULT '',
  `created_by` varchar(255) NOT NULL DEFAULT '',
  `created_at` bigint(20) NOT NULL,
  `done_at` bigint(20) NOT NULL DEFAULT '0',
  `claim_until` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `state` (`state`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'admin_audit' AND INDEX_NAME = 'target') = 0,
  'ALTER TABLE `admin_audit` ADD KEY `target` (`target`(64), `id`)',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'messages' AND INDEX_NAME = 'date2') = 0,
  'ALTER TABLE `messages` ADD KEY `date2` (`date2`)',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'date2') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `date2` (`date2`)',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;
