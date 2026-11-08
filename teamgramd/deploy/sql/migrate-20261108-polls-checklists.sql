-- Polls, quizzes and checklists (task 062).
--
-- polls: one row per poll, keyed by the poll's id, which every copy of a
-- message with the poll and every forward of it shares (updateMessagePoll
-- names the poll by poll_id). poll_data is the poll as sent: flags, question,
-- answers with their options; correct_options and solution_data hold what a
-- voter learns only after voting, so the copies of the message in the boxes
-- never contain them. peer_type, peer_id, message_id and dialog_message_id
-- are where the poll was first sent, in its creator's dialog: they address
-- the creator's unread votes and the closing of the poll. close_date is the
-- moment the sweep closes the poll (0: never), closed holds it once done.
--
-- poll_votes: one row per chosen option of a voter. The counts and the
-- totals are always computed from these rows, so parallel votes cannot make
-- them drift.
--
-- todo_lists: the current state of a checklist of a private chat or a basic
-- group, keyed by the message's dialog_message_id, which all copies share.
-- A channel holds one copy of the message and needs no such row.
--
-- New tables only: safe on an empty and on a populated database, and safe to
-- run again.

CREATE TABLE IF NOT EXISTS `polls` (
  `id` bigint(20) NOT NULL,
  `creator_user_id` bigint(20) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `message_id` int(11) NOT NULL,
  `dialog_message_id` bigint(20) NOT NULL DEFAULT '0',
  `poll_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `correct_options` text COLLATE utf8mb4_bin NOT NULL,
  `solution_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `next_option` int(11) NOT NULL DEFAULT '0',
  `closed` tinyint(1) NOT NULL DEFAULT '0',
  `close_date` bigint(20) NOT NULL DEFAULT '0',
  `has_unread` tinyint(1) NOT NULL DEFAULT '0',
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `closing` (`closed`, `close_date`),
  KEY `creator_unread` (`creator_user_id`, `has_unread`, `peer_type`, `peer_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Polls and quizzes';

CREATE TABLE IF NOT EXISTS `poll_votes` (
  `poll_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `opt` varbinary(100) NOT NULL,
  `date` bigint(20) NOT NULL,
  PRIMARY KEY (`poll_id`, `user_id`, `opt`),
  KEY `by_option` (`poll_id`, `opt`, `date`, `user_id`),
  KEY `by_date` (`poll_id`, `date`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='The options each voter chose';

CREATE TABLE IF NOT EXISTS `todo_lists` (
  `dialog_message_id` bigint(20) NOT NULL,
  `creator_user_id` bigint(20) NOT NULL,
  `data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `completions` mediumtext COLLATE utf8mb4_bin NOT NULL,
  PRIMARY KEY (`dialog_message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Checklists of private chats and basic groups';
