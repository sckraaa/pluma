-- Views and forwards of the posts of broadcast channels (task 046).
--
-- channel_messages.views and forwards: the counters of a post, the one copy
-- of the message being in channel_messages; message_data does not hold them.
-- views starts at 1, the view of the post's author, as the clients show a
-- post they have just sent (Android SendMessagesHelper.java:2424, tdesktop
-- history_item.cpp:698, iOS EnqueueMessage.swift:1012, Web K
-- appMessagesManager.ts:4093). The columns are also there on the messages of
-- supergroups, which have no counters and do not read them.
--
-- channel_message_views: who has viewed which post, so that a user counts
-- once however often the clients ask (messages.getMessagesViews with
-- increment). A post's author has no row: it is in the initial 1.
--
-- channel_message_forwards: one row per forward of a post, keyed by the
-- forward's random_id, so that a repeated request does not count twice.
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'views') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `views` int(11) NOT NULL DEFAULT ''1'' AFTER `pinned`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'forwards') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `forwards` int(11) NOT NULL DEFAULT ''0'' AFTER `views`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_message_views` (
  `channel_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `message_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Who viewed the posts of broadcast channels';

CREATE TABLE IF NOT EXISTS `channel_message_forwards` (
  `channel_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `random_id` bigint(20) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `message_id`, `user_id`, `random_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='The forwards of the posts of broadcast channels';
