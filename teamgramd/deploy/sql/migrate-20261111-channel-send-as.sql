-- Writing to a supergroup as one's channel or as the group (task 079,
-- channels.getSendAs, send_as of the sending methods).
--
-- channel_messages.send_as_id: the channel a member wrote the message as,
-- the supergroup itself for an anonymous admin; 0 for a message from its
-- sender. sender_user_id stays the member who wrote it: its edits, its
-- deletion and its read marks are the member's.
--
-- channel_default_send_as: the peer a user chose to write to a supergroup
-- as (messages.saveDefaultSendAs, channelFull.default_send_as); no row is
-- the user itself.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'send_as_id') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `send_as_id` bigint(20) NOT NULL DEFAULT ''0'' AFTER `sender_user_id`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_default_send_as` (
  `user_id` bigint(20) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `send_as_id` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
