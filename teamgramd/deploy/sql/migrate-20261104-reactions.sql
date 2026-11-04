-- Reactions to messages of private chats and basic groups and the tags of
-- Saved Messages (task 053).
--
-- message_reactions: one row per reaction of a user to a message, keyed by
-- the message's dialog_message_id, which every copy of the message in the
-- boxes of the chat's members shares, so a reaction is stored once.
-- reaction is the reaction's key: "e" and the emoticon (reactionEmoji), or
-- "c" and the custom emoji's document id (reactionCustomEmoji).
-- chosen_order orders the user's own reactions to the message (the client's
-- chosen_order); unread marks a reaction its message's author has not seen
-- yet (messagePeerReaction.unread, dialogs.unread_reactions_count).
-- The columns reaction, reaction_date, reaction_unread and has_reaction of
-- messages (one reaction per copy, upstream) are not used.
--
-- saved_reaction_tags: the titles a user gave to tags of Saved Messages
-- (messages.updateSavedReactionTag); the tags themselves are the reactions
-- to messages of the chat with oneself.
--
-- user_reaction_settings: the default reaction of a user
-- (messages.setDefaultReaction), the quick reaction of the clients, and when
-- the user cleared its recent reactions (messages.clearRecentReactions): the
-- recent ones are its own reactions outside Saved Messages since then.
--
-- New tables only: safe on an empty and on a populated database, and safe to
-- run again.

CREATE TABLE IF NOT EXISTS `message_reactions` (
  `dialog_message_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `reaction` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `chosen_order` int(11) NOT NULL,
  `big` tinyint(1) NOT NULL DEFAULT '0',
  `unread` tinyint(1) NOT NULL DEFAULT '0',
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`dialog_message_id`, `user_id`, `reaction`),
  KEY `user_date` (`user_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Reactions to messages of private chats and basic groups';

CREATE TABLE IF NOT EXISTS `saved_reaction_tags` (
  `user_id` bigint(20) NOT NULL,
  `reaction` varchar(64) COLLATE utf8mb4_bin NOT NULL,
  `title` varchar(64) COLLATE utf8mb4_unicode_ci NOT NULL,
  PRIMARY KEY (`user_id`, `reaction`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Titles of the tags of Saved Messages';

CREATE TABLE IF NOT EXISTS `user_reaction_settings` (
  `user_id` bigint(20) NOT NULL,
  `default_reaction` varchar(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `recent_cleared_at` bigint(20) NOT NULL DEFAULT '0',
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Reaction settings of a user';
