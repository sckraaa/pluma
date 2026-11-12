-- Direct messages of channels, "monoforums" (task 084, ADR 0055;
-- core.telegram.org/api/monoforum):
--   channels.broadcast_messages_allowed  the broadcast channel takes direct
--                                        messages (channels.updatePaidMessagesPrice);
--   channels.monoforum                   the row is the supergroup that holds
--                                        the direct messages of a channel;
--   channels.linked_monoforum_id         the channel of a monoforum and the
--                                        monoforum of a channel; it stays when
--                                        the direct messages are switched off,
--                                        so that switching them on again
--                                        brings the same chats back;
--   channel_messages.saved_peer_id       the subscriber whose chat with the
--                                        channel the message is in (the
--                                        message's saved_peer_id); 0 outside
--                                        a monoforum;
--   channel_monoforum_sublists           the chat of each subscriber with the
--                                        channel: its last message, how far
--                                        the admins read what the subscriber
--                                        wrote and how far the subscriber
--                                        read the channel's answers, the
--                                        unread mark of the admins;
--   channel_monoforum_drafts             an admin's draft in one such chat.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'broadcast_messages_allowed') = 0,
  'ALTER TABLE `channels` ADD COLUMN `broadcast_messages_allowed` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'monoforum') = 0,
  'ALTER TABLE `channels` ADD COLUMN `monoforum` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'linked_monoforum_id') = 0,
  'ALTER TABLE `channels` ADD COLUMN `linked_monoforum_id` bigint(20) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'saved_peer_id') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `saved_peer_id` bigint(20) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'sublist') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `sublist` (`channel_id`, `saved_peer_id`, `id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_monoforum_sublists` (
  `channel_id` bigint(20) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `top_message` int(11) NOT NULL DEFAULT '0',
  `read_inbox_max_id` int(11) NOT NULL DEFAULT '0',
  `read_outbox_max_id` int(11) NOT NULL DEFAULT '0',
  `unread_mark` tinyint(1) NOT NULL DEFAULT '0',
  `date2` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`channel_id`, `peer_id`),
  KEY `list` (`channel_id`, `top_message`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Chats of subscribers with a channel (monoforum sublists)';

CREATE TABLE IF NOT EXISTS `channel_monoforum_drafts` (
  `user_id` bigint(20) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `draft_message_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `channel_id`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Drafts of admins in monoforum sublists';
