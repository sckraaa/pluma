-- Pluma task 067: notify settings of one topic of a forum.
--
-- user_topic_notify_settings holds a user's settings for one topic
-- (account.updateNotifySettings with inputNotifyForumTopic): -1 and '-1' are
-- fields not set, as in user_notify_settings. No row: the topic follows its
-- forum.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `user_topic_notify_settings` (
  `user_id` bigint(20) NOT NULL,
  `channel_id` bigint(20) NOT NULL,
  `top_msg_id` int(11) NOT NULL,
  `show_previews` int(11) NOT NULL DEFAULT '-1',
  `silent` int(11) NOT NULL DEFAULT '-1',
  `mute_until` int(11) NOT NULL DEFAULT '-1',
  `sound` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '-1',
  PRIMARY KEY (`user_id`, `channel_id`, `top_msg_id`),
  KEY `topic` (`channel_id`, `top_msg_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Notify settings of one topic of a forum';
