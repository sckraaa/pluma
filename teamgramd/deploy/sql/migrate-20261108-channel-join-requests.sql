-- Requests to join a channel or a supergroup (task 041).
--
-- channel_join_requests: one row per user asking to join a channel, keyed by
-- the channel and the user: a second request of the same user finds the row
-- (the request stands, INVITE_REQUEST_SENT again). link is the hash of the
-- invite link asked through (chat_invites.link), empty for a request through
-- the channel's public username or when the link has been deleted since.
-- date2 orders the list of requests, newest first.
--
-- A new table only: safe on an empty and on a populated database, and safe
-- to run again.

CREATE TABLE IF NOT EXISTS `channel_join_requests` (
  `channel_id` bigint(20) NOT NULL,
  `user_id` bigint(20) NOT NULL,
  `link` varchar(32) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `date2` bigint(20) NOT NULL,
  PRIMARY KEY (`channel_id`, `user_id`),
  KEY `channel_date` (`channel_id`, `date2`, `user_id`),
  KEY `channel_link` (`channel_id`, `link`, `date2`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='Requests to join channels and supergroups, not answered yet';
