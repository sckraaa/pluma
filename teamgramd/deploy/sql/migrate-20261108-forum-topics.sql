-- Forums and topics of supergroups (task 060).
--
-- channel_messages.topic_id: the topic a message of a forum is in, the id of
-- the topic's messageActionTopicCreate; 0 for the General topic (id 1) and
-- for every message outside a forum. A supergroup that becomes a forum has
-- all its messages in General, with no row rewritten.
--
-- channels.forum_tabs: the forum shows its topics as tabs
-- (core.telegram.org/method/channels.toggleForum).
--
-- dialogs.view_forum_as_messages: a user reads the forum as one feed of
-- messages (channels.toggleViewForumAsMessages), per account.
--
-- channel_forum_topics: the topics of a forum; pinned_order above 0 is a
-- pinned topic, the highest first. top_message orders the list. A row stays
-- when its forum is switched off and when the topic is deleted.
--
-- How far a user read a topic is its row of channel_thread_reads, with
-- top_id the topic's id (1 for General).
--
-- Safe on an empty and on a populated database, and safe to run again.

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND COLUMN_NAME = 'topic_id') = 0,
  'ALTER TABLE `channel_messages` ADD COLUMN `topic_id` int(11) NOT NULL DEFAULT ''0'' AFTER `reply_top_id`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'topic') = 0,
  'ALTER TABLE `channel_messages` ADD KEY `topic` (`channel_id`, `topic_id`, `id`), ALGORITHM = INPLACE, LOCK = NONE',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channels' AND COLUMN_NAME = 'forum_tabs') = 0,
  'ALTER TABLE `channels` ADD COLUMN `forum_tabs` tinyint(1) NOT NULL DEFAULT ''0'' AFTER `forum`, ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

SET @ddl = IF((SELECT COUNT(*) FROM information_schema.COLUMNS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'dialogs' AND COLUMN_NAME = 'view_forum_as_messages') = 0,
  'ALTER TABLE `dialogs` ADD COLUMN `view_forum_as_messages` tinyint(1) NOT NULL DEFAULT ''0'', ALGORITHM = INSTANT',
  'DO 0');
PREPARE ddl FROM @ddl;
EXECUTE ddl;
DEALLOCATE PREPARE ddl;

CREATE TABLE IF NOT EXISTS `channel_forum_topics` (
  `channel_id` bigint(20) NOT NULL,
  `topic_id` int(11) NOT NULL,
  `creator_user_id` bigint(20) NOT NULL,
  `title` varchar(512) COLLATE utf8mb4_unicode_ci NOT NULL,
  `title_missing` tinyint(1) NOT NULL DEFAULT '0',
  `icon_color` int(11) NOT NULL DEFAULT '0',
  `icon_emoji_id` bigint(20) NOT NULL DEFAULT '0',
  `date` bigint(20) NOT NULL,
  `closed` tinyint(1) NOT NULL DEFAULT '0',
  `hidden` tinyint(1) NOT NULL DEFAULT '0',
  `pinned_order` int(11) NOT NULL DEFAULT '0',
  `top_message` int(11) NOT NULL DEFAULT '0',
  `deleted` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`channel_id`, `topic_id`),
  KEY `list` (`channel_id`, `deleted`, `top_message`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Topics of forum supergroups';
