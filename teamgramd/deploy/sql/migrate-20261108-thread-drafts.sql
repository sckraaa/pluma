-- Drafts of the topics of a forum (task 063): updateDraftMessage names the
-- topic with top_msg_id. The draft of the chat itself stays in dialogs.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `thread_drafts` (
  `user_id` bigint(20) NOT NULL,
  `peer_type` int(11) NOT NULL,
  `peer_id` bigint(20) NOT NULL,
  `top_msg_id` int(11) NOT NULL,
  `draft_message_data` mediumtext COLLATE utf8mb4_bin NOT NULL,
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`user_id`, `peer_type`, `peer_id`, `top_msg_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Drafts of forum topics';
