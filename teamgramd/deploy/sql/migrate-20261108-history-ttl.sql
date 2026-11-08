-- Auto-deletion of messages (task 063, messages.setHistoryTTL).
--
-- channels.ttl_period: the auto-delete period of new messages of a channel
-- or supergroup; 0 is off, as chats.ttl_period and dialogs.ttl_period are.
--
-- message_expiry and channel_message_expiry: the messages that have a
-- ttl_period, by the time they go. A row is written with the message, in
-- the transaction that writes it, and deleted in the transaction that
-- deletes the message: the worker of msg deletes what is due, and a restart
-- loses nothing.
--
-- A new column and new tables only: safe on an empty and on a populated
-- database, and safe to run again.

SET @ddl = (SELECT IF(COUNT(*) = 0,
  'ALTER TABLE `channels` ADD COLUMN `ttl_period` int(11) NOT NULL DEFAULT ''0''',
  'SELECT 1')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'ttl_period');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS `message_expiry` (
  `user_id` bigint(20) NOT NULL,
  `user_message_box_id` int(11) NOT NULL,
  `expire_at` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `user_message_box_id`),
  KEY `expire_at` (`expire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='When a message of a user box goes';

CREATE TABLE IF NOT EXISTS `channel_message_expiry` (
  `channel_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `expire_at` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `message_id`),
  KEY `expire_at` (`expire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='When a message of a channel goes';
