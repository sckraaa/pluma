-- The voice and round video messages of channels and supergroups each member
-- has played (task 080): media_unread is per member, as the clients expect
-- it (channels.readMessageContents). A message without a row of its reader
-- is unread for it, so a member who joins later finds the older ones
-- unread too.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_contents_read` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  PRIMARY KEY (`channel_id`, `user_id`, `message_id`),
  KEY `channel_message` (`channel_id`, `message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
