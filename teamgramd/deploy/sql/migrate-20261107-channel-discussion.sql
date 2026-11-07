-- Comments of channels: the discussion group (task 048).
--
-- channel_messages.reply_top_id: the thread a message of a supergroup is in,
-- the id of the message the thread starts from (the copy of a channel's
-- post in its discussion group, or the message first replied to); 0 for a
-- message outside threads. The comments of a post and the count under it
-- are read by it.
--
-- channel_discussion_posts: the copy of each post of a broadcast channel in
-- its discussion group, written with the post in one transaction; a post
-- has one copy, and a copy one post.
--
-- channel_thread_reads: how far a user read a thread of a supergroup
-- (messages.readDiscussion).
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'reply_top_id') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `reply_top_id` int(11) NOT NULL DEFAULT ''0'' AFTER `pinned`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'thread') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `thread` (`channel_id`, `reply_top_id`, `id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_discussion_posts` (
  `channel_id` bigint(20) NOT NULL,
  `post_id` int(11) NOT NULL,
  `group_id` bigint(20) NOT NULL,
  `group_message_id` int(11) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`channel_id`, `post_id`),
  UNIQUE KEY `group_message` (`group_id`, `group_message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Copies of channel posts in their discussion groups';

CREATE TABLE IF NOT EXISTS `channel_thread_reads` (
  `user_id` bigint(20) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `top_id` int(11) NOT NULL,
  `read_max_id` int(11) NOT NULL,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `channel_id`, `top_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='How far users read threads of supergroups';
