-- Task 105, part 7: the queue of updates with qts other than the messages
-- of secret chats (ADR 0067, p. 6; pkg/qtsqueue): what a connected bot of
-- Telegram Business is told of the business chats it serves
-- (updateBotBusinessConnect, updateBotNewBusinessMessage,
-- updateBotEditBusinessMessage, updateBotDeleteBusinessMessage).
--
-- One row per update and authorization (a key and the account signed in on
-- it). qts is the authorization's own counter, encrypted_chat_devices.qts,
-- which the queue of secret chats (encrypted_chat_queue) takes its numbers
-- from as well: the two queues never give one number twice, and
-- updates.getDifference merges them by qts. A row lives until the
-- authorization confirms its qts (the qts of updates.getDifference) and 7
-- days at most.
--
-- update_data is the update with its qts, as JSON, without the users and
-- chats; user_ids are the users it names, which the difference carries.
--
-- The statement is CREATE TABLE IF NOT EXISTS: running the file again
-- changes nothing.
CREATE TABLE IF NOT EXISTS `qts_updates` (
  `auth_key_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `qts` int(11) NOT NULL,
  `update_data` mediumblob NOT NULL COMMENT 'the update with its qts, JSON, without users and chats',
  `user_ids` varchar(1024) NOT NULL DEFAULT '' COMMENT 'the users the update names, decimal ids joined by commas',
  `date` int(11) NOT NULL,
  PRIMARY KEY (`auth_key_id`, `user_id`, `qts`),
  KEY `date` (`date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
