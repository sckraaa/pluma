-- When each member of a supergroup read it up to which message (task 078):
-- messages.getMessageReadParticipants of a supergroup. Rows older than the
-- clients ask for are deleted as reads come in.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_read_marks` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `max_id` int(11) NOT NULL,
  `read_at` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `user_id`, `max_id`),
  KEY `channel_max_id` (`channel_id`, `max_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
