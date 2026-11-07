-- Reactions to messages of channels and supergroups (task 044).
--
-- channel_message_reactions: one row per reaction of a user to a message of
-- a channel, keyed by the channel and the message's id, as the one copy of
-- the message is (channel_messages). reaction is the reaction's key: "e"
-- and the emoticon (reactionEmoji), or "c" and the custom emoji's document
-- id (reactionCustomEmoji), as in message_reactions. chosen_order orders
-- the user's own reactions to the message; unread marks a reaction the
-- message's author has not seen yet.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_message_reactions` (
  `channel_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `reaction` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `chosen_order` int(11) NOT NULL,
  `big` tinyint(1) NOT NULL DEFAULT '0',
  `unread` tinyint(1) NOT NULL DEFAULT '0',
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `message_id`, `user_id`, `reaction`),
  KEY `user_date` (`user_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Reactions to messages of channels and supergroups';
